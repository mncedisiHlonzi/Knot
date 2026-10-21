package versions

import (
	"context"
	"errors"
	"fmt"
	"strings"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgconn"
	"github.com/jackc/pgx/v5/pgxpool"
)

// entityTypeVersion is the entity_features discriminator this write path uses.
const entityTypeVersion = "version"

// featureExecer is the subset of pgx shared by the pool and a transaction, so the
// feature upsert can run inside CreateVersion's transaction.
type featureExecer interface {
	Exec(ctx context.Context, sql string, arguments ...any) (pgconn.CommandTag, error)
}

// featureWriter is the feature upsert CreateVersion calls. It is a function type
// (and a field on the store) so a test can simulate a write failure and prove the
// version rolls back with it; production installs the real writer.
type featureWriter func(ctx context.Context, exec featureExecer, entityID string) error

// upsertVersionFeatureSQL rebuilds one version's entity_features row from the
// source tables: the version's own language, and its story's pillar and place.
//
// Writing the projection in SQL keeps it identical to migration 0020 and
// BackfillEntityFeatures, so the read model cannot drift from its definition.
const upsertVersionFeatureSQL = `
	INSERT INTO entity_features (entity_id, entity_type, language, pillar, place_lower, updated_at)
	SELECT v.id, 'version', v.language, s.pillar::text, lower(s.approximate_location), now()
	FROM story_versions v
	JOIN stories s ON s.id = v.story_id
	WHERE v.id = $1
	ON CONFLICT (entity_id) DO UPDATE SET
		entity_type = EXCLUDED.entity_type,
		language = EXCLUDED.language,
		pillar = EXCLUDED.pillar,
		place_lower = EXCLUDED.place_lower,
		updated_at = now()`

// upsertVersionFeature runs the projection for one version on exec.
func upsertVersionFeature(ctx context.Context, exec featureExecer, versionID string) error {
	if _, err := exec.Exec(ctx, upsertVersionFeatureSQL, versionID); err != nil {
		return fmt.Errorf("versions: upsert feature: %w", err)
	}

	return nil
}

// versionColumns is the canonical SELECT/RETURNING column list. It is a constant
// so every query in this file stays consistent with scanVersion, and its order
// is the order scanVersion reads.
const versionColumns = `id, story_id, parent_version_id, author_id, language, title, body, adaptation_note, created_at, updated_at`

// versionColumnsFor prefixes the canonical column list with a table alias, for the
// queries that join story_versions to itself (the recursive ancestry walk, where an
// unqualified column name would be ambiguous). It splits on ", ", which is safe
// because versionColumns is a constant list of bare column names.
func versionColumnsFor(alias string) string {
	columns := strings.Split(versionColumns, ", ")
	for i, column := range columns {
		columns[i] = alias + "." + column
	}

	return strings.Join(columns, ", ")
}

// PostgresStore is the pgx-backed implementation of VersionStore.
//
// It holds an injected pool and no other state, so it is safe for concurrent use.
type PostgresStore struct {
	pool *pgxpool.Pool
	// featureUpsert writes the version's entity_features row. It is a field so a
	// test can simulate a write failure; NewPostgresStore installs the real
	// writer.
	featureUpsert featureWriter
}

// Compile-time proof that the store satisfies the domain contract.
var _ VersionStore = (*PostgresStore)(nil)

// NewPostgresStore returns a store backed by pool. The pool is injected rather
// than reached for globally so that callers own its lifecycle.
func NewPostgresStore(pool *pgxpool.Pool) (*PostgresStore, error) {
	if pool == nil {
		return nil, fmt.Errorf("versions: postgres store requires a non-nil pool")
	}
	return &PostgresStore{pool: pool, featureUpsert: upsertVersionFeature}, nil
}

// CreateVersion inserts a version and returns the stored row, including the id
// and timestamps PostgreSQL generated.
//
// A root version is inserted by passing an empty ParentVersionID, which is
// mapped onto SQL NULL; every other version names its parent. The
// story_versions_story_id_root_unique index rejects a second root for a story.
//
// The insert and the version's entity_features row share one transaction: if the
// feature upsert fails, the version rolls back with it, so a committed version
// always has its feature row (KNOT-027a).
func (s *PostgresStore) CreateVersion(ctx context.Context, version StoryVersion) (StoryVersion, error) {
	const query = `
		INSERT INTO story_versions (
			story_id, parent_version_id, author_id, language, title, body, adaptation_note
		)
		VALUES ($1, $2, $3, $4, $5, $6, $7)
		RETURNING ` + versionColumns

	tx, err := s.pool.Begin(ctx)
	if err != nil {
		return StoryVersion{}, fmt.Errorf("versions: begin create version: %w", err)
	}
	defer func() { _ = tx.Rollback(ctx) }()

	created, err := scanVersion(tx.QueryRow(
		ctx,
		query,
		version.StoryID,
		nullIfEmpty(version.ParentVersionID),
		version.AuthorID,
		version.Language,
		version.Title,
		version.Body,
		nullIfEmpty(version.AdaptationNote),
	))
	if err != nil {
		return StoryVersion{}, fmt.Errorf("versions: create version: %w", err)
	}

	if err := s.featureUpsert(ctx, tx, created.ID); err != nil {
		return StoryVersion{}, fmt.Errorf("versions: create version: %w", err)
	}

	if err := tx.Commit(ctx); err != nil {
		return StoryVersion{}, fmt.Errorf("versions: commit create version: %w", err)
	}

	return created, nil
}

// GetVersion returns the version with the given id, or ErrNotFound.
//
// A malformed id is treated as not-found rather than as a database error, which
// keeps the id column's index usable instead of casting it to text in SQL.
func (s *PostgresStore) GetVersion(ctx context.Context, id string) (StoryVersion, error) {
	if !isUUID(id) {
		return StoryVersion{}, ErrNotFound
	}

	const query = `SELECT ` + versionColumns + ` FROM story_versions WHERE id = $1`

	version, err := scanVersion(s.pool.QueryRow(ctx, query, id))
	if err != nil {
		if errors.Is(err, pgx.ErrNoRows) {
			return StoryVersion{}, ErrNotFound
		}
		return StoryVersion{}, fmt.Errorf("versions: get version: %w", err)
	}

	return version, nil
}

// ListByStory returns every version of one story, oldest first, so a version
// always precedes its children. It returns ErrNotFound when the story itself
// does not exist, which is how the caller tells "no such story" apart from "a
// story with no versions" (the latter is unreachable: every story has a root).
//
// The list is flat. The tree is an adjacency list, so assembling it into a
// nesting is the caller's job; doing it here would either sort for one display
// or leak a traversal order into the wire format.
func (s *PostgresStore) ListByStory(ctx context.Context, storyID string) ([]StoryVersion, error) {
	if !isUUID(storyID) {
		return nil, ErrNotFound
	}

	var exists bool
	if err := s.pool.QueryRow(
		ctx,
		`SELECT EXISTS (SELECT 1 FROM stories WHERE id = $1)`,
		storyID,
	).Scan(&exists); err != nil {
		return nil, fmt.Errorf("versions: check story: %w", err)
	}
	if !exists {
		return nil, ErrNotFound
	}

	const query = `SELECT ` + versionColumns + ` FROM story_versions WHERE story_id = $1 ORDER BY created_at ASC, id ASC`

	rows, err := s.pool.Query(ctx, query, storyID)
	if err != nil {
		return nil, fmt.Errorf("versions: list versions: %w", err)
	}
	defer rows.Close()

	versions := make([]StoryVersion, 0)
	for rows.Next() {
		version, err := scanVersion(rows)
		if err != nil {
			return nil, fmt.Errorf("versions: list versions: %w", err)
		}
		versions = append(versions, version)
	}
	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("versions: list versions: %w", err)
	}

	return versions, nil
}

// ListAncestry returns the ancestry closure of the given versions in one read.
//
// A recursive query walks parent_version_id from each seed to its root, so the
// caller gets every row it needs to rebuild each seed's lineage without issuing
// one query per version (KNOT-022). The seed list is expanded into numbered
// placeholders because pgx cannot infer the element type of a string slice used
// bare; each placeholder is cast to uuid so PostgreSQL compares like with like.
//
// The closure is finite without an explicit depth limit because parent_version_id
// references a version of the same story, so the walk terminates at each root,
// where parent_version_id is NULL and the recursion produces no further rows.
// UNION (not UNION ALL) collapses rows reached by more than one path, which is
// what a shared ancestor would produce; a cycle would terminate there too rather
// than loop forever.
//
// The recursive term JOINs the CTE rather than selecting from it in a subquery:
// PostgreSQL refuses to see the recursive reference inside a subquery at all
// (SQLSTATE 42P19). That is also why the two branches qualify every column -- a
// join makes an unqualified list ambiguous.
func (s *PostgresStore) ListAncestry(ctx context.Context, versionIDs []string) ([]StoryVersion, error) {
	if len(versionIDs) == 0 {
		// Nothing to walk. An empty seed list must not become `IN ()`, which is
		// not valid SQL.
		return []StoryVersion{}, nil
	}

	args := make([]any, 0, len(versionIDs))
	placeholders := make([]string, 0, len(versionIDs))
	for _, id := range versionIDs {
		args = append(args, id)
		placeholders = append(placeholders, fmt.Sprintf("$%d::uuid", len(args)))
	}

	query := `
		WITH RECURSIVE ancestry AS (
			SELECT ` + versionColumnsFor("seed") + `
			FROM story_versions seed
			WHERE seed.id IN (` + strings.Join(placeholders, ", ") + `)
			UNION
			SELECT ` + versionColumnsFor("child") + `
			FROM story_versions child
			JOIN ancestry ON child.id = ancestry.parent_version_id
		)
		SELECT ` + versionColumns + `
		FROM ancestry`

	rows, err := s.pool.Query(ctx, query, args...)
	if err != nil {
		return nil, fmt.Errorf("versions: list ancestry: %w", err)
	}
	defer rows.Close()

	versions := make([]StoryVersion, 0)
	for rows.Next() {
		version, err := scanVersion(rows)
		if err != nil {
			return nil, fmt.Errorf("versions: list ancestry: %w", err)
		}
		versions = append(versions, version)
	}
	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("versions: list ancestry: %w", err)
	}

	return versions, nil
}

// rowScanner is the subset of pgx.Row and pgx.Rows that scanVersion needs.
type rowScanner interface {
	Scan(dest ...any) error
}

// scanVersion reads one row of versionColumns into a StoryVersion. It returns
// the driver error unwrapped so callers can classify it (for example as
// pgx.ErrNoRows).
func scanVersion(row rowScanner) (StoryVersion, error) {
	var (
		version StoryVersion
		parent  *string
		note    *string
	)

	err := row.Scan(
		&version.ID,
		&version.StoryID,
		&parent,
		&version.AuthorID,
		&version.Language,
		&version.Title,
		&version.Body,
		&note,
		&version.CreatedAt,
		&version.UpdatedAt,
	)
	if err != nil {
		return StoryVersion{}, err
	}

	if parent != nil {
		version.ParentVersionID = *parent
	}
	if note != nil {
		version.AdaptationNote = *note
	}

	return version, nil
}

// nullIfEmpty maps "" onto SQL NULL, which is what the nullable columns expect.
func nullIfEmpty(value string) *string {
	if value == "" {
		return nil
	}
	return &value
}
