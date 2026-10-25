// Package auth is the identity domain's authentication layer.
//
// It owns three storage contracts and their PostgreSQL implementations:
//
//   - Session           — one row per signed-in device (session.go).
//   - Credential        — one row per way a user can prove who they are
//     (credential.go).
//   - VerificationToken — one row per single-use proof of address ownership
//     (verification_token.go).
//
// It deliberately contains no service, no token minting and no HTTP. Those
// arrive in KNOT-031; keeping them out is what lets this package be tested
// against a database and nothing else, and it is what stops the storage layer
// from quietly growing policy. The account model it serves is KNOT-ADR-079, the
// token strategy it will be used by is KNOT-ADR-080, and the refresh tokens it
// stores are opaque strings whose plaintext the server never keeps.
//
// Every method takes a context, every error is wrapped with %w, and every store
// is built by a constructor that takes an injected pool: there is no global
// state and nothing reaches for a connection of its own.
package auth

import (
	"errors"
	"fmt"
)

// ErrInvalidUserID is returned when an operation names a user id that is not a
// UUID. It is reported before a query is sent so a malformed id can never turn
// into a cast error from the database.
var ErrInvalidUserID = errors.New("auth: invalid user id")

// uniqueViolation is the PostgreSQL SQLSTATE raised by a unique constraint
// violation. It is how a duplicate credential or token is recognised.
const uniqueViolation = "23505"

// nullIfEmpty maps an empty string to SQL NULL.
//
// It exists so the store never writes ” where the schema means "unknown": an
// empty IP address and an unknown IP address are the same thing to every reader
// of the table, and only one of them is honest.
func nullIfEmpty(value string) *string {
	if value == "" {
		return nil
	}

	return &value
}

// isUUID reports whether s is a canonical 8-4-4-4-12 hexadecimal UUID.
//
// The check is local rather than a dependency, and it is a shape check rather
// than a parse: the only thing it has to guarantee is that the value can be
// compared against a uuid column without a cast error.
func isUUID(s string) bool {
	if len(s) != 36 {
		return false
	}

	for i := 0; i < len(s); i++ {
		switch i {
		case 8, 13, 18, 23:
			if s[i] != '-' {
				return false
			}
		default:
			if !isHexDigit(s[i]) {
				return false
			}
		}
	}

	return true
}

// isHexDigit reports whether b is an ASCII hexadecimal digit.
func isHexDigit(b byte) bool {
	switch {
	case b >= '0' && b <= '9':
		return true
	case b >= 'a' && b <= 'f':
		return true
	case b >= 'A' && b <= 'F':
		return true
	default:
		return false
	}
}

// wrapUUIDError reports a malformed user id consistently.
func wrapUUIDError(operation, userID string) error {
	return fmt.Errorf("%s: user id %q: %w", operation, userID, ErrInvalidUserID)
}
