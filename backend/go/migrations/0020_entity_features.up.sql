-- 0020_entity_features.up.sql
--
-- The entity feature read model (KNOT-026; platform ADR KNOT-ADR-075).
--
-- A story's language, pillar and place live in two different tables: the pillar
-- and place on `stories`, the language on its root version. A version's language
-- is its own, while its pillar and place belong to the story it adapts. The
-- relevance engine and, later, an ML model want one flat, cheap row per entity
-- to join against, so this table denormalises exactly those three features.
--
-- It is a read model, not a source of truth: it is derived data, rebuilt by
-- `stories.PostgresStore.BackfillEntityFeatures` and (in a later task) maintained
-- on write. `entity_id` is the primary key because an entity appears at most
-- once; there is no foreign key, because the column names either a story or a
-- version.
--
-- This migration also performs the initial backfill for every existing story and
-- version, so the table is usable the moment it exists. The SELECT is wrapped in
-- a derived table because a bare `INSERT ... SELECT ... UNION ALL ... ON
-- CONFLICT` is ambiguous to the parser (the ON can be read as part of the last
-- SELECT's join syntax).
--
-- The migration runner wraps this file in a transaction, and every statement is
-- IF NOT EXISTS / ON CONFLICT, so the file is safe to run twice by hand.

CREATE TABLE IF NOT EXISTS entity_features (
    entity_id   UUID PRIMARY KEY,
    entity_type TEXT NOT NULL CHECK (entity_type IN ('story', 'version')),
    language    TEXT NULL,
    pillar      TEXT NULL,
    place_lower TEXT NULL,
    updated_at  TIMESTAMPTZ NOT NULL DEFAULT now()
);

INSERT INTO entity_features (entity_id, entity_type, language, pillar, place_lower, updated_at)
SELECT
    features.entity_id,
    features.entity_type,
    features.language,
    features.pillar,
    features.place_lower,
    now()
FROM (
    -- A story carries its root version's language, and its own pillar and place.
    SELECT
        s.id AS entity_id,
        'story'::text AS entity_type,
        v.language AS language,
        s.pillar::text AS pillar,
        lower(s.approximate_location) AS place_lower
    FROM stories s
    JOIN story_versions v ON v.id = s.root_version_id

    UNION ALL

    -- A version carries its own language, and its story's pillar and place.
    SELECT
        v.id AS entity_id,
        'version'::text AS entity_type,
        v.language AS language,
        s.pillar::text AS pillar,
        lower(s.approximate_location) AS place_lower
    FROM story_versions v
    JOIN stories s ON s.id = v.story_id
) AS features
ON CONFLICT (entity_id) DO UPDATE SET
    entity_type = EXCLUDED.entity_type,
    language = EXCLUDED.language,
    pillar = EXCLUDED.pillar,
    place_lower = EXCLUDED.place_lower,
    updated_at = now();
