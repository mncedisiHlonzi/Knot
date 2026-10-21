-- 0019_interactions.down.sql
--
-- Reverses 0019_interactions.up.sql. The indexes fall with the table (they are
-- created implicitly on it), so a single DROP is enough.
--
-- The migration runner wraps this file in a transaction.

DROP TABLE IF EXISTS user_interactions;
