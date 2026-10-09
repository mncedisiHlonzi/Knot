package identity

import (
	"context"
	"errors"
	"fmt"
	"net/mail"
	"strings"
	"unicode/utf8"
)

// Validation limits. They are intentionally modest: this is an MVP for real
// people, not a place to accept unbounded input.
const (
	// MinPasswordLength is the shortest accepted password.
	MinPasswordLength = 8
	// maxPasswordBytes bounds hashing work so a huge body cannot be turned into
	// a CPU denial of service.
	maxPasswordBytes  = 1024
	minDisplayNameLen = 1
	maxDisplayNameLen = 80
	maxEmailLength    = 254
	maxPhoneLength    = 32
	maxLocationLength = 120
	maxLanguages      = 20
	maxLanguageLength = 35
)

// RegisterInput is the input to Register. It mirrors the registration request
// body but is a domain type, not an HTTP type.
type RegisterInput struct {
	// Email is required and is normalised to lower case.
	Email string
	// Password is required and must be at least MinPasswordLength characters.
	Password string
	// DisplayName is required and must be 1-80 characters.
	DisplayName string
	// PreferredLanguages is optional; it may be empty but never nil once stored.
	PreferredLanguages []string
	// ApproximateLocation is optional.
	ApproximateLocation string
	// Phone is optional.
	Phone string
}

// LoginInput is the input to Login.
type LoginInput struct {
	// Email is required.
	Email string
	// Password is required.
	Password string
}

// Service holds the identity business rules.
//
// It depends on the UserStore abstraction and the TokenIssuer, and it knows
// nothing about HTTP, JSON, or SQL.
type Service struct {
	store  UserStore
	tokens *TokenIssuer
	// dummyHash is verified when an email is unknown, so that a failed login
	// costs the same as a real one and cannot be used to enumerate accounts.
	dummyHash string
}

// NewService wires a store and a token issuer together.
func NewService(store UserStore, tokens *TokenIssuer) (*Service, error) {
	if store == nil {
		return nil, fmt.Errorf("identity: service requires a user store")
	}
	if tokens == nil {
		return nil, fmt.Errorf("identity: service requires a token issuer")
	}

	dummy, err := HashPassword("knot-timing-equalisation-placeholder")
	if err != nil {
		return nil, fmt.Errorf("identity: build timing-equalisation hash: %w", err)
	}

	return &Service{store: store, tokens: tokens, dummyHash: dummy}, nil
}

// Register validates the input, stores a new user, and issues a token pair.
//
// It returns a *ValidationError for bad input and ErrEmailTaken when the email is
// already registered.
func (s *Service) Register(ctx context.Context, in RegisterInput) (*AuthResult, error) {
	email, err := validateEmail(in.Email)
	if err != nil {
		return nil, err
	}
	if err := validatePassword(in.Password); err != nil {
		return nil, err
	}
	displayName, err := validateDisplayName(in.DisplayName)
	if err != nil {
		return nil, err
	}
	languages, err := validateLanguages(in.PreferredLanguages)
	if err != nil {
		return nil, err
	}
	phone, err := validateOptional(in.Phone, "phone", maxPhoneLength)
	if err != nil {
		return nil, err
	}
	location, err := validateOptional(in.ApproximateLocation, "approximate_location", maxLocationLength)
	if err != nil {
		return nil, err
	}

	hash, err := HashPassword(in.Password)
	if err != nil {
		return nil, fmt.Errorf("identity: hash password: %w", err)
	}

	user, err := s.store.CreateUser(ctx, &User{
		Email:               email,
		Phone:               phone,
		PasswordHash:        hash,
		DisplayName:         displayName,
		PreferredLanguages:  languages,
		ApproximateLocation: location,
	})
	if err != nil {
		if errors.Is(err, ErrEmailTaken) {
			return nil, ErrEmailTaken
		}
		return nil, fmt.Errorf("identity: register: %w", err)
	}

	tokens, err := s.issueTokens(user.ID)
	if err != nil {
		return nil, err
	}

	return &AuthResult{User: user, Tokens: tokens}, nil
}

// Login verifies credentials and issues a token pair.
//
// An unknown email and a wrong password both return ErrInvalidCredentials, with
// the same work performed, so the endpoint does not leak which accounts exist.
func (s *Service) Login(ctx context.Context, in LoginInput) (*AuthResult, error) {
	email := normalizeEmail(in.Email)
	if email == "" || in.Password == "" {
		return nil, ErrInvalidCredentials
	}

	user, err := s.store.FindUserByEmail(ctx, email)
	if err != nil {
		if errors.Is(err, ErrUserNotFound) {
			// Burn equivalent work so timing does not distinguish the cases.
			_, _ = VerifyPassword(s.dummyHash, in.Password)
			return nil, ErrInvalidCredentials
		}
		return nil, fmt.Errorf("identity: login: %w", err)
	}

	ok, err := VerifyPassword(user.PasswordHash, in.Password)
	if err != nil {
		// A stored hash we cannot parse is a server-side fault, not a bad
		// password, so it must not be reported as invalid credentials.
		return nil, fmt.Errorf("identity: login: %w", err)
	}
	if !ok {
		return nil, ErrInvalidCredentials
	}

	tokens, err := s.issueTokens(user.ID)
	if err != nil {
		return nil, err
	}

	return &AuthResult{User: user, Tokens: tokens}, nil
}

// UserByID resolves a user id to an account.
//
// It exists so handlers can turn the authenticated subject into a profile
// without reaching for the store themselves. It returns ErrUserNotFound when no
// such user exists.
func (s *Service) UserByID(ctx context.Context, id string) (*User, error) {
	user, err := s.store.FindUserByID(ctx, id)
	if err != nil {
		if errors.Is(err, ErrUserNotFound) {
			return nil, ErrUserNotFound
		}
		return nil, fmt.Errorf("identity: find user by id: %w", err)
	}
	return user, nil
}

// SetAvatarURL records the object key of the user's current avatar and returns
// the updated account.
//
// The key must sit directly under the user's own prefix in the media bucket, so
// a mistake in the HTTP layer cannot make one account's avatar point at another
// account's object. It returns ErrUserNotFound when no such user exists.
func (s *Service) SetAvatarURL(ctx context.Context, userID, key string) (*User, error) {
	if err := validateAvatarKey(userID, key); err != nil {
		return nil, err
	}

	updated, err := s.store.UpdateAvatarURL(ctx, userID, key)
	if err != nil {
		if errors.Is(err, ErrUserNotFound) {
			return nil, ErrUserNotFound
		}
		return nil, fmt.Errorf("identity: set avatar url: %w", err)
	}

	return updated, nil
}

// avatarKeyNamespace is the bucket prefix reserved for avatars. Every object in
// it is namespaced by the owning user id, so a key can always be traced back to
// exactly one account.
const avatarKeyNamespace = "avatars/"

// AvatarKeyPrefix returns the bucket prefix that belongs to userID. Callers that
// mint avatar object keys use it so the namespace is spelled in exactly one place.
func AvatarKeyPrefix(userID string) string {
	return avatarKeyNamespace + userID + "/"
}

// validateAvatarKey rejects a key that is not a single object directly inside the
// given user's own avatar prefix.
func validateAvatarKey(userID, key string) error {
	prefix := AvatarKeyPrefix(userID)
	if !strings.HasPrefix(key, prefix) {
		return &ValidationError{Field: "avatar", Message: "must be an object under the user's own avatar prefix"}
	}

	// The remainder must name one object and must not be able to climb out of the
	// prefix with a slash.
	object := strings.TrimPrefix(key, prefix)
	if object == "" || strings.Contains(object, "/") {
		return &ValidationError{Field: "avatar", Message: "must name a single object directly under the user's avatar prefix"}
	}

	return nil
}

// issueTokens mints both tokens for a user id.
func (s *Service) issueTokens(userID string) (TokenPair, error) {
	access, err := s.tokens.IssueAccessToken(userID)
	if err != nil {
		return TokenPair{}, err
	}
	refresh, err := s.tokens.IssueRefreshToken(userID)
	if err != nil {
		return TokenPair{}, err
	}

	return TokenPair{
		AccessToken:  access,
		RefreshToken: refresh,
		ExpiresIn:    int(AccessTokenTTL.Seconds()),
	}, nil
}

// normalizeEmail trims and lower-cases an address so that lookups and the unique
// constraint agree on a single canonical form.
func normalizeEmail(raw string) string {
	return strings.ToLower(strings.TrimSpace(raw))
}

// validateEmail normalises and validates an address, returning the canonical form.
func validateEmail(raw string) (string, error) {
	email := normalizeEmail(raw)
	if email == "" {
		return "", &ValidationError{Field: "email", Message: "is required"}
	}
	if len(email) > maxEmailLength {
		return "", &ValidationError{Field: "email", Message: fmt.Sprintf("must be at most %d characters", maxEmailLength)}
	}

	addr, err := mail.ParseAddress(email)
	// addr.Address == email rejects display-name forms such as `Knot <a@b>`.
	if err != nil || addr.Address != email {
		return "", &ValidationError{Field: "email", Message: "must be a valid email address"}
	}

	return email, nil
}

// validatePassword enforces the minimum length and the upper work bound.
func validatePassword(password string) error {
	if len(password) > maxPasswordBytes {
		return &ValidationError{Field: "password", Message: fmt.Sprintf("must be at most %d bytes", maxPasswordBytes)}
	}
	if utf8.RuneCountInString(password) < MinPasswordLength {
		return &ValidationError{Field: "password", Message: fmt.Sprintf("must be at least %d characters", MinPasswordLength)}
	}
	return nil
}

// validateDisplayName enforces 1-80 characters after trimming.
func validateDisplayName(raw string) (string, error) {
	name := strings.TrimSpace(raw)
	length := utf8.RuneCountInString(name)
	if length < minDisplayNameLen {
		return "", &ValidationError{Field: "display_name", Message: "is required"}
	}
	if length > maxDisplayNameLen {
		return "", &ValidationError{Field: "display_name", Message: fmt.Sprintf("must be at most %d characters", maxDisplayNameLen)}
	}
	return name, nil
}

// validateLanguages trims the entries, drops blanks, and bounds the result.
func validateLanguages(raw []string) ([]string, error) {
	if len(raw) > maxLanguages {
		return nil, &ValidationError{Field: "preferred_languages", Message: fmt.Sprintf("must contain at most %d entries", maxLanguages)}
	}

	languages := make([]string, 0, len(raw))
	for _, entry := range raw {
		trimmed := strings.TrimSpace(entry)
		if trimmed == "" {
			continue
		}
		if utf8.RuneCountInString(trimmed) > maxLanguageLength {
			return nil, &ValidationError{Field: "preferred_languages", Message: fmt.Sprintf("entries must be at most %d characters", maxLanguageLength)}
		}
		languages = append(languages, trimmed)
	}

	return languages, nil
}

// validateOptional trims an optional field and bounds its length.
func validateOptional(raw, field string, maxLength int) (string, error) {
	value := strings.TrimSpace(raw)
	if value == "" {
		return "", nil
	}
	if len(value) > maxLength {
		return "", &ValidationError{Field: field, Message: fmt.Sprintf("must be at most %d characters", maxLength)}
	}
	return value, nil
}
