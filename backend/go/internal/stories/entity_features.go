package stories

import (
	"context"
	"fmt"

	"github.com/jackc/pgx/v5/pgconn"
)

// The entity-type values `entity_features` accepts. They mirror the table's
// CHECK constraint so Go and SQL cannot disagree.
const (
	// EntityTypeStory is a story row, keyed by the story id.
	EntityTypeStory = "story"
	// EntityTypeVersion is a story version, keyed by the version id.
	EntityTypeVersion = "version"
)

// featureExecer is the subset of pgx shared by the pool and a transaction, so
// the same upsert runs either standalone or inside a content write.
type featureExecer interface {
	Exec(ctx context.Context, sql string, arguments ...any) (pgconn.CommandTag, error)
}

// featureWriter is the upsert the create paths call. It is a function type (and
// a field on the store) so a test can simulate a write failure and prove the
// content write rolls back with it; production never replaces the default.
type featureWriter func(ctx context.Context, exec featureExecer, entityID, entityType string) error

// upsertStoryFeatureSQL rebuilds one story's feature row from the source tables.
//
// The SELECT (rather than values passed from Go) is deliberate: the row is the
// same projection migration 0020 and BackfillEntityFeatures write, so a story's
// language, pillar and place can never disagree with the read model's own
// definition. An upsert rather than an insert makes a re-run idempotent.
const upsertStoryFeatureSQL = `
	INSERT INTO entity_features (entity_id, entity_type, language, pillar, place_lower, updated_at)
	SELECT s.id, 'story', v.language, s.pillar::text, lower(s.approximate_location), now()
	FROM stories s
	JOIN story_versions v ON v.id = s.root_version_id
	WHERE s.id = $1
	ON CONFLICT (entity_id) DO UPDATE SET
		entity_type = EXCLUDED.entity_type,
		language = EXCLUDED.language,
		pillar = EXCLUDED.pillar,
		place_lower = EXCLUDED.place_lower,
		updated_at = now()`

// upsertVersionFeatureSQL rebuilds one version's feature row: its own language,
// and its story's pillar and place.
const upsertVersionFeatureSQL = `
	INSERT INTO entity_features (entity_id, entity_type, language, pillar, place_lower, updated_at)
	SELECT v.id, 'version', v.language, s.pillar::text, lower(s.approximate_location), now()
	FROM story_versions v
	JOIN stories s ON s.id = v.story_id
	WHERE v.id = $1
	ON CONFLICT (entity_id) DO UPDATE SET
		entity_type = EXCLUDED.entity_type,
		language = EXCLUDED.language,
		pillar = EXCLUDED.pillar,
		place_lower = EXCLUDED.place_lower,
		updated_at = now()`

// UpsertEntityFeature writes one story's or version's row in the read model.
//
// It is the standalone form of the write the create paths make; those call it on
// their own transaction instead. A blank or unknown entity type is rejected
// before SQL runs, and an id that names no row writes nothing (the SELECT yields
// no rows) rather than failing.
func (s *PostgresStore) UpsertEntityFeature(ctx context.Context, entityID, entityType string) error {
	return upsertEntityFeature(ctx, s.pool, entityID, entityType)
}

// upsertEntityFeature runs the projection for one entity on exec.
func upsertEntityFeature(ctx context.Context, exec featureExecer, entityID, entityType string) error {
	var query string
	switch entityType {
	case EntityTypeStory:
		query = upsertStoryFeatureSQL
	case EntityTypeVersion:
		query = upsertVersionFeatureSQL
	default:
		return fmt.Errorf("stories: unknown entity type %q", entityType)
	}

	if _, err := exec.Exec(ctx, query, entityID); err != nil {
		return fmt.Errorf("stories: upsert %s feature: %w", entityType, err)
	}

	return nil
}

// EntityFeature is one row of the entity_features read model.
//
// The read model denormalises a story's or version's language, pillar and place
// so ranking can join one cheap row per candidate instead of reaching across the
// content tables (KNOT-026).
type EntityFeature struct {
	// EntityID is the story id or the version id.
	EntityID string
	// EntityType is "story" or "version".
	EntityType string
	// Language is the entity's ISO 639-3 code, empty when unknown.
	Language string
	// Pillar is the story's pillar, empty when unknown.
	Pillar string
	// PlaceLower is the normalised approximate place, empty when none.
	PlaceLower string
}

// FeaturesFor loads the read-model rows for the given entity ids in **one**
// query, keyed by entity id.
//
// It is the batched read the feed's ranking pass uses: a page of candidates is
// enriched with one statement, never one per candidate. An id with no row is
// simply absent from the map, which the caller reads as "fall back to the feed
// item's own fields" — entities written after the last backfill have no row yet
// (KNOT-026, KNOT-027). A malformed id cannot name a row, so the statement
// yields nothing for it rather than failing the page.
func (s *PostgresStore) FeaturesFor(ctx context.Context, entityIDs []string) (map[string]EntityFeature, error) {
	if len(entityIDs) == 0 {
		return map[string]EntityFeature{}, nil
	}

	const query = `
		SELECT entity_id::text, entity_type, language, pillar, place_lower
		FROM entity_features
		WHERE entity_id = ANY($1::uuid[])`

	rows, err := s.pool.Query(ctx, query, entityIDs)
	if err != nil {
		return nil, fmt.Errorf("stories: features for: %w", err)
	}
	defer rows.Close()

	features := make(map[string]EntityFeature, len(entityIDs))
	for rows.Next() {
		var (
			feature  EntityFeature
			language *string
			pillar   *string
			placeLow *string
		)
		if err := rows.Scan(&feature.EntityID, &feature.EntityType, &language, &pillar, &placeLow); err != nil {
			return nil, fmt.Errorf("stories: scan feature: %w", err)
		}
		feature.Language = textOrEmpty(language)
		feature.Pillar = textOrEmpty(pillar)
		feature.PlaceLower = textOrEmpty(placeLow)
		features[feature.EntityID] = feature
	}
	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("stories: features for: %w", err)
	}

	return features, nil
}

// textOrEmpty dereferences a nullable text column, mapping NULL onto "".
func textOrEmpty(value *string) string {
	if value == nil {
		return ""
	}
	return *value
}
