package auth

import (
	"context"
	"errors"
	"time"
)

// Credential types. The set is closed by a CHECK constraint on the column, so a
// new method needs a migration as well as a constant: that is deliberate, because
// a method nobody has written storage or policy for is not a method.
const (
	// CredentialTypePassword stores an argon2id PHC string.
	CredentialTypePassword = "password"
	// CredentialTypeOAuthGoogle stores the provider's stable subject id.
	CredentialTypeOAuthGoogle = "oauth_google"
	// CredentialTypeMagicLink stores the secret of a single-use link.
	CredentialTypeMagicLink = "magic_link"
)

var (
	// ErrCredentialNotFound is returned when a user has no credential of the
	// requested type.
	ErrCredentialNotFound = errors.New("auth: credential not found")
	// ErrCredentialExists is returned when the user already has a credential of
	// that type. A method is replaced with Update, never added twice.
	ErrCredentialExists = errors.New("auth: credential already exists for this user and type")
	// ErrInvalidCredentialType is returned for a type outside the closed set.
	ErrInvalidCredentialType = errors.New("auth: invalid credential type")
)

// IsCredentialType reports whether value is one of the CredentialType constants.
//
// The stores call it before inserting so a bad type fails with a clear error
// naming the value, instead of as a CHECK constraint violation from PostgreSQL.
func IsCredentialType(value string) bool {
	switch value {
	case CredentialTypePassword, CredentialTypeOAuthGoogle, CredentialTypeMagicLink:
		return true
	default:
		return false
	}
}

// Credential is one way a user can prove who they are.
//
// The secret itself is never selected into this struct by any endpoint; what
// CredentialData holds depends on the type, and only the code that verifies that
// type knows how to read it.
type Credential struct {
	// ID is the credential's own identity.
	ID string
	// UserID is the account the credential belongs to.
	UserID string
	// CredentialType is one of the CredentialType constants.
	CredentialType string
	// CredentialData is the method's opaque secret material: an argon2id PHC
	// string for a password, a provider subject id for OAuth, a single-use
	// secret for a magic link. The column is TEXT on purpose — each method
	// stores exactly one string, so giving it a shape would only invite the
	// wrong one to be read.
	CredentialData string
	// CreatedAt is when the credential was added.
	CreatedAt time.Time
	// UpdatedAt is when its data last changed.
	UpdatedAt time.Time
}

// CredentialStore is the storage contract for credentials.
//
// There is no FindByID and no List: credentials are only ever looked up as "this
// user, this method", and a method that is not in that shape has no caller.
type CredentialStore interface {
	// Create stores a new credential and returns the stored row. A user that
	// already has a credential of this type is ErrCredentialExists.
	Create(ctx context.Context, credential *Credential) (*Credential, error)

	// FindByUserIDAndType returns the user's credential of that type, or
	// ErrCredentialNotFound.
	FindByUserIDAndType(ctx context.Context, userID, credentialType string) (*Credential, error)

	// Update replaces the credential's data and refreshes updated_at. The type
	// and the owner are not updateable: changing which method a row represents
	// is a delete and an insert, and moving a credential to another user would
	// be an account takeover.
	Update(ctx context.Context, credential *Credential) (*Credential, error)
}
