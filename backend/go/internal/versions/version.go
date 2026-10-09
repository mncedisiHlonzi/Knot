// Package versions implements the Knot Tell My People domain: the human
// adaptations of a story across languages, which together form a Language Tree.
//
// The package is deliberately layered, mirroring internal/stories:
//
//	version.go        domain types, limits, errors, and the store contract
//	service.go        business rules (CreateAdaptation, GetVersion, GetTree)
//	postgres_store.go the PostgreSQL implementation of the store contract
//
// A story's versions form a tree expressed as an adjacency list: every version
// carries a parent_version_id, and the root version of a story has none. The
// tree is assembled from a flat list of versions elsewhere (the client today);
// this package returns the flat list with its parent pointers intact.
//
// Nothing in this package knows about HTTP, JSON, or SQL types. User, story, and
// version ids are plain strings holding canonical UUID text, which is the
// convention internal/identity and internal/stories already established
// (KNOT-ADR-010).
package versions

import (
	"context"
	"errors"
	"fmt"
	"time"
)

// Sentinel errors. Handlers map these onto HTTP status codes; they are never
// returned to clients verbatim.
var (
	// ErrNotFound is returned when no version matches the requested id, or when
	// the story a version list was requested for does not exist.
	ErrNotFound = errors.New("versions: version not found")
	// ErrValidation is returned for rejected input. Every validation failure in
	// this package also carries a *ValidationError naming the offending field,
	// so errors.Is(err, ErrValidation) and errors.As(err, &validation) are both
	// true for the same value.
	ErrValidation = errors.New("versions: invalid input")
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
	return fmt.Sprintf("versions: invalid %s: %s", e.Field, e.Message)
}

// Unwrap makes every ValidationError satisfy errors.Is(err, ErrValidation), so
// callers can test either the sentinel or the concrete type.
func (e *ValidationError) Unwrap() error { return ErrValidation }

// Field limits. A version's title, body, and language follow the same rules the
// stories domain applied when this content lived on the story itself, so the
// move into versions is invisible to authors. Lengths are counted in runes so a
// multi-byte script is not penalised.
const (
	// minTitleLen is the shortest accepted title.
	minTitleLen = 1
	// MaxTitleLen is the longest accepted title.
	MaxTitleLen = 200
	// MaxBodyLen is the longest accepted body.
	MaxBodyLen = 10000
	// MaxAdaptationNoteLength bounds the optional note an adapter leaves.
	MaxAdaptationNoteLength = 1000
	// minLanguageLen is the shortest accepted language tag.
	minLanguageLen = 2
	// maxLanguageLen is the longest accepted language tag.
	maxLanguageLen = 8
)

// StoryVersion is one written version of a story.
//
// ParentVersionID is empty for the root version of a story and set for every
// adaptation. AdaptationNote is empty when the adapter left none.
type StoryVersion struct {
	// ID is the canonical UUID text assigned by PostgreSQL.
	ID string
	// StoryID is the story this version belongs to.
	StoryID string
	// ParentVersionID is the version this one adapts, or "" for the root.
	ParentVersionID string
	// AuthorID is the user who wrote this version. For the root it is the
	// original author of the story; for an adaptation it is the adapter.
	AuthorID string
	// Language is the tag of the language this version is written in.
	Language string
	// Title is the version's headline.
	Title string
	// Body is the version itself.
	Body string
	// AdaptationNote is the optional explanation an adapter left behind.
	AdaptationNote string
	// CreatedAt is the insertion time.
	CreatedAt time.Time
	// UpdatedAt is maintained by the database.
	UpdatedAt time.Time
}

// IsRoot reports whether this version is the root of its story.
func (v StoryVersion) IsRoot() bool { return v.ParentVersionID == "" }

// CreateAdaptationInput is the input to CreateAdaptation. It is a domain type,
// not an HTTP type, so the handler layer stays free of validation rules.
type CreateAdaptationInput struct {
	// StoryID is required and must be canonical UUID text. It comes from the
	// request path.
	StoryID string
	// ParentVersionID is required and must belong to StoryID.
	ParentVersionID string
	// AuthorID is required and must be canonical UUID text. In the running
	// server it comes from the authenticated request, never from the body.
	AuthorID string
	// Language is required and must be a 2-8 character tag.
	Language string
	// Title is required and must be 1-MaxTitleLen characters.
	Title string
	// Body is required and must be 1-MaxBodyLen characters.
	Body string
	// AdaptationNote is optional, up to MaxAdaptationNoteLength characters.
	AdaptationNote string
}

// VersionStore is the persistence contract for story versions. The service
// depends on this interface rather than on pgx, so the business rules can be
// tested without a database.
type VersionStore interface {
	// CreateVersion inserts version and returns the stored row.
	CreateVersion(ctx context.Context, version StoryVersion) (StoryVersion, error)
	// GetVersion returns the version with the given id, or ErrNotFound.
	GetVersion(ctx context.Context, id string) (StoryVersion, error)
	// ListByStory returns every version of one story, ordered oldest first. It
	// returns ErrNotFound when no story has the given id, so a caller can tell
	// "no such story" from "a story with no versions".
	ListByStory(ctx context.Context, storyID string) ([]StoryVersion, error)
}

// Notifier records that a user adapted content. It is satisfied by the
// notifications service, but the versions package depends only on this one
// method, so it never imports the notifications package (KNOT-ADR-040).
//
// A call is non-critical: an implementation records the notification and reports
// a failure, and the caller ignores that failure rather than failing the
// adaptation that triggered it (KNOT-ADR-038).
type Notifier interface {
	// NotifyVersionCreated records that actorID adapted the version identified by
	// versionID, which was authored by recipientID.
	NotifyVersionCreated(ctx context.Context, recipientID, actorID, versionID string) error
}

// Service holds the Tell My People business rules.
//
// It depends on the VersionStore abstraction and a Notifier, and knows nothing
// about HTTP, JSON, or SQL.
type Service struct {
	store    VersionStore
	notifier Notifier
}

// NewService wires a store and a notifier into the versions domain.
func NewService(store VersionStore, notifier Notifier) (*Service, error) {
	if store == nil {
		return nil, fmt.Errorf("versions: service requires a version store")
	}
	if notifier == nil {
		return nil, fmt.Errorf("versions: service requires a notifier")
	}
	return &Service{store: store, notifier: notifier}, nil
}

// isUUID reports whether s is a canonical 8-4-4-4-12 hexadecimal UUID string.
//
// It duplicates the unexported validator in internal/identity and
// internal/stories rather than exporting one: the alternative would widen a
// package's API for a helper that is three lines of character testing
// (KNOT-ADR-010).
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
