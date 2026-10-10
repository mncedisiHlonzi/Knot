// Package reactions implements the Knot perspective-reaction domain: the four
// knowledge-quality signals a reader can leave on a story, a version, a comment,
// or a bridge.
//
// A reaction is a signal of perspective, not a like (KNOT-ADR-050). The four
// signals do not compete: a user may hold any combination of them on the same
// entity, and none of them cancels another. They never sort or rank content, and
// an unpopular perspective is never suppressed — the reaction bar reports counts
// and nothing else.
//
// The package is deliberately layered, mirroring internal/stories and
// internal/conversations:
//
//	reaction.go       domain types, the closed sets, errors, the store contract
//	service.go        business rules (Toggle, ListForEntity, SummaryForEntity, …)
//	postgres_store.go the PostgreSQL implementation of the store contract
//
// Nothing in this package knows about HTTP, JSON, or SQL types. User and entity
// ids are plain strings holding canonical UUID text (KNOT-ADR-010).
//
// Entity existence is NOT this package's concern: `entity_id` is polymorphic and
// has no foreign key, so the HTTP layer resolves the entity through its own
// domain service before it toggles a reaction (KNOT-ADR-050). That keeps this
// package free of a dependency on any content domain.
package reactions

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
	ErrValidation = errors.New("reactions: invalid input")
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
	return fmt.Sprintf("reactions: invalid %s: %s", e.Field, e.Message)
}

// Unwrap makes every ValidationError satisfy errors.Is(err, ErrValidation).
func (e *ValidationError) Unwrap() error { return ErrValidation }

// EntityType is the kind of content a reaction is attached to.
//
// The set is closed: it is a CHECK constraint in the 0013_reactions migration,
// and an unknown value cannot be written even by a mistake in a caller.
type EntityType string

// The supported entity types. A reaction targets exactly one of these.
const (
	// EntityStory is a whole story.
	EntityStory EntityType = "story"
	// EntityVersion is one version (adaptation) of a story.
	EntityVersion EntityType = "version"
	// EntityComment is one comment.
	EntityComment EntityType = "comment"
	// EntityBridge is one bridge.
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

// ReactionType is one of the four perspective signals.
//
// The set is closed: it is a CHECK constraint in the 0013_reactions migration.
type ReactionType string

// The four perspective signals from the Master Brief (§10 Community
// Perspectives). They are not ordered and they do not compete.
const (
	// RingsTrue is "rings true to me": the reader finds it credible.
	RingsTrue ReactionType = "rings_true"
	// KnowItDifferently is "I know it differently": the reader holds a
	// different account. It is a perspective, not a correction.
	KnowItDifferently ReactionType = "know_it_differently"
	// AddsSomethingNew is "adds something new": the reader learned something.
	AddsSomethingNew ReactionType = "adds_something_new"
	// NeedsASource is "needs a source": the reader wants evidence.
	NeedsASource ReactionType = "needs_a_source"
)

// ReactionTypes is the four signals in display order, so a caller never has to
// spell the set out or guess an order.
var ReactionTypes = []ReactionType{RingsTrue, KnowItDifferently, AddsSomethingNew, NeedsASource}

// Valid reports whether t is one of the four signals.
func (t ReactionType) Valid() bool {
	switch t {
	case RingsTrue, KnowItDifferently, AddsSomethingNew, NeedsASource:
		return true
	default:
		return false
	}
}

// Reaction is one user's one signal on one entity.
type Reaction struct {
	// ID is the canonical UUID text assigned by PostgreSQL.
	ID string
	// UserID is the user who left the signal.
	UserID string
	// EntityType is the kind of content the signal is attached to.
	EntityType EntityType
	// EntityID is the id of that content.
	EntityID string
	// ReactionType is which of the four signals this is.
	ReactionType ReactionType
	// CreatedAt is the insertion time.
	CreatedAt time.Time
}

// Summary is the count of each of the four signals on one entity.
//
// It is a value type with a zero that means "no reactions", so an entity with no
// reactions needs no special case: the zero Summary is the correct answer.
type Summary struct {
	// RingsTrue is how many users said "rings true".
	RingsTrue int
	// KnowItDifferently is how many users said "know it differently".
	KnowItDifferently int
	// AddsSomethingNew is how many users said "adds something new".
	AddsSomethingNew int
	// NeedsASource is how many users said "needs a source".
	NeedsASource int
}

// Count returns the number of reactions of one signal type.
func (s Summary) Count(t ReactionType) int {
	switch t {
	case RingsTrue:
		return s.RingsTrue
	case KnowItDifferently:
		return s.KnowItDifferently
	case AddsSomethingNew:
		return s.AddsSomethingNew
	case NeedsASource:
		return s.NeedsASource
	default:
		return 0
	}
}

// Add increments the count for one signal type. An unknown type is ignored, so a
// row written before a future type existed cannot corrupt a summary.
func (s *Summary) Add(t ReactionType) {
	switch t {
	case RingsTrue:
		s.RingsTrue++
	case KnowItDifferently:
		s.KnowItDifferently++
	case AddsSomethingNew:
		s.AddsSomethingNew++
	case NeedsASource:
		s.NeedsASource++
	}
}

// Total returns the number of reactions across all four signals.
func (s Summary) Total() int {
	return s.RingsTrue + s.KnowItDifferently + s.AddsSomethingNew + s.NeedsASource
}

// ToggleInput is the input to Toggle. It is a domain type, not an HTTP type, so
// the handler layer stays free of validation rules.
type ToggleInput struct {
	// UserID is required and must be canonical UUID text. In the running server
	// it comes from the authenticated request, never from the body.
	UserID string
	// EntityType is required and must be one of the four kinds.
	EntityType EntityType
	// EntityID is required and must be canonical UUID text. Its existence is
	// checked by the caller, not here.
	EntityID string
	// ReactionType is required and must be one of the four signals.
	ReactionType ReactionType
}

// ReactionStore is the persistence contract for reactions. The service depends on
// this interface rather than on pgx, so the business rules can be tested without
// a database.
type ReactionStore interface {
	// Toggle adds the reaction when it is absent and removes it when it is
	// present, in one transaction. It reports whether the reaction is now held.
	Toggle(ctx context.Context, reaction Reaction) (bool, error)
	// ListForEntity returns every reaction on one entity, newest first.
	ListForEntity(ctx context.Context, entityType EntityType, entityID string) ([]Reaction, error)
	// Summaries returns the counts for each of the given entity ids of one type.
	// An entity with no reactions has no entry.
	Summaries(ctx context.Context, entityType EntityType, entityIDs []string) (map[string]Summary, error)
	// MyReactions returns, for one user, the signals they hold on each of the
	// given entity ids of one type. An entity the user has not reacted to has no
	// entry.
	MyReactions(ctx context.Context, userID string, entityType EntityType, entityIDs []string) (map[string][]ReactionType, error)
}
