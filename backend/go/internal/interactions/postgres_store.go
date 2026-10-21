package interactions

import (
	"context"
	"fmt"
	"sort"
	"time"

	"github.com/jackc/pgx/v5/pgxpool"
)

// PostgresStore is the pgx-backed implementation of Store.
//
// It holds an injected pool and no other state, so it is safe for concurrent use.
type PostgresStore struct {
	pool *pgxpool.Pool
}

// Compile-time proof that the store satisfies the read contract.
var _ Store = (*PostgresStore)(nil)

// Compile-time proof that the store satisfies the write contract.
var _ LogStore = (*PostgresStore)(nil)

// NewPostgresStore returns a store backed by pool. The pool is injected rather
// than reached for globally so that callers own its lifecycle.
func NewPostgresStore(pool *pgxpool.Pool) (*PostgresStore, error) {
	if pool == nil {
		return nil, fmt.Errorf("interactions: postgres store requires a non-nil pool")
	}
	return &PostgresStore{pool: pool}, nil
}

// reactionKey identifies one reaction entity. The entity id alone is not enough:
// the same UUID could, in principle, name both a story and a version, and the
// entity type is part of the reactions table's key.
type reactionKey struct {
	entityType string
	entityID   string
}

// Latest returns at most the newest reaction and the newest comment for each
// target, merged newest-first.
//
// It issues exactly two statements per call — one over `reactions` for every
// target's reaction entity, and one over `comments` for every target's
// conversation version — regardless of how many entries the page holds, so a
// page is never an N+1 (KNOT-028).
//
// An entity with no interactions is absent from the result. A malformed or empty
// target is skipped rather than failing the page: social proof is enrichment and
// must never make the feed unreadable.
func (s *PostgresStore) Latest(ctx context.Context, targets []Target) (map[string][]Interaction, error) {
	result := make(map[string][]Interaction, len(targets))
	if len(targets) == 0 {
		return result, nil
	}

	// Deduplicate the two id sets, and remember which feed key each row belongs
	// to. A story and an adaptation cannot share a key, but two targets could name
	// the same reaction entity in a malformed page; the first one wins.
	keyByReaction := make(map[reactionKey]string, len(targets))
	keyByVersion := make(map[string]string, len(targets))
	reactionTypes := make([]string, 0, len(targets))
	reactionIDs := make([]string, 0, len(targets))
	commentVersionIDs := make([]string, 0, len(targets))

	for _, target := range targets {
		if target.Key == "" {
			continue
		}

		pair := reactionKey{
			entityType: string(target.ReactionEntityType),
			entityID:   target.ReactionEntityID,
		}
		if pair.entityType != "" && pair.entityID != "" {
			if _, seen := keyByReaction[pair]; !seen {
				keyByReaction[pair] = target.Key
				reactionTypes = append(reactionTypes, pair.entityType)
				reactionIDs = append(reactionIDs, pair.entityID)
			}
		}

		if target.CommentVersionID != "" {
			if _, seen := keyByVersion[target.CommentVersionID]; !seen {
				keyByVersion[target.CommentVersionID] = target.Key
				commentVersionIDs = append(commentVersionIDs, target.CommentVersionID)
			}
		}
	}

	byKey := make(map[string][]Interaction, len(targets))

	if len(reactionIDs) > 0 {
		// `DISTINCT ON` keeps the newest reaction per entity; the id is the
		// deterministic tie-break for two rows written in the same transaction.
		const reactionQuery = `
			SELECT DISTINCT ON (r.entity_type, r.entity_id)
			       r.entity_type, r.entity_id, r.id, r.user_id, r.reaction_type, r.created_at
			FROM reactions r
			JOIN unnest($1::text[], $2::uuid[]) AS target(entity_type, entity_id)
			  ON r.entity_type = target.entity_type
			 AND r.entity_id = target.entity_id
			ORDER BY r.entity_type, r.entity_id, r.created_at DESC, r.id DESC`

		rows, err := s.pool.Query(ctx, reactionQuery, reactionTypes, reactionIDs)
		if err != nil {
			return nil, fmt.Errorf("interactions: latest reactions: %w", err)
		}

		for rows.Next() {
			var (
				entityType   string
				entityID     string
				id           string
				userID       string
				reactionType string
				createdAt    time.Time
			)
			if err := rows.Scan(&entityType, &entityID, &id, &userID, &reactionType, &createdAt); err != nil {
				rows.Close()
				return nil, fmt.Errorf("interactions: scan latest reaction: %w", err)
			}

			key := keyByReaction[reactionKey{entityType: entityType, entityID: entityID}]
			byKey[key] = append(byKey[key], Interaction{
				Kind:      KindReaction,
				ActorID:   userID,
				Detail:    reactionType,
				CreatedAt: createdAt,
				ID:        id,
			})
		}
		if err := rows.Err(); err != nil {
			rows.Close()
			return nil, fmt.Errorf("interactions: latest reactions: %w", err)
		}
		rows.Close()
	}

	if len(commentVersionIDs) > 0 {
		// Replies are comments too: the conversation a card opens shows them under
		// their parent, so the newest reply is as much a social signal as a new
		// top-level comment. The version index serves the ORDER BY exactly.
		const commentQuery = `
			SELECT DISTINCT ON (c.version_id)
			       c.version_id, c.id, c.author_id, c.created_at
			FROM comments c
			WHERE c.version_id = ANY($1::uuid[])
			ORDER BY c.version_id, c.created_at DESC, c.id DESC`

		rows, err := s.pool.Query(ctx, commentQuery, commentVersionIDs)
		if err != nil {
			return nil, fmt.Errorf("interactions: latest comments: %w", err)
		}

		for rows.Next() {
			var (
				versionID string
				id        string
				authorID  string
				createdAt time.Time
			)
			if err := rows.Scan(&versionID, &id, &authorID, &createdAt); err != nil {
				rows.Close()
				return nil, fmt.Errorf("interactions: scan latest comment: %w", err)
			}

			key := keyByVersion[versionID]
			byKey[key] = append(byKey[key], Interaction{
				Kind:      KindComment,
				ActorID:   authorID,
				CreatedAt: createdAt,
				ID:        id,
			})
		}
		if err := rows.Err(); err != nil {
			rows.Close()
			return nil, fmt.Errorf("interactions: latest comments: %w", err)
		}
		rows.Close()
	}

	// Merge the two kinds for each entry and keep the newest two. At most one of
	// each kind exists here, so this is a two-element sort; the explicit truncate
	// documents the contract rather than guarding an overflow.
	for key, list := range byKey {
		sort.SliceStable(list, func(i, j int) bool {
			if !list[i].CreatedAt.Equal(list[j].CreatedAt) {
				return list[i].CreatedAt.After(list[j].CreatedAt)
			}
			return list[i].ID > list[j].ID
		})
		if len(list) > 2 {
			list = list[:2]
		}
		result[key] = list
	}

	return result, nil
}

// Insert appends one interaction row.
//
// The weight is written explicitly (cast to the column's REAL) rather than
// recomputed in SQL, so the row records the ladder that was in force when the
// action happened. The caller has already resolved it through Service.Log.
func (s *PostgresStore) Insert(ctx context.Context, record Record) error {
	const query = `
		INSERT INTO user_interactions
			(user_id, entity_type, entity_id, interaction_type, interaction_weight)
		VALUES ($1, $2, $3, $4, $5::real)`

	if _, err := s.pool.Exec(
		ctx,
		query,
		record.UserID,
		record.EntityType,
		record.EntityID,
		string(record.Type),
		record.Weight,
	); err != nil {
		return fmt.Errorf("interactions: insert: %w", err)
	}

	return nil
}

// PillarEngagement returns the total interaction weight one user has spent on
// content in each pillar, keyed by pillar.
//
// The log records the entity, not its pillar, so the sum joins `entity_features`
// — the read model that denormalises pillar for stories and versions. Matching
// on both id and type keeps the join honest if a story and a version ever share
// an id. A user with no logged engagement gets an empty map, which the relevance
// package reads as "no affinity yet" rather than as an error (KNOT-026).
func (s *PostgresStore) PillarEngagement(ctx context.Context, userID string) (map[string]float64, error) {
	const query = `
		SELECT ef.pillar, SUM(ui.interaction_weight)::float8
		FROM user_interactions ui
		JOIN entity_features ef
		  ON ef.entity_id = ui.entity_id
		 AND ef.entity_type = ui.entity_type
		WHERE ui.user_id = $1
		  AND ef.pillar IS NOT NULL
		GROUP BY ef.pillar`

	rows, err := s.pool.Query(ctx, query, userID)
	if err != nil {
		return nil, fmt.Errorf("interactions: pillar engagement: %w", err)
	}
	defer rows.Close()

	weights := make(map[string]float64)
	for rows.Next() {
		var (
			pillar string
			weight float64
		)
		if err := rows.Scan(&pillar, &weight); err != nil {
			return nil, fmt.Errorf("interactions: scan pillar engagement: %w", err)
		}
		weights[pillar] = weight
	}
	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("interactions: pillar engagement: %w", err)
	}

	return weights, nil
}
