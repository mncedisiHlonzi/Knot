-- 0009_structured_place.up.sql
--
-- Structured place data: latitude, longitude, and country for stories and Rooted
-- signals.
-- Column reference: docs/DATA_MODEL.md, KNOT-ADR-034.
--
-- A place was free text: discovery grouped stories by the normalised
-- `approximate_location_lower` copy, and the map resolved a place name to a point
-- with a hand-maintained, client-side table of ~66 well-known cities
-- (KNOT-ADR-020). A small town or a rural place typed into the field therefore
-- never appeared on the map.
--
-- These columns let the mobile client store the coordinate a geocoding search
-- returned (KNOT-ADR-035), so the map plots the place exactly and every place is
-- mappable, not only the ones someone had already added to the table.
--
-- The three columns are nullable and additive:
--   * `latitude` / `longitude` are NULL together or set together (the service
--     enforces the pair; both are NULL for rows created before this migration).
--   * `place_country` is the country name the geocoder reported, bounded to 100
--     characters by the application; NULL when it was not supplied.
--   * `approximate_location` (text) is kept for backward compatibility and for
--     display: a new story still stores the selected place name there, and the
--     `approximate_location_lower` copy discovery falls back to still works for
--     legacy rows.
--
-- The migration runner wraps this file in a transaction.

ALTER TABLE stories ADD COLUMN latitude DOUBLE PRECISION NULL;
ALTER TABLE stories ADD COLUMN longitude DOUBLE PRECISION NULL;
ALTER TABLE stories ADD COLUMN place_country TEXT NULL;

ALTER TABLE rooted_signals ADD COLUMN latitude DOUBLE PRECISION NULL;
ALTER TABLE rooted_signals ADD COLUMN longitude DOUBLE PRECISION NULL;
ALTER TABLE rooted_signals ADD COLUMN place_country TEXT NULL;
