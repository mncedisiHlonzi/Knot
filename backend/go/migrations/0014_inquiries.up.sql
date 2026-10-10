-- 0014_inquiries.up.sql
--
-- Curious Inquiries: a question about a place, and the public answers to it.
-- Column reference: docs/DATA_MODEL.md, KNOT-ADR-055, KNOT-ADR-056.
--
-- An inquiry is a question someone asks about a place ("Why do the cattle come
-- home at the same hour every evening in Manguzi?"). An answer is a public,
-- attributed reply from another person. Inquiries are deliberately unlike stories:
--
--   * they are never *authored content about a place* but a *question asked of*
--     the people rooted there, so they route to the first Rooted users of that
--     place rather than to a feed (KNOT-ADR-056);
--   * they are always public and always attributed — there is no anonymous
--     inquiry and no anonymous answer (KNOT-ADR-055);
--   * they stay open forever: there is no accepted answer and no closing, because
--     a place's knowledge is plural and an early answer is not the final word
--     (KNOT-ADR-055).
--
-- `answer_count` is denormalised onto the inquiry so the list and the detail can
-- show "3 answers" without a correlated COUNT per row. It is maintained in
-- application code inside the same transaction that inserts the answer, under a
-- row lock on the inquiry (the KNOT-ADR-054 pattern), so two answers arriving at
-- once cannot both write the same count.
--
-- `language` is an ISO 639-3 code validated by the application against
-- internal/language (KNOT-ADR-046). It is not constrained by a CHECK here because
-- the canonical list is compile-time data the application owns; the notifications
-- and reactions migrations take the same position.
--
-- `place` is optional (NULL for a question that is not about a particular place)
-- and, like a Rooted signal's place, is city-or-region precision free text bounded
-- by the application. `latitude`/`longitude` are the structured coordinate the
-- mobile location picker resolved, and the service stores them together or not at
-- all (KNOT-ADR-034).
--
-- Every index is created with IF NOT EXISTS and every constraint is added behind a
-- pg_constraint guard, so this file can be re-applied by hand without erroring.
--
-- The migration runner wraps this file in a transaction, so the tables, their
-- indexes, and the widened notification CHECKs either all appear or none do.

CREATE TABLE IF NOT EXISTS inquiries (
	id            UUID PRIMARY KEY DEFAULT gen_random_uuid(),
	author_id     UUID NOT NULL REFERENCES users (id) ON DELETE CASCADE,
	title         TEXT NOT NULL,
	body          TEXT NOT NULL,
	language      TEXT NOT NULL,
	place         TEXT NULL,
	place_country TEXT NULL,
	latitude      DOUBLE PRECISION NULL,
	longitude     DOUBLE PRECISION NULL,
	answer_count  INTEGER NOT NULL DEFAULT 0,
	created_at    TIMESTAMPTZ NOT NULL DEFAULT now(),
	updated_at    TIMESTAMPTZ NOT NULL DEFAULT now()
);

-- Serves GET /inquiries, which is the (created_at DESC, id DESC) keyset order.
CREATE INDEX IF NOT EXISTS inquiries_created_at_id_idx
	ON inquiries (created_at DESC, id DESC);

-- Serves the ?place= filter on GET /inquiries, and the place a detail response
-- names.
CREATE INDEX IF NOT EXISTS inquiries_place_idx
	ON inquiries (place);

-- Serves a user's own inquiries, and the wall's inquiry branch.
CREATE INDEX IF NOT EXISTS inquiries_author_id_idx
	ON inquiries (author_id);

CREATE TABLE IF NOT EXISTS inquiry_answers (
	id         UUID PRIMARY KEY DEFAULT gen_random_uuid(),
	inquiry_id UUID NOT NULL REFERENCES inquiries (id) ON DELETE CASCADE,
	author_id  UUID NOT NULL REFERENCES users (id) ON DELETE CASCADE,
	language   TEXT NOT NULL,
	body       TEXT NOT NULL,
	created_at TIMESTAMPTZ NOT NULL DEFAULT now(),
	updated_at TIMESTAMPTZ NOT NULL DEFAULT now()
);

-- Serves GET /inquiries/{id}/answers, which reads oldest first: an answer thread
-- reads as a conversation, so the earliest answer leads.
CREATE INDEX IF NOT EXISTS inquiry_answers_inquiry_created_idx
	ON inquiry_answers (inquiry_id, created_at ASC, id ASC);

-- Serves a user's own answers, and the wall's inquiry_answer branch.
CREATE INDEX IF NOT EXISTS inquiry_answers_author_id_idx
	ON inquiry_answers (author_id);

-- Rooted routing. Finding "the first Rooted users of this place" needs an index
-- on the place a signal declares; 0005 rooted the table on user_id only, because
-- every read until now was by user (KNOT-ADR-056).
CREATE INDEX IF NOT EXISTS rooted_signals_place_idx
	ON rooted_signals (place);

-- Widen the notifications event set with the two inquiry events. The 0010
-- constraint is inline, so PostgreSQL named it; it is dropped by name when present
-- and by definition otherwise, so a differently-named constraint is still replaced
-- rather than left in place (the 0013 pattern).
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
			CHECK (event_type IN (
				'version.created',
				'comment.created',
				'bridge.created',
				'reaction.created',
				'inquiry.answered',
				'inquiry.nearby'
			));
	END IF;
END
$$;

-- Widen the notifications entity set with the inquiry an inquiry notification
-- points at. Both new events name an inquiry, so the client opens the same screen
-- whichever of the two it receives (KNOT-ADR-056).
ALTER TABLE notifications DROP CONSTRAINT IF EXISTS notifications_entity_type_check;

DO $$
DECLARE
	existing text;
BEGIN
	SELECT conname INTO existing
	FROM pg_constraint
	WHERE conrelid = 'notifications'::regclass
	  AND contype = 'c'
	  AND pg_get_constraintdef(oid) LIKE '%entity_type%';

	IF existing IS NOT NULL THEN
		EXECUTE format('ALTER TABLE notifications DROP CONSTRAINT %I', existing);
	END IF;

	IF NOT EXISTS (
		SELECT 1
		FROM pg_constraint
		WHERE conname = 'notifications_entity_type_check'
		  AND conrelid = 'notifications'::regclass
	) THEN
		ALTER TABLE notifications
			ADD CONSTRAINT notifications_entity_type_check
			CHECK (entity_type IN ('story', 'version', 'comment', 'bridge', 'inquiry'));
	END IF;
END
$$;

-- Reactions on an inquiry. The 0013 reactions table already keys on
-- (entity_type, entity_id), so an inquiry is addressed by the same table with
-- entity_type = 'inquiry': no new table and no new column. Only its closed
-- entity-kind set has to admit the new kind (KNOT-ADR-057).
--
-- Answers are deliberately NOT added here. An answer is a reply, and replies carry
-- no reactions at MVP (KNOT-ADR-052); comments, which they most resemble, had
-- their reactions removed outright (KNOT-ADR-053). Widening the set for an answer
-- would be a contradiction of both.
ALTER TABLE reactions DROP CONSTRAINT IF EXISTS reactions_entity_type_check;

DO $$
DECLARE
	existing text;
BEGIN
	SELECT conname INTO existing
	FROM pg_constraint
	WHERE conrelid = 'reactions'::regclass
	  AND contype = 'c'
	  AND pg_get_constraintdef(oid) LIKE '%entity_type%';

	IF existing IS NOT NULL THEN
		EXECUTE format('ALTER TABLE reactions DROP CONSTRAINT %I', existing);
	END IF;

	IF NOT EXISTS (
		SELECT 1
		FROM pg_constraint
		WHERE conname = 'reactions_entity_type_check'
		  AND conrelid = 'reactions'::regclass
	) THEN
		ALTER TABLE reactions
			ADD CONSTRAINT reactions_entity_type_check
			CHECK (entity_type IN ('story', 'version', 'comment', 'bridge', 'inquiry'));
	END IF;
END
$$;
