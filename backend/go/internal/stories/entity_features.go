package stories

import (
	"context"
	"fmt"
)

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
