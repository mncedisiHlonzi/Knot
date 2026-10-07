-- 0002_stories.up.sql
--
-- Stories: the atomic content unit. Every later feature (Tell My People, Language
-- Tree, Conversations, Bridges) references stories.id.
-- Column reference: docs/DATA_MODEL.md

CREATE TABLE stories (
    id                   UUID PRIMARY KEY DEFAULT gen_random_uuid(),
    author_id            UUID NOT NULL REFERENCES users(id) ON DELETE CASCADE,
    pillar               TEXT NOT NULL CHECK (pillar IN ('wonder', 'heritage')),
    language             TEXT NOT NULL,
    title                TEXT NOT NULL,
    body                 TEXT NOT NULL,
    approximate_location TEXT NULL,
    media_urls           TEXT[] NOT NULL DEFAULT '{}',
    sensitive            BOOLEAN NOT NULL DEFAULT false,
    created_at           TIMESTAMPTZ NOT NULL DEFAULT now(),
    updated_at           TIMESTAMPTZ NOT NULL DEFAULT now()
);

-- The composite index is what cursor pagination orders and seeks by. It must match
-- the feed's ORDER BY (created_at DESC, id DESC) exactly.
CREATE INDEX stories_created_at_id_idx ON stories (created_at DESC, id DESC);

CREATE INDEX stories_author_id_idx ON stories (author_id);

CREATE INDEX stories_pillar_idx ON stories (pillar);
