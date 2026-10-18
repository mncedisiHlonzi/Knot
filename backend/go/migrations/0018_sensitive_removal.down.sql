-- 0018_sensitive_removal.down.sql
--
-- Restores the SHAPE of the removed flag, not its data: the previous values are
-- gone once the up migration commits. A rollback recreates the column with the
-- schema default so older code can run against the restored shape.
--
-- The runner applies up migrations only; this file exists so a rollback is
-- explicit and reviewable.

ALTER TABLE stories ADD COLUMN sensitive BOOLEAN NOT NULL DEFAULT false;
