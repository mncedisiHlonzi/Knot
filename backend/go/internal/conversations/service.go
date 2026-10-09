package conversations

import (
	"context"
	"errors"
	"fmt"
	"strings"
	"unicode/utf8"
)

// Service holds the conversation business rules.
//
// It depends on the CommentStore and BridgeStore abstractions and a Notifier,
// and knows nothing about HTTP, JSON, or SQL.
type Service struct {
	comments CommentStore
	bridges  BridgeStore
	notifier Notifier
}

// NewService wires the two stores and a notifier into the conversations domain.
func NewService(comments CommentStore, bridges BridgeStore, notifier Notifier) (*Service, error) {
	if comments == nil {
		return nil, fmt.Errorf("conversations: service requires a comment store")
	}
	if bridges == nil {
		return nil, fmt.Errorf("conversations: service requires a bridge store")
	}
	if notifier == nil {
		return nil, fmt.Errorf("conversations: service requires a notifier")
	}
	return &Service{comments: comments, bridges: bridges, notifier: notifier}, nil
}

// CreateComment validates the input, stores the comment, and returns it.
//
// It returns a *ValidationError for bad input, and ErrNotFound when the version
// does not exist (or the version id is not a UUID, so it cannot name a version).
func (s *Service) CreateComment(ctx context.Context, in CreateCommentInput) (Comment, error) {
	comment, err := validateCreateComment(in)
	if err != nil {
		return Comment{}, err
	}

	created, err := s.comments.CreateComment(ctx, comment)
	if err != nil {
		if errors.Is(err, ErrNotFound) {
			return Comment{}, ErrNotFound
		}
		return Comment{}, fmt.Errorf("conversations: create comment: %w", err)
	}

	s.notifyCommentCreated(ctx, created)

	return created, nil
}

// notifyCommentCreated tells the author of a version that someone commented on
// it, unless they wrote the comment themselves.
//
// A notification is a side effect. A lookup failure or a failed notification is
// ignored here, because the comment has already been stored and must not be lost
// (KNOT-ADR-038).
func (s *Service) notifyCommentCreated(ctx context.Context, comment Comment) {
	recipientID, err := s.comments.VersionAuthor(ctx, comment.VersionID)
	if err != nil {
		return
	}
	if recipientID == comment.AuthorID {
		return
	}
	_ = s.notifier.NotifyCommentCreated(ctx, recipientID, comment.AuthorID, comment.ID)
}

// ListComments returns one page of a version's comments, newest first, plus the
// cursor that resumes after it.
//
// An empty rawCursor asks for the first page. The returned next cursor is the
// empty string when the page is the last one. A version that does not exist is
// reported as ErrNotFound.
func (s *Service) ListComments(ctx context.Context, versionID string, rawCursor string, limit int) ([]Comment, string, error) {
	if !isUUID(versionID) {
		return nil, "", ErrNotFound
	}
	if limit < 1 || limit > MaxListLimit {
		return nil, "", &ValidationError{
			Field:   "limit",
			Message: fmt.Sprintf("must be between 1 and %d", MaxListLimit),
		}
	}

	var cursor *Cursor
	if rawCursor != "" {
		decoded, err := DecodeCursor(rawCursor)
		if err != nil {
			return nil, "", err
		}
		cursor = &decoded
	}

	page, next, err := s.comments.ListComments(ctx, versionID, cursor, limit)
	if err != nil {
		if errors.Is(err, ErrNotFound) {
			return nil, "", ErrNotFound
		}
		return nil, "", fmt.Errorf("conversations: list comments: %w", err)
	}

	if page == nil {
		// Emit [] rather than null so a client never has to special-case an
		// empty thread.
		page = []Comment{}
	}

	nextCursor := ""
	if next != nil {
		nextCursor = next.Encode()
	}

	return page, nextCursor, nil
}

// CreateBridge validates the input, resolves the target version, writes the
// target comment on that version and the bridge in one transaction, and returns
// the bridge together with both comments.
//
// The target comment is written on the story's version in the target language,
// so the bridge connects two conversations. It returns a *ValidationError for bad
// input — including a target_language that equals the source comment's language,
// a story with no version in the target language, or a source already bridged
// into that language — and ErrNotFound when the source comment does not exist.
func (s *Service) CreateBridge(ctx context.Context, in CreateBridgeInput) (Bridge, Comment, Comment, error) {
	input, err := validateCreateBridge(in)
	if err != nil {
		return Bridge{}, Comment{}, Comment{}, err
	}

	// The source comment's language is what the target language must differ from,
	// and its version locates the story whose other versions the bridge may join.
	// Both facts live on the stored row, so the source has to be read first.
	source, err := s.comments.GetComment(ctx, input.SourceCommentID)
	if err != nil {
		if errors.Is(err, ErrNotFound) {
			return Bridge{}, Comment{}, Comment{}, ErrNotFound
		}
		return Bridge{}, Comment{}, Comment{}, fmt.Errorf("conversations: load source comment: %w", err)
	}

	if source.Language == input.TargetLanguage {
		return Bridge{}, Comment{}, Comment{}, &ValidationError{
			Field:   "target_language",
			Message: "must differ from the source comment's language",
		}
	}

	// The target comment joins the story's conversation in the target language,
	// which is what makes a bridge a link between two conversations. A story with
	// no version in that language has nothing to bridge into.
	targetVersionID, err := s.bridges.FindTargetVersion(ctx, source.VersionID, input.TargetLanguage)
	if err != nil {
		if errors.Is(err, ErrNotFound) {
			return Bridge{}, Comment{}, Comment{}, &ValidationError{
				Field:   "target_language",
				Message: "the story has no version in that language",
			}
		}
		return Bridge{}, Comment{}, Comment{}, fmt.Errorf("conversations: resolve target version: %w", err)
	}

	target := Comment{
		VersionID: targetVersionID,
		AuthorID:  input.AuthorID,
		Language:  input.TargetLanguage,
		Body:      input.Body,
	}

	bridge, createdTarget, err := s.bridges.CreateBridge(ctx, target, source.ID, input.AdaptationNote)
	if err != nil {
		if errors.Is(err, ErrAlreadyBridged) {
			return Bridge{}, Comment{}, Comment{}, &ValidationError{
				Field:   "target_language",
				Message: "this comment has already been bridged into that language",
			}
		}
		if errors.Is(err, ErrNotFound) {
			return Bridge{}, Comment{}, Comment{}, ErrNotFound
		}
		return Bridge{}, Comment{}, Comment{}, fmt.Errorf("conversations: create bridge: %w", err)
	}

	// Tell the author of the bridged comment that it crossed a language, unless
	// they bridged it themselves. The bridge is already stored, so the
	// notification is a non-critical side effect (KNOT-ADR-038).
	if source.AuthorID != bridge.AuthorID {
		_ = s.notifier.NotifyBridgeCreated(ctx, source.AuthorID, bridge.AuthorID, bridge.ID)
	}

	return bridge, source, createdTarget, nil
}

// GetBridge returns a single bridge by id.
//
// An id that is not canonical UUID text is reported as ErrNotFound rather than
// as a validation error: the resource genuinely does not exist, and answering
// 404 keeps the id column's index usable instead of casting it to text in SQL.
func (s *Service) GetBridge(ctx context.Context, id string) (Bridge, error) {
	if !isUUID(id) {
		return Bridge{}, ErrNotFound
	}

	bridge, err := s.bridges.GetBridge(ctx, id)
	if err != nil {
		if errors.Is(err, ErrNotFound) {
			return Bridge{}, ErrNotFound
		}
		return Bridge{}, fmt.Errorf("conversations: get bridge: %w", err)
	}

	return bridge, nil
}

// GetComment returns a single comment by id, with the id of the story its version
// belongs to resolved (Comment.StoryID).
//
// It is how a notification tap resolves a comment id to the thread it belongs to:
// the version and the story come back together, so the client needs no second
// lookup.
//
// An id that is not canonical UUID text is reported as ErrNotFound rather than as
// a validation error: the resource genuinely does not exist, and answering 404
// keeps the id column's index usable instead of casting it to text in SQL.
func (s *Service) GetComment(ctx context.Context, id string) (Comment, error) {
	if !isUUID(id) {
		return Comment{}, ErrNotFound
	}

	comment, err := s.comments.GetComment(ctx, id)
	if err != nil {
		if errors.Is(err, ErrNotFound) {
			return Comment{}, ErrNotFound
		}
		return Comment{}, fmt.Errorf("conversations: get comment: %w", err)
	}

	return comment, nil
}

// ListBridgesForComment returns every bridge in which the comment is the source
// or the target. A comment that does not exist is reported as ErrNotFound.
func (s *Service) ListBridgesForComment(ctx context.Context, commentID string) ([]Bridge, error) {
	if !isUUID(commentID) {
		return nil, ErrNotFound
	}

	bridges, err := s.bridges.ListBridgesForComment(ctx, commentID)
	if err != nil {
		if errors.Is(err, ErrNotFound) {
			return nil, ErrNotFound
		}
		return nil, fmt.Errorf("conversations: list bridges for comment: %w", err)
	}

	if bridges == nil {
		bridges = []Bridge{}
	}

	return bridges, nil
}

// ListBridgesForStory returns every bridge anchored to one of a story's
// versions, newest first. A story that does not exist is reported as ErrNotFound.
//
// It has no HTTP route yet: a per-version bridge count in the Language Tree
// would need a response the approved endpoints do not expose. It exists so that
// endpoint can be added without changing this package.
func (s *Service) ListBridgesForStory(ctx context.Context, storyID string) ([]Bridge, error) {
	if !isUUID(storyID) {
		return nil, ErrNotFound
	}

	bridges, err := s.bridges.ListBridgesForStory(ctx, storyID)
	if err != nil {
		if errors.Is(err, ErrNotFound) {
			return nil, ErrNotFound
		}
		return nil, fmt.Errorf("conversations: list bridges for story: %w", err)
	}

	if bridges == nil {
		bridges = []Bridge{}
	}

	return bridges, nil
}

// validateCreateComment applies every input rule and returns the comment to store.
//
// The body is stored verbatim so the author's formatting survives; it is only
// trimmed to decide whether it is empty.
func validateCreateComment(in CreateCommentInput) (Comment, error) {
	// A version id from the request path that is not a UUID cannot name a
	// version, so it is reported as not-found rather than as a validation error.
	if !isUUID(in.VersionID) {
		return Comment{}, ErrNotFound
	}

	if !isUUID(in.AuthorID) {
		return Comment{}, &ValidationError{Field: "author_id", Message: "must be a UUID"}
	}

	language, err := validateLanguage(in.Language, "language")
	if err != nil {
		return Comment{}, err
	}

	body, err := validateBody(in.Body)
	if err != nil {
		return Comment{}, err
	}

	return Comment{
		VersionID: in.VersionID,
		AuthorID:  in.AuthorID,
		Language:  language,
		Body:      body,
	}, nil
}

// validateCreateBridge applies every input rule and returns the normalised input.
func validateCreateBridge(in CreateBridgeInput) (CreateBridgeInput, error) {
	if !isUUID(in.SourceCommentID) {
		return CreateBridgeInput{}, ErrNotFound
	}

	if !isUUID(in.AuthorID) {
		return CreateBridgeInput{}, &ValidationError{Field: "author_id", Message: "must be a UUID"}
	}

	language, err := validateLanguage(in.TargetLanguage, "target_language")
	if err != nil {
		return CreateBridgeInput{}, err
	}

	body, err := validateBody(in.Body)
	if err != nil {
		return CreateBridgeInput{}, err
	}

	note := strings.TrimSpace(in.AdaptationNote)
	if utf8.RuneCountInString(note) > MaxAdaptationNoteLength {
		return CreateBridgeInput{}, &ValidationError{
			Field:   "adaptation_note",
			Message: fmt.Sprintf("must be at most %d characters", MaxAdaptationNoteLength),
		}
	}

	return CreateBridgeInput{
		SourceCommentID: in.SourceCommentID,
		AuthorID:        in.AuthorID,
		TargetLanguage:  language,
		Body:            body,
		AdaptationNote:  note,
	}, nil
}

// validateBody requires a non-empty body within MaxBodyLen runes and returns it
// verbatim.
func validateBody(raw string) (string, error) {
	if utf8.RuneCountInString(strings.TrimSpace(raw)) < minBodyLen {
		return "", &ValidationError{Field: "body", Message: "is required"}
	}
	if utf8.RuneCountInString(raw) > MaxBodyLen {
		return "", &ValidationError{
			Field:   "body",
			Message: fmt.Sprintf("must be at most %d characters", MaxBodyLen),
		}
	}
	return raw, nil
}

// validateLanguage normalises a language tag to lower case and checks its
// length, reporting failures against the given field name. Only ASCII letters
// are accepted: the 2-8 character bound in the product spec describes a simple
// tag, not a full BCP 47 production.
func validateLanguage(raw, field string) (string, error) {
	language := strings.ToLower(strings.TrimSpace(raw))

	if utf8.RuneCountInString(language) < minLanguageLen || utf8.RuneCountInString(language) > maxLanguageLen {
		return "", &ValidationError{
			Field:   field,
			Message: fmt.Sprintf("must be between %d and %d characters", minLanguageLen, maxLanguageLen),
		}
	}

	for i := 0; i < len(language); i++ {
		if language[i] < 'a' || language[i] > 'z' {
			return "", &ValidationError{
				Field:   field,
				Message: "must contain only letters",
			}
		}
	}

	return language, nil
}
