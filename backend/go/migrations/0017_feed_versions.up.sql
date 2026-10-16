-- 0017_feed_versions.up.sql
--
-- Versions in the feed (KNOT-022).
--
-- GET /stories becomes a merged feed: a UNION ALL of stories and the adaptations
-- of stories (story_versions rows that have a parent), ordered by
-- (created_at DESC, id DESC). stories already carries
-- stories_created_at_id_idx from 0002, so the story branch is served from an
-- index in feed order; this adds the matching index for the version branch, so
-- the merged page is a merge of two ordered index scans rather than a sort of
-- every version in the table.
--
-- The index is **partial**, matching the version branch's own predicate
-- (parent_version_id IS NOT NULL): a root version is never a feed entry of its
-- own -- it IS its story, and the story branch carries it -- so the root rows do
-- not belong in the index at all. This mirrors the partial unique root index
-- added in 0003 and keeps this one to exactly the rows the feed orders.
--
-- No table or column changes: this is a read path, and the Language Tree is
-- untouched. See KNOT-ADR-070 and docs/DATA_MODEL.md.
--
-- The migration runner wraps this file in a transaction, so the index either
-- exists afterwards or nothing was run.

CREATE INDEX story_versions_created_at_id_idx
    ON story_versions (created_at DESC, id DESC)
    WHERE parent_version_id IS NOT NULL;
