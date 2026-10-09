-- 0007_user_avatar.down.sql
--
-- Rollback for 0007: drop the users.avatar_url column.
--
-- The uploaded objects in the media bucket are deliberately NOT deleted here: a
-- schema rollback must never destroy user data. They are left in place for an
-- operator to sweep if the feature is retired for good.

ALTER TABLE users DROP COLUMN IF EXISTS avatar_url;
