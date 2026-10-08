-- 0005_rooted.down.sql
--
-- Reverses 0005_rooted.up.sql. Dropping the table drops its indexes and its
-- constraints with it. The runner only goes up; this file exists so a rollback is
-- explicit and reviewable.

DROP TABLE IF EXISTS rooted_signals;
