-- 0024_media_derivatives.up.sql
--
-- Media derivatives: the renditions the feed and the story detail read instead of
-- the original upload (KNOT-037; KNOT-ADR-091).
--
-- A row already names the original object in `storage_key`. These three columns
-- name the renditions generated beside it in the same bucket:
--
--   thumbnail_key  a JPEG at most 400px wide, for a feed card
--   medium_key     a JPEG at most 1200px wide, for the story detail
--   poster_key     a JPEG frame of a video, used as its thumbnail
--
-- All three are NULLABLE on purpose. A derivative is an optimisation, not a
-- requirement: an upload whose decoder is missing, whose video FFmpeg could not
-- read, or that predates this migration has no rendition, and every reader falls
-- back to `storage_key`. That is why there is no backfill here — the rows that
-- exist are still perfectly servable — and why the columns carry no NOT NULL and
-- no default.
--
-- Like `storage_key`, each value is an OBJECT KEY, never a URL: the bucket stays
-- private and the API builds the path a client fetches (KNOT-ADR-029).
--
-- The migration runner wraps this file in a transaction.

ALTER TABLE story_media ADD COLUMN thumbnail_key TEXT;
ALTER TABLE story_media ADD COLUMN medium_key TEXT;
ALTER TABLE story_media ADD COLUMN poster_key TEXT;

COMMENT ON COLUMN story_media.thumbnail_key IS
    'Object key of the feed thumbnail (JPEG, max 400px wide), or NULL when none was generated';
COMMENT ON COLUMN story_media.medium_key IS
    'Object key of the story-detail rendition (JPEG, max 1200px wide), or NULL when none was generated';
COMMENT ON COLUMN story_media.poster_key IS
    'Object key of a video poster frame (JPEG, max 400px wide), or NULL for images or when extraction failed';
