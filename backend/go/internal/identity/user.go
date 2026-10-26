// Package identity implements the Knot identity domain: user registration,
// password hashing, authentication, and JWT issuance.
//
// The package is deliberately layered:
//
//	user.go           domain types and errors
//	store.go          the persistence contract (UserStore)
//	postgres_store.go the PostgreSQL implementation of that contract
//	service.go        business rules (Register, Login)
//	password.go       argon2id hashing in PHC string format
//	token.go          HS256 JWT issuance and verification
//
// Nothing in this package knows about HTTP, and nothing outside
// internal/identity, internal/httpapi, and internal/config imports these types
// (cmd/knot is the composition root and wires them together).
package identity

import (
	"errors"
	"fmt"
	"time"
)

// Sentinel errors returned by the service and store layers. Handlers map these
// to HTTP status codes; they are never returned to clients verbatim.
var (
	// ErrEmailTaken is returned when registering an email that already exists.
	ErrEmailTaken = errors.New("identity: email already registered")
	// ErrInvalidCredentials is returned for both an unknown email and a wrong
	// password. The two cases are deliberately indistinguishable so the API
	// cannot be used to enumerate accounts.
	ErrInvalidCredentials = errors.New("identity: invalid credentials")
	// ErrUserNotFound is returned by the store when no row matches. It is
	// internal: Login translates it into ErrInvalidCredentials.
	ErrUserNotFound = errors.New("identity: user not found")
	// ErrInvalidHash is returned when a stored password hash cannot be parsed.
	ErrInvalidHash = errors.New("identity: invalid password hash")
	// ErrInvalidToken is returned when a JWT is malformed, expired, signed with
	// the wrong key, or of the wrong token type.
	ErrInvalidToken = errors.New("identity: invalid token")
	// ErrEmailNotVerified is returned when sign-in is attempted on an account
	// whose address has never been proven. It is reported separately from
	// ErrInvalidCredentials because the remedy is different — check the inbox —
	// and because the caller has already proved the password, so naming the state
	// reveals nothing they did not know (KNOT-ADR-083).
	ErrEmailNotVerified = errors.New("identity: email not verified")
	// ErrAccountDeleted is returned when an account is inside its deletion window.
	// The row still exists; it is not a person any more (KNOT-ADR-081).
	ErrAccountDeleted = errors.New("identity: account deleted")
	// ErrAccountPendingPurge is returned when an account's recovery window has
	// closed but the purge job has not reached it yet. It is a different answer
	// from ErrAccountDeleted because nothing can bring the account back: the only
	// honest reply is that it is gone (KNOT-ADR-089).
	ErrAccountPendingPurge = errors.New("identity: account pending purge")
)

// DeletionRecoveryWindow is how long a deleted account can be recovered before the
// purge job anonymises it.
//
// It is a constant rather than a config value because it is a promise made to the
// user in the deletion confirmation, and a promise that varies per deployment is
// not a promise. Thirty days is long enough to change your mind and short enough to
// mean something (KNOT-ADR-081).
const DeletionRecoveryWindow = 30 * 24 * time.Hour

// ValidationError describes a rejected field on a request. Handlers expose the
// Field and Message so clients can highlight the offending input.
type ValidationError struct {
	// Field is the request field that failed validation.
	Field string
	// Message explains the failure in a client-safe way. It never contains
	// secrets or internal detail.
	Message string
}

// Error implements the error interface.
func (e *ValidationError) Error() string {
	return fmt.Sprintf("identity: invalid %s: %s", e.Field, e.Message)
}

// User is a Knot account.
//
// PasswordHash is the encoded argon2id PHC string. It must never be serialised
// into an API response — the HTTP layer maps User onto its own response type
// rather than marshalling this struct directly.
type User struct {
	ID                  string
	Email               string
	Phone               string
	PasswordHash        string
	DisplayName         string
	PreferredLanguages  []string
	ApproximateLocation string
	// Role is the account's moderation role: "user", "moderator", or "admin".
	// It is assigned manually (there is no API to change it), defaults to
	// "user", and is read by the moderation layer to gate the 017b queue.
	Role string
	// AvatarURL is the object key of the user's avatar in the media bucket, not
	// a public URL. Empty means the user has no avatar. The HTTP layer turns it
	// into the backend path clients fetch (see avatar_handler.go).
	AvatarURL string
	CreatedAt time.Time
	UpdatedAt time.Time
	// EmailVerifiedAt is when the address was proven to belong to this account,
	// or nil while it has not been. Sign-in requires it to be set (KNOT-ADR-083),
	// and the zero value of a freshly registered account is nil, which is why it is
	// a pointer rather than a time.Time: "never verified" is not a moment.
	EmailVerifiedAt *time.Time
	// DeletedAt is when the owner asked for the account to be deleted, or nil while
	// it is live. A deleted account still exists: its content is read by other
	// people, so the row stays and the person goes (KNOT-ADR-081).
	DeletedAt *time.Time
	// DeletionScheduledAt is when the recovery window closes and the purge job may
	// anonymise the account. It is `DeletedAt + DeletionRecoveryWindow`, stored
	// rather than derived so the window can be shortened or extended for one account
	// without rewriting the history of its deletion.
	DeletionScheduledAt *time.Time
	// PurgedAt is when the account was anonymised, or nil while it has not been.
	// It is what makes the purge idempotent: a purged row can never be due again
	// (KNOT-ADR-089).
	PurgedAt *time.Time
}

// TokenType values carried in the "typ" claim so an access token can never be
// substituted for a refresh token, and vice versa.
const (
	// TokenTypeAccess marks a short-lived access token.
	TokenTypeAccess = "access"
	// TokenTypeRefresh marks a long-lived refresh token.
	TokenTypeRefresh = "refresh"
)

// TokenPair is the pair of tokens issued by Register and Login.
type TokenPair struct {
	// AccessToken is the short-lived bearer token for the Authorization header.
	AccessToken string
	// RefreshToken is the long-lived token used to obtain a new access token.
	// The refresh endpoint is not implemented yet; only issuance exists.
	RefreshToken string
	// ExpiresIn is the access token's lifetime in seconds.
	ExpiresIn int
}

// AuthResult is what a successful Register or Login returns.
type AuthResult struct {
	// User is the authenticated account, without its password hash being
	// relevant to callers (the HTTP layer never serialises PasswordHash).
	User *User
	// Tokens holds the freshly issued access and refresh tokens.
	Tokens TokenPair
}
