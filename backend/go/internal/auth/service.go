package auth

import (
	"context"
	"errors"
	"fmt"
	"log/slog"
	"strings"
	"time"

	"github.com/knot/backend/internal/cache"
	"github.com/knot/backend/internal/identity"
)

// Policy constants. They are exported because a client, a test, and the security
// document all need to state the same numbers, and SECURITY.md is only honest if
// it can point at the constant rather than at a paragraph.
const (
	// MaxActiveSessions is how many live sessions one account may have. The
	// oldest is revoked when a login would exceed it (KNOT-ADR-084).
	MaxActiveSessions = 5
	// EmailVerificationTTL is how long a verification link is redeemable. It is
	// long because a verification email is often opened hours later, and short
	// enough that a leaked mail archive is not a permanent key.
	EmailVerificationTTL = 24 * time.Hour
	// PasswordResetTTL is how long a reset link is redeemable. It is far shorter
	// than verification because a reset link can take over an account
	// (KNOT-ADR-087).
	PasswordResetTTL = 30 * time.Minute
	// passwordResetKeyPrefix namespaces reset tokens in Redis. The key is the
	// token's hash, never the token.
	passwordResetKeyPrefix = "knot:auth:reset:"
)

// Defaults for the links logged to the server console until real email exists
// (KNOT-ADR-088). They use the app's own scheme so a developer can paste one into
// a device that has the scheme registered, and they are overridable with
// WithVerificationLink / WithPasswordResetLink.
const (
	defaultVerificationLinkBase  = "knot://auth/verify-email?token="
	defaultPasswordResetLinkBase = "knot://auth/reset-password?token="
)

// Service errors. Each maps to exactly one HTTP status in the handler, which is
// the only place they are turned into words a client sees.
var (
	// ErrInvalidRefreshToken is returned for a refresh token that is malformed,
	// unknown, or belongs to a session that is already revoked.
	ErrInvalidRefreshToken = errors.New("auth: invalid refresh token")
	// ErrRefreshTokenReused is returned when a refresh token that was already
	// rotated is presented again. Every session the user has is revoked before it
	// is returned: a token that is used twice means two parties hold it, and there
	// is no way to tell which one is the account owner (KNOT-ADR-085).
	ErrRefreshTokenReused = errors.New("auth: refresh token reuse detected")
	// ErrInvalidVerificationToken is returned for a verification token that is
	// unknown, already used, expired, or of the wrong type.
	ErrInvalidVerificationToken = errors.New("auth: invalid verification token")
	// ErrInvalidPasswordResetToken is returned for a reset token that is unknown
	// or has expired.
	ErrInvalidPasswordResetToken = errors.New("auth: invalid password reset token")
)

// Deps are the collaborators the authentication service needs.
//
// They are passed as a struct rather than as six positional arguments because
// every field is required and several are interfaces of similar shape; a
// positional call site of six identifiers is where an argument gets swapped and
// nobody notices until the session store is writing credentials.
type Deps struct {
	// Users is the account store: creating users, resolving them by email or id,
	// and writing the two fields authentication owns (verification state and the
	// password hash).
	Users identity.UserStore
	// Sessions is the device store.
	Sessions SessionStore
	// Credentials is the per-method secret store that replaces the single
	// password hash column (KNOT-ADR-079).
	Credentials CredentialStore
	// Verifications is the single-use token store for address verification.
	Verifications VerificationTokenStore
	// Deletions cancels a pending account deletion when somebody signs in to a
	// deleted account inside its recovery window (KNOT-ADR-081).
	Deletions DeletionCanceller
	// Tokens signs and parses access tokens.
	Tokens TokenIssuer
	// Cache is Redis: rate-limit counters and the short-lived password reset
	// token (KNOT-ADR-082, KNOT-ADR-087).
	Cache cache.Cache
	// Logger records the links that stand in for email (KNOT-ADR-088) and any
	// background failure the caller must not be told about.
	Logger *slog.Logger
}

// TokenIssuer is the slice of identity.TokenIssuer the service uses.
//
// Depending on the interface rather than the concrete issuer is what lets the
// service's tests run without real signing material, and it documents the exact
// pair of operations authentication needs: mint an access token for a session,
// and read one back.
type TokenIssuer interface {
	IssueAccessTokenForSession(userID, sessionID string) (string, error)
	ParseAccessToken(raw string) (*identity.Claims, error)
}

// DeletionCanceller cancels a pending account deletion.
//
// It is a one-method interface rather than the whole identity service so the auth
// service states exactly what it needs from the account lifecycle: sign-in can undo a
// deletion, and nothing else (KNOT-ADR-081).
//
// It is safe to call for an account that was never deleted — the identity service
// treats that as a successful no-op — which is what keeps a race between two devices
// signing in at once harmless.
type DeletionCanceller interface {
	CancelDeletion(ctx context.Context, userID string) error
}

// Option configures a Service at construction time.
type Option func(*Service)

// WithClock replaces the service's clock. It exists for tests that need a
// verification token to be expired without waiting a day.
func WithClock(now func() time.Time) Option {
	return func(s *Service) { s.now = now }
}

// WithVerificationLink sets the prefix of the verification link logged to the
// console.
func WithVerificationLink(base string) Option {
	return func(s *Service) { s.verificationLinkBase = base }
}

// WithPasswordResetLink sets the prefix of the password reset link logged to the
// console.
func WithPasswordResetLink(base string) Option {
	return func(s *Service) { s.passwordResetLinkBase = base }
}

// Service owns every authentication policy Knot has: registration, email
// verification, sign-in, token refresh, session listing and revocation, and
// password reset.
//
// It holds no state of its own beyond its collaborators and a clock, so it is
// safe for concurrent use. It knows nothing about HTTP: it takes domain inputs,
// returns domain outputs, and reports failures as sentinel errors the HTTP layer
// maps to statuses.
type Service struct {
	users         identity.UserStore
	sessions      SessionStore
	credentials   CredentialStore
	verifications VerificationTokenStore
	deletions     DeletionCanceller
	tokens        TokenIssuer
	cache         cache.Cache
	logger        *slog.Logger

	now func() time.Time
	// dummyHash is verified when an email is unknown, so that a failed sign-in
	// costs the same as a real one and cannot be used to enumerate accounts.
	dummyHash             string
	verificationLinkBase  string
	passwordResetLinkBase string
}

// NewService builds the authentication service.
//
// It fails when a collaborator is missing rather than returning a service that
// panics on first use, and it builds the timing-equalisation hash once here
// rather than per sign-in.
func NewService(deps Deps, opts ...Option) (*Service, error) {
	if deps.Users == nil {
		return nil, fmt.Errorf("auth: service requires a user store")
	}
	if deps.Sessions == nil {
		return nil, fmt.Errorf("auth: service requires a session store")
	}
	if deps.Credentials == nil {
		return nil, fmt.Errorf("auth: service requires a credential store")
	}
	if deps.Verifications == nil {
		return nil, fmt.Errorf("auth: service requires a verification token store")
	}
	if deps.Deletions == nil {
		return nil, fmt.Errorf("auth: service requires an account deletion canceller")
	}
	if deps.Tokens == nil {
		return nil, fmt.Errorf("auth: service requires a token issuer")
	}
	if deps.Cache == nil {
		return nil, fmt.Errorf("auth: service requires a cache")
	}
	if deps.Logger == nil {
		return nil, fmt.Errorf("auth: service requires a logger")
	}

	dummy, err := identity.HashPassword("knot-timing-equalisation-placeholder")
	if err != nil {
		return nil, fmt.Errorf("auth: build timing-equalisation hash: %w", err)
	}

	service := &Service{
		users:                 deps.Users,
		sessions:              deps.Sessions,
		credentials:           deps.Credentials,
		verifications:         deps.Verifications,
		deletions:             deps.Deletions,
		tokens:                deps.Tokens,
		cache:                 deps.Cache,
		logger:                deps.Logger,
		now:                   time.Now,
		dummyHash:             dummy,
		verificationLinkBase:  defaultVerificationLinkBase,
		passwordResetLinkBase: defaultPasswordResetLinkBase,
	}

	for _, opt := range opts {
		opt(service)
	}

	return service, nil
}

// RegisterInput is the input to Register. It mirrors the registration request
// body but is a domain type, not an HTTP type.
type RegisterInput struct {
	Email               string
	Password            string
	DisplayName         string
	PreferredLanguages  []string
	ApproximateLocation string
	Phone               string
}

// LoginInput is the input to Login.
type LoginInput struct {
	Email    string
	Password string
	// DeviceInfo is whatever the client chose to say about itself. It is stored
	// as JSONB and shown back to the user in the device list, so it is never used
	// for an authorisation decision.
	DeviceInfo map[string]any
	// IPAddress is the address the sign-in came from, or "" when unknown.
	IPAddress string
}

// TokenOutput is what a successful sign-in or refresh returns.
type TokenOutput struct {
	// User is the authenticated account.
	User *identity.User
	// AccessToken is the short-lived bearer token.
	AccessToken string
	// RefreshToken is the opaque token to store and present on refresh. It is
	// never the same string twice: every refresh rotates it (KNOT-ADR-085).
	RefreshToken string
	// ExpiresIn is the access token's lifetime in seconds.
	ExpiresIn int
	// SessionID is the session the pair belongs to. It is also inside the access
	// token as a claim; it is returned separately so a caller does not have to
	// parse its own token.
	SessionID string
	// Recovered reports that this sign-in cancelled a pending account deletion, so
	// the client can say so rather than silently pretending nothing happened.
	Recovered bool
}

// Register creates an account, stores its password credential, and issues an
// email-verification token.
//
// It returns **no tokens**: an unverified account cannot sign in, so handing out
// a session here would contradict the rule it is about to enforce
// (KNOT-ADR-083). The verification token is written to the log instead of being
// emailed, because there is no email provider yet (KNOT-ADR-088).
func (s *Service) Register(ctx context.Context, in RegisterInput) (*identity.User, error) {
	email, err := identity.ValidateEmail(in.Email)
	if err != nil {
		return nil, err
	}
	if err := identity.ValidatePassword(in.Password); err != nil {
		return nil, err
	}
	displayName, err := identity.ValidateDisplayName(in.DisplayName)
	if err != nil {
		return nil, err
	}
	languages, err := identity.ValidateLanguages(in.PreferredLanguages)
	if err != nil {
		return nil, err
	}
	phone, err := identity.ValidateOptional(in.Phone, "phone", 32)
	if err != nil {
		return nil, err
	}
	location, err := identity.ValidateOptional(in.ApproximateLocation, "approximate_location", 120)
	if err != nil {
		return nil, err
	}

	hash, err := identity.HashPassword(in.Password)
	if err != nil {
		return nil, fmt.Errorf("auth: hash password: %w", err)
	}

	user, err := s.users.CreateUser(ctx, &identity.User{
		Email:               email,
		Phone:               phone,
		PasswordHash:        hash,
		DisplayName:         displayName,
		PreferredLanguages:  languages,
		ApproximateLocation: location,
	})
	if err != nil {
		if errors.Is(err, identity.ErrEmailTaken) {
			return nil, identity.ErrEmailTaken
		}
		return nil, fmt.Errorf("auth: register: %w", err)
	}

	// The credential row is the future home of the password secret (KNOT-ADR-079).
	// A failure here does not fail the registration: the account is complete and
	// sign-in reads users.password_hash, so the honest response is a working
	// account and a loud log rather than an error for a user who would then find
	// their email taken by their own half-created row.
	s.storePasswordCredential(ctx, user.ID, hash)

	if err := s.issueEmailVerification(ctx, user); err != nil {
		return nil, err
	}

	return user, nil
}

// issueEmailVerification mints a verification token and logs the link.
func (s *Service) issueEmailVerification(ctx context.Context, user *identity.User) error {
	secret, err := newSecret()
	if err != nil {
		return fmt.Errorf("auth: register: %w", err)
	}

	token, err := s.verifications.Create(ctx, &VerificationToken{
		UserID:    user.ID,
		TokenHash: hashSecret(secret),
		TokenType: VerificationTokenEmailVerification,
		ExpiresAt: s.now().Add(EmailVerificationTTL),
	})
	if err != nil {
		return fmt.Errorf("auth: register: issue verification token: %w", err)
	}

	s.logger.InfoContext(ctx,
		"email verification link (no email provider yet; KNOT-ADR-088)",
		slog.String("email", user.Email),
		slog.String("expires_at", token.ExpiresAt.UTC().Format(time.RFC3339)),
		slog.String("url", s.verificationLinkBase+secret),
	)

	return nil
}

// VerifyEmail marks the account as verified and consumes the token.
//
// The order matters: the user row is written before the token is consumed, so a
// crash in between leaves an account that is verified and a token that is still
// redeemable — which a retry turns into a no-op. The reverse order would consume
// the token and leave the account unverified, and the user would have no way back
// in.
func (s *Service) VerifyEmail(ctx context.Context, token string) error {
	trimmed := strings.TrimSpace(token)
	if trimmed == "" {
		return ErrInvalidVerificationToken
	}

	stored, err := s.verifications.FindByTokenHash(ctx, hashSecret(trimmed))
	if err != nil {
		if errors.Is(err, ErrVerificationTokenNotFound) {
			return ErrInvalidVerificationToken
		}
		return fmt.Errorf("auth: verify email: %w", err)
	}

	if stored.TokenType != VerificationTokenEmailVerification || stored.Used() || stored.Expired(s.now()) {
		return ErrInvalidVerificationToken
	}

	if _, err := s.users.MarkEmailVerified(ctx, stored.UserID); err != nil {
		if errors.Is(err, identity.ErrUserNotFound) {
			return ErrInvalidVerificationToken
		}
		return fmt.Errorf("auth: verify email: %w", err)
	}

	if err := s.verifications.Consume(ctx, stored.ID); err != nil && !errors.Is(err, ErrVerificationTokenConsumed) {
		return fmt.Errorf("auth: verify email: consume token: %w", err)
	}

	return nil
}

// Login verifies credentials and opens a session.
//
// An unknown email and a wrong password both return ErrInvalidCredentials, with
// the same work performed, so the endpoint does not leak which accounts exist. An
// unverified address is reported as ErrEmailNotVerified only after the password
// has been checked, because reaching that error means the caller already knows
// the password.
func (s *Service) Login(ctx context.Context, in LoginInput) (*TokenOutput, error) {
	email := identity.NormalizeEmail(in.Email)
	if email == "" || in.Password == "" {
		return nil, identity.ErrInvalidCredentials
	}

	// The lookup deliberately includes deleted accounts: an account inside its
	// recovery window must still be findable, because signing in is how somebody
	// takes it back. The deletion state is then read from the row, and the password
	// is checked before any of it is acted on.
	user, err := s.users.FindUserByEmail(ctx, email)
	if err != nil {
		if errors.Is(err, identity.ErrUserNotFound) {
			_, _ = identity.VerifyPassword(s.dummyHash, in.Password)
			return nil, identity.ErrInvalidCredentials
		}
		return nil, fmt.Errorf("auth: login: %w", err)
	}

	// A purged account has been anonymised: there is no password left to match and
	// no person to sign in as.
	if user.PurgedAt != nil {
		_, _ = identity.VerifyPassword(s.dummyHash, in.Password)
		return nil, identity.ErrAccountPendingPurge
	}

	ok, err := identity.VerifyPassword(user.PasswordHash, in.Password)
	if err != nil {
		return nil, fmt.Errorf("auth: login: %w", err)
	}
	if !ok {
		return nil, identity.ErrInvalidCredentials
	}

	// Deletion states, in the order the window makes them possible. Recovery happens
	// here — before any token is minted — so a successful sign-in can never hand out
	// a session for an account that is still deleted (KNOT-ADR-081).
	recovered := false
	if user.DeletedAt != nil {
		switch {
		case user.DeletionScheduledAt == nil:
			// Deleted with no window recorded: there is nothing to recover into.
			return nil, identity.ErrAccountDeleted
		case !s.now().Before(*user.DeletionScheduledAt):
			// The window has closed. The purge has not necessarily run yet, but the
			// account is gone as far as its owner is concerned.
			return nil, identity.ErrAccountPendingPurge
		default:
			if err := s.deletions.CancelDeletion(ctx, user.ID); err != nil {
				return nil, fmt.Errorf("auth: login: cancel deletion: %w", err)
			}
			recovered = true
		}
	}

	if user.EmailVerifiedAt == nil {
		return nil, identity.ErrEmailNotVerified
	}

	output, err := s.openSession(ctx, user, in.DeviceInfo, in.IPAddress)
	if err != nil {
		return nil, err
	}
	output.Recovered = recovered

	return output, nil
}

// openSession writes a session, enforces the device cap, and mints the pair.
func (s *Service) openSession(ctx context.Context, user *identity.User, deviceInfo map[string]any, ipAddress string) (*TokenOutput, error) {
	sessionID, err := newSessionID()
	if err != nil {
		return nil, fmt.Errorf("auth: login: %w", err)
	}

	refreshToken, refreshHash, err := newRefreshToken(sessionID)
	if err != nil {
		return nil, fmt.Errorf("auth: login: %w", err)
	}

	session, err := s.sessions.Create(ctx, &Session{
		ID:               sessionID,
		UserID:           user.ID,
		RefreshTokenHash: refreshHash,
		DeviceInfo:       deviceInfo,
		IPAddress:        ipAddress,
	})
	if err != nil {
		return nil, fmt.Errorf("auth: login: create session: %w", err)
	}

	s.enforceSessionCap(ctx, user.ID)

	accessToken, err := s.tokens.IssueAccessTokenForSession(user.ID, session.ID)
	if err != nil {
		return nil, fmt.Errorf("auth: login: issue access token: %w", err)
	}

	return &TokenOutput{
		User:         user,
		AccessToken:  accessToken,
		RefreshToken: refreshToken,
		ExpiresIn:    int(identity.AccessTokenTTL.Seconds()),
		SessionID:    session.ID,
	}, nil
}

// enforceSessionCap revokes the oldest sessions until the account is at or below
// MaxActiveSessions (KNOT-ADR-084).
//
// A failure is logged, never returned: the sign-in itself succeeded, and refusing
// a correct password because an old session could not be closed would turn a
// housekeeping problem into a lockout. The next sign-in retries the cap.
func (s *Service) enforceSessionCap(ctx context.Context, userID string) {
	live, err := s.sessions.ListForUser(ctx, userID)
	if err != nil {
		s.logger.WarnContext(ctx, "session cap: could not list sessions",
			slog.String("user_id", userID), slog.String("error", err.Error()))
		return
	}

	// ListForUser is newest first, so everything past the cap is a session older
	// than the ones the user is currently using.
	for i := MaxActiveSessions; i < len(live); i++ {
		if err := s.sessions.Revoke(ctx, live[i].ID); err != nil {
			s.logger.WarnContext(ctx, "session cap: could not revoke the oldest session",
				slog.String("user_id", userID),
				slog.String("session_id", live[i].ID),
				slog.String("error", err.Error()),
			)
		}
	}
}

// Refresh rotates a session's refresh token and mints a new pair.
//
// Every call invalidates the token it was given: the old hash is replaced in the
// same statement that checks it, so two clients racing with one token cannot both
// succeed. A token presented after it was rotated is *reuse*, and every session
// the account has is revoked, because a token in two hands means one of them is
// not the owner (KNOT-ADR-085).
func (s *Service) Refresh(ctx context.Context, refreshToken string) (*TokenOutput, error) {
	sessionID, secret, err := parseRefreshToken(strings.TrimSpace(refreshToken))
	if err != nil {
		return nil, ErrInvalidRefreshToken
	}

	session, err := s.sessions.FindByID(ctx, sessionID)
	if err != nil {
		if errors.Is(err, ErrSessionNotFound) {
			return nil, ErrInvalidRefreshToken
		}
		return nil, fmt.Errorf("auth: refresh: %w", err)
	}

	// A session that is already dead cannot be repaired and has nothing left to
	// contain: signing out, or a previous reuse detection, already ended it.
	if !session.Active() {
		return nil, ErrInvalidRefreshToken
	}

	rotatedToken, rotatedHash, err := newRefreshToken(sessionID)
	if err != nil {
		return nil, fmt.Errorf("auth: refresh: %w", err)
	}

	rotated, err := s.sessions.RotateRefreshToken(ctx, sessionID, hashSecret(secret), rotatedHash)
	if err != nil {
		if errors.Is(err, ErrSessionRefreshTokenMismatch) {
			return nil, s.containReuse(ctx, session.UserID, sessionID)
		}
		if errors.Is(err, ErrSessionNotFound) {
			return nil, ErrInvalidRefreshToken
		}
		return nil, fmt.Errorf("auth: refresh: %w", err)
	}

	user, err := s.users.FindActiveUserByID(ctx, rotated.UserID)
	if err != nil {
		// The session outlived its account: close it rather than refresh it.
		if errors.Is(err, identity.ErrUserNotFound) {
			_ = s.sessions.Revoke(ctx, rotated.ID)
			return nil, ErrInvalidRefreshToken
		}
		return nil, fmt.Errorf("auth: refresh: %w", err)
	}

	accessToken, err := s.tokens.IssueAccessTokenForSession(user.ID, rotated.ID)
	if err != nil {
		return nil, fmt.Errorf("auth: refresh: issue access token: %w", err)
	}

	return &TokenOutput{
		User:         user,
		AccessToken:  accessToken,
		RefreshToken: rotatedToken,
		ExpiresIn:    int(identity.AccessTokenTTL.Seconds()),
		SessionID:    rotated.ID,
	}, nil
}

// containReuse revokes the compromised session and every other session the
// account has, and returns the error the caller reports.
//
// RevokeAllForUser is deliberately idempotent, so a burst of replays cannot make
// this fail. Any error it does hit is logged and swallowed: the token is refused
// either way, and the direction of the failure is safe (sessions stay revoked).
func (s *Service) containReuse(ctx context.Context, userID, sessionID string) error {
	s.logger.WarnContext(ctx,
		"refresh token reuse detected; revoking every session for the account",
		slog.String("user_id", userID),
		slog.String("session_id", sessionID),
	)

	if err := s.sessions.RevokeAllForUser(ctx, userID); err != nil {
		s.logger.ErrorContext(ctx, "could not revoke all sessions after reuse detection",
			slog.String("user_id", userID), slog.String("error", err.Error()))
	}

	return ErrRefreshTokenReused
}

// Logout revokes one session.
//
// It is idempotent: a session that is already gone, or already revoked, reports
// success, because the caller's intent — "this session must not work any more" —
// is satisfied either way and a client should not have to distinguish the cases
// on a sign-out path.
func (s *Service) Logout(ctx context.Context, userID, sessionID string) error {
	if strings.TrimSpace(sessionID) == "" {
		return ErrSessionNotFound
	}

	session, err := s.sessions.FindByID(ctx, sessionID)
	if err != nil {
		if errors.Is(err, ErrSessionNotFound) {
			return nil
		}
		return fmt.Errorf("auth: logout: %w", err)
	}

	// A session id is only ever read from the caller's own access token, but the
	// ownership check stays: it is what makes the handler safe even if a future
	// caller passes an id from somewhere else.
	if session.UserID != userID {
		return ErrSessionNotFound
	}

	if err := s.sessions.Revoke(ctx, session.ID); err != nil {
		if errors.Is(err, ErrSessionNotFound) {
			return nil
		}
		return fmt.Errorf("auth: logout: %w", err)
	}

	return nil
}

// ListSessions returns the account's live sessions, newest first.
func (s *Service) ListSessions(ctx context.Context, userID string) ([]*Session, error) {
	sessions, err := s.sessions.ListForUser(ctx, userID)
	if err != nil {
		if errors.Is(err, ErrInvalidUserID) {
			return nil, ErrSessionNotFound
		}
		return nil, fmt.Errorf("auth: list sessions: %w", err)
	}

	return sessions, nil
}

// RevokeSession ends one of the caller's own sessions.
//
// A session that belongs to somebody else is reported as not found rather than as
// forbidden: the API must not confirm that an id exists on another account.
func (s *Service) RevokeSession(ctx context.Context, userID, sessionID string) error {
	session, err := s.sessions.FindByID(ctx, sessionID)
	if err != nil {
		if errors.Is(err, ErrSessionNotFound) {
			return ErrSessionNotFound
		}
		return fmt.Errorf("auth: revoke session: %w", err)
	}

	if session.UserID != userID {
		return ErrSessionNotFound
	}

	if err := s.sessions.Revoke(ctx, session.ID); err != nil {
		if errors.Is(err, ErrSessionNotFound) {
			return ErrSessionNotFound
		}
		return fmt.Errorf("auth: revoke session: %w", err)
	}

	return nil
}

// ForgotPassword issues a password reset token if the address belongs to an
// account, and does nothing at all if it does not.
//
// It returns nil in every case, including for an address that is not registered
// and for one that is malformed: the endpoint's whole contract is that it answers
// the same way whether or not an account exists (KNOT-ADR-087). The token itself
// lives in Redis under its own hash, expires in PasswordResetTTL, and is logged
// instead of emailed (KNOT-ADR-088).
func (s *Service) ForgotPassword(ctx context.Context, email string) error {
	normalized := identity.NormalizeEmail(email)
	if normalized == "" {
		return nil
	}

	user, err := s.users.FindActiveUserByEmail(ctx, normalized)
	if err != nil {
		if errors.Is(err, identity.ErrUserNotFound) {
			return nil
		}
		return fmt.Errorf("auth: forgot password: %w", err)
	}

	secret, err := newSecret()
	if err != nil {
		return fmt.Errorf("auth: forgot password: %w", err)
	}

	key := passwordResetKeyPrefix + hashSecret(secret)
	if err := s.cache.SetEX(ctx, key, user.ID, PasswordResetTTL); err != nil {
		return fmt.Errorf("auth: forgot password: store token: %w", err)
	}

	s.logger.InfoContext(ctx,
		"password reset link (no email provider yet; KNOT-ADR-088)",
		slog.String("email", user.Email),
		slog.String("expires_in", PasswordResetTTL.String()),
		slog.String("url", s.passwordResetLinkBase+secret),
	)

	return nil
}

// ResetPassword sets a new password from a reset token and signs the account out
// everywhere.
//
// The order is deliberate. The password is written first, then every session is
// revoked, then the token is deleted. If revocation fails the error is returned
// while the token is still valid, so the client can retry and the account does
// not end up with a new password and live sessions authorised by the old one
// (KNOT-ADR-087).
func (s *Service) ResetPassword(ctx context.Context, token, newPassword string) error {
	if err := identity.ValidatePassword(newPassword); err != nil {
		return err
	}

	trimmed := strings.TrimSpace(token)
	if trimmed == "" {
		return ErrInvalidPasswordResetToken
	}

	key := passwordResetKeyPrefix + hashSecret(trimmed)

	userID, err := s.cache.Get(ctx, key)
	if err != nil {
		if errors.Is(err, cache.ErrMiss) {
			return ErrInvalidPasswordResetToken
		}
		return fmt.Errorf("auth: reset password: read token: %w", err)
	}

	user, err := s.users.FindActiveUserByID(ctx, userID)
	if err != nil {
		if errors.Is(err, identity.ErrUserNotFound) {
			return ErrInvalidPasswordResetToken
		}
		return fmt.Errorf("auth: reset password: %w", err)
	}

	hash, err := identity.HashPassword(newPassword)
	if err != nil {
		return fmt.Errorf("auth: reset password: hash password: %w", err)
	}

	if _, err := s.users.UpdatePasswordHash(ctx, user.ID, hash); err != nil {
		return fmt.Errorf("auth: reset password: %w", err)
	}

	s.storePasswordCredential(ctx, user.ID, hash)

	if err := s.sessions.RevokeAllForUser(ctx, user.ID); err != nil {
		return fmt.Errorf("auth: reset password: revoke sessions: %w", err)
	}

	// Deleting the token is what makes it single use. A failure here is logged
	// rather than returned: the password has already changed and the sessions are
	// gone, so telling the user the reset failed would be untrue, and the token
	// expires on its own within PasswordResetTTL.
	if err := s.cache.Delete(ctx, key); err != nil {
		s.logger.ErrorContext(ctx, "could not delete a used password reset token",
			slog.String("user_id", user.ID), slog.String("error", err.Error()))
	}

	return nil
}

// storePasswordCredential writes (or replaces) the `password` credential row.
//
// `users.password_hash` remains the value sign-in reads until a later task moves
// the secret across (KNOT-ADR-079), so this is a mirror rather than a source of
// truth today. A failure is logged and swallowed on purpose: every caller has
// already changed the authoritative value, and failing the request afterwards
// would report a change that did in fact happen.
func (s *Service) storePasswordCredential(ctx context.Context, userID, hash string) {
	existing, err := s.credentials.FindByUserIDAndType(ctx, userID, CredentialTypePassword)
	switch {
	case err == nil:
		existing.CredentialData = hash
		if _, err := s.credentials.Update(ctx, existing); err != nil {
			s.logger.WarnContext(ctx, "could not update the password credential",
				slog.String("user_id", userID), slog.String("error", err.Error()))
		}
	case errors.Is(err, ErrCredentialNotFound):
		if _, err := s.credentials.Create(ctx, &Credential{
			UserID:         userID,
			CredentialType: CredentialTypePassword,
			CredentialData: hash,
		}); err != nil && !errors.Is(err, ErrCredentialExists) {
			s.logger.WarnContext(ctx, "could not create the password credential",
				slog.String("user_id", userID), slog.String("error", err.Error()))
		}
	default:
		s.logger.WarnContext(ctx, "could not read the password credential",
			slog.String("user_id", userID), slog.String("error", err.Error()))
	}
}
