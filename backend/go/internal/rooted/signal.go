// Package rooted implements the Knot Rooted domain: a user's self-declared
// connection to a place, expressed as a place name and a duration bucket.
//
// Rooted is Knot's trust and community layer. It is deliberately honest (a signal
// is self-declared, never verified), privacy-conscious (a place name at city or
// region precision, never a precise location), and simple (no vouching, no score,
// no gating in this task).
//
// The package is layered, mirroring internal/stories:
//
//	signal.go         domain types, the duration buckets, the store contract, errors
//	service.go        business rules (SetSignal, GetMySignals, GetPublicSignals, …)
//	postgres_store.go the PostgreSQL implementation of the store contract
//
// Nothing in this package knows about HTTP, JSON, or SQL types. User ids are plain
// strings holding canonical UUID text, the convention internal/identity and
// internal/stories already established (KNOT-ADR-010).
package rooted

import (
	"context"
	"errors"
	"fmt"
	"time"
)

// Sentinel errors. Handlers map these onto HTTP status codes; they are never
// returned to clients verbatim.
var (
	// ErrValidation is returned for rejected input. Every validation failure in
	// this package also carries a *ValidationError naming the offending field, so
	// errors.Is(err, ErrValidation) and errors.As(err, &validation) are both true
	// for the same value.
	ErrValidation = errors.New("rooted: invalid input")
	// ErrUserNotFound is returned when the user a signal belongs to does not
	// exist.
	ErrUserNotFound = errors.New("rooted: user not found")
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
	return fmt.Sprintf("rooted: invalid %s: %s", e.Field, e.Message)
}

// Unwrap makes every ValidationError satisfy errors.Is(err, ErrValidation), so
// callers can test either the sentinel or the concrete type.
func (e *ValidationError) Unwrap() error { return ErrValidation }

// DurationBucket is how long a person has been connected to a place.
//
// The set is closed: it is a CHECK constraint in the 0005_rooted migration, and a
// value that is not one of the five is rejected before it reaches the store.
type DurationBucket string

// The supported duration buckets.
const (
	// DurationLifelong is a connection held for a lifetime.
	DurationLifelong DurationBucket = "lifelong"
	// DurationManyYears is a connection of many years.
	DurationManyYears DurationBucket = "many_years"
	// DurationSeveralYears is a connection of several years.
	DurationSeveralYears DurationBucket = "several_years"
	// DurationAFewYears is a connection of a few years.
	DurationAFewYears DurationBucket = "a_few_years"
	// DurationRecently is a recent connection.
	DurationRecently DurationBucket = "recently"
)

// DurationBuckets lists the supported buckets, in the order the UI offers them.
var DurationBuckets = []DurationBucket{
	DurationLifelong,
	DurationManyYears,
	DurationSeveralYears,
	DurationAFewYears,
	DurationRecently,
}

// Valid reports whether d is one of the supported buckets.
func (d DurationBucket) Valid() bool {
	switch d {
	case DurationLifelong, DurationManyYears, DurationSeveralYears, DurationAFewYears, DurationRecently:
		return true
	default:
		return false
	}
}

// Place limits. A place is city or region precision only: free text, but bounded
// so it cannot carry an address, and with no line breaks or tabs so it stays a
// single short line.
const (
	// MinPlaceLength is the shortest accepted place, after trimming.
	MinPlaceLength = 1
	// MaxPlaceLength is the longest accepted place, after trimming.
	MaxPlaceLength = 80
	// MaxPlaceCountryLength bounds the optional country name the geocoder
	// reported for a place.
	MaxPlaceCountryLength = 100
)

// Signal is a user's declared connection to a place.
//
// A signal is self-declared and never verified. IsPrimary marks the user's one
// active signal; the 0005_rooted migration enforces at most one primary signal per
// user through a partial unique index.
type Signal struct {
	// ID is the canonical UUID text assigned by PostgreSQL.
	ID string
	// UserID is the user the signal belongs to.
	UserID string
	// Place is the declared place, at city or region precision.
	Place string
	// Latitude and Longitude are the structured coordinate of the place, or nil.
	// They are set together or not at all (KNOT-ADR-034), so Rooted can later be
	// plotted on the map.
	Latitude  *float64
	Longitude *float64
	// PlaceCountry is the country name the geocoder reported, or nil.
	PlaceCountry *string
	// DurationBucket is how long the connection has been held.
	DurationBucket DurationBucket
	// IsPublic is false when the owner has hidden the signal from public read.
	// The owner can always read their own signals.
	IsPublic bool
	// IsPrimary marks the user's one active signal.
	IsPrimary bool
	// CreatedAt is the insertion time.
	CreatedAt time.Time
	// UpdatedAt is maintained by the database.
	UpdatedAt time.Time
}

// SetSignalInput is the input to SetSignal. It is a domain type, not an HTTP type,
// so the handler layer stays free of validation rules.
type SetSignalInput struct {
	// Place is required and must be 1-MaxPlaceLength characters after trimming.
	Place string
	// Latitude, Longitude, and PlaceCountry are optional structured place data.
	// Latitude and Longitude must be supplied together (KNOT-ADR-034).
	Latitude     *float64
	Longitude    *float64
	PlaceCountry string
	// DurationBucket is required and must be one of the five supported buckets.
	DurationBucket DurationBucket
	// IsPublic controls whether the signal is readable by anyone. Signals are
	// public by default; the flag is how an owner hides one.
	IsPublic bool
}

// RootedStore is the persistence contract for rooted signals. The service depends
// on this interface rather than on pgx, so the business rules can be tested
// without a database.
type RootedStore interface {
	// SetPrimary upserts the user's primary signal and returns the stored row,
	// including the id and timestamps PostgreSQL maintains. Setting a second
	// signal replaces the first rather than creating a second primary.
	SetPrimary(ctx context.Context, userID string, signal Signal) (Signal, error)

	// ListByUser returns every signal for a user, primary first. It returns an
	// empty slice, not an error, when the user has none.
	ListByUser(ctx context.Context, userID string) ([]Signal, error)

	// ListPublicByUser returns only the public signals for a user, primary first.
	ListPublicByUser(ctx context.Context, userID string) ([]Signal, error)

	// BatchPrimaryPublic returns the primary public signal for each of the given
	// user ids, keyed by user id, in a single query. A user with no primary public
	// signal is absent from the map.
	BatchPrimaryPublic(ctx context.Context, userIDs []string) (map[string]Signal, error)

	// UserExists reports whether a user row exists. It is how the public-read path
	// tells "no such user" from "a user with no signals".
	UserExists(ctx context.Context, userID string) (bool, error)
}

// isUUID reports whether s is a canonical 8-4-4-4-12 hexadecimal UUID string.
//
// It duplicates the unexported validator in internal/identity, internal/stories,
// internal/versions, and internal/conversations rather than exporting one: the
// alternative would widen a package's API for a helper that is three lines of
// character testing (KNOT-ADR-010).
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
