-- 0025_users_languages_gin.up.sql
--
-- The feed audience query filters readers by preferred language (KNOT-036,
-- KNOT-ADR-093). That rule was an unindexed scan of `users`; this adds the GIN
-- index the containment predicate needs (KNOT-036-fix3, KNOT-ADR-095).
--
-- The audience query spells the language rule as
-- `u.preferred_languages @> ARRAY[$2::text]` rather than
-- `$2::text = ANY(u.preferred_languages)`. The two are equivalent for a scalar,
-- but only the `@>` form can become a GIN index scan: verified with EXPLAIN on the
-- local database, where `= ANY` stayed a sequential scan even with
-- `enable_seqscan = off`, and `@>` produced a Bitmap Index Scan on this index.
--
-- Deliberately NOT `CREATE INDEX CONCURRENTLY`. The runner applies each migration
-- inside a transaction (`applyOne` in migrations.go), and CONCURRENTLY cannot run
-- inside one. The trade-off: this statement takes a SHARE lock and blocks writes
-- to `users` while the index builds. On an MVP-sized table that is milliseconds; a
-- future deployment against a large live table should add a concurrent-index path
-- to the runner rather than change this file.
--
-- The index is added with IF NOT EXISTS so a re-run is harmless even if the
-- schema_migrations row was lost.

CREATE INDEX IF NOT EXISTS users_preferred_languages_gin
    ON users USING GIN (preferred_languages);
