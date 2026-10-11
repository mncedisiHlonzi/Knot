-- 0016_search.down.sql
--
-- Reverses 0016_search.up.sql: drops the two GIN indexes and the two generated
-- tsvector columns.
--
-- Nothing here can lose content: `search_vector` is derived data that PostgreSQL
-- rebuilds from `title` and `body` if the column is ever added back, and the
-- indexes hold no rows of their own. Dropping the columns is therefore safe in
-- either direction.
--
-- The migration runner has no "down" subcommand, so this file is applied by hand
-- with psql (docs/DEVELOPMENT.md). Index drops are guarded with IF EXISTS and the
-- column drops are guarded on information_schema, so re-applying is also a no-op.

DROP INDEX IF EXISTS story_versions_search_vector_idx;
DROP INDEX IF EXISTS inquiries_search_vector_idx;

DO $$
BEGIN
	IF EXISTS (
		SELECT 1
		FROM information_schema.columns
		WHERE table_name = 'story_versions'
		  AND column_name = 'search_vector'
	) THEN
		ALTER TABLE story_versions DROP COLUMN search_vector;
	END IF;

	IF EXISTS (
		SELECT 1
		FROM information_schema.columns
		WHERE table_name = 'inquiries'
		  AND column_name = 'search_vector'
	) THEN
		ALTER TABLE inquiries DROP COLUMN search_vector;
	END IF;
END $$;
