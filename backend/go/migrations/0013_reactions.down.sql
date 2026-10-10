-- 0013_reactions.down.sql — reverses 0013_reactions.up.sql.
--
-- The reactions table and its indexes are dropped, and the notifications
-- `event_type` CHECK is restored to the three events 0010 defined. Any
-- reaction.created rows must be removed before the narrower CHECK can be added,
-- so they are deleted first: the event has no meaning without the migration that
-- produced it.

DELETE FROM notifications WHERE event_type = 'reaction.created';

-- The reaction type travels with the event, so it goes when the event does.
ALTER TABLE notifications DROP CONSTRAINT IF EXISTS notifications_reaction_type_check;
ALTER TABLE notifications DROP COLUMN IF EXISTS reaction_type;

-- Put the 0010 constraint back. Drop whatever check currently covers event_type,
-- then add the original one, so a re-run is safe.
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
			CHECK (event_type IN ('version.created', 'comment.created', 'bridge.created'));
	END IF;
END
$$;

DROP INDEX IF EXISTS reactions_user_idx;
DROP INDEX IF EXISTS reactions_entity_idx;
DROP INDEX IF EXISTS reactions_unique_per_user_entity_type;
DROP TABLE IF EXISTS reactions;
