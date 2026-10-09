package migrations

import (
	"context"
	"fmt"
	"os"
	"strings"
	"testing"
	"time"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"
)

// Migration 0011 rewrites languages that were stored under the old ISO 639-1
// contract, so the only honest way to test it is against real rows in the real
// schema. These tests run inside a transaction that is always rolled back: they
// write two-letter languages, rewrite them, and leave the database exactly as they
// found it.
//
// They are skipped, not failed, when KNOT_POSTGRES_DSN is unset, when PostgreSQL is
// unreachable, or when the tables have not been migrated — none of those is a
// defect in the migration.

// migrationTx opens a transaction on the test database and returns it. The caller
// never receives a pool, because it must not be able to commit.
func migrationTx(t *testing.T) (context.Context, pgx.Tx) {
	t.Helper()

	dsn := strings.TrimSpace(os.Getenv("KNOT_POSTGRES_DSN"))
	if dsn == "" {
		t.Skip("KNOT_POSTGRES_DSN is not set; skipping PostgreSQL migration test")
	}

	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)

	pool, err := pgxpool.New(ctx, dsn)
	if err != nil {
		cancel()
		t.Skipf("could not build a pool from KNOT_POSTGRES_DSN: %v", err)
	}
	if err := pool.Ping(ctx); err != nil {
		pool.Close()
		cancel()
		t.Skipf("PostgreSQL at KNOT_POSTGRES_DSN is not reachable: %v", err)
	}

	var migrated bool
	err = pool.QueryRow(ctx, `
		SELECT to_regclass('public.users') IS NOT NULL
		   AND to_regclass('public.stories') IS NOT NULL
		   AND to_regclass('public.story_versions') IS NOT NULL
		   AND to_regclass('public.comments') IS NOT NULL
		   AND to_regclass('public.bridges') IS NOT NULL`,
	).Scan(&migrated)
	if err != nil {
		pool.Close()
		cancel()
		t.Skipf("could not inspect the schema: %v", err)
	}
	if !migrated {
		pool.Close()
		cancel()
		t.Skip("the tables do not exist; run `go run ./cmd/knot migrate up` first")
	}

	tx, err := pool.Begin(ctx)
	if err != nil {
		pool.Close()
		cancel()
		t.Fatalf("begin migration test transaction: %v", err)
	}

	t.Cleanup(func() {
		// Rollback, never commit: the database must be unchanged afterwards.
		_ = tx.Rollback(context.WithoutCancel(ctx))
		pool.Close()
		cancel()
	})

	return ctx, tx
}

// migrationFile returns the SQL of an embedded migration file.
func migrationFile(t *testing.T, name string) string {
	t.Helper()

	body, err := files.ReadFile(name)
	if err != nil {
		t.Fatalf("read embedded %s: %v", name, err)
	}
	if len(body) == 0 {
		t.Fatalf("embedded %s is empty", name)
	}

	return string(body)
}

// execSQL runs a statement and fails the test on error.
func execSQL(t *testing.T, ctx context.Context, tx pgx.Tx, sql string, args ...any) {
	t.Helper()

	if _, err := tx.Exec(ctx, sql, args...); err != nil {
		t.Fatalf("exec failed: %v\nsql: %s", err, sql)
	}
}

// scanOne runs a single-row statement and returns its first column as text.
func scanOne(t *testing.T, ctx context.Context, tx pgx.Tx, sql string, args ...any) string {
	t.Helper()

	var value string
	if err := tx.QueryRow(ctx, sql, args...).Scan(&value); err != nil {
		t.Fatalf("query failed: %v\nsql: %s", err, sql)
	}

	return value
}

// languageFixture is the data the migration is pointed at: a story whose root
// version is two letters with a child version in another two-letter language, two
// comments, a bridge, and four users whose preferred languages must survive the
// rewrite element by element.
type languageFixture struct {
	storyID        string
	rootVersionID  string
	childVersionID string
	sourceComment  string
	targetComment  string
	bridgeID       string
	ownerID        string
	otherID        string
	emptyID        string
	unknownID      string
}

// seedLanguages inserts the fixture.
//
// A story and its root version are inserted together in one statement, because
// `stories.root_version_id` and `story_versions.story_id` point at each other and
// PostgreSQL checks both foreign keys at the end of the statement.
func seedLanguages(t *testing.T, ctx context.Context, tx pgx.Tx, tag string) languageFixture {
	t.Helper()

	var fixture languageFixture

	const insertUser = `
		INSERT INTO users (email, password_hash, display_name, preferred_languages)
		VALUES ($1, 'test-hash', $2, $3)
		RETURNING id`

	fixture.ownerID = scanOne(t, ctx, tx, insertUser,
		tag+"-owner@example.test", "Migration owner", []string{"en", "zu"})
	fixture.otherID = scanOne(t, ctx, tx, insertUser,
		tag+"-other@example.test", "Migration other", []string{"en", "nso"})
	// An empty array must stay an empty array, never become NULL.
	fixture.emptyID = scanOne(t, ctx, tx, `
		INSERT INTO users (email, password_hash, display_name, preferred_languages)
		VALUES ($1, 'test-hash', 'Migration empty', ARRAY[]::TEXT[])
		RETURNING id`, tag+"-empty@example.test")
	// A two-letter value with no ISO 639-3 counterpart must be left alone.
	fixture.unknownID = scanOne(t, ctx, tx, insertUser,
		tag+"-unknown@example.test", "Migration unknown", []string{"zz"})

	fixture.storyID = scanOne(t, ctx, tx, `
		WITH new_story AS (
			INSERT INTO stories (author_id, pillar, root_version_id)
			VALUES ($1, 'wonder', gen_random_uuid())
			RETURNING id, root_version_id
		)
		INSERT INTO story_versions (id, story_id, parent_version_id, author_id, language, title, body)
		SELECT root_version_id, id, NULL, $1, 'en', 'A migration story', 'Body' FROM new_story
		RETURNING story_id`, fixture.ownerID)

	fixture.rootVersionID = scanOne(t, ctx, tx,
		`SELECT id FROM story_versions WHERE story_id = $1 AND parent_version_id IS NULL`,
		fixture.storyID)

	fixture.childVersionID = scanOne(t, ctx, tx, `
		INSERT INTO story_versions (story_id, parent_version_id, author_id, language, title, body)
		VALUES ($1, $2, $3, 'zu', 'Indaba', 'Umzimba')
		RETURNING id`, fixture.storyID, fixture.rootVersionID, fixture.otherID)

	fixture.sourceComment = scanOne(t, ctx, tx, `
		INSERT INTO comments (version_id, author_id, language, body)
		VALUES ($1, $2, 'fr', 'Un commentaire')
		RETURNING id`, fixture.rootVersionID, fixture.ownerID)
	// The target comment is already three letters and must not be touched.
	fixture.targetComment = scanOne(t, ctx, tx, `
		INSERT INTO comments (version_id, author_id, language, body)
		VALUES ($1, $2, 'nso', 'Polelo')
		RETURNING id`, fixture.rootVersionID, fixture.otherID)

	fixture.bridgeID = scanOne(t, ctx, tx, `
		INSERT INTO bridges (source_comment_id, target_comment_id, author_id, target_language)
		VALUES ($1, $2, $3, 'af')
		RETURNING id`, fixture.sourceComment, fixture.targetComment, fixture.ownerID)

	return fixture
}

// TestMigration0011RewritesTwoLetterCodesAndReversesThem is the whole point of the
// migration: two-letter languages written under the old contract become
// three-letter ones, three-letter ones are left alone, the rewrite is idempotent,
// and the down migration reverses what it can without losing anything.
func TestMigration0011RewritesTwoLetterCodesAndReversesThem(t *testing.T) {
	ctx, tx := migrationTx(t)

	tag := fmt.Sprintf("knot-mig-%d", time.Now().UnixNano())
	fixture := seedLanguages(t, ctx, tx, tag)

	up := migrationFile(t, "0011_iso_639_3.up.sql")
	down := migrationFile(t, "0011_iso_639_3.down.sql")

	// --- up -----------------------------------------------------------------
	execSQL(t, ctx, tx, up)

	assertLanguage(t, ctx, tx, "story_versions", "language", fixture.rootVersionID, "eng")
	assertLanguage(t, ctx, tx, "story_versions", "language", fixture.childVersionID, "zul")
	assertLanguage(t, ctx, tx, "comments", "language", fixture.sourceComment, "fra")
	assertLanguage(t, ctx, tx, "bridges", "target_language", fixture.bridgeID, "afr")

	// A value that was already three letters is not a two-letter code, so the
	// rewrite must not have touched it.
	assertLanguage(t, ctx, tx, "comments", "language", fixture.targetComment, "nso")

	assertPreferred(t, ctx, tx, fixture.ownerID, []string{"eng", "zul"})
	// Element-wise, in order; a three-letter element passes through.
	assertPreferred(t, ctx, tx, fixture.otherID, []string{"eng", "nso"})
	// An empty array stays an empty array, not NULL.
	assertPreferred(t, ctx, tx, fixture.emptyID, []string{})
	// A two-letter value with no counterpart is left alone rather than guessed at.
	assertPreferred(t, ctx, tx, fixture.unknownID, []string{"zz"})

	// --- up again: idempotent ------------------------------------------------
	execSQL(t, ctx, tx, up)

	assertLanguage(t, ctx, tx, "story_versions", "language", fixture.rootVersionID, "eng")
	assertLanguage(t, ctx, tx, "comments", "language", fixture.sourceComment, "fra")
	assertLanguage(t, ctx, tx, "comments", "language", fixture.targetComment, "nso")
	assertLanguage(t, ctx, tx, "bridges", "target_language", fixture.bridgeID, "afr")
	assertPreferred(t, ctx, tx, fixture.ownerID, []string{"eng", "zul"})

	// --- down: best effort ---------------------------------------------------
	execSQL(t, ctx, tx, down)

	assertLanguage(t, ctx, tx, "story_versions", "language", fixture.rootVersionID, "en")
	assertLanguage(t, ctx, tx, "story_versions", "language", fixture.childVersionID, "zu")
	assertLanguage(t, ctx, tx, "comments", "language", fixture.sourceComment, "fr")
	assertLanguage(t, ctx, tx, "bridges", "target_language", fixture.bridgeID, "af")

	// Codes with no two-letter counterpart survive the round trip unchanged, which
	// is what makes the down migration lossless.
	assertLanguage(t, ctx, tx, "comments", "language", fixture.targetComment, "nso")

	assertPreferred(t, ctx, tx, fixture.ownerID, []string{"en", "zu"})
	assertPreferred(t, ctx, tx, fixture.otherID, []string{"en", "nso"})
	assertPreferred(t, ctx, tx, fixture.emptyID, []string{})
	assertPreferred(t, ctx, tx, fixture.unknownID, []string{"zz"})
}

// TestMigration0011LeavesNothingBehind checks that the mapping table is temporary
// and does not survive the migration.
func TestMigration0011LeavesNothingBehind(t *testing.T) {
	ctx, tx := migrationTx(t)

	execSQL(t, ctx, tx, migrationFile(t, "0011_iso_639_3.up.sql"))

	var leftovers int
	if err := tx.QueryRow(ctx, `
		SELECT count(*)
		FROM pg_class
		WHERE relname IN ('knot_iso639_1_to_3', 'knot_iso639_3_to_1')`,
	).Scan(&leftovers); err != nil {
		t.Fatalf("count leftover relations: %v", err)
	}
	if leftovers != 0 {
		t.Errorf("found %d leftover mapping table(s), want 0", leftovers)
	}
}

// assertLanguage reads one language value and compares it.
func assertLanguage(t *testing.T, ctx context.Context, tx pgx.Tx, table, column, id, want string) {
	t.Helper()

	// The table and column names are compile-time constants in this file, never
	// caller input; the id is a parameter.
	query := "SELECT " + column + " FROM " + table + " WHERE id = $1"

	var got string
	if err := tx.QueryRow(ctx, query, id).Scan(&got); err != nil {
		t.Fatalf("read %s.%s for %s: %v", table, column, id, err)
	}
	if got != want {
		t.Errorf("%s.%s = %q, want %q", table, column, got, want)
	}
}

// assertPreferred reads a user's preferred languages and compares them in order.
func assertPreferred(t *testing.T, ctx context.Context, tx pgx.Tx, userID string, want []string) {
	t.Helper()

	var got []string
	if err := tx.QueryRow(ctx, "SELECT preferred_languages FROM users WHERE id = $1", userID).Scan(&got); err != nil {
		t.Fatalf("read preferred_languages for %s: %v", userID, err)
	}

	if len(got) != len(want) {
		t.Fatalf("preferred_languages = %v, want %v", got, want)
	}
	for i := range got {
		if got[i] != want[i] {
			t.Errorf("preferred_languages[%d] = %q, want %q (whole value %v)", i, got[i], want[i], got)
		}
	}
}
