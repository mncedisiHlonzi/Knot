-- 0004_conversations.up.sql
--
-- Bridges and cross-language conversations: comments on a story version, and the
-- bridges that connect a comment in one language to a comment in another.
--
-- A bridge creates a new comment (a row in comments, on the same version as the
-- source) plus a bridges row that references both comments. The source
-- conversation is left intact; the bridge is a first-class object.
-- See KNOT-ADR-014 and KNOT-ADR-015 in docs/DECISIONS.md.
--
-- Column reference: docs/DATA_MODEL.md
--
-- The migration runner wraps this file in a transaction, so both tables either
-- appear or neither does.

CREATE TABLE comments (
    id         UUID PRIMARY KEY DEFAULT gen_random_uuid(),
    version_id UUID NOT NULL REFERENCES story_versions(id) ON DELETE CASCADE,
    author_id  UUID NOT NULL REFERENCES users(id) ON DELETE CASCADE,
    language   TEXT NOT NULL,
    body       TEXT NOT NULL,
    created_at TIMESTAMPTZ NOT NULL DEFAULT now(),
    updated_at TIMESTAMPTZ NOT NULL DEFAULT now()
);

-- The composite index is what comment pagination orders and seeks by. It must
-- match the thread's ORDER BY (created_at DESC, id DESC) within one version.
CREATE INDEX comments_version_id_created_at_idx ON comments (version_id, created_at DESC, id DESC);
CREATE INDEX comments_author_id_idx ON comments (author_id);
CREATE INDEX comments_language_idx ON comments (language);

CREATE TABLE bridges (
    id                UUID PRIMARY KEY DEFAULT gen_random_uuid(),
    source_comment_id UUID NOT NULL REFERENCES comments(id) ON DELETE CASCADE,
    target_comment_id UUID NOT NULL REFERENCES comments(id) ON DELETE CASCADE,
    author_id         UUID NOT NULL REFERENCES users(id) ON DELETE CASCADE,
    target_language   TEXT NOT NULL,
    adaptation_note   TEXT NULL,
    created_at        TIMESTAMPTZ NOT NULL DEFAULT now(),
    CHECK (source_comment_id <> target_comment_id)
);

CREATE INDEX bridges_source_comment_id_idx ON bridges (source_comment_id);
CREATE INDEX bridges_target_comment_id_idx ON bridges (target_comment_id);
CREATE INDEX bridges_author_id_idx ON bridges (author_id);

-- A given pair of comments can be bridged at most once.
CREATE UNIQUE INDEX bridges_unique_pair ON bridges (source_comment_id, target_comment_id);

-- A source comment can be bridged into any one language at most once. Without
-- this, the same comment could be bridged into French over and over, producing
-- many near-duplicate targets claiming to be the same adaptation.
CREATE UNIQUE INDEX bridges_one_per_target_language ON bridges (source_comment_id, target_language);
