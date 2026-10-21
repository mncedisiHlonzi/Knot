package interactions

import (
	"context"
	"fmt"
	"strings"
)

// InteractionType is one kind of engagement the platform records.
//
// The set is closed: it is a CHECK constraint in the 0019_interactions
// migration, and an unknown value is rejected here before it reaches the store.
type InteractionType string

// The recorded interaction types.
const (
	// InteractionView is an entry opened without a demonstrated full read.
	InteractionView InteractionType = "view"
	// InteractionReadFull is an entry read to the end.
	InteractionReadFull InteractionType = "read_full"
	// InteractionReaction is one perspective signal left on an entity.
	InteractionReaction InteractionType = "reaction"
	// InteractionComment is one comment posted on a version's conversation.
	InteractionComment InteractionType = "comment"
	// InteractionAdapt is one adaptation written from an existing telling.
	InteractionAdapt InteractionType = "adapt"
)

// Entity type values the log accepts. A comment's or an adaptation's engagement
// is recorded against the story or version it was acted on, so these are the only
// two.
const (
	EntityStory   = "story"
	EntityVersion = "version"
)

// The engagement ladder, in points.
//
// The numbers are deliberately far apart: a passive view must never outweigh a
// write, and the ratios are an editorial judgement rather than a tuned model.
// They are the input to SimpleRanker's pillar affinity, and a future ML model
// will consume the same rows.
const (
	WeightView     float64 = 1.0
	WeightReadFull float64 = 3.0
	WeightReaction float64 = 5.0
	WeightComment  float64 = 7.0
	WeightAdapt    float64 = 10.0
)

// interactionWeights is the lookup table behind Weight.
var interactionWeights = map[InteractionType]float64{
	InteractionView:     WeightView,
	InteractionReadFull: WeightReadFull,
	InteractionReaction: WeightReaction,
	InteractionComment:  WeightComment,
	InteractionAdapt:    WeightAdapt,
}

// Weight answers the score for one interaction type.
func Weight(interactionType InteractionType) (float64, bool) {
	weight, ok := interactionWeights[interactionType]
	return weight, ok
}

// Record is one row of the interaction log, with the weight already resolved.
type Record struct {
	// UserID is the account whose engagement this is.
	UserID string
	// EntityType is EntityStory or EntityVersion.
	EntityType string
	// EntityID is the story or version the user acted on.
	EntityID string
	// Type is the interaction.
	Type InteractionType
	// Weight is the score applied to Type at write time, so the stored row keeps
	// the ladder that was in force when it happened.
	Weight float64
}

// LogStore is the write contract. The Postgres store implements it; a test
// double can capture records without a database.
type LogStore interface {
	// Insert appends one interaction row.
	Insert(ctx context.Context, record Record) error
}

// Service records user engagement with stories and versions.
//
// It is the write half of this package; the read model in interaction.go is the
// other. `Log` performs exactly one parameterised INSERT and returns the store's
// error unchanged (wrapped), so a caller can test the ladder without a database.
//
// Log is deliberately synchronous and deterministic. **The non-blocking boundary
// is the HTTP layer**: httpapi's `recordInteraction` runs it in a goroutine with
// a context detached from the request and bounded by a timeout, so a slow or
// failing insert can neither delay nor fail the user's action (KNOT-026).
type Service struct {
	store LogStore
}

// NewService returns a service that writes through store.
func NewService(store LogStore) (*Service, error) {
	if store == nil {
		return nil, fmt.Errorf("interactions: service requires a non-nil store")
	}
	return &Service{store: store}, nil
}

// Log resolves the interaction's weight and appends one row.
//
// A blank user or entity id, an unknown entity type, or an unknown interaction
// type is a programming error and is returned before any write: the caller must
// never be able to invent a weight. A store failure is wrapped so the caller can
// log it without exposing it to the user.
func (s *Service) Log(ctx context.Context, userID, entityType, entityID, interactionType string) error {
	user := strings.TrimSpace(userID)
	entity := strings.TrimSpace(entityID)
	entityKind := strings.TrimSpace(entityType)
	kind := InteractionType(strings.TrimSpace(interactionType))

	if user == "" {
		return fmt.Errorf("interactions: log requires a user id")
	}
	if entity == "" {
		return fmt.Errorf("interactions: log requires an entity id")
	}
	if entityKind != EntityStory && entityKind != EntityVersion {
		return fmt.Errorf("interactions: unknown entity type %q", entityKind)
	}

	weight, ok := Weight(kind)
	if !ok {
		return fmt.Errorf("interactions: unknown interaction type %q", kind)
	}

	record := Record{
		UserID:     user,
		EntityType: entityKind,
		EntityID:   entity,
		Type:       kind,
		Weight:     weight,
	}
	if err := s.store.Insert(ctx, record); err != nil {
		return fmt.Errorf("interactions: log %s: %w", kind, err)
	}

	return nil
}
