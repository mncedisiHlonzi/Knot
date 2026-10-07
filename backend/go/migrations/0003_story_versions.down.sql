-- 0003_story_versions.down.sql
--
-- Reverses 0003_story_versions.up.sql.
--
-- Each story's root version content is copied back onto the stories row, the
-- story -> root version link is dropped, and story_versions is removed. Only the
-- root version survives a rollback: adaptations have no place in the pre-0003
-- schema and are discarded with the table.
--
-- NOTE: as with 0001 and 0002, the migration runner only applies up migrations.
-- This file exists so a rollback is explicit and reviewable when performed
-- deliberately.

-- 1. Re-add the moved content columns (nullable until backfilled).
ALTER TABLE stories ADD COLUMN language TEXT;
ALTER TABLE stories ADD COLUMN title TEXT;
ALTER TABLE stories ADD COLUMN body TEXT;

-- 2. Backfill them from each story's root version.
UPDATE stories s SET language = v.language, title = v.title, body = v.body
FROM story_versions v WHERE v.id = s.root_version_id;

-- 3. Restore the NOT NULL constraints the columns carried before the up migration.
ALTER TABLE stories ALTER COLUMN language SET NOT NULL;
ALTER TABLE stories ALTER COLUMN title SET NOT NULL;
ALTER TABLE stories ALTER COLUMN body SET NOT NULL;

-- 4. Drop the story -> root version link, in the reverse order of creation.
DROP INDEX IF EXISTS stories_root_version_id_unique;
ALTER TABLE stories DROP CONSTRAINT IF EXISTS stories_root_version_id_fkey;
ALTER TABLE stories DROP COLUMN root_version_id;

-- 5. Drop the versions table; its indexes and constraints go with it.
DROP TABLE IF EXISTS story_versions;
