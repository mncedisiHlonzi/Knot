-- 0016_search.up.sql
--
-- Full-text search: a generated tsvector and a GIN index on the two tables that
-- actually carry searchable text. Column reference: docs/DATA_MODEL.md,
-- KNOT-ADR-065.
--
-- Search covers three content types — stories, versions, and inquiries — but the
-- text lives on only two tables. A story carries no title or body of its own: its
-- content is the root version (`story_versions` where `parent_version_id IS
-- NULL`), so a story is found by searching its root version and returning the
-- story id. That is why `stories` gets no `search_vector` here; adding one would
-- duplicate text that already has a home (KNOT-ADR-020, KNOT-ADR-065).
--
-- The `simple` configuration is used deliberately. Knot is multilingual: isiZulu
-- and Sepedi content must not be stemmed with an English algorithm, and there is
-- no single stemmer for the languages Knot serves. `simple` lower-cases and
-- tokenises without stemming, so it behaves identically for every language
-- (KNOT-ADR-065). The cost — no stem-based recall ("running" does not match
-- "run") — is accepted and recorded in docs/DEVELOPMENT.md.
--
-- `tsvector` values are weighted: the title is weight A and the body weight B, so
-- `ts_rank` prefers a title hit over a body hit without a second index.
--
-- The column is GENERATED ALWAYS ... STORED, so it is maintained by PostgreSQL on
-- every insert and update and can never drift from the text it indexes. The
-- two-argument `to_tsvector(regconfig, text)` form is used (rather than the
-- one-argument form) because only it is immutable, which a generated column
-- requires.
--
-- ALTER TABLE ... ADD COLUMN has no IF NOT EXISTS, so each column is added behind
-- an information_schema guard and each index behind CREATE INDEX IF NOT EXISTS.
-- The file can therefore be re-applied by hand without erroring. The migration
-- runner wraps it in a transaction, so the columns and their indexes either all
-- appear or none do.

-- story_versions: the content behind both "story" (root versions) and "version"
-- (adaptations) results.
DO $$
BEGIN
	IF NOT EXISTS (
		SELECT 1
		FROM information_schema.columns
		WHERE table_name = 'story_versions'
		  AND column_name = 'search_vector'
	) THEN
		ALTER TABLE story_versions
			ADD COLUMN search_vector tsvector
			GENERATED ALWAYS AS (
				setweight(to_tsvector('simple', coalesce(title, '')), 'A') ||
				setweight(to_tsvector('simple', coalesce(body, '')), 'B')
			) STORED;
	END IF;
END $$;

-- inquiries: both the list and the detail carry title and body.
DO $$
BEGIN
	IF NOT EXISTS (
		SELECT 1
		FROM information_schema.columns
		WHERE table_name = 'inquiries'
		  AND column_name = 'search_vector'
	) THEN
		ALTER TABLE inquiries
			ADD COLUMN search_vector tsvector
			GENERATED ALWAYS AS (
				setweight(to_tsvector('simple', coalesce(title, '')), 'A') ||
				setweight(to_tsvector('simple', coalesce(body, '')), 'B')
			) STORED;
	END IF;
END $$;

-- GIN is the only index type that answers `search_vector @@ query` for an
-- arbitrary query, which is exactly what the search endpoint asks of it.
CREATE INDEX IF NOT EXISTS story_versions_search_vector_idx
	ON story_versions USING gin (search_vector);

CREATE INDEX IF NOT EXISTS inquiries_search_vector_idx
	ON inquiries USING gin (search_vector);
