-- 0008_story_media.down.sql
--
-- Rollback for 0008: drop the story_media table.
--
-- The uploaded objects in the media bucket are deliberately NOT deleted here: a
-- schema rollback must never destroy user data. They are left in place for an
-- operator to sweep if the feature is retired for good.

DROP TABLE IF EXISTS story_media;
