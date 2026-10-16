-- 0017_feed_versions.down.sql
--
-- Drops the feed-order index for the version branch (KNOT-022). The feed still
-- works without it: the merged page falls back to sorting the union. Nothing
-- else in the schema depended on it.

DROP INDEX story_versions_created_at_id_idx;
