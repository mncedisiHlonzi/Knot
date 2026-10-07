-- 0002_stories.down.sql
--
-- Reverses 0002_stories.up.sql.
--
-- NOTE: as with 0001, the migration runner only applies up migrations. This file
-- exists so a rollback is explicit and reviewable when performed deliberately.

DROP TABLE IF EXISTS stories;
