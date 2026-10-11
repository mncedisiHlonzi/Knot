package search

// Package search implements Knot's full-text search across the three content
// types that carry searchable text: stories, versions, and inquiries.
//
// Search is one read over three tables. A **story** result is the root version of
// a story (a story row carries no title or body of its own — KNOT-ADR-020), a
// **version** result is an adaptation, and an **inquiry** result is a question.
// The three are unified into one ranked list so a reader searching "Manguzi" sees
// the stories, the adaptations, and the questions about that place together,
// rather than being asked to pick a corpus first (KNOT-ADR-065).
//
// The package is layered, mirroring internal/stories and internal/inquiries:
//
//	search.go         domain types, limits, the store contract, errors
//	cursor.go         the opaque (rank, created_at, id) pagination cursor
//	service.go        the business rules (validation, normalisation)
//	postgres_store.go the PostgreSQL implementation of the store contract
//
// Nothing in this package knows about HTTP, JSON, or SQL types. User ids are
// plain strings holding canonical UUID text (KNOT-ADR-010).
//
// The block filter is a property of the query, not of this package: the store
// reads the viewer's hidden-author set from the request context exactly as every
// other content list does, so a blocked author's content never reaches the
// ranking (KNOT-ADR-064).

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
	ErrValidation = errors.New("search: invalid input")
)

// ValidationError describes a rejected field on a request. Handlers expose the
// Field and Message so clients can highlight the offending input.
type ValidationError struct {
	// Field is the request field that failed validation.
	Field string
	// Message explains the failure in a client-safe way.
	Message string
}

// Error implements the error interface.
func (e *ValidationError) Error() string {
	return fmt.Sprintf("search: invalid %s: %s", e.Field, e.Message)
}

// Unwrap makes every ValidationError satisfy errors.Is(err, ErrValidation), so
// callers can test either the sentinel or the concrete type.
func (e *ValidationError) Unwrap() error { return ErrValidation }

// Kind is the content type a result came from. It is also the value of the
// `type` query parameter, so a client can round-trip it verbatim.
type Kind string

const (
	// KindStory is a story, matched through its root version.
	KindStory Kind = "story"
	// KindVersion is an adaptation — a non-root version of a story.
	KindVersion Kind = "version"
	// KindInquiry is a question.
	KindInquiry Kind = "inquiry"
)

// Valid reports whether k is one of the three supported kinds. The empty value is
// not valid: "no kind filter" is expressed by leaving the field out, never by an
// empty string, so a typo can never silently widen a search.
func (k Kind) Valid() bool {
	switch k {
	case KindStory, KindVersion, KindInquiry:
		return true
	default:
		return false
	}
}

// Query and page limits. The service enforces them so the domain, not just the
// HTTP layer, refuses an unusable request.
const (
	// MinQueryLength is the shortest accepted search phrase, counted in runes. A
	// single character matches almost everything, so it is refused rather than
	// served; an empty phrase is allowed when a filter narrows the search.
	MinQueryLength = 2
	// DefaultLimit is the page size the API uses when none is requested.
	DefaultLimit = 20
	// MaxLimit is the largest page the API will serve.
	MaxLimit = 50
	// PreviewLength is how many characters of the body a result carries. A search
	// result is a pointer, not a reading surface: the client shows a couple of
	// lines and opens the entity for the rest.
	PreviewLength = 200
)

// Result is one item in a ranked search page.
//
// It is deliberately a flat projection over the three content types rather than a
// union of three domain types: the wire format is the same for all of them, and a
// client renders one card from one shape.
type Result struct {
	// Kind is which corpus the result came from.
	Kind Kind
	// ID is the story id for a story, the version id for a version, and the
	// inquiry id for an inquiry — the id a client navigates to.
	ID string
	// StoryID is the story a version belongs to, or nil for a story (whose ID is
	// already the story) and for an inquiry. It lets a client open a version
	// through its story.
	StoryID *string
	// Title is the entity's title.
	Title string
	// Preview is the beginning of the body, at most PreviewLength characters.
	Preview string
	// Language is the ISO 639-3 code the text is written in.
	Language string
	// Pillar is the story's pillar for a story or version, and nil for an inquiry,
	// which has no pillar.
	Pillar *string
	// Place is the story's approximate location or the inquiry's place, or nil when
	// none was recorded.
	Place *string
	// AuthorID is the author of the matched row: the story's author for a story,
	// the adapter for a version, the asker for an inquiry.
	AuthorID string
	// CreatedAt is the entity's creation time.
	CreatedAt time.Time
	// Rank is the relevance score the row was ordered by. It is carried so the
	// store can encode the pagination cursor from the last row; it is never part of
	// the wire response.
	Rank float64
}

// SearchInput is a search request as it arrives from the transport layer, before
// validation. Every filter is optional; the zero value is not a valid request
// because it asks for everything with no query and no filter.
type SearchInput struct {
	// Query is the phrase to match. It may be empty only when at least one filter
	// is set.
	Query string
	// Kind narrows the search to one content type. Empty means all three.
	Kind Kind
	// Pillar narrows to one pillar. Empty means no pillar filter. A pillar filter
	// excludes inquiries, which have no pillar.
	Pillar string
	// Language narrows to one ISO 639-3 code. Empty means no language filter.
	Language string
	// AuthorID narrows to one author. Empty means no author filter.
	AuthorID string
	// Place narrows to one place, matched exactly against the stored spelling.
	// Empty means no place filter.
	Place string
	// Cursor resumes a previous page. Empty starts at the first page.
	Cursor string
	// Limit is how many results to return. The service resolves it to
	// DefaultLimit when it is zero and rejects anything above MaxLimit.
	Limit int
}

// StoreQuery is a validated, normalised search the store executes.
//
// It exists so the store never re-derives a rule the service already applied: by
// the time a StoreQuery exists, the query length, the enums, the UUID, and the
// cursor have all been checked.
type StoreQuery struct {
	// Query is the trimmed search phrase, or "" for a filter-only search.
	Query string
	// Kind is the single content type to search, or "" for all three.
	Kind Kind
	// Pillar is the pillar to filter on, or "".
	Pillar string
	// Language is the ISO 639-3 code to filter on, or "".
	Language string
	// AuthorID is the canonical UUID text of the author to filter on, or "".
	AuthorID string
	// Place is the exact place to filter on, or "".
	Place string
	// Cursor is the decoded position to resume from, or nil for the first page.
	Cursor *Cursor
	// Limit is the page size, already bounded.
	Limit int
}

// SearchStore is the persistence contract for search. The service depends on this
// interface rather than on pgx, so the business rules can be tested without a
// database.
type SearchStore interface {
	// Search returns one page of ranked results for query, plus the cursor that
	// resumes after it (nil when this page is the last one). The result is never
	// nil.
	Search(ctx context.Context, query StoreQuery) ([]Result, *Cursor, error)
}
