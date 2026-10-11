-- 0015_moderation.down.sql
--
-- Reverses 0015_moderation.up.sql: drops the five moderation tables and the role
-- column. Dropping a table drops its indexes with it, so the indexes are not
-- dropped separately.
--
-- DROP TABLE IF EXISTS and DROP COLUMN IF EXISTS keep this re-runnable, matching
-- the up file. The role column's CHECK constraint is dropped with the column, so
-- no separate constraint drop is needed.

DROP TABLE IF EXISTS audit_log;
DROP TABLE IF EXISTS moderation_actions;
DROP TABLE IF EXISTS moderation_cases;
DROP TABLE IF EXISTS reports;
DROP TABLE IF EXISTS blocks;

ALTER TABLE users DROP COLUMN IF EXISTS role;
