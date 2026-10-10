// Package inquiries implements the Knot Curious Inquiries domain: a question
// about a place, and the public answers to it.
//
// An inquiry is how someone asks the people rooted in a place something only they
// would know. It is deliberately unlike a story: a story is authored content about
// a place, whereas an inquiry is a question asked *of* the people there. That is
// why asking routes to the first Rooted users of the named place rather than into
// a feed (KNOT-ADR-056).
//
// Three product rules shape the domain and are enforced here rather than in the
// HTTP layer (KNOT-ADR-055):
//
//   - every inquiry and every answer is public and attributed; there is no
//     anonymous variant, and a response always names its author;
//   - an inquiry stays open forever: there is no accepted answer, no closing, and
//     no ranking, because knowledge of a place is plural;
//   - nothing is editable or deletable at MVP, so the domain exposes no update or
//     delete operation at all.
//
// Authoring an inquiry is not transactional with routing it: the inquiry is
// committed first, and the notifications that announce it are written afterwards
// and may fail without failing the ask, exactly as comments and versions already
// behave (KNOT-ADR-038).
//
// The package is layered, mirroring internal/stories and internal/conversations:
//
//	inquiry.go        domain types, limits, the store contract, errors
//	cursor.go         the opaque (created_at, id) pagination cursor
//	service.go        the business rules (validation, routing, notification)
//	postgres_store.go the PostgreSQL implementation of the store contract
//
// Nothing in this package knows about HTTP, JSON, or SQL types. User ids are plain
// strings holding canonical UUID text, the convention internal/identity and
// internal/stories established (KNOT-ADR-010).
package inquiries

import (
	"context"
	"errors"
	"fmt"
	"time"
)

// Sentinel errors. Handlers map these onto HTTP status codes; they are never
// returned to clients verbatim.
var (
	// ErrNotFound is returned when an inquiry or an answer does not exist.
	ErrNotFound = errors.New("inquiries: not found")
	// ErrUserNotFound is returned when the author an inquiry or answer belongs to
	// does not exist.
	ErrUserNotFound = errors.New("inquiries: user not found")
	// ErrValidation is returned for rejected input. Every validation failure in
	// this package also carries a *ValidationError naming the offending field, so
	// errors.Is(err, ErrValidation) and errors.As(err, &validation) are both true
	// for the same value.
	ErrValidation = errors.New("inquiries: invalid input")
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
	return fmt.Sprintf("inquiries: invalid %s: %s", e.Field, e.Message)
}

// Unwrap makes every ValidationError satisfy errors.Is(err, ErrValidation), so
// callers can test either the sentinel or the concrete type.
func (e *ValidationError) Unwrap() error { return ErrValidation }

// Input limits. Lengths are counted in runes, so a multi-byte script is not
// penalised; every one of them is enforced by the service before anything reaches
// the store.
const (
	// MinTitleLength is the shortest accepted title, after trimming.
	MinTitleLength = 1
	// MaxTitleLength is the longest accepted title, after trimming.
	MaxTitleLength = 200
	// MinBodyLength is the shortest accepted body, after trimming.
	MinBodyLength = 1
	// MaxBodyLength is the longest accepted question body, after trimming.
	MaxBodyLength = 5000
	// MaxAnswerBodyLength is the longest accepted answer, after trimming. It equals
	// MaxBodyLength: an answer is a full reply, not a comment on one.
	MaxAnswerBodyLength = 5000
	// MinPlaceLength is the shortest accepted place, after trimming. A place is
	// optional; when it is given it must be a real place name.
	MinPlaceLength = 1
	// MaxPlaceLength is the longest accepted place, after trimming. A place is
	// city or region precision free text, bounded so it cannot carry an address.
	MaxPlaceLength = 100
	// MaxPlaceCountryLength bounds the optional country name the geocoder reported
	// for a place.
	MaxPlaceCountryLength = 100
)

// Page sizes. The service enforces them so the domain, not just the HTTP layer,
// refuses an unbounded query.
const (
	// DefaultListLimit is the page size the API uses when none is requested.
	DefaultListLimit = 20
	// MaxListLimit is the largest inquiry page the API will serve.
	MaxListLimit = 50
	// DefaultAnswerListLimit is the answer page size the API uses when none is
	// requested. It is larger than DefaultListLimit because an answer thread is
	// read as a whole conversation and is expected to be short.
	DefaultAnswerListLimit = 50
	// MaxAnswerListLimit is the largest answer page the API will serve.
	MaxAnswerListLimit = 100
)

// NearbyRecipientLimit is how many of a place's Rooted users an inquiry is routed
// to. It is deliberately small: routing exists to give a new question its first
// readers, not to broadcast, and everyone else reaches it through the public list
// (KNOT-ADR-056).
const NearbyRecipientLimit = 5

// Inquiry is a question someone asked about a place.
type Inquiry struct {
	// ID is the canonical UUID text assigned by PostgreSQL.
	ID string
	// AuthorID is the user who asked.
	AuthorID string
	// Title is the question in one line, 1-MaxTitleLength characters.
	Title string
	// Body is the question's full text, 1-MaxBodyLength characters.
	Body string
	// Language is the ISO 639-3 code the question is written in.
	Language string
	// Place is the place the question is about, or nil when it is not about a
	// particular place. A question with a place is the one that routes to that
	// place's Rooted users.
	Place *string
	// PlaceCountry is the country name the geocoder reported, or nil.
	PlaceCountry *string
	// Latitude and Longitude are the structured coordinate of the place, or nil.
	// They are set together or not at all (KNOT-ADR-034), so the question can be
	// plotted on the map.
	Latitude  *float64
	Longitude *float64
	// AnswerCount is how many public answers the inquiry holds. It is maintained
	// by the store inside the answer-insert transaction, so it is never stale for a
	// committed answer.
	AnswerCount int
	// CreatedAt is the insertion time. It is the list's primary sort key.
	CreatedAt time.Time
	// UpdatedAt is the last time the row itself changed.
	UpdatedAt time.Time
}

// Answer is one public reply to an inquiry.
type Answer struct {
	// ID is the canonical UUID text assigned by PostgreSQL.
	ID string
	// InquiryID is the inquiry being answered.
	InquiryID string
	// AuthorID is the user who answered.
	AuthorID string
	// Language is the ISO 639-3 code the answer is written in.
	Language string
	// Body is the answer's text, 1-MaxAnswerBodyLength characters.
	Body string
	// CreatedAt is the insertion time. It is the thread's primary sort key.
	CreatedAt time.Time
	// UpdatedAt is the last time the row itself changed.
	UpdatedAt time.Time
}

// CreateInquiryInput is the input to CreateInquiry. It is a domain type, not an
// HTTP type, so the handler layer stays free of validation rules.
type CreateInquiryInput struct {
	// Title is required and must be 1-MaxTitleLength characters after trimming.
	Title string
	// Body is required and must be 1-MaxBodyLength characters after trimming.
	Body string
	// Language is required and must be a valid ISO 639-3 code.
	Language string
	// Place is optional. When supplied it must be 1-MaxPlaceLength characters
	// after trimming, on one line, and it is what routes the inquiry.
	Place string
	// Latitude, Longitude, and PlaceCountry are optional structured place data.
	// Latitude and Longitude must be supplied together (KNOT-ADR-034).
	Latitude     *float64
	Longitude    *float64
	PlaceCountry string
}

// CreateAnswerInput is the input to CreateAnswer.
type CreateAnswerInput struct {
	// Body is required and must be 1-MaxAnswerBodyLength characters after
	// trimming.
	Body string
	// Language is required and must be a valid ISO 639-3 code.
	Language string
}

// InquiryStore is the persistence contract for inquiries and their answers. The
// service depends on this interface rather than on pgx, so the business rules can
// be tested without a database.
type InquiryStore interface {
	// CreateInquiry stores an inquiry and returns it with the id and timestamps
	// PostgreSQL assigned.
	CreateInquiry(ctx context.Context, inquiry Inquiry) (Inquiry, error)

	// GetInquiry returns one inquiry, or ErrNotFound when it does not exist.
	GetInquiry(ctx context.Context, id string) (Inquiry, error)

	// ListInquiries returns at most limit inquiries, newest first, starting after
	// cursor (nil starts at the newest). A non-empty place restricts the page to
	// that exact place. The returned cursor resumes after the page, or is nil when
	// the page is the last one.
	ListInquiries(ctx context.Context, cursor *Cursor, limit int, place string) ([]Inquiry, *Cursor, error)

	// CreateAnswer appends an answer to an inquiry and increments the inquiry's
	// answer count, in one transaction. It returns the stored answer and the id of
	// the inquiry's author, which the service needs to notify the asker without a
	// second lookup. It returns ErrNotFound when the inquiry does not exist and
	// ErrUserNotFound when the answering author does not.
	CreateAnswer(ctx context.Context, answer Answer) (Answer, string, error)

	// ListAnswers returns at most limit answers to an inquiry, oldest first,
	// starting after cursor. A malformed inquiry id yields an empty slice; the
	// inquiry's existence is the service's concern.
	ListAnswers(ctx context.Context, inquiryID string, cursor *Cursor, limit int) ([]Answer, *Cursor, error)

	// GetAnswer returns one answer, or ErrNotFound when it does not exist.
	GetAnswer(ctx context.Context, id string) (Answer, error)
}

// isUUID reports whether s is a canonical 8-4-4-4-12 hexadecimal UUID string.
//
// It duplicates the unexported validator in internal/identity, internal/stories,
// internal/versions, internal/conversations, and internal/rooted rather than
// exporting one: the alternative would widen a package's API for a helper that is
// three lines of character testing (KNOT-ADR-010).
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
	return (b >= '0' && b <= '9') || (b >= 'a' && b <= 'f') || (b >= 'A' && b <= 'F')
}
