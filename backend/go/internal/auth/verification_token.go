package auth

import (
	"context"
	"errors"
	"time"
)

// Verification token types. The set is closed by a CHECK constraint, for the same
// reason as the credential types: a type the schema accepts but no code handles
// is a way to mint tokens nothing can redeem.
const (
	// VerificationTokenEmailVerification proves the address belongs to the user.
	VerificationTokenEmailVerification = "email_verification"
	// VerificationTokenPasswordReset authorises setting a new password.
	VerificationTokenPasswordReset = "password_reset"
)

var (
	// ErrVerificationTokenNotFound is returned when a token hash or id matches no
	// row.
	ErrVerificationTokenNotFound = errors.New("auth: verification token not found")
	// ErrVerificationTokenConsumed is returned when a token has already been
	// used. It is separate from not-found because a second attempt to use a link
	// is usually a real person opening it twice, not an attacker with a guess.
	ErrVerificationTokenConsumed = errors.New("auth: verification token already consumed")
	// ErrInvalidVerificationTokenType is returned for a type outside the closed
	// set.
	ErrInvalidVerificationTokenType = errors.New("auth: invalid verification token type")
)

// IsVerificationTokenType reports whether value is one of the
// VerificationToken* constants.
func IsVerificationTokenType(value string) bool {
	switch value {
	case VerificationTokenEmailVerification, VerificationTokenPasswordReset:
		return true
	default:
		return false
	}
}

// VerificationToken is one single-use proof of address ownership.
//
// Only the hash of the token is stored, so a backup of this table cannot be used
// to verify an address or reset a password (KNOT-ADR-080).
type VerificationToken struct {
	// ID is the token's own identity, and what Consume takes.
	ID string
	// UserID is the account the token was issued for.
	UserID string
	// TokenHash is the SHA-256 of the token, hex encoded. The plaintext was sent
	// to the user and is not recoverable from this row.
	TokenHash string
	// TokenType is one of the VerificationToken* constants.
	TokenType string
	// ExpiresAt is when the token stops being redeemable.
	ExpiresAt time.Time
	// ConsumedAt is when the token was redeemed, or nil while it is unused.
	ConsumedAt *time.Time
	// CreatedAt is when the token was issued.
	CreatedAt time.Time
}

// Expired reports whether the token has expired as of now.
//
// Expiry is not a column state: it is a comparison against the clock, and the
// store deliberately does not enforce it. Enforcing it in the query would make
// an expired token indistinguishable from one that never existed, and "your link
// has expired" is a different message to "that link is not valid".
func (t *VerificationToken) Expired(now time.Time) bool {
	return !now.Before(t.ExpiresAt)
}

// Used reports whether the token has been redeemed.
func (t *VerificationToken) Used() bool {
	return t.ConsumedAt != nil
}

// VerificationTokenStore is the storage contract for verification tokens.
//
// It has no Delete: consumed and expired tokens are history, and a token that
// was used is exactly what a "someone tried to reset my password" investigation
// needs to see.
type VerificationTokenStore interface {
	// Create stores a new token and returns the stored row.
	Create(ctx context.Context, token *VerificationToken) (*VerificationToken, error)

	// FindByTokenHash returns the token for a hash, redeemed or not. Deciding
	// whether it may still be used requires Expired and Used, which is policy
	// the service owns.
	FindByTokenHash(ctx context.Context, tokenHash string) (*VerificationToken, error)

	// Consume marks a token as redeemed, exactly once. A token that is already
	// redeemed is ErrVerificationTokenConsumed; an id that matches no row is
	// ErrVerificationTokenNotFound.
	Consume(ctx context.Context, tokenID string) error
}
