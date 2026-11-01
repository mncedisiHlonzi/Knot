-- 0026_media_status: the derivatives-only pipeline (KNOT-037-fix7).
--
-- Media is no longer processed inside the upload request. The row is written
-- first, with `status = 'processing'`, and a background worker fills in the
-- objects and flips the row to 'ready' (or 'failed'). A reader therefore has to
-- be able to tell "not ready yet" from "there is nothing here", which is what
-- this column records.
--
-- The default is 'ready' on purpose: every row written before this migration was
-- processed synchronously, so it IS ready, and no backfill is needed.
ALTER TABLE story_media
  ADD COLUMN status TEXT NOT NULL DEFAULT 'ready'
    CHECK (status IN ('processing', 'ready', 'failed'));

COMMENT ON COLUMN story_media.status IS
  'processing: queued or running; ready: every derivative exists; failed: the worker gave up (the reason is logged). Rows written before KNOT-037-fix7 are ready.';

-- The pipeline now stores derivatives only. `storage_key` keeps its column but
-- changes meaning: it names the object a reader gets when it asks for the full
-- rendition — the 2048px AVIF display for an image, the 1080p H.264 stream for a
-- video. Rows written before this migration still name their original upload
-- there, which is why a request for `display`/`stream` falls back to it.
COMMENT ON COLUMN story_media.storage_key IS
  'The primary object: the display AVIF (image) or the stream MP4 (video) since KNOT-037-fix7, or the original upload for a row written earlier.';

COMMENT ON COLUMN story_media.thumbnail_key IS
  'The feed thumbnail: a 400px WebP since KNOT-037-fix7 (an AVIF or JPEG from an earlier row). NULL for videos, which play instead of showing a cover.';

COMMENT ON COLUMN story_media.medium_key IS
  'Retired by KNOT-037-fix7: rows written earlier may still name a 1200px JPEG here, and it stays served for them. New rows leave it NULL.';

COMMENT ON COLUMN story_media.poster_key IS
  'Retired by KNOT-037-fix7 (videos autoplay, so no cover is generated). Rows written earlier may still name a poster frame, and it stays served for them.';

-- A failed row is an operational signal an operator will query for.
CREATE INDEX story_media_status_idx ON story_media (status) WHERE status <> 'ready';
