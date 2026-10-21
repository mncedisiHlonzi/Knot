-- 0019_interactions.up.sql
--
-- The interaction log: one row for every significant thing a user does with a
-- piece of content (KNOT-026, task-local numbering; the platform ADR is
-- KNOT-ADR-075).
--
-- The relevance engine needs a durable, queryable record of engagement, not a
-- derived counter: a view is 1 point, a full read 3, a reaction 5, a comment 7,
-- and an adaptation 10. The weights are applied by the Go service
-- (internal/interactions) so the ladder lives in code where it is readable and
-- testable; the row stores the weight that was applied, so a future re-tuning
-- does not rewrite history.
--
-- `entity_type` and `interaction_type` are closed sets enforced here as well as
-- in Go: an unknown value is a programming error, not data. `entity_id` is a
-- bare UUID with no foreign key for the same reason `reactions.entity_id` has
-- none — one column cannot reference both `stories` and `story_versions`.
--
-- The user foreign key cascades: deleting an account deletes its log, which is
-- the privacy-preserving default (the log is behavioural data).
--
-- The composite index is what the ranker reads: one user's recent engagement,
-- newest first.
--
-- The migration runner wraps this file in a transaction, and every statement is
-- IF NOT EXISTS, so the file is safe to run twice by hand.

CREATE TABLE IF NOT EXISTS user_interactions (
    id                 UUID PRIMARY KEY DEFAULT gen_random_uuid(),
    user_id            UUID NOT NULL REFERENCES users(id) ON DELETE CASCADE,
    entity_type        TEXT NOT NULL CHECK (entity_type IN ('story', 'version')),
    entity_id          UUID NOT NULL,
    interaction_type   TEXT NOT NULL CHECK (
                           interaction_type IN ('view', 'read_full', 'reaction', 'comment', 'adapt')
                       ),
    interaction_weight REAL NOT NULL CHECK (interaction_weight > 0),
    created_at         TIMESTAMPTZ NOT NULL DEFAULT now()
);

CREATE INDEX IF NOT EXISTS user_interactions_user_created_idx
    ON user_interactions (user_id, created_at DESC);

-- Serves the engagement summary once it joins the log to entity_features: the
-- join is by entity id, and a story and a version can share neither id nor type.
CREATE INDEX IF NOT EXISTS user_interactions_entity_idx
    ON user_interactions (entity_type, entity_id);
