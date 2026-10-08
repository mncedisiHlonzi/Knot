-- 0006_discovery.down.sql
--
-- Reverses 0006_discovery.up.sql: drops the normalised location column and its
-- index. The runner has no down path; this file exists so a rollback is explicit
-- and reviewable.

DROP INDEX stories_approximate_location_lower_idx;

ALTER TABLE stories DROP COLUMN approximate_location_lower;
