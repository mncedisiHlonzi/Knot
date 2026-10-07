-- 0004_conversations.down.sql
--
-- Reverses 0004_conversations.up.sql.
--
-- Bridges reference comments and comments reference story_versions, so both new
-- tables are dropped and nothing on the existing tables changes. The indexes and
-- constraints defined inside the CREATE TABLE and alongside it go with the tables.
--
-- NOTE: as with 0001-0003, the migration runner only applies up migrations. This
-- file exists so a rollback is explicit and reviewable when performed deliberately.

DROP TABLE IF EXISTS bridges;
DROP TABLE IF EXISTS comments;
