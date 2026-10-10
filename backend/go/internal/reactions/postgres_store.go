package reactions

import (
	"context"
	"fmt"

	"github.com/jackc/pgx/v5/pgxpool"
)

// reactionColumns is the canonical SELECT column list, kept in the order
// scanReaction reads.
const reactionColumns = `id, user_id, entity_type, entity_id, reaction_type, created_at`

// PostgresStore is the pgx-backed implementation of ReactionStore.
//
// It holds an injected pool and no other state, so it is safe for concurrent use.
type PostgresStore struct {
	pool *pgxpool.Pool
}

// Compile-time proof that the store satisfies the domain contract.
var _ ReactionStore = (*PostgresStore)(nil)

// NewPostgresStore returns a store backed by pool. The pool is injected rather
// than reached for globally so that callers own its lifecycle.
func NewPostgresStore(pool *pgxpool.Pool) (*PostgresStore, error) {
	if pool == nil {
		return nil, fmt.Errorf("reactions: postgres store requires a non-nil pool")
	}
	return &PostgresStore{pool: pool}, nil
}

// Toggle adds the reaction when it is absent and removes it when it is present.
//
// Both writes run in one transaction so the entity is never observed with the
// signal half-applied. The insert relies on the unique index as its conflict
// target: a row it does not create already exists, so the same call deletes it.
func (s *PostgresStore) Toggle(ctx context.Context, reaction Reaction) (bool, error) {
	tx, err := s.pool.Begin(ctx)
	if err != nil {
		return false, fmt.Errorf("reactions: begin toggle: %w", err)
	}
	// A rollback after a successful commit is a no-op, so this covers every
	// early return.
	defer func() { _ = tx.Rollback(ctx) }()

	const insert = `
		INSERT INTO reactions (user_id, entity_type, entity_id, reaction_type)
		VALUES ($1, $2, $3, $4)
		ON CONFLICT (user_id, entity_type, entity_id, reaction_type) DO NOTHING`

	tag, err := tx.Exec(
		ctx,
		insert,
		reaction.UserID,
		string(reaction.EntityType),
		reaction.EntityID,
		string(reaction.ReactionType),
	)
	if err != nil {
		return false, fmt.Errorf("reactions: insert: %w", err)
	}

	if tag.RowsAffected() == 1 {
		if err := tx.Commit(ctx); err != nil {
			return false, fmt.Errorf("reactions: commit toggle: %w", err)
		}
		return true, nil
	}

	const remove = `
		DELETE FROM reactions
		WHERE user_id = $1 AND entity_type = $2 AND entity_id = $3 AND reaction_type = $4`

	if _, err := tx.Exec(
		ctx,
		remove,
		reaction.UserID,
		string(reaction.EntityType),
		reaction.EntityID,
		string(reaction.ReactionType),
	); err != nil {
		return false, fmt.Errorf("reactions: delete: %w", err)
	}

	if err := tx.Commit(ctx); err != nil {
		return false, fmt.Errorf("reactions: commit toggle: %w", err)
	}

	return false, nil
}

// ListForEntity returns every reaction on one entity, newest first. A malformed
// entity id cannot name a row, so it returns an empty list rather than an error.
func (s *PostgresStore) ListForEntity(ctx context.Context, entityType EntityType, entityID string) ([]Reaction, error) {
	if !isUUID(entityID) {
		return []Reaction{}, nil
	}

	query := `SELECT ` + reactionColumns + `
		FROM reactions
		WHERE entity_type = $1 AND entity_id = $2
		ORDER BY created_at DESC, id DESC`

	rows, err := s.pool.Query(ctx, query, string(entityType), entityID)
	if err != nil {
		return nil, fmt.Errorf("reactions: list for entity: %w", err)
	}
	defer rows.Close()

	list := make([]Reaction, 0, 8)
	for rows.Next() {
		reaction, err := scanReaction(rows)
		if err != nil {
			return nil, fmt.Errorf("reactions: list for entity: %w", err)
		}
		list = append(list, reaction)
	}
	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("reactions: list for entity: %w", err)
	}

	return list, nil
}

// Summaries returns the counts for each of the given entity ids of one type, in
// one grouped query. An entity with no reactions has no entry in the result.
func (s *PostgresStore) Summaries(ctx context.Context, entityType EntityType, entityIDs []string) (map[string]Summary, error) {
	if len(entityIDs) == 0 {
		return map[string]Summary{}, nil
	}

	const query = `
		SELECT entity_id, reaction_type, count(*)
		FROM reactions
		WHERE entity_type = $1 AND entity_id = ANY($2::uuid[])
		GROUP BY entity_id, reaction_type`

	rows, err := s.pool.Query(ctx, query, string(entityType), entityIDs)
	if err != nil {
		return nil, fmt.Errorf("reactions: summaries: %w", err)
	}
	defer rows.Close()

	summaries := make(map[string]Summary, len(entityIDs))
	for rows.Next() {
		var entityID string
		var reactionType string
		var count int
		if err := rows.Scan(&entityID, &reactionType, &count); err != nil {
			return nil, fmt.Errorf("reactions: summaries: %w", err)
		}

		summary := summaries[entityID]
		summary.addCount(ReactionType(reactionType), count)
		summaries[entityID] = summary
	}
	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("reactions: summaries: %w", err)
	}

	return summaries, nil
}

// MyReactions returns, for one user, the signals they hold on each of the given
// entity ids of one type, in one query. An entity the user has not reacted to has
// no entry.
func (s *PostgresStore) MyReactions(ctx context.Context, userID string, entityType EntityType, entityIDs []string) (map[string][]ReactionType, error) {
	if len(entityIDs) == 0 {
		return map[string][]ReactionType{}, nil
	}

	const query = `
		SELECT entity_id, reaction_type
		FROM reactions
		WHERE user_id = $1 AND entity_type = $2 AND entity_id = ANY($3::uuid[])
		ORDER BY created_at ASC, id ASC`

	rows, err := s.pool.Query(ctx, query, userID, string(entityType), entityIDs)
	if err != nil {
		return nil, fmt.Errorf("reactions: my reactions: %w", err)
	}
	defer rows.Close()

	held := make(map[string][]ReactionType, len(entityIDs))
	for rows.Next() {
		var entityID string
		var reactionType string
		if err := rows.Scan(&entityID, &reactionType); err != nil {
			return nil, fmt.Errorf("reactions: my reactions: %w", err)
		}
		held[entityID] = append(held[entityID], ReactionType(reactionType))
	}
	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("reactions: my reactions: %w", err)
	}

	return held, nil
}

// rowScanner is the minimal surface scanReaction needs, satisfied by both
// pgx.Row and pgx.Rows.
type rowScanner interface {
	Scan(dest ...any) error
}

// scanReaction reads one reaction row in reactionColumns order.
func scanReaction(row rowScanner) (Reaction, error) {
	var reaction Reaction
	var entityType string
	var reactionType string

	if err := row.Scan(
		&reaction.ID,
		&reaction.UserID,
		&entityType,
		&reaction.EntityID,
		&reactionType,
		&reaction.CreatedAt,
	); err != nil {
		return Reaction{}, err
	}

	reaction.EntityType = EntityType(entityType)
	reaction.ReactionType = ReactionType(reactionType)

	return reaction, nil
}

// addCount adds n to the count for one signal type. It is the batched-aggregation
// counterpart of Summary.Add, which adds one at a time.
func (s *Summary) addCount(t ReactionType, n int) {
	switch t {
	case RingsTrue:
		s.RingsTrue += n
	case KnowItDifferently:
		s.KnowItDifferently += n
	case AddsSomethingNew:
		s.AddsSomethingNew += n
	case NeedsASource:
		s.NeedsASource += n
	}
}
