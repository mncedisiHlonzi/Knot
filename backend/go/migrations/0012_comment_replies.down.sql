-- 0012_comment_replies (down) — drop the parent pointer and everything that
-- depended on it.
--
-- This is a clean reversal, not a best-effort one: the column is new in this
-- migration, so dropping it loses exactly what this migration added. Replies
-- become top-level comments, which is what they were before. Nothing else is
-- touched, and no row is deleted.
--
-- The constraint is dropped explicitly before the column for readability; the
-- column's own DROP would take it, and the foreign key, with it. Every statement
-- is IF EXISTS, so the file is safe to run twice.

DROP INDEX IF EXISTS comments_parent_comment_id_idx;

ALTER TABLE comments
    DROP CONSTRAINT IF EXISTS comments_parent_comment_id_not_self;

ALTER TABLE comments
    DROP COLUMN IF EXISTS parent_comment_id;
