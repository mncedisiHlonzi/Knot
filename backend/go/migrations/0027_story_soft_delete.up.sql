-- Soft deletion for stories (KNOT-053).
--
-- Like accounts (KNOT-034, KNOT-ADR-081), a story is never removed the moment its
-- author asks: `deleted_at` hides it from every read while the row, its versions,
-- its comments and its media objects stay in place. The author has 30 days to
-- recover it, and a future purge job (not this task) is what finally removes it.
--
-- NULL means "not deleted", which is the only state existing rows can be in, so
-- the column needs no backfill and no default.
ALTER TABLE stories
    ADD COLUMN deleted_at TIMESTAMP WITH TIME ZONE;

-- The purge job's scan, and the operator's "what is pending?" query. The feed
-- predicates are `deleted_at IS NULL`, which the existing ordering indexes already
-- serve, so a partial index on the deleted rows is the one worth having.
CREATE INDEX stories_deleted_at_idx ON stories (deleted_at)
    WHERE deleted_at IS NOT NULL;
