DROP INDEX IF EXISTS story_media_status_idx;

ALTER TABLE story_media DROP COLUMN IF EXISTS status;

COMMENT ON COLUMN story_media.storage_key IS
  'Object key of the uploaded original in the media bucket (KNOT-037).';

COMMENT ON COLUMN story_media.thumbnail_key IS
  'Object key of the feed thumbnail (AVIF, max 800px wide), or NULL when none was generated (KNOT-037, KNOT-037-fix6).';

COMMENT ON COLUMN story_media.medium_key IS
  'Object key of the story-detail rendition (JPEG, max 1200px wide), or NULL when none was generated (KNOT-037).';

COMMENT ON COLUMN story_media.poster_key IS
  'Object key of a video poster frame (JPEG, max 400px wide) (KNOT-037).';
