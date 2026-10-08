-- 0006_discovery.up.sql
--
-- Discovery: a normalised, indexed copy of each story's approximate location.
--
-- Story discovery groups stories by place. Place names are free text entered by
-- people, so the same place arrives as "Cape Town", "cape town", or "Cape Town "
-- with trailing space. Grouping on the raw column would treat those as three
-- places; grouping on `approximate_location_lower` treats them as one.
--
-- This is normalisation-for-grouping only. It stores no coordinates, performs no
-- geocoding, and adds no spatial index: the map client resolves a place name to a
-- point visually, on the device. See KNOT-ADR-020.
--
-- The application maintains the column on write (story create), so every new row
-- is normalised at the source; this migration backfills the rows that already
-- exist. The expression is `lower(trim(...))`, and the write path uses the same
-- expression, so an existing row and a newly written row are normalised alike.
--
-- Column reference: docs/DATA_MODEL.md
--
-- The migration runner wraps this file in a transaction, so the column, the
-- backfill, and the index either all appear or none do.

ALTER TABLE stories ADD COLUMN approximate_location_lower TEXT;

UPDATE stories
SET approximate_location_lower = lower(trim(approximate_location))
WHERE approximate_location IS NOT NULL;

CREATE INDEX stories_approximate_location_lower_idx ON stories (approximate_location_lower);
