package notifications

import (
	"context"
	"errors"
	"fmt"
)

// Page sizes. The service enforces them so the domain, not just the HTTP layer,
// refuses an unbounded query.
const (
	// DefaultListLimit is the page size the API uses when none is requested.
	DefaultListLimit = 20
	// MaxListLimit is the largest page the API will serve.
	MaxListLimit = 50
)

// Logger is the slice of slog.Logger this package needs: a warning when a
// notification write fails. Depending on an interface keeps the service tests
// free of a logger.
type Logger interface {
	WarnContext(ctx context.Context, msg string, args ...any)
}

// Service holds the notification business rules.
//
// It depends on the NotificationStore abstraction and a Logger, and it knows
// nothing about HTTP, JSON, SQL, or any content domain.
type Service struct {
	store  NotificationStore
	logger Logger
}

// NewService wires a store and a logger into the notification domain.
func NewService(store NotificationStore, logger Logger) (*Service, error) {
	if store == nil {
		return nil, fmt.Errorf("notifications: service requires a notification store")
	}
	if logger == nil {
		return nil, fmt.Errorf("notifications: service requires a logger")
	}
	return &Service{store: store, logger: logger}, nil
}

// Create validates the input and stores a notification.
//
// A notification whose recipient is its own actor is refused with
// ErrSelfNotification rather than stored: acting on your own content never
// notifies you, and the schema enforces the same rule (KNOT-ADR-038). Callers
// treat that error as expected and ignore it.
func (s *Service) Create(ctx context.Context, in CreateInput) (Notification, error) {
	notification, err := validateCreate(in)
	if err != nil {
		return Notification{}, err
	}

	stored, err := s.store.Create(ctx, notification)
	if err != nil {
		return Notification{}, fmt.Errorf("notifications: create: %w", err)
	}

	return stored, nil
}

// List returns one page of a user's notifications, newest first, plus the cursor
// that resumes after it.
//
// An empty rawCursor asks for the first page. The returned next cursor is the
// empty string when the page is the last one. A user id that is not a UUID cannot
// name a user, so it is reported as ErrNotFound.
func (s *Service) List(ctx context.Context, userID string, rawCursor string, limit int) ([]Notification, string, error) {
	if !isUUID(userID) {
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

	page, next, err := s.store.List(ctx, userID, cursor, limit)
	if err != nil {
		return nil, "", fmt.Errorf("notifications: list: %w", err)
	}

	if page == nil {
		// Emit [] rather than null so a client never special-cases an empty inbox.
		page = []Notification{}
	}

	nextCursor := ""
	if next != nil {
		nextCursor = next.Encode()
	}

	return page, nextCursor, nil
}

// UnreadCount returns how many of a user's notifications are unread.
func (s *Service) UnreadCount(ctx context.Context, userID string) (int, error) {
	if !isUUID(userID) {
		return 0, ErrNotFound
	}

	count, err := s.store.UnreadCount(ctx, userID)
	if err != nil {
		return 0, fmt.Errorf("notifications: unread count: %w", err)
	}

	return count, nil
}

// MarkRead marks one notification read.
//
// A notification that does not exist, or that belongs to another user, is
// reported as ErrNotFound — the same answer for both, so the endpoint cannot be
// used to probe whether a notification id exists. Marking an already-read
// notification is not an error.
func (s *Service) MarkRead(ctx context.Context, id, userID string) error {
	if !isUUID(id) || !isUUID(userID) {
		return ErrNotFound
	}

	if err := s.store.MarkRead(ctx, id, userID); err != nil {
		if errors.Is(err, ErrNotFound) {
			return ErrNotFound
		}
		return fmt.Errorf("notifications: mark read: %w", err)
	}

	return nil
}

// MarkAllRead marks every unread notification for a user read and returns how
// many rows were updated.
func (s *Service) MarkAllRead(ctx context.Context, userID string) (int, error) {
	if !isUUID(userID) {
		return 0, ErrNotFound
	}

	updated, err := s.store.MarkAllRead(ctx, userID)
	if err != nil {
		return 0, fmt.Errorf("notifications: mark all read: %w", err)
	}

	return updated, nil
}

// NotifyVersionCreated records that actorID adapted content authored by
// recipientID.
//
// It is the primitive hook a content domain calls: the arguments are ids the
// caller already holds, so the caller never imports this package's types
// (KNOT-ADR-040). A failure is logged and returned; the caller ignores it so the
// adaptation that triggered it still succeeds (KNOT-ADR-038).
func (s *Service) NotifyVersionCreated(ctx context.Context, recipientID, actorID, versionID string) error {
	return s.notify(ctx, recipientID, actorID, EventVersionCreated, EntityVersion, versionID, "")
}

// NotifyCommentCreated records that actorID commented on a version authored by
// recipientID.
func (s *Service) NotifyCommentCreated(ctx context.Context, recipientID, actorID, commentID string) error {
	return s.notify(ctx, recipientID, actorID, EventCommentCreated, EntityComment, commentID, "")
}

// NotifyBridgeCreated records that actorID bridged a comment authored by
// recipientID.
func (s *Service) NotifyBridgeCreated(ctx context.Context, recipientID, actorID, bridgeID string) error {
	return s.notify(ctx, recipientID, actorID, EventBridgeCreated, EntityBridge, bridgeID, "")
}

// NotifyReactionCreated records that actorID left a reaction on content
// authored by recipientID (KNOT-ADR-050).
//
// entityType is the target's own kind (story, version, comment, or bridge): a
// reaction reuses the entity it points at rather than introducing a new one, so
// the client opens the same screen a tap on the content would. reactionType is
// which of the four signals was left, so the inbox can name it.
func (s *Service) NotifyReactionCreated(ctx context.Context, recipientID, actorID, entityType, reactionType, entityID string) error {
	return s.notify(ctx, recipientID, actorID, EventReactionCreated, EntityType(entityType), entityID, reactionType)
}

// notify is the shared body of the three hooks. It never returns a wrapped
// store error to a caller in another domain that would have to know this
// package's error types: it returns the error Create produced, having logged it.
func (s *Service) notify(ctx context.Context, recipientID, actorID string, event EventType, entity EntityType, entityID, reactionType string) error {
	_, err := s.Create(ctx, CreateInput{
		UserID:       recipientID,
		ActorID:      actorID,
		EventType:    event,
		EntityType:   entity,
		EntityID:     entityID,
		ReactionType: reactionType,
	})
	if err == nil {
		return nil
	}

	// A self-notification is expected and benign, so it is not logged as a
	// problem.
	if errors.Is(err, ErrSelfNotification) {
		return err
	}

	s.logger.WarnContext(
		ctx,
		"could not record notification",
		"event_type", string(event),
		"entity_id", entityID,
		"error", err.Error(),
	)

	return err
}

// validateCreate applies every input rule and returns the notification to store.
//
// The recipient and the actor must differ (the schema enforces the same rule),
// the event must be known, and the entity type must be the one the event
// requires, so a stored notification always points at something the client can
// open.
func validateCreate(in CreateInput) (Notification, error) {
	if !isUUID(in.UserID) {
		return Notification{}, &ValidationError{Field: "user_id", Message: "must be a UUID"}
	}
	if !isUUID(in.ActorID) {
		return Notification{}, &ValidationError{Field: "actor_id", Message: "must be a UUID"}
	}
	if in.UserID == in.ActorID {
		return Notification{}, ErrSelfNotification
	}

	if !in.EventType.Valid() {
		return Notification{}, &ValidationError{
			Field:   "event_type",
			Message: "must be one of version.created, comment.created, bridge.created, reaction.created",
		}
	}
	if !in.EventType.acceptsEntityType(in.EntityType) {
		return Notification{}, &ValidationError{
			Field:   "entity_type",
			Message: fmt.Sprintf("must be a valid entity for event %s", in.EventType),
		}
	}
	if !isUUID(in.EntityID) {
		return Notification{}, &ValidationError{Field: "entity_id", Message: "must be a UUID"}
	}

	// The reaction type travels only with reaction.created, so a mis-wired caller
	// cannot file a signal against an adaptation.
	if in.EventType == EventReactionCreated {
		if in.ReactionType == "" {
			return Notification{}, &ValidationError{Field: "reaction_type", Message: "is required for event reaction.created"}
		}
	} else if in.ReactionType != "" {
		return Notification{}, &ValidationError{
			Field:   "reaction_type",
			Message: fmt.Sprintf("must be empty for event %s", in.EventType),
		}
	}

	return Notification{
		UserID:       in.UserID,
		ActorID:      in.ActorID,
		EventType:    in.EventType,
		EntityType:   in.EntityType,
		EntityID:     in.EntityID,
		ReactionType: in.ReactionType,
	}, nil
}

// isUUID reports whether s is a canonical 8-4-4-4-12 hexadecimal UUID string.
//
// It duplicates the unexported validator in the other domains rather than
// exporting one: the alternative would widen a package's API for a helper that
// is a few lines of character testing (KNOT-ADR-010).
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
