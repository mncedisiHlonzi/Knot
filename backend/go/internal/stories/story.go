// Package stories implements the Knot stories domain: the cultural artefacts
// users publish, and the feed that lists them.
//
// The package is deliberately layered, mirroring internal/identity:
//
//	story.go          domain types, limits, errors, and the store contract
//	cursor.go         opaque pagination cursors for the feed
//	service.go        business rules (CreateStory, GetStory, ListStories)
//	postgres_store.go the PostgreSQL implementation of the store contract
//
// Nothing in this package knows about HTTP, JSON, or SQL types. User ids are
// plain strings holding canonical UUID text, which is the convention
// internal/identity already established for every id it handles. That keeps the
// backend's dependency set at the three direct modules recorded in ADR-007.
package stories

import (
	"context"
	"errors"
	"fmt"
	"time"
)

// Sentinel errors. Handlers map these onto HTTP status codes; they are never
// returned to clients verbatim.
var (
	// ErrNotFound is returned when no story matches the requested id.
	ErrNotFound = errors.New("stories: story not found")
	// ErrValidation is returned for rejected input. Every validation failure in
	// this package also carries a *ValidationError naming the offending field,
	// so errors.Is(err, ErrValidation) and errors.As(err, &validation) are both
	// true for the same value.
	ErrValidation = errors.New("stories: invalid input")
)

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
	return fmt.Sprintf("stories: invalid %s: %s", e.Field, e.Message)
}

// Unwrap makes every ValidationError satisfy errors.Is(err, ErrValidation), so
// callers can test either the sentinel or the concrete type.
func (e *ValidationError) Unwrap() error { return ErrValidation }

// Pillar is the storytelling lens a story is filed under. The two values are a
// product decision, so they are a closed set here and a CHECK constraint in the
// 0002_stories migration.
type Pillar string

// The supported pillars.
const (
	// PillarWonder is a story told because it is surprising or delightful.
	PillarWonder Pillar = "wonder"
	// PillarHeritage is a story told because it preserves something inherited.
	PillarHeritage Pillar = "heritage"
)

// Valid reports whether p is one of the supported pillars.
func (p Pillar) Valid() bool {
	switch p {
	case PillarWonder, PillarHeritage:
		return true
	default:
		return false
	}
}

// Field limits. They are intentionally modest: this is an MVP for real people,
// not a place to accept unbounded input. Lengths are counted in runes so a
// multi-byte script is not penalised.
const (
	// minTitleLen is the shortest accepted title.
	minTitleLen = 1
	// MaxTitleLen is the longest accepted title.
	MaxTitleLen = 200
	// MaxBodyLen is the longest accepted body.
	MaxBodyLen = 10000
	// MaxLocationLength bounds the optional approximate location.
	MaxLocationLength = 100
	// MaxPlaceCountryLength bounds the optional country name the geocoder
	// reported for a place. It is generous for a country name.
	MaxPlaceCountryLength = 100
	// MaxMediaURLs bounds how many media links one story may carry.
	MaxMediaURLs = 20
	// maxMediaURLLength bounds a single media link.
	maxMediaURLLength = 2048
)

// Feed page sizes. The service enforces them so the domain, not just the HTTP
// layer, refuses an unbounded query.
const (
	// DefaultListLimit is the page size the API uses when none is requested.
	DefaultListLimit = 20
	// MaxListLimit is the largest page the API will serve.
	MaxListLimit = 50
)

// Story is a published story.
//
// A story's written content — its language, title, and body — lives in its root
// version (see KNOT-ADR-012). The fields below are resolved from that root
// version, so a Story always carries the content a reader sees, while the
// version tree holds every adaptation of it.
type Story struct {
	// ID is the canonical UUID text assigned by PostgreSQL.
	ID string
	// AuthorID is the id of the user who published the story: the original
	// author, who is also the author of the root version.
	AuthorID string
	// RootVersionID is the id of the story's root version, the version every
	// adaptation descends from.
	RootVersionID string
	// Pillar is the storytelling lens the story was filed under.
	Pillar Pillar
	// Language is the root version's language tag.
	Language string
	// Title is the root version's headline.
	Title string
	// Body is the root version's body.
	Body string
	// ApproximateLocation is an optional, deliberately coarse place name.
	ApproximateLocation string
	// Latitude and Longitude are the structured coordinate of the place, or nil
	// when the author did not attach one. They are set together or not at all
	// (KNOT-ADR-034): the pair is what the Discovery Map plots, so a lone
	// latitude is meaningless.
	Latitude  *float64
	Longitude *float64
	// PlaceCountry is the country name the geocoder reported for the place, or
	// nil. It is a display hint, not a stable code.
	PlaceCountry *string
	// MediaURLs is the attached media, never nil once stored.
	MediaURLs []string
	// Sensitive marks a story that should not be surfaced without care.
	Sensitive bool
	// CreatedAt is the insertion time. It is the primary feed sort key.
	CreatedAt time.Time
	// UpdatedAt is maintained by the database.
	UpdatedAt time.Time
}

// CreateStoryInput is the input to CreateStory. It is a domain type, not an HTTP
// type, so the handler layer stays free of validation rules.
//
// The Language, Title, and Body become the story's root version; the remaining
// fields stay on the story row itself.
type CreateStoryInput struct {
	// AuthorID is required and must be canonical UUID text. In the running
	// server it comes from the authenticated request, never from the body.
	AuthorID string
	// Pillar is required and must be one of the supported pillars.
	Pillar Pillar
	// Latitude, Longitude, and PlaceCountry are optional structured place data.
	// Latitude and Longitude must be supplied together (KNOT-ADR-034).
	Latitude     *float64
	Longitude    *float64
	PlaceCountry string
	// Language is required and must be a 2-8 character tag.
	Language string
	// Title is required and must be 1-MaxTitleLen characters.
	Title string
	// Body is required and must be 1-MaxBodyLen characters.
	Body string
	// ApproximateLocation is optional.
	ApproximateLocation string
	// MediaURLs is optional; it may be empty but never nil once stored.
	MediaURLs []string
	// Sensitive is optional and defaults to false.
	Sensitive bool
}

// StoryStore is the persistence contract for stories. The service depends on
// this interface rather than on pgx, so the business rules can be tested without
// a database.
type StoryStore interface {
	// CreateStory inserts the story together with its root version and returns
	// the stored story with its root version content resolved. Both rows are
	// written in one transaction: a failure while writing the root version must
	// leave no story behind.
	CreateStory(ctx context.Context, story Story) (Story, error)
	// GetStory returns the story with the given id, or ErrNotFound.
	GetStory(ctx context.Context, id string) (Story, error)
	// ListStories returns at most limit stories newer-to-older, starting after
	// cursor. A nil cursor starts at the newest story. The returned cursor is
	// the position to resume from, or nil when the page is the last one.
	ListStories(ctx context.Context, cursor *Cursor, limit int) ([]Story, *Cursor, error)
}

// Service holds the stories business rules.
//
// It depends on the StoryStore abstraction and knows nothing about HTTP, JSON,
// or SQL.
type Service struct {
	store StoryStore
}

// NewService wires a store into the stories domain.
func NewService(store StoryStore) (*Service, error) {
	if store == nil {
		return nil, fmt.Errorf("stories: service requires a story store")
	}
	return &Service{store: store}, nil
}
