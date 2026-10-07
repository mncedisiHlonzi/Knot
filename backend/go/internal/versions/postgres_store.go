package versions

import (
	"context"
	"errors"
	"fmt"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"
)

// versionColumns is the canonical SELECT/RETURNING column list. It is a constant
// so every query in this file stays consistent with scanVersion, and its order
// is the order scanVersion reads.
const versionColumns = `id, story_id, parent_version_id, author_id, language, title, body, adaptation_note, created_at, updated_at`

// PostgresStore is the pgx-backed implementation of VersionStore.
//
// It holds an injected pool and no other state, so it is safe for concurrent use.
type PostgresStore struct {
	pool *pgxpool.Pool
}

// Compile-time proof that the store satisfies the domain contract.
var _ VersionStore = (*PostgresStore)(nil)

// NewPostgresStore returns a store backed by pool. The pool is injected rather
// than reached for globally so that callers own its lifecycle.
func NewPostgresStore(pool *pgxpool.Pool) (*PostgresStore, error) {
	if pool == nil {
		return nil, fmt.Errorf("versions: postgres store requires a non-nil pool")
	}
	return &PostgresStore{pool: pool}, nil
}

// CreateVersion inserts a version and returns the stored row, including the id
// and timestamps PostgreSQL generated.
//
// A root version is inserted by passing an empty ParentVersionID, which is
// mapped onto SQL NULL; every other version names its parent. The
// story_versions_story_id_root_unique index rejects a second root for a story.
func (s *PostgresStore) CreateVersion(ctx context.Context, version StoryVersion) (StoryVersion, error) {
	const query = `
		INSERT INTO story_versions (
			story_id, parent_version_id, author_id, language, title, body, adaptation_note
		)
		VALUES ($1, $2, $3, $4, $5, $6, $7)
		RETURNING ` + versionColumns

	row := s.pool.QueryRow(
		ctx,
		query,
		version.StoryID,
		nullIfEmpty(version.ParentVersionID),
		version.AuthorID,
		version.Language,
		version.Title,
		version.Body,
		nullIfEmpty(version.AdaptationNote),
	)

	created, err := scanVersion(row)
	if err != nil {
		return StoryVersion{}, fmt.Errorf("versions: create version: %w", err)
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
