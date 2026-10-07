package conversations

import (
	"context"
	"time"
)

// Bridge is the first-class object that connects a comment in one language to a
// comment in another.
//
// A bridge names its source comment and its target comment. The target comment is
// a real comment on the story's version in TargetLanguage — a different version
// of the same story, so the bridge joins two conversations rather than adding to
// one. The source comment is left untouched, and the bridge is the record that
// says the target is an adaptation of the source (see KNOT-ADR-014).
type Bridge struct {
	// ID is the canonical UUID text assigned by PostgreSQL.
	ID string
	// SourceCommentID is the comment that was bridged from.
	SourceCommentID string
	// TargetCommentID is the new comment that was created in the target language.
	TargetCommentID string
	// AuthorID is the user who made the bridge, and so the author of the target
	// comment.
	AuthorID string
	// TargetLanguage is the language the source comment was bridged into.
	TargetLanguage string
	// AdaptationNote is the optional note the bridger left, or "".
	AdaptationNote string
	// CreatedAt is the insertion time.
	CreatedAt time.Time
}

// CreateBridgeInput is the input to CreateBridge. It is a domain type, not an
// HTTP type, so the handler layer stays free of validation rules.
type CreateBridgeInput struct {
	// SourceCommentID is required and must be canonical UUID text. It comes from
	// the request path.
	SourceCommentID string
	// AuthorID is required and must be canonical UUID text. In the running
	// server it comes from the authenticated request, never from the body.
	AuthorID string
	// TargetLanguage is required, must be a 2-8 character tag, and must differ
	// from the source comment's language.
	TargetLanguage string
	// Body is the target comment's body, required and 1-MaxBodyLen characters.
	Body string
	// AdaptationNote is optional, up to MaxAdaptationNoteLength characters.
	AdaptationNote string
}

// BridgeStore is the persistence contract for bridges. The service depends on
// this interface rather than on pgx, so the business rules can be tested without
// a database.
type BridgeStore interface {
	// FindTargetVersion returns the id of the version of the source comment's
	// story that is written in targetLanguage, or ErrNotFound when the story has
	// no such version. It is how a bridge decides which conversation the target
	// comment joins; when several versions share the language, the oldest is
	// returned so the choice is deterministic.
	FindTargetVersion(ctx context.Context, sourceVersionID, targetLanguage string) (string, error)
	// CreateBridge inserts the target comment and the bridge that points at
	// sourceCommentID in one transaction, and returns both stored rows. It
	// returns ErrNotFound when the target comment's version does not exist, and
	// ErrAlreadyBridged when the source has already been bridged into the
	// target comment's language.
	CreateBridge(ctx context.Context, target Comment, sourceCommentID, adaptationNote string) (Bridge, Comment, error)
	// GetBridge returns the bridge with the given id, or ErrNotFound.
	GetBridge(ctx context.Context, id string) (Bridge, error)
	// ListBridgesForComment returns every bridge in which commentID is the
	// source or the target, newest first. It returns ErrNotFound when no comment
	// has the given id.
	ListBridgesForComment(ctx context.Context, commentID string) ([]Bridge, error)
	// ListBridgesForStory returns every bridge whose source comment belongs to a
	// version of the story, newest first. It returns ErrNotFound when no story
	// has the given id.
	ListBridgesForStory(ctx context.Context, storyID string) ([]Bridge, error)
}
