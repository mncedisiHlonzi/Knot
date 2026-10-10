package reactions

import (
	"context"
	"fmt"
	"path"

	"github.com/knot/backend/internal/identity"
)

// Actor is the resolved public identity of the user who left a reaction. It is
// the same projection the rest of the API exposes about an author, so a client
// renders a reactor exactly like any other author.
type Actor struct {
	// ID is the reactor's user id.
	ID string
	// DisplayName is the reactor's name, or "" when their account could not be
	// resolved (for example, a deleted user).
	DisplayName string
	// AvatarURL is the backend path to fetch the reactor's avatar from, or ""
	// when they have none.
	AvatarURL string
}

// ReactionWithActor is one reaction plus the identity of the user who left it.
type ReactionWithActor struct {
	// Reaction is the stored signal.
	Reaction Reaction
	// Actor is the resolved reactor.
	Actor Actor
}

// ActorLookup resolves the accounts behind a set of user ids in one batched call.
//
// It is deliberately narrow: the service only needs to name and picture a
// reactor, so it depends on the single method that does that rather than on the
// whole identity service (the same contract the HTTP layer's author lookup uses).
type ActorLookup interface {
	UsersByIDs(ctx context.Context, ids []string) (map[string]*identity.User, error)
}

// Logger is the slice of slog.Logger this package needs: a warning when actor
// enrichment fails. Depending on an interface keeps the service tests free of a
// logger.
type Logger interface {
	WarnContext(ctx context.Context, msg string, args ...any)
}

// Service holds the reaction business rules.
//
// It depends on the ReactionStore abstraction, an actor lookup, and a logger, and
// it knows nothing about HTTP, JSON, SQL, or any content domain.
type Service struct {
	store  ReactionStore
	actors ActorLookup
	logger Logger
}

// NewService wires a store, an actor lookup, and a logger into the domain.
func NewService(store ReactionStore, actors ActorLookup, logger Logger) (*Service, error) {
	if store == nil {
		return nil, fmt.Errorf("reactions: service requires a reaction store")
	}
	if actors == nil {
		return nil, fmt.Errorf("reactions: service requires an actor lookup")
	}
	if logger == nil {
		return nil, fmt.Errorf("reactions: service requires a logger")
	}
	return &Service{store: store, actors: actors, logger: logger}, nil
}

// Toggle adds the given signal when the user does not hold it and removes it when
// they do, then returns the entity's updated summary.
//
// The second result reports whether the signal is now held: true when this call
// created it, false when it removed it. The caller uses it to decide whether to
// fire a notification — only creating a reaction notifies anyone (KNOT-ADR-050).
//
// Existence of the entity is the caller's responsibility: `entity_id` is
// polymorphic, so the HTTP layer resolves it through the owning domain service
// before calling here (KNOT-ADR-050).
func (s *Service) Toggle(ctx context.Context, in ToggleInput) (Summary, bool, error) {
	if err := validateToggle(in); err != nil {
		return Summary{}, false, err
	}

	active, err := s.store.Toggle(ctx, Reaction{
		UserID:       in.UserID,
		EntityType:   in.EntityType,
		EntityID:     in.EntityID,
		ReactionType: in.ReactionType,
	})
	if err != nil {
		return Summary{}, false, fmt.Errorf("reactions: toggle: %w", err)
	}

	summary, err := s.SummaryForEntity(ctx, in.EntityType, in.EntityID)
	if err != nil {
		return Summary{}, false, err
	}

	return summary, active, nil
}

// ListForEntity returns every reaction on one entity, newest first, with each
// reactor resolved in one batched read.
//
// Enrichment is supplementary: a lookup failure is logged and swallowed rather
// than failing the read, so the signals stay readable when identity is
// unavailable. A reactor that cannot be resolved carries an empty name and no
// avatar, which the client renders as an unknown user.
func (s *Service) ListForEntity(ctx context.Context, entityType EntityType, entityID string) ([]ReactionWithActor, error) {
	if !entityType.Valid() {
		return nil, &ValidationError{Field: "entity_type", Message: "must be one of story, version, comment, bridge"}
	}
	if !isUUID(entityID) {
		return nil, &ValidationError{Field: "entity_id", Message: "must be a UUID"}
	}

	list, err := s.store.ListForEntity(ctx, entityType, entityID)
	if err != nil {
		return nil, fmt.Errorf("reactions: list for entity: %w", err)
	}

	actorIDs := make([]string, 0, len(list))
	for _, reaction := range list {
		actorIDs = append(actorIDs, reaction.UserID)
	}
	actors := s.actorAttributions(ctx, actorIDs)

	out := make([]ReactionWithActor, 0, len(list))
	for _, reaction := range list {
		actor := actors[reaction.UserID]
		if actor.ID == "" {
			actor.ID = reaction.UserID
		}
		out = append(out, ReactionWithActor{Reaction: reaction, Actor: actor})
	}

	return out, nil
}

// SummaryForEntity returns the counts for one entity. An entity with no reactions
// returns the zero Summary, not an error.
func (s *Service) SummaryForEntity(ctx context.Context, entityType EntityType, entityID string) (Summary, error) {
	if !entityType.Valid() {
		return Summary{}, &ValidationError{Field: "entity_type", Message: "must be one of story, version, comment, bridge"}
	}
	if !isUUID(entityID) {
		return Summary{}, &ValidationError{Field: "entity_id", Message: "must be a UUID"}
	}

	summaries, err := s.store.Summaries(ctx, entityType, []string{entityID})
	if err != nil {
		return Summary{}, fmt.Errorf("reactions: summary: %w", err)
	}

	return summaries[entityID], nil
}

// BatchSummaries returns the counts for a page of entity ids of one type, in one
// query, so enriching a feed page never fans out into a query per row.
func (s *Service) BatchSummaries(ctx context.Context, entityType EntityType, entityIDs []string) (map[string]Summary, error) {
	if !entityType.Valid() {
		return nil, &ValidationError{Field: "entity_type", Message: "must be one of story, version, comment, bridge"}
	}
	if len(entityIDs) == 0 {
		return map[string]Summary{}, nil
	}

	summaries, err := s.store.Summaries(ctx, entityType, validIDs(entityIDs))
	if err != nil {
		return nil, fmt.Errorf("reactions: batch summaries: %w", err)
	}
	if summaries == nil {
		return map[string]Summary{}, nil
	}

	return summaries, nil
}

// BatchMyReactions returns, for one user, the signals they hold on a page of
// entity ids of one type, in one query.
//
// An empty user id (an unauthenticated reader) returns an empty map without a
// database round trip, so a public read costs nothing extra for a signed-out
// client.
func (s *Service) BatchMyReactions(ctx context.Context, userID string, entityType EntityType, entityIDs []string) (map[string][]ReactionType, error) {
	if !entityType.Valid() {
		return nil, &ValidationError{Field: "entity_type", Message: "must be one of story, version, comment, bridge"}
	}
	if userID == "" || len(entityIDs) == 0 {
		return map[string][]ReactionType{}, nil
	}
	if !isUUID(userID) {
		return nil, &ValidationError{Field: "user_id", Message: "must be a UUID"}
	}

	held, err := s.store.MyReactions(ctx, userID, entityType, validIDs(entityIDs))
	if err != nil {
		return nil, fmt.Errorf("reactions: batch my reactions: %w", err)
	}
	if held == nil {
		return map[string][]ReactionType{}, nil
	}

	return held, nil
}

// actorAttributions loads and projects every distinct reactor in one call.
func (s *Service) actorAttributions(ctx context.Context, userIDs []string) map[string]Actor {
	unique := uniqueIDs(userIDs)
	if len(unique) == 0 {
		return nil
	}

	users, err := s.actors.UsersByIDs(ctx, unique)
	if err != nil {
		s.logger.WarnContext(ctx, "reaction actor enrichment failed", "error", err.Error())
		return nil
	}

	actors := make(map[string]Actor, len(users))
	for id, user := range users {
		if user == nil {
			continue
		}
		actors[id] = Actor{
			ID:          user.ID,
			DisplayName: user.DisplayName,
			AvatarURL:   avatarPath(user),
		}
	}

	return actors
}

// avatarPath builds the avatar URL a client should fetch, or "" when the user has
// no avatar. The query parameter carries the object's file name, so replacing an
// avatar yields a different URL (the same shape the rest of the API exposes).
func avatarPath(user *identity.User) string {
	if user == nil || user.AvatarURL == "" {
		return ""
	}
	return "/users/" + user.ID + "/avatar?v=" + path.Base(user.AvatarURL)
}

// uniqueIDs returns the distinct non-empty ids in first-seen order.
func uniqueIDs(ids []string) []string {
	seen := make(map[string]bool, len(ids))
	out := make([]string, 0, len(ids))

	for _, id := range ids {
		if id == "" || seen[id] {
			continue
		}
		seen[id] = true
		out = append(out, id)
	}

	return out
}

// validIDs drops ids that are not canonical UUIDs, so one malformed id in a page
// cannot fail the whole batched query (the `= ANY($n::uuid[])` cast would reject
// it). The affected row simply has no reactions in the result.
func validIDs(ids []string) []string {
	out := make([]string, 0, len(ids))
	for _, id := range ids {
		if isUUID(id) {
			out = append(out, id)
		}
	}
	return out
}

// validateToggle applies every input rule.
func validateToggle(in ToggleInput) error {
	if !isUUID(in.UserID) {
		return &ValidationError{Field: "user_id", Message: "must be a UUID"}
	}
	if !in.EntityType.Valid() {
		return &ValidationError{Field: "entity_type", Message: "must be one of story, version, comment, bridge"}
	}
	if !isUUID(in.EntityID) {
		return &ValidationError{Field: "entity_id", Message: "must be a UUID"}
	}
	if !in.ReactionType.Valid() {
		return &ValidationError{Field: "reaction_type", Message: "must be one of rings_true, know_it_differently, adds_something_new, needs_a_source"}
	}
	return nil
}

// isUUID reports whether s is a canonical 8-4-4-4-12 hexadecimal UUID string.
//
// It duplicates the unexported validator in the other domains rather than
// exporting one: the alternative would widen a package's API for a helper that is
// a few lines of character testing (KNOT-ADR-010).
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
