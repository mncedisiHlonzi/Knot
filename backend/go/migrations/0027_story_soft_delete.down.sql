-- Reverse 0027_story_soft_delete.
--
-- Dropping the column also drops its index, so the table returns to exactly the
-- shape it had before KNOT-053. Any story a soft delete was hiding becomes visible
-- again, which is the honest meaning of rolling this back.
ALTER TABLE stories
    DROP COLUMN IF EXISTS deleted_at;
