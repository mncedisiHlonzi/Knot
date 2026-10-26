-- 0024_media_derivatives.down.sql
--
-- Rolling back drops the three derivative columns. The objects they named are
-- deliberately left in the bucket: they are derived data that can be regenerated,
-- and deleting user-visible media on a schema rollback would be destructive
-- (KNOT-037).
--
-- The migration runner wraps this file in a transaction.

ALTER TABLE story_media DROP COLUMN poster_key;
ALTER TABLE story_media DROP COLUMN medium_key;
ALTER TABLE story_media DROP COLUMN thumbnail_key;
