// Package conversations implements the Knot conversation layer: comments on a
// story version, and the bridges that connect a comment in one language to a
// comment in another.
//
// The package is deliberately layered, mirroring internal/stories and
// internal/versions:
//
//	comment.go        Comment, CommentStore, package errors and limits
//	bridge.go         Bridge, BridgeStore
//	cursor.go         opaque pagination cursors for a comment thread
//	service.go        business rules (CreateComment, ListComments, CreateBridge, …)
//	postgres_store.go the PostgreSQL implementations of the store contracts
//
// A comment belongs to exactly one story version. A bridge is a first-class
// object that references a source comment and a target comment: creating one
// writes a new target comment on the same version as the source, in the target
// language, plus a bridge row joining the two. The source conversation is never
// modified. Comment threading (replies) is deliberately not part of this model;
// replies happen only by bridging (see KNOT-ADR-014).
//
// Nothing in this package knows about HTTP, JSON, or SQL types. User, version,
// comment, and bridge ids are plain strings holding canonical UUID text
// (KNOT-ADR-010).
package conversations

import (
	"context"
	"errors"
	"fmt"
	"time"
)

// Sentinel errors. Handlers map these onto HTTP status codes; they are never
// returned to clients verbatim.
var (
	// ErrNotFound is returned when no row matches the requested id, or when the
	// version, comment, or story a list was requested for does not exist.
	ErrNotFound = errors.New("conversations: not found")
	// ErrValidation is returned for rejected input. Every validation failure in
	// this package also carries a *ValidationError naming the offending field,
	// so errors.Is(err, ErrValidation) and errors.As(err, &validation) are both
	// true for the same value.
	ErrValidation = errors.New("conversations: invalid input")
	// ErrAlreadyBridged is returned when the constraints refuse a second bridge
	// of the same comment into the same language. The service turns it into a
	// field validation error so the client learns which field to change.
	ErrAlreadyBridged = errors.New("conversations: comment already bridged into that language")
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
	return fmt.Sprintf("conversations: invalid %s: %s", e.Field, e.Message)
}

// Unwrap makes every ValidationError satisfy errors.Is(err, ErrValidation), so
// callers can test either the sentinel or the concrete type.
func (e *ValidationError) Unwrap() error { return ErrValidation }

// Field limits. A comment is short by design: it is a reply in a conversation,
// not another story. Lengths are counted in runes so a multi-byte script is not
// penalised.
const (
	// minBodyLen is the shortest accepted comment body.
	minBodyLen = 1
	// MaxBodyLen is the longest accepted comment body.
	MaxBodyLen = 5000
	// MaxAdaptationNoteLength bounds the optional note a bridge carries.
	MaxAdaptationNoteLength = 1000
	// minLanguageLen is the shortest accepted language tag.
	minLanguageLen = 2
	// maxLanguageLen is the longest accepted language tag.
	maxLanguageLen = 8
)

// Thread page sizes. The service enforces them so the domain, not just the HTTP
// layer, refuses an unbounded query.
const (
	// DefaultListLimit is the page size the API uses when none is requested.
	DefaultListLimit = 20
	// MaxListLimit is the largest page the API will serve.
	MaxListLimit = 50
)

// Comment is one comment on a story version.
type Comment struct {
	// ID is the canonical UUID text assigned by PostgreSQL.
	ID string
	// VersionID is the story version the comment is attached to.
	VersionID string
	// AuthorID is the user who wrote the comment.
	AuthorID string
	// Language is the tag of the language the comment is written in.
	Language string
	// Body is the comment itself, stored verbatim.
	Body string
	// CreatedAt is the insertion time. It is the thread's primary sort key.
	CreatedAt time.Time
	// UpdatedAt is maintained by the database.
	UpdatedAt time.Time
}

// CreateCommentInput is the input to CreateComment. It is a domain type, not an
// HTTP type, so the handler layer stays free of validation rules.
type CreateCommentInput struct {
	// VersionID is required and must be canonical UUID text. It comes from the
	// request path.
	VersionID string
	// AuthorID is required and must be canonical UUID text. In the running
	// server it comes from the authenticated request, never from the body.
	AuthorID string
	// Language is required and must be a 2-8 character tag.
	Language string
	// Body is required and must be 1-MaxBodyLen characters.
	Body string
}

// CommentStore is the persistence contract for comments. The service depends on
// this interface rather than on pgx, so the business rules can be tested without
// a database.
type CommentStore interface {
	// CreateComment inserts comment and returns the stored row. It returns
	// ErrNotFound when the version the comment names does not exist.
	CreateComment(ctx context.Context, comment Comment) (Comment, error)
	// GetComment returns the comment with the given id, or ErrNotFound.
	GetComment(ctx context.Context, id string) (Comment, error)
	// ListComments returns at most limit comments of one version, newest first,
	// starting after cursor (nil starts at the newest). The returned cursor
	// resumes after the page, or is nil when the page is the last one. It
	// returns ErrNotFound when the version does not exist.
	ListComments(ctx context.Context, versionID string, cursor *Cursor, limit int) ([]Comment, *Cursor, error)
}

// isUUID reports whether s is a canonical 8-4-4-4-12 hexadecimal UUID string.
//
// It duplicates the unexported validator in internal/identity, internal/stories,
// and internal/versions rather than exporting one: the alternative would widen a
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
