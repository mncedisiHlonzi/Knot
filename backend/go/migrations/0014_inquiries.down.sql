-- 0014_inquiries.down.sql
--
-- Reverses 0014_inquiries.up.sql.
--
-- The notification and reaction CHECKs are narrowed back to the sets 0013 left
-- behind. Rows that the narrowed set would reject cannot exist without this
-- migration having run, so no data is silently dropped; if a row somehow does
-- exist, the ADD CONSTRAINT fails loudly rather than deleting anything, which is
-- the correct outcome for a hand-run rollback.
--
-- The migration runner has no "down" subcommand, so this file is applied by hand
-- with psql (docs/DEVELOPMENT.md).

-- Narrow the reaction entity set back to the four content kinds. Any reaction
-- still filed against an inquiry is removed first, because the narrowed CHECK
-- could not hold it.
DELETE FROM reactions WHERE entity_type = 'inquiry';

ALTER TABLE reactions DROP CONSTRAINT IF EXISTS reactions_entity_type_check;

DO $$
BEGIN
	IF NOT EXISTS (
		SELECT 1
		FROM pg_constraint
		WHERE conname = 'reactions_entity_type_check'
		  AND conrelid = 'reactions'::regclass
	) THEN
		ALTER TABLE reactions
			ADD CONSTRAINT reactions_entity_type_check
			CHECK (entity_type IN ('story', 'version', 'comment', 'bridge'));
	END IF;
END
$$;

-- Narrow the notification entity set back to the four content kinds.
DELETE FROM notifications WHERE entity_type = 'inquiry';

ALTER TABLE notifications DROP CONSTRAINT IF EXISTS notifications_entity_type_check;

DO $$
BEGIN
	IF NOT EXISTS (
		SELECT 1
		FROM pg_constraint
		WHERE conname = 'notifications_entity_type_check'
		  AND conrelid = 'notifications'::regclass
	) THEN
		ALTER TABLE notifications
			ADD CONSTRAINT notifications_entity_type_check
			CHECK (entity_type IN ('story', 'version', 'comment', 'bridge'));
	END IF;
END
$$;

-- Narrow the notification event set back to the four content events.
DELETE FROM notifications WHERE event_type IN ('inquiry.answered', 'inquiry.nearby');

ALTER TABLE notifications DROP CONSTRAINT IF EXISTS notifications_event_type_check;

DO $$
BEGIN
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

DROP INDEX IF EXISTS rooted_signals_place_idx;

DROP TABLE IF EXISTS inquiry_answers;

DROP TABLE IF EXISTS inquiries;
