-- 0001_users.down.sql
--
-- Reverses 0001_users.up.sql.
--
-- NOTE: the migration runner in this repository only applies up migrations.
-- This file exists so a rollback is explicit, reviewable, and reproducible when
-- performed deliberately. There is no `migrate down` CLI command.

DROP TABLE IF EXISTS users;
