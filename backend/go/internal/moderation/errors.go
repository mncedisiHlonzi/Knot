package moderation

import (
	"errors"
	"fmt"
)

// Sentinel errors. Handlers map these onto HTTP status codes; they are never
// returned to clients verbatim.
var (
	// ErrValidation is returned for rejected input. Every validation failure also
	// carries a *ValidationError naming the offending field, so errors.Is(err,
	// ErrValidation) and errors.As(err, &validation) are both true for the same
	// value.
	ErrValidation = errors.New("moderation: invalid input")

	// ErrSelfBlock is returned when a user tries to block themselves. The schema
	// forbids it with a CHECK; the service rejects it earlier with a clear message.
	ErrSelfBlock = errors.New("moderation: cannot block yourself")

	// ErrBlocked is returned when a write targets content authored by a user who
	// is on either side of a block. Handlers map it to 403 "blocked" (KNOT-ADR-060).
	ErrBlocked = errors.New("moderation: interaction is blocked")

	// ErrAlreadyReported is returned when the same user reports the same entity a
	// second time. The unique index enforces it; the service surfaces it so the
	// handler can answer 409.
	ErrAlreadyReported = errors.New("moderation: you have already reported this")

	// ErrEntityNotFound is returned when a report names an entity that does not
	// exist. Handlers map it to 404.
	ErrEntityNotFound = errors.New("moderation: reported entity not found")
)

// ValidationError describes a rejected field on a request. Handlers expose the
// Field and Message so clients can highlight the offending input.
type ValidationError struct {
	// Field is the request field that failed validation.
	Field string
	// Message explains the failure in a client-safe way. It never contains secrets
	// or internal detail.
	Message string
}

// Error implements the error interface.
func (e *ValidationError) Error() string {
	return fmt.Sprintf("moderation: invalid %s: %s", e.Field, e.Message)
}

// Unwrap makes every ValidationError satisfy errors.Is(err, ErrValidation), so
// callers can test either the sentinel or the concrete type.
func (e *ValidationError) Unwrap() error { return ErrValidation }
