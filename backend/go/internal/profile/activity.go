// Package profile implements the Knot profile wall: one user's public activity
// stream, merged from the six things a person can author — stories, versions
// (adaptations), comments, bridges, inquiries, and answers to inquiries — into one
// chronological, cursor-paginated page.
//
// It is a read model over the content domains rather than a domain of its own: it
// stores nothing and writes nothing. It imports identity only to resolve the
// wall's owner, and it never exposes private state — the wall is public
// (KNOT-ADR-042).
//
// Nothing in this package knows about HTTP, JSON wire framing, or SQL types. Ids
// are plain strings holding canonical UUID text (KNOT-ADR-010).
//
// The layered files mirror internal/stories and internal/conversations:
//
//	activity.go       the Activity model, its payload, limits and errors
//	cursor.go         the opaque (created_at, id) pagination cursor
//	store.go          the ActivityStore contract
//	service.go        GetProfile: resolve the owner, then one page of activities
//	postgres_store.go the PostgreSQL implementation, a UNION ALL over four tables
package profile

import (
	"context"
	"errors"
	"fmt"
	"time"

	"github.com/knot/backend/internal/identity"
)

// Sentinel errors. Handlers map these onto HTTP status codes; they are never
// returned to clients verbatim.
var (
	// ErrNotFound is returned when the profile's owner does not exist.
	ErrNotFound = errors.New("profile: not found")
	// ErrValidation is returned for rejected input. Every validation failure in
	// this package also carries a *ValidationError naming the offending field, so
	// errors.Is(err, ErrValidation) and errors.As(err, &validation) are both true
	// for the same value.
	ErrValidation = errors.New("profile: invalid input")
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
	return fmt.Sprintf("profile: invalid %s: %s", e.Field, e.Message)
}

// Unwrap makes every ValidationError satisfy errors.Is(err, ErrValidation), so
// callers can test either the sentinel or the concrete type.
func (e *ValidationError) Unwrap() error { return ErrValidation }

// Page sizes. The service enforces them so the domain, not just the HTTP layer,
// refuses an unbounded query.
const (
	// DefaultListLimit is the page size the API uses when none is requested.
	DefaultListLimit = 20
	// MaxListLimit is the largest page the API will serve.
	MaxListLimit = 50
	// MaxBodyPreviewChars is the longest comment preview an activity carries. It is
	// enforced in SQL (left(body, n)) so a full comment body never travels to a
	// client through the wall.
	MaxBodyPreviewChars = 200
)

// Kind is the type of one activity on a wall. The set is closed: the store only
// produces these six values.
type Kind string

// The six activity kinds.
const (
	// KindStory is a story the user published (its root version).
	KindStory Kind = "story"
	// KindVersion is an adaptation the user wrote on someone else's story.
	KindVersion Kind = "version"
	// KindComment is a comment the user wrote on a version.
	KindComment Kind = "comment"
	// KindBridge is a bridge the user made from another person's comment.
	KindBridge Kind = "bridge"
	// KindInquiry is a question the user asked about a place (KNOT-ADR-057).
	KindInquiry Kind = "inquiry"
	// KindInquiryAnswer is an answer the user gave to someone else's question
	// (KNOT-ADR-057).
	KindInquiryAnswer Kind = "inquiry_answer"
)

// Payload is the kind-specific detail of an activity. Only the fields that belong
// to the activity's Kind are populated; the JSON tags carry `omitempty` so the
// wire form is exactly the subset a client needs for that kind:
//
//	story          { title, pillar, language }
//	version        { story_id, story_title, language }
//	comment        { version_id, story_id, body_preview }
//	bridge         { source_comment_id, version_id, target_language }
//	inquiry        { title, place }
//	inquiry_answer { inquiry_id, inquiry_title, body_preview }
//
// The payload is context, not authorship: everything in it is already public on
// the entity it names (KNOT-ADR-042).
type Payload struct {
	// Title is the story's title, or the inquiry's title. Set for KindStory and
	// KindInquiry.
	Title string `json:"title,omitempty"`
	// Pillar is the story's pillar (wonder or heritage). Set for KindStory.
	Pillar string `json:"pillar,omitempty"`
	// Language is the activity's language tag. Set for KindStory (the root
	// version's) and KindVersion (the adaptation's).
	Language string `json:"language,omitempty"`

	// StoryID is the story the activity belongs to. Set for KindVersion and
	// KindComment.
	StoryID string `json:"story_id,omitempty"`
	// StoryTitle is the title of the story's root version, so an adaptation can
	// name the story it belongs to. Set for KindVersion.
	StoryTitle string `json:"story_title,omitempty"`

	// VersionID is the version the activity points at. Set for KindComment (the
	// commented version) and KindBridge (the source comment's version).
	VersionID string `json:"version_id,omitempty"`
	// BodyPreview is the first MaxBodyPreviewChars characters of a comment or an
	// answer. Set for KindComment and KindInquiryAnswer.
	BodyPreview string `json:"body_preview,omitempty"`

	// SourceCommentID is the comment a bridge was made from. Set for KindBridge.
	SourceCommentID string `json:"source_comment_id,omitempty"`
	// TargetLanguage is the language a bridge was made into. Set for KindBridge.
	TargetLanguage string `json:"target_language,omitempty"`

	// Place is the place an inquiry is about, at the precision the asker gave. Set
	// for KindInquiry, and empty for an inquiry that names no place.
	//
	// A JSON null from the database (a place-less inquiry) unmarshals to the empty
	// string, which `omitempty` then drops: an inquiry with no place carries no
	// place field at all, rather than an explicit null.
	Place string `json:"place,omitempty"`
	// InquiryID is the inquiry an answer belongs to. Set for KindInquiryAnswer.
	InquiryID string `json:"inquiry_id,omitempty"`
	// InquiryTitle is the title of the inquiry an answer belongs to, so an answer
	// card can name the question it answers. Set for KindInquiryAnswer.
	InquiryTitle string `json:"inquiry_title,omitempty"`
}

// Activity is one entry on a profile wall: what the user did, when, and the
// context needed to open it.
type Activity struct {
	// Kind is what the user did.
	Kind Kind
	// ID is the id of the thing itself — a story, version, comment, bridge,
	// inquiry, or inquiry answer.
	ID string
	// CreatedAt is when it was authored. It is the wall's primary sort key.
	CreatedAt time.Time
	// Payload is the kind-specific context.
	Payload Payload
}

// Profile is one user's wall: the public identity header plus one page of
// activities. The activities page may be empty; the user never is (a missing
// owner is ErrNotFound, not an empty profile).
type Profile struct {
	// User is the wall's owner, resolved from identity.
	User *identity.User
	// Activities is one page of the owner's activity, newest first. It is never
	// nil, so a client renders an empty state rather than a null.
	Activities []Activity
}

// UserLookup resolves a profile owner's account.
//
// It is deliberately narrower than the whole identity service: the profile only
// needs to name its owner, not to register, log in, or change an account.
type UserLookup interface {
	// UserByID returns the account with the given id, or identity.ErrUserNotFound.
	UserByID(ctx context.Context, id string) (*identity.User, error)
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
