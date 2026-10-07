-- 0003_story_versions.up.sql
--
-- Tell My People: story versions and the Language Tree.
--
-- Before this migration, stories held a single version of content directly
-- (language, title, body). This migration moves that content into
-- story_versions, gives every story a root version (parent_version_id IS NULL),
-- and lets humans adapt a story across languages by adding child versions.
--
-- The version tree is an adjacency list: each version points at its parent.
-- See KNOT-ADR-011 and KNOT-ADR-012 in docs/DECISIONS.md.
--
-- Column reference: docs/DATA_MODEL.md
--
-- The migration runner wraps this file in a transaction, so the four steps below
-- either all apply or none do.

-- 1. story_versions: one row per written version of a story.
CREATE TABLE story_versions (
    id                UUID PRIMARY KEY DEFAULT gen_random_uuid(),
    story_id          UUID NOT NULL REFERENCES stories(id) ON DELETE CASCADE,
    parent_version_id UUID NULL REFERENCES story_versions(id) ON DELETE CASCADE,
    author_id         UUID NOT NULL REFERENCES users(id) ON DELETE CASCADE,
    language          TEXT NOT NULL,
    title             TEXT NOT NULL,
    body              TEXT NOT NULL,
    adaptation_note   TEXT NULL,
    created_at        TIMESTAMPTZ NOT NULL DEFAULT now(),
    updated_at        TIMESTAMPTZ NOT NULL DEFAULT now(),
    CHECK (parent_version_id IS NULL OR parent_version_id <> id)
);

CREATE INDEX story_versions_story_id_idx ON story_versions (story_id);
CREATE INDEX story_versions_parent_version_id_idx ON story_versions (parent_version_id);
CREATE INDEX story_versions_author_id_idx ON story_versions (author_id);

-- The partial unique index enforces "exactly one root version per story": only
-- rows whose parent_version_id IS NULL participate in the uniqueness rule, so a
-- story may have many children but only one root.
CREATE UNIQUE INDEX story_versions_story_id_root_unique ON story_versions (story_id) WHERE parent_version_id IS NULL;

-- 2. Backfill: every existing story becomes its own root version, preserving its
--    original author and timestamps. This is a no-op on an empty database.
INSERT INTO story_versions (story_id, parent_version_id, author_id, language, title, body, created_at, updated_at)
SELECT id, NULL, author_id, language, title, body, created_at, updated_at FROM stories;

-- 3. stories.root_version_id points at the story's root version and is required.
ALTER TABLE stories ADD COLUMN root_version_id UUID;
UPDATE stories s SET root_version_id = v.id FROM story_versions v WHERE v.story_id = s.id AND v.parent_version_id IS NULL;
ALTER TABLE stories ALTER COLUMN root_version_id SET NOT NULL;
ALTER TABLE stories ADD CONSTRAINT stories_root_version_id_fkey FOREIGN KEY (root_version_id) REFERENCES story_versions(id) ON DELETE RESTRICT;
CREATE UNIQUE INDEX stories_root_version_id_unique ON stories (root_version_id);

-- 4. Content now lives in versions, not in stories. author_id is deliberately
--    kept: it remains the original author of the story.
ALTER TABLE stories DROP COLUMN title;
ALTER TABLE stories DROP COLUMN body;
ALTER TABLE stories DROP COLUMN language;
