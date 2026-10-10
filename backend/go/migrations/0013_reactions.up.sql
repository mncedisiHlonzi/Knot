-- 0013_reactions — the four perspective reactions, and the reaction.created event.
--
-- A reaction is a perspective signal a reader leaves on a piece of content: it
-- "rings true", they "know it differently", it "adds something new", or it "needs
-- a source". They are not likes and they do not compete: one user may hold any
-- combination of the four on the same entity, which is what the unique index
-- below enforces (KNOT-ADR-050).
--
-- `entity_type` is a closed set enforced here; `entity_id` is a bare UUID with no
-- foreign key, because one column cannot reference four tables (story, version,
-- comment, bridge). Referential integrity is enforced by the application: every
-- toggle route resolves the entity through its own domain service first.
--
-- `ON DELETE CASCADE` is on `user_id` only. When an entity is deleted (rare at
-- MVP) its reactions are left behind as orphans; a sweep is a future task
-- (disclosed in KNOT-ADR-050).
--
-- This migration also widens the notifications `event_type` CHECK to admit
-- `reaction.created`. The event set is closed by that CHECK, so the constraint
-- must change in the same migration that starts writing the new event.
--
-- The migration runner wraps this file in a transaction. Everything here is
-- idempotent (IF NOT EXISTS and catalogue guards), so the file is safe to run
-- twice by hand.

CREATE TABLE IF NOT EXISTS reactions (
	id            UUID PRIMARY KEY DEFAULT gen_random_uuid(),
	user_id       UUID NOT NULL REFERENCES users(id) ON DELETE CASCADE,
	entity_type   TEXT NOT NULL CHECK (entity_type IN ('story', 'version', 'comment', 'bridge')),
	entity_id     UUID NOT NULL,
	reaction_type TEXT NOT NULL CHECK (reaction_type IN
	                  ('rings_true', 'know_it_differently', 'adds_something_new', 'needs_a_source')),
	created_at    TIMESTAMPTZ NOT NULL DEFAULT now()
);

-- One user may hold each of the four signals on an entity at most once. This is
-- also the toggle's conflict target, so an insert of the same signal is a no-op.
CREATE UNIQUE INDEX IF NOT EXISTS reactions_unique_per_user_entity_type
	ON reactions (user_id, entity_type, entity_id, reaction_type);

-- Serves the count aggregation for one entity, and the batched aggregation for a
-- page of entities (`entity_type = $1 AND entity_id = ANY($2)`).
CREATE INDEX IF NOT EXISTS reactions_entity_idx
	ON reactions (entity_type, entity_id);

-- Serves "which of these did I react to", the my_reactions enrichment.
CREATE INDEX IF NOT EXISTS reactions_user_idx
	ON reactions (user_id);

-- Widen the notifications event set. The 0010 constraint is inline, so PostgreSQL
-- named it; it is dropped by name when present and by definition otherwise, so a
-- differently-named constraint is still replaced rather than left in place.
ALTER TABLE notifications DROP CONSTRAINT IF EXISTS notifications_event_type_check;

DO $$
DECLARE
	existing text;
BEGIN
	SELECT conname INTO existing
	FROM pg_constraint
	WHERE conrelid = 'notifications'::regclass
	  AND contype = 'c'
	  AND pg_get_constraintdef(oid) LIKE '%event_type%';

	IF existing IS NOT NULL THEN
		EXECUTE format('ALTER TABLE notifications DROP CONSTRAINT %I', existing);
	END IF;

	IF NOT EXISTS (
		SELECT 1
		FROM pg_constraint
		WHERE conname = 'notifications_event_type_check'
		  AND conrelid = 'notifications'::regclass
	) THEN
		ALTER TABLE notifications
			ADD CONSTRAINT notifications_event_type_check
			CHECK (event_type IN ('version.created', 'comment.created', 'bridge.created', 'reaction.created'));
	END IF;
END
$$;

-- The inbox names the signal that was left (for example, "Rings true on your
-- story"), so a reaction.created notification records which of the four it was.
-- It is NULL for every other event, and the CHECK keeps it inside the closed set.
ALTER TABLE notifications ADD COLUMN IF NOT EXISTS reaction_type TEXT NULL;

DO $$
BEGIN
	IF NOT EXISTS (
		SELECT 1
		FROM pg_constraint
		WHERE conname = 'notifications_reaction_type_check'
		  AND conrelid = 'notifications'::regclass
	) THEN
		ALTER TABLE notifications
			ADD CONSTRAINT notifications_reaction_type_check
			CHECK (reaction_type IS NULL OR reaction_type IN
				('rings_true', 'know_it_differently', 'adds_something_new', 'needs_a_source'));
	END IF;
END
$$;
