-- 0009_structured_place.down.sql
--
-- Rollback for 0009: drop the structured place columns from stories and
-- rooted_signals.
--
-- Only the columns are dropped. No data is destroyed beyond them: the free-text
-- `approximate_location` (stories) and `place` (rooted_signals) columns remain,
-- so a story or a signal still names the place after a rollback.

ALTER TABLE stories DROP COLUMN IF EXISTS latitude;
ALTER TABLE stories DROP COLUMN IF EXISTS longitude;
ALTER TABLE stories DROP COLUMN IF EXISTS place_country;

ALTER TABLE rooted_signals DROP COLUMN IF EXISTS latitude;
ALTER TABLE rooted_signals DROP COLUMN IF EXISTS longitude;
ALTER TABLE rooted_signals DROP COLUMN IF EXISTS place_country;
