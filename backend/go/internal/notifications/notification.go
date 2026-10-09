package notifications

// Package notifications implements the Knot in-app notification domain: a record
// that one user's content was acted on by another user.
//
// The package is deliberately layered, mirroring internal/stories:
//
//	notification.go   domain types, the event and entity sets, errors, store contract
//	cursor.go         opaque pagination cursors for the inbox
//	service.go        business rules (Create, List, UnreadCount, MarkRead, …)
//	postgres_store.go the PostgreSQL implementation of the store contract
//
// The package is deliberately **domain-agnostic** (KNOT-ADR-040). It knows
// nothing about stories, versions, comments, or bridges beyond the strings it
// stores: a caller resolves who should be notified and passes primitive ids. That
// keeps the dependency graph acyclic — versions and conversations depend on a
// one-method hook they define themselves, never on this package's types — and it
// is what makes the notification write a leaf operation that cannot drag a
// content domain into a cycle.
//
// Delivery is in-app only (KNOT-ADR-039): there is no push channel here, and the
// store has no concept of delivery. Nothing in this package knows about HTTP,
// JSON, or SQL types.

import (
	"context"
	"errors"
	"fmt"
	"time"
)

// Sentinel errors. Handlers map these onto HTTP status codes; they are never
// returned to clients verbatim.
var (
	// ErrNotFound is returned when no notification matches the requested id, or
	// when the id exists but belongs to another user.
	ErrNotFound = errors.New("notifications: notification not found")
	// ErrSelfNotification is returned when the recipient and the actor are the
	// same user. It is not a client error: acting on your own content is ordinary
	// and simply does not notify you (KNOT-ADR-038). Callers ignore it silently.
	ErrSelfNotification = errors.New("notifications: the actor and the recipient are the same user")
	// ErrValidation is returned for rejected input. Every validation failure in
	// this package also carries a *ValidationError naming the offending field, so
	// errors.Is(err, ErrValidation) and errors.As(err, &validation) are both true
	// for the same value.
	ErrValidation = errors.New("notifications: invalid input")
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
	return fmt.Sprintf("notifications: invalid %s: %s", e.Field, e.Message)
}

// Unwrap makes every ValidationError satisfy errors.Is(err, ErrValidation).
func (e *ValidationError) Unwrap() error { return ErrValidation }

// EventType is what happened. The set is closed: it is a CHECK constraint in the
// 0010_notifications migration, and an unknown value cannot be written even by a
// mistake in a caller.
type EventType string

// The supported events. Each one maps to exactly one entity type.
const (
	// EventVersionCreated fires when someone adapts a story or a version the
	// recipient authored.
	EventVersionCreated EventType = "version.created"
	// EventCommentCreated fires when someone comments on a version the recipient
	// authored.
	EventCommentCreated EventType = "comment.created"
	// EventBridgeCreated fires when someone bridges a comment the recipient
	// authored into another language.
	EventBridgeCreated EventType = "bridge.created"
)

// Valid reports whether e is one of the supported events.
func (e EventType) Valid() bool {
	switch e {
	case EventVersionCreated, EventCommentCreated, EventBridgeCreated:
		return true
	default:
		return false
	}
}

// EntityType is the kind of thing a notification points at, so the client knows
// which screen to open.
type EntityType string

// The supported entity types.
const (
	// EntityStory is a story.
	EntityStory EntityType = "story"
	// EntityVersion is a story version.
	EntityVersion EntityType = "version"
	// EntityComment is a comment.
	EntityComment EntityType = "comment"
	// EntityBridge is a bridge.
	EntityBridge EntityType = "bridge"
)

// Valid reports whether t is one of the supported entity types.
func (t EntityType) Valid() bool {
	switch t {
	case EntityStory, EntityVersion, EntityComment, EntityBridge:
		return true
	default:
		return false
	}
}

// EntityType returns the entity an event must reference, and whether the event is
// known.
//
// The mapping is one-to-one for the current event set, and the service enforces
// it, so a caller cannot file "an adaptation" against a comment and produce a
// notification the client cannot open.
func (e EventType) EntityType() (EntityType, bool) {
	switch e {
	case EventVersionCreated:
		return EntityVersion, true
	case EventCommentCreated:
		return EntityComment, true
	case EventBridgeCreated:
		return EntityBridge, true
	default:
		return "", false
	}
}

// Notification is one in-app notification.
type Notification struct {
	// ID is the canonical UUID text assigned by PostgreSQL.
	ID string
	// UserID is the recipient: the user whose content was acted on.
	UserID string
	// ActorID is the user who acted. It is never equal to UserID.
	ActorID string
	// EventType is what happened.
	EventType EventType
	// EntityType is the kind of thing EntityID names.
	EntityType EntityType
	// EntityID is the id of the entity the client should open.
	EntityID string
	// ReadAt is when the recipient read the notification, or nil while unread.
	ReadAt *time.Time
	// CreatedAt is the creation time. It is the inbox's primary sort key.
	CreatedAt time.Time
}

// IsRead reports whether the notification has been read.
func (n Notification) IsRead() bool { return n.ReadAt != nil }

// CreateInput is the input to Create. It is a domain type, not an HTTP type.
//
// Every field is primitive on purpose: a caller in another domain passes ids it
// already holds and never imports this package's content types (KNOT-ADR-040).
type CreateInput struct {
	// UserID is the recipient and must be canonical UUID text.
	UserID string
	// ActorID is the user who acted and must be canonical UUID text.
	ActorID string
	// EventType is what happened.
	EventType EventType
	// EntityType is the kind of thing EntityID names. It must match EventType.
	EntityType EntityType
	// EntityID is the id of the entity and must be canonical UUID text.
	EntityID string
}

// NotificationStore is the persistence contract for notifications. The service
// depends on this interface rather than on pgx, so the business rules can be
// tested without a database.
type NotificationStore interface {
	// Create inserts a notification and returns the stored row.
	Create(ctx context.Context, notification Notification) (Notification, error)
	// List returns at most limit notifications for a user, newest first, starting
	// after cursor. A nil cursor starts at the newest. The returned cursor is the
	// position to resume from, or nil when the page is the last one.
	List(ctx context.Context, userID string, cursor *Cursor, limit int) ([]Notification, *Cursor, error)
	// UnreadCount returns how many of a user's notifications have not been read.
	UnreadCount(ctx context.Context, userID string) (int, error)
	// MarkRead marks one notification read, if it belongs to userID. It returns
	// ErrNotFound when no such notification exists for that user. Marking an
	// already-read notification is not an error.
	MarkRead(ctx context.Context, id, userID string) error
	// MarkAllRead marks every unread notification for a user read and returns how
	// many rows were updated.
	MarkAllRead(ctx context.Context, userID string) (int, error)
}
