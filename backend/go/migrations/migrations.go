// Package migrations holds the Knot backend's SQL migrations and the minimal
// runner that applies them.
//
// The SQL files are embedded into the binary with go:embed, so migrations always
// ship with the code that expects them. There is deliberately no third-party
// migration library (no goose, no golang-migrate, no atlas): the runner is small,
// auditable, and has no behaviour we do not need.
//
// Only "up" migrations are applied by this package. The matching .down.sql files
// are kept in the repository so a rollback is explicit and reviewable, but there
// is no automated down path.
package migrations

import (
	"context"
	"embed"
	"fmt"
	"path"
	"sort"
	"strconv"
	"strings"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgconn"
)

//go:embed *.sql
var files embed.FS

// advisoryLockKey is an arbitrary but stable key used to serialise migration
// runs. Two processes starting at once will not apply the same migration twice.
const advisoryLockKey int64 = 8410719001

// DB is the minimal database surface the runner needs. *pgxpool.Pool satisfies
// it, as does a *pgx.Conn.
type DB interface {
	Exec(ctx context.Context, sql string, arguments ...any) (pgconn.CommandTag, error)
	Query(ctx context.Context, sql string, args ...any) (pgx.Rows, error)
	QueryRow(ctx context.Context, sql string, args ...any) pgx.Row
	Begin(ctx context.Context) (pgx.Tx, error)
}

// Migration is one embedded up migration.
type Migration struct {
	// Version is the leading number in the file name, e.g. 1 for 0001_users.up.sql.
	Version int64
	// Name is the file name without the version prefix or extension, e.g. "users".
	Name string
	// FileName is the full embedded file name, e.g. "0001_users.up.sql".
	FileName string
	// SQL is the migration body.
	SQL string
}

const createMigrationsTable = `
CREATE TABLE IF NOT EXISTS schema_migrations (
    version    BIGINT PRIMARY KEY,
    name       TEXT NOT NULL,
    applied_at TIMESTAMPTZ NOT NULL DEFAULT now()
)`

// Load returns every embedded up migration, ordered by version ascending.
//
// It returns an error if a file name does not follow the
// <version>_<name>.up.sql convention, or if two files share a version.
func Load() ([]Migration, error) {
	entries, err := files.ReadDir(".")
	if err != nil {
		return nil, fmt.Errorf("migrations: read embedded files: %w", err)
	}

	seen := make(map[int64]string)
	var out []Migration

	for _, entry := range entries {
		fileName := entry.Name()
		if entry.IsDir() || !strings.HasSuffix(fileName, ".up.sql") {
			continue
		}

		version, name, err := parseFileName(fileName)
		if err != nil {
			return nil, err
		}
		if previous, dup := seen[version]; dup {
			return nil, fmt.Errorf("migrations: duplicate version %d in %q and %q", version, previous, fileName)
		}
		seen[version] = fileName

		body, err := files.ReadFile(fileName)
		if err != nil {
			return nil, fmt.Errorf("migrations: read %q: %w", fileName, err)
		}

		out = append(out, Migration{
			Version:  version,
			Name:     name,
			FileName: fileName,
			SQL:      string(body),
		})
	}

	sort.Slice(out, func(i, j int) bool { return out[i].Version < out[j].Version })
	return out, nil
}

// Up applies every migration that has not been applied yet, in version order.
//
// Each migration runs inside its own transaction together with the row that
// records it, so a failure leaves the database on the previous version rather
// than half-migrated. A session-level advisory lock serialises concurrent runs.
//
// It returns the versions it applied, which is empty when the database is
// already up to date.
func Up(ctx context.Context, db DB) ([]int64, error) {
	all, err := Load()
	if err != nil {
		return nil, err
	}

	if _, err := db.Exec(ctx, createMigrationsTable); err != nil {
		return nil, fmt.Errorf("migrations: create schema_migrations: %w", err)
	}

	if _, err := db.Exec(ctx, "SELECT pg_advisory_lock($1)", advisoryLockKey); err != nil {
		return nil, fmt.Errorf("migrations: acquire advisory lock: %w", err)
	}
	defer func() {
		// Best effort: the lock is also released when the session ends.
		_, _ = db.Exec(context.WithoutCancel(ctx), "SELECT pg_advisory_unlock($1)", advisoryLockKey)
	}()

	applied, err := appliedVersions(ctx, db)
	if err != nil {
		return nil, err
	}

	var newlyApplied []int64
	for _, m := range all {
		if applied[m.Version] {
			continue
		}
		if err := applyOne(ctx, db, m); err != nil {
			return newlyApplied, err
		}
		newlyApplied = append(newlyApplied, m.Version)
	}

	return newlyApplied, nil
}

// appliedVersions returns the set of versions already recorded as applied.
func appliedVersions(ctx context.Context, db DB) (map[int64]bool, error) {
	rows, err := db.Query(ctx, "SELECT version FROM schema_migrations")
	if err != nil {
		return nil, fmt.Errorf("migrations: read schema_migrations: %w", err)
	}
	defer rows.Close()

	applied := make(map[int64]bool)
	for rows.Next() {
		var version int64
		if err := rows.Scan(&version); err != nil {
			return nil, fmt.Errorf("migrations: scan version: %w", err)
		}
		applied[version] = true
	}
	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("migrations: iterate schema_migrations: %w", err)
	}

	return applied, nil
}

// applyOne runs a single migration and records it, atomically.
func applyOne(ctx context.Context, db DB, m Migration) error {
	tx, err := db.Begin(ctx)
	if err != nil {
		return fmt.Errorf("migrations: begin %s: %w", m.FileName, err)
	}
	defer func() { _ = tx.Rollback(context.WithoutCancel(ctx)) }()

	if _, err := tx.Exec(ctx, m.SQL); err != nil {
		return fmt.Errorf("migrations: apply %s: %w", m.FileName, err)
	}
	if _, err := tx.Exec(ctx, "INSERT INTO schema_migrations (version, name) VALUES ($1, $2)", m.Version, m.Name); err != nil {
		return fmt.Errorf("migrations: record %s: %w", m.FileName, err)
	}
	if err := tx.Commit(ctx); err != nil {
		return fmt.Errorf("migrations: commit %s: %w", m.FileName, err)
	}

	return nil
}

// parseFileName splits "0001_users.up.sql" into version 1 and name "users".
func parseFileName(fileName string) (int64, string, error) {
	base := strings.TrimSuffix(fileName, ".up.sql")
	prefix, name, found := strings.Cut(base, "_")
	if !found || prefix == "" || name == "" {
		return 0, "", fmt.Errorf("migrations: %q must be named <version>_<name>.up.sql", fileName)
	}

	version, err := strconv.ParseInt(prefix, 10, 64)
	if err != nil {
		return 0, "", fmt.Errorf("migrations: %q has a non-numeric version prefix: %w", fileName, err)
	}

	return version, name, nil
}

// FileNameFor is a small helper used by tests and tooling to build the canonical
// file name for a version and name pair.
func FileNameFor(version int64, name string) string {
	return path.Join(fmt.Sprintf("%04d_%s.up.sql", version, name))
}
