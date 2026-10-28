package migrations

import (
	"context"
	"os"
	"strings"
	"testing"
	"time"

	"github.com/jackc/pgx/v5/pgxpool"
)

// Migration 0025 adds the GIN index the feed audience query needs to filter
// readers by language (KNOT-036-fix3, KNOT-ADR-095).
//
// Two properties are tested without a database — that the migration is embedded
// with the index the query needs, and that it avoids CONCURRENTLY because the
// runner wraps it in a transaction — and one against the real schema, because "the
// migration applied" is only true if the index is actually there and valid.

const ginIndexName = "users_preferred_languages_gin"

func TestUsersLanguagesGinMigrationIsLoaded(t *testing.T) {
	all, err := Load()
	if err != nil {
		t.Fatalf("Load() error = %v, want nil", err)
	}

	var found *Migration
	for i := range all {
		if all[i].Version == 25 {
			found = &all[i]
			break
		}
	}
	if found == nil {
		t.Fatal("migration version 25 is not embedded")
	}
	if found.Name != "users_languages_gin" {
		t.Errorf("name = %q, want %q", found.Name, "users_languages_gin")
	}

	if !strings.Contains(found.SQL, "CREATE INDEX IF NOT EXISTS "+ginIndexName) {
		t.Errorf("migration does not create %s:\n%s", ginIndexName, found.SQL)
	}
	if !strings.Contains(found.SQL, "USING GIN (preferred_languages)") {
		t.Errorf("migration does not build a GIN index on preferred_languages:\n%s", found.SQL)
	}

	// The runner applies each migration inside a transaction (applyOne), where
	// CREATE INDEX CONCURRENTLY cannot run. Pinning the absence keeps a future edit
	// from breaking every deploy with "cannot run inside a transaction block".
	// Comments are stripped first: the migration's own prose says why it does not
	// use CONCURRENTLY, and that sentence must not fail the check.
	if strings.Contains(stripSQLComments(found.SQL), "CONCURRENTLY") {
		t.Error("migration uses CONCURRENTLY, which cannot run inside the runner's transaction")
	}
}

// stripSQLComments removes whole-line `--` comments, so a test can assert on the
// statements a migration runs rather than on its explanation.
func stripSQLComments(sql string) string {
	lines := strings.Split(sql, "\n")
	kept := make([]string, 0, len(lines))
	for _, line := range lines {
		if strings.HasPrefix(strings.TrimSpace(line), "--") {
			continue
		}
		kept = append(kept, line)
	}
	return strings.Join(kept, "\n")
}

func TestUsersLanguagesGinDownDropsTheIndex(t *testing.T) {
	body, err := files.ReadFile("0025_users_languages_gin.down.sql")
	if err != nil {
		t.Fatalf("read down migration: %v", err)
	}

	if !strings.Contains(string(body), "DROP INDEX IF EXISTS "+ginIndexName) {
		t.Errorf("down migration does not drop %s:\n%s", ginIndexName, body)
	}
}

// TestUsersPreferredLanguagesIndexExists is the only honest check that the
// migration applied: it reads the index out of the live catalog.
//
// It skips rather than fails when the database has not been migrated to 0025 --
// that is a setup gap, not a defect in the migration. CI applies every migration
// before `go test`, so it runs there.
func TestUsersPreferredLanguagesIndexExists(t *testing.T) {
	dsn := strings.TrimSpace(os.Getenv("KNOT_POSTGRES_DSN"))
	if dsn == "" {
		t.Skip("KNOT_POSTGRES_DSN is not set; skipping the index catalog check")
	}

	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer cancel()

	pool, err := pgxpool.New(ctx, dsn)
	if err != nil {
		t.Skipf("could not build a pool from KNOT_POSTGRES_DSN: %v", err)
	}
	defer pool.Close()

	if err := pool.Ping(ctx); err != nil {
		t.Skipf("PostgreSQL at KNOT_POSTGRES_DSN is not reachable: %v", err)
	}

	var applied bool
	if err := pool.QueryRow(ctx,
		`SELECT EXISTS (SELECT 1 FROM schema_migrations WHERE version = 25)`,
	).Scan(&applied); err != nil {
		t.Skipf("could not read schema_migrations: %v", err)
	}
	if !applied {
		t.Skip("migration 0025 has not been applied to this database; run migrate up")
	}

	var method, valid bool
	err = pool.QueryRow(ctx, `
		SELECT i.indisvalid, am.amname = 'gin'
		FROM pg_index i
		JOIN pg_class c ON c.oid = i.indexrelid
		JOIN pg_namespace n ON n.oid = c.relnamespace
		JOIN pg_am am ON am.oid = c.relam
		WHERE c.relname = $1
			AND n.nspname = current_schema()
			AND i.indrelid = 'users'::regclass`,
		ginIndexName,
	).Scan(&valid, &method)
	if err != nil {
		t.Fatalf("index %s is missing after migration 0025: %v", ginIndexName, err)
	}
	if !valid {
		t.Errorf("index %s is not valid", ginIndexName)
	}
	if !method {
		t.Errorf("index %s is not a GIN index", ginIndexName)
	}
}
