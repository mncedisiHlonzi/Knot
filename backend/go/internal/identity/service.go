package identity

import (
	"context"
	"errors"
	"fmt"
	"net/mail"
	"strings"
	"time"
	"unicode"
	"unicode/utf8"

	"github.com/knot/backend/internal/language"
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
	// sessions ends every session a user has. It is optional because the identity
	// domain does not own sessions — the auth domain does — but deletion is not
	// deletion if a token minted a minute earlier still opens the account
	// (KNOT-ADR-081). The composition root wires it with the session store.
	sessions SessionRevoker
	// dummyHash is verified when an email is unknown, so that a failed login
	// costs the same as a real one and cannot be used to enumerate accounts.
	dummyHash string
}

// SessionRevoker ends every session belonging to a user.
//
// It is a one-method interface so this package states what it needs from the session
// domain without importing it: `auth` imports `identity`, never the other way round.
type SessionRevoker interface {
	RevokeAllForUser(ctx context.Context, userID string) error
}

// Option configures a Service at construction time.
type Option func(*Service)

// WithSessionRevoker gives the service a way to end a user's sessions, which it uses
// when an account is deleted.
func WithSessionRevoker(revoker SessionRevoker) Option {
	return func(s *Service) { s.sessions = revoker }
}

// NewService wires a store and a token issuer together.
func NewService(store UserStore, tokens *TokenIssuer, opts ...Option) (*Service, error) {
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

	service := &Service{store: store, tokens: tokens, dummyHash: dummy}
	for _, opt := range opts {
		opt(service)
	}

	return service, nil
}

// Register validates the input, stores a new user, and issues a token pair.
//
// It returns a *ValidationError for bad input and ErrEmailTaken when the email is
// already registered.
func (s *Service) Register(ctx context.Context, in RegisterInput) (*AuthResult, error) {
	email, err := ValidateEmail(in.Email)
	if err != nil {
		return nil, err
	}
	if err := ValidatePassword(in.Password); err != nil {
		return nil, err
	}
	displayName, err := ValidateDisplayName(in.DisplayName)
	if err != nil {
		return nil, err
	}
	languages, err := ValidateLanguages(in.PreferredLanguages)
	if err != nil {
		return nil, err
	}
	phone, err := ValidateOptional(in.Phone, "phone", maxPhoneLength)
	if err != nil {
		return nil, err
	}
	location, err := ValidateOptional(in.ApproximateLocation, "approximate_location", maxLocationLength)
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
	email := NormalizeEmail(in.Email)
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

// UsersByIDs resolves a batch of user ids to accounts keyed by id.
//
// Ids that are malformed or that match no row are simply absent from the map
// rather than being an error: an inbox must still render when one of its actors
// has been deleted. An empty input yields an empty map. Duplicate ids are looked
// up once.
//
// It exists so a page of notifications can be enriched with one query instead of
// one query per notification.
func (s *Service) UsersByIDs(ctx context.Context, ids []string) (map[string]*User, error) {
	unique := make([]string, 0, len(ids))
	seen := make(map[string]struct{}, len(ids))
	for _, id := range ids {
		if !isUUID(id) {
			continue
		}
		if _, ok := seen[id]; ok {
			continue
		}
		seen[id] = struct{}{}
		unique = append(unique, id)
	}

	users := make(map[string]*User, len(unique))
	if len(unique) == 0 {
		return users, nil
	}

	found, err := s.store.FindUsersByID(ctx, unique)
	if err != nil {
		return nil, fmt.Errorf("identity: find users by id: %w", err)
	}
	for _, user := range found {
		if user == nil {
			continue
		}
		users[user.ID] = user
	}

	return users, nil
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

// RequestDeletion soft-deletes an account after verifying its owner's password.
//
// The password is required because deletion is destructive and is one tap away in a
// settings list: a phone left unlocked should not be enough to end an account. The
// account is not erased — its content stays readable and attributed — and the person
// can come back for DeletionRecoveryWindow by signing in (KNOT-ADR-081).
//
// It is idempotent: deleting an account that is already deleted reports success and
// leaves the original timestamps alone, so a retried request cannot extend somebody's
// recovery window.
func (s *Service) RequestDeletion(ctx context.Context, userID, password string) error {
	user, err := s.store.FindUserByID(ctx, userID)
	if err != nil {
		if errors.Is(err, ErrUserNotFound) {
			return ErrUserNotFound
		}
		return fmt.Errorf("identity: request deletion: %w", err)
	}

	// A purged account has no password left to check and nothing to delete.
	if user.PurgedAt != nil {
		return ErrAccountPendingPurge
	}

	ok, err := VerifyPassword(user.PasswordHash, password)
	if err != nil {
		return fmt.Errorf("identity: request deletion: %w", err)
	}
	if !ok {
		return ErrInvalidCredentials
	}

	if user.DeletedAt != nil {
		return nil
	}

	// Sessions go first. If this fails nothing has been marked, so the request can be
	// retried and the account is never left deleted with a live token still able to
	// reach it.
	if s.sessions != nil {
		if err := s.sessions.RevokeAllForUser(ctx, user.ID); err != nil {
			return fmt.Errorf("identity: request deletion: revoke sessions: %w", err)
		}
	}

	if _, err := s.store.MarkDeleted(ctx, user.ID); err != nil {
		if errors.Is(err, ErrUserNotFound) {
			return ErrUserNotFound
		}
		return fmt.Errorf("identity: request deletion: %w", err)
	}

	return nil
}

// CancelDeletion cancels a pending deletion and reports whether the account is live
// again.
//
// It is safe to call for an account that was never deleted: the store treats it as a
// successful no-op. That is what lets sign-in cancel unconditionally once it has seen
// a deletion timestamp, so two devices signing in at once cannot produce a spurious
// failure for one of them.
func (s *Service) CancelDeletion(ctx context.Context, userID string) error {
	if _, err := s.store.ClearDeletion(ctx, userID); err != nil {
		if errors.Is(err, ErrUserNotFound) {
			return ErrUserNotFound
		}
		return fmt.Errorf("identity: cancel deletion: %w", err)
	}

	return nil
}

// RecoverAccount cancels a pending deletion for the account owning email, once the
// caller has proved it with the password.
//
// It is the explicit counterpart to sign-in's automatic recovery, for a person who
// would rather not sign in on the device that made the mistake. An unknown address
// and a wrong password are indistinguishable, and the unknown-address path burns the
// same hashing work so timing cannot tell them apart either.
//
// An account whose window has already closed reports ErrAccountPendingPurge: there is
// nothing left to recover, and only the purge job can finish the job.
func (s *Service) RecoverAccount(ctx context.Context, email, password string) error {
	user, err := s.store.FindUserByEmail(ctx, NormalizeEmail(email))
	if err != nil {
		if errors.Is(err, ErrUserNotFound) {
			_, _ = VerifyPassword(s.dummyHash, password)
			return ErrInvalidCredentials
		}
		return fmt.Errorf("identity: recover account: %w", err)
	}

	if user.PurgedAt != nil {
		return ErrAccountPendingPurge
	}

	ok, err := VerifyPassword(user.PasswordHash, password)
	if err != nil {
		return fmt.Errorf("identity: recover account: %w", err)
	}
	if !ok {
		return ErrInvalidCredentials
	}

	if user.DeletedAt == nil {
		// Nothing was pending: the account is already live, so the person's intent is
		// satisfied and the endpoint has nothing to undo.
		return nil
	}

	if user.DeletionScheduledAt != nil && !time.Now().Before(*user.DeletionScheduledAt) {
		return ErrAccountPendingPurge
	}

	if err := s.CancelDeletion(ctx, user.ID); err != nil {
		return fmt.Errorf("identity: recover account: %w", err)
	}

	return nil
}

// PurgeDeletedUsers anonymises every account whose recovery window has closed, and
// returns how many it purged.
//
// It is the manual trigger for MVP (KNOT-034): the job is deliberately not started on
// a timer, so nothing anonymises an account by surprise until a scheduled runner
// exists (KNOT-034a). Running it twice is safe — an already purged row is never due
// again (KNOT-ADR-089).
func (s *Service) PurgeDeletedUsers(ctx context.Context) (int, error) {
	purged, err := s.store.PurgeDueUsers(ctx, time.Now())
	if err != nil {
		return 0, fmt.Errorf("identity: purge deleted users: %w", err)
	}

	return purged, nil
}

// NormalizeEmail trims and lower-cases an address so that lookups and the unique
// constraint agree on a single canonical form.
func NormalizeEmail(raw string) string {
	return strings.ToLower(strings.TrimSpace(raw))
}

// ValidateEmail normalises and validates an address, returning the canonical form.
func ValidateEmail(raw string) (string, error) {
	email := NormalizeEmail(raw)
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

// ValidatePassword enforces the account password policy (KNOT-ADR-086): at least
// MinPasswordLength characters, at most maxPasswordBytes bytes, and at least one
// letter and one digit.
//
// It is exported because two callers must apply exactly the same rule: this
// package's registration and the authentication service's registration and
// password reset. A policy that exists twice drifts, and the drift is always
// discovered by a user who cannot reset a password the app told them was fine.
func ValidatePassword(password string) error {
	if len(password) > maxPasswordBytes {
		return &ValidationError{Field: "password", Message: fmt.Sprintf("must be at most %d bytes", maxPasswordBytes)}
	}
	if utf8.RuneCountInString(password) < MinPasswordLength {
		return &ValidationError{Field: "password", Message: fmt.Sprintf("must be at least %d characters", MinPasswordLength)}
	}

	// The composition rule is deliberately weak: length carries most of the
	// strength, and a letter plus a digit is enough to reject the passwords that
	// are actually typed in a hurry without turning the field into a puzzle.
	var hasLetter, hasDigit bool
	for _, r := range password {
		switch {
		case unicode.IsLetter(r):
			hasLetter = true
		case unicode.IsDigit(r):
			hasDigit = true
		}
		if hasLetter && hasDigit {
			break
		}
	}

	if !hasLetter || !hasDigit {
		return &ValidationError{Field: "password", Message: "must contain at least one letter and one number"}
	}

	return nil
}

// ValidateDisplayName enforces 1-80 characters after trimming.
func ValidateDisplayName(raw string) (string, error) {
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

// ValidateLanguages trims the entries, drops blanks, and requires every remaining
// entry to be a canonical ISO 639-3 code.
func ValidateLanguages(raw []string) ([]string, error) {
	if len(raw) > maxLanguages {
		return nil, &ValidationError{Field: "preferred_languages", Message: fmt.Sprintf("must contain at most %d entries", maxLanguages)}
	}

	valid := make([]string, 0, len(raw))
	for _, entry := range raw {
		trimmed := strings.TrimSpace(entry)
		if trimmed == "" {
			continue
		}
		if !language.IsValid(trimmed) {
			return nil, &ValidationError{Field: "preferred_languages", Message: "entries must be valid ISO 639-3 language codes, such as eng or zul"}
		}
		valid = append(valid, trimmed)
	}

	return valid, nil
}

// ValidateOptional trims an optional field and bounds its length.
func ValidateOptional(raw, field string, maxLength int) (string, error) {
	value := strings.TrimSpace(raw)
	if value == "" {
		return "", nil
	}
	if len(value) > maxLength {
		return "", &ValidationError{Field: field, Message: fmt.Sprintf("must be at most %d characters", maxLength)}
	}
	return value, nil
}
