-- 0018_sensitive_removal.up.sql
--
-- The sensitive-content flag is removed from the platform (KNOT-023c,
-- KNOT-ADR-072). It marked a story that "should not be surfaced without care",
-- but nothing server-side ever enforced that care: the only effect was a
-- client-side blur. Rather than leave a column no code writes, the column is
-- dropped.
--
-- The migration runner wraps this file in a transaction, so the column either
-- disappears or nothing was run. There is no data worth preserving: the flag was
-- a boolean defaulting to false, and no surface depended on it.

ALTER TABLE stories DROP COLUMN sensitive;
