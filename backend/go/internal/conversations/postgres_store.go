package conversations

import (
	"context"
	"errors"
	"fmt"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgconn"
	"github.com/jackc/pgx/v5/pgxpool"
)

// PostgreSQL SQLSTATEs the store classifies. A unique violation is how the
// bridge constraints refuse a duplicate; a foreign key violation is how an
// unknown version or comment is recognised.
const (
	uniqueViolation      = "23505"
	foreignKeyViolation  = "23503"
	commentsVersionFK    = "comments_version_id_fkey"
	bridgesSourceFK      = "bridges_source_comment_id_fkey"
	bridgesLanguageIndex = "bridges_one_per_target_language"
)

// commentColumns and bridgeColumns are the canonical SELECT/RETURNING column
// lists. They are constants so every query stays consistent with the scanners,
// and their order is the order the scanners read.
const (
	commentColumns = `id, version_id, author_id, language, body, created_at, updated_at`
	bridgeColumns  = `id, source_comment_id, target_comment_id, author_id, target_language, adaptation_note, created_at`
)

// PostgresStore is the pgx-backed implementation of CommentStore and
// BridgeStore.
//
// It holds an injected pool and no other state, so it is safe for concurrent use.
type PostgresStore struct {
	pool *pgxpool.Pool
}

// Compile-time proof that the store satisfies both domain contracts.
var (
	_ CommentStore = (*PostgresStore)(nil)
	_ BridgeStore  = (*PostgresStore)(nil)
)

// NewPostgresStore returns a store backed by pool. The pool is injected rather
// than reached for globally so that callers own its lifecycle.
func NewPostgresStore(pool *pgxpool.Pool) (*PostgresStore, error) {
	if pool == nil {
		return nil, fmt.Errorf("conversations: postgres store requires a non-nil pool")
	}
	return &PostgresStore{pool: pool}, nil
}

// CreateComment inserts a comment and returns the stored row, including the id
// and timestamps PostgreSQL generated.
//
// A comment on a version that does not exist is reported as ErrNotFound rather
// than as a raw driver error, so the handler can answer 404.
func (s *PostgresStore) CreateComment(ctx context.Context, comment Comment) (Comment, error) {
	const query = `
		INSERT INTO comments (version_id, author_id, language, body)
		VALUES ($1, $2, $3, $4)
		RETURNING ` + commentColumns

	created, err := scanComment(s.pool.QueryRow(ctx, query, comment.VersionID, comment.AuthorID, comment.Language, comment.Body))
	if err != nil {
		if isForeignKeyViolation(err, commentsVersionFK) {
			return Comment{}, ErrNotFound
		}
		return Comment{}, fmt.Errorf("conversations: create comment: %w", err)
	}

	return created, nil
}

// GetComment returns the comment with the given id, or ErrNotFound.
//
// A malformed id is treated as not-found rather than as a database error, which
// keeps the id column's index usable instead of casting it to text in SQL.
func (s *PostgresStore) GetComment(ctx context.Context, id string) (Comment, error) {
	if !isUUID(id) {
		return Comment{}, ErrNotFound
	}

	const query = `SELECT ` + commentColumns + ` FROM comments WHERE id = $1`

	comment, err := scanComment(s.pool.QueryRow(ctx, query, id))
	if err != nil {
		if errors.Is(err, pgx.ErrNoRows) {
			return Comment{}, ErrNotFound
		}
		return Comment{}, fmt.Errorf("conversations: get comment: %w", err)
	}

	return comment, nil
}

// ListComments returns one page of a version's comments, newest first, plus the
// cursor that resumes after it (nil when this page is the last one).
//
// Pagination is keyset, matching `comments_version_id_created_at_idx`
// (version_id, created_at DESC, id DESC): the next page is everything strictly
// "before" the last row of this page in that order. It returns ErrNotFound when
// the version does not exist, so the caller can tell "no such version" from "a
// version with no comments".
func (s *PostgresStore) ListComments(ctx context.Context, versionID string, cursor *Cursor, limit int) ([]Comment, *Cursor, error) {
	if !isUUID(versionID) {
		return nil, nil, ErrNotFound
	}

	var exists bool
	if err := s.pool.QueryRow(
		ctx,
		`SELECT EXISTS (SELECT 1 FROM story_versions WHERE id = $1)`,
		versionID,
	).Scan(&exists); err != nil {
		return nil, nil, fmt.Errorf("conversations: check version: %w", err)
	}
	if !exists {
		return nil, nil, ErrNotFound
	}

	query := `SELECT ` + commentColumns + ` FROM comments WHERE version_id = $1`
	args := []any{versionID}

	if cursor != nil {
		// The explicit casts are required because a row comparison does not
		// reliably infer the parameter types, and they are safe because
		// DecodeCursor has already proven the id is canonical UUID text.
		query += ` AND (created_at, id) < ($2::timestamptz, $3::uuid)`
		args = append(args, cursor.CreatedAt(), cursor.ID())
	}

	query += fmt.Sprintf(` ORDER BY created_at DESC, id DESC LIMIT $%d`, len(args)+1)
	args = append(args, limit+1)

	rows, err := s.pool.Query(ctx, query, args...)
	if err != nil {
		return nil, nil, fmt.Errorf("conversations: list comments: %w", err)
	}
	defer rows.Close()

	page := make([]Comment, 0, limit)
	for rows.Next() {
		comment, err := scanComment(rows)
		if err != nil {
			return nil, nil, fmt.Errorf("conversations: list comments: %w", err)
		}
		page = append(page, comment)
	}
	if err := rows.Err(); err != nil {
		return nil, nil, fmt.Errorf("conversations: list comments: %w", err)
	}

	if len(page) <= limit {
		return page, nil, nil
	}

	last := page[limit-1]
	next := NewCursor(last.CreatedAt, last.ID)

	return page[:limit], &next, nil
}

// FindTargetVersion returns the id of the version of the source comment's story
// that is written in targetLanguage, or ErrNotFound when the story has no such
// version.
//
// Both the source and the target comment of a bridge must belong to the same
// story, so the search is anchored on the source version's story_id. When
// several versions share the target language, the oldest is returned so the
// choice is deterministic rather than dependent on physical row order.
func (s *PostgresStore) FindTargetVersion(ctx context.Context, sourceVersionID, targetLanguage string) (string, error) {
	if !isUUID(sourceVersionID) {
		return "", ErrNotFound
	}

	const query = `
		SELECT target.id
		FROM story_versions source
		JOIN story_versions target ON target.story_id = source.story_id
		WHERE source.id = $1 AND target.language = $2
		ORDER BY target.created_at ASC, target.id ASC
		LIMIT 1`

	var id string
	if err := s.pool.QueryRow(ctx, query, sourceVersionID, targetLanguage).Scan(&id); err != nil {
		if errors.Is(err, pgx.ErrNoRows) {
			return "", ErrNotFound
		}
		return "", fmt.Errorf("conversations: find target version: %w", err)
	}

	return id, nil
}

// CreateBridge inserts the target comment and the bridge that points at the
// source comment, in one transaction, and returns both stored rows.
//
// The two rows are one act: a bridge without its target comment, or a target
// comment with no bridge joining it to its source, would be an orphan. A failure
// on either insert rolls both back. The source comment already exists (the
// service read it), so only the target comment is inserted here.
func (s *PostgresStore) CreateBridge(ctx context.Context, target Comment, sourceCommentID, adaptationNote string) (Bridge, Comment, error) {
	tx, err := s.pool.Begin(ctx)
	if err != nil {
		return Bridge{}, Comment{}, fmt.Errorf("conversations: begin bridge transaction: %w", err)
	}
	// Rollback is a no-op once Commit has succeeded.
	defer func() { _ = tx.Rollback(context.WithoutCancel(ctx)) }()

	const insertComment = `
		INSERT INTO comments (version_id, author_id, language, body)
		VALUES ($1, $2, $3, $4)
		RETURNING ` + commentColumns

	createdTarget, err := scanComment(tx.QueryRow(ctx, insertComment, target.VersionID, target.AuthorID, target.Language, target.Body))
	if err != nil {
		if isForeignKeyViolation(err, commentsVersionFK) {
			return Bridge{}, Comment{}, ErrNotFound
		}
		return Bridge{}, Comment{}, fmt.Errorf("conversations: insert bridged comment: %w", err)
	}

	const insertBridge = `
		INSERT INTO bridges (source_comment_id, target_comment_id, author_id, target_language, adaptation_note)
		VALUES ($1, $2, $3, $4, $5)
		RETURNING ` + bridgeColumns

	createdBridge, err := scanBridge(tx.QueryRow(
		ctx,
		insertBridge,
		sourceCommentID,
		createdTarget.ID,
		target.AuthorID,
		target.Language,
		nullIfEmpty(adaptationNote),
	))
	if err != nil {
		if isUniqueViolation(err, bridgesLanguageIndex) {
			return Bridge{}, Comment{}, ErrAlreadyBridged
		}
		if isForeignKeyViolation(err, bridgesSourceFK) {
			return Bridge{}, Comment{}, ErrNotFound
		}
		return Bridge{}, Comment{}, fmt.Errorf("conversations: insert bridge: %w", err)
	}

	if err := tx.Commit(ctx); err != nil {
		return Bridge{}, Comment{}, fmt.Errorf("conversations: commit bridge transaction: %w", err)
	}

	return createdBridge, createdTarget, nil
}

// GetBridge returns the bridge with the given id, or ErrNotFound.
func (s *PostgresStore) GetBridge(ctx context.Context, id string) (Bridge, error) {
	if !isUUID(id) {
		return Bridge{}, ErrNotFound
	}

	const query = `SELECT ` + bridgeColumns + ` FROM bridges WHERE id = $1`

	bridge, err := scanBridge(s.pool.QueryRow(ctx, query, id))
	if err != nil {
		if errors.Is(err, pgx.ErrNoRows) {
			return Bridge{}, ErrNotFound
		}
		return Bridge{}, fmt.Errorf("conversations: get bridge: %w", err)
	}

	return bridge, nil
}

// ListBridgesForComment returns every bridge in which the comment is the source
// or the target, newest first, or ErrNotFound when no comment has the given id.
func (s *PostgresStore) ListBridgesForComment(ctx context.Context, commentID string) ([]Bridge, error) {
	if !isUUID(commentID) {
		return nil, ErrNotFound
	}

	var exists bool
	if err := s.pool.QueryRow(
		ctx,
		`SELECT EXISTS (SELECT 1 FROM comments WHERE id = $1)`,
		commentID,
	).Scan(&exists); err != nil {
		return nil, fmt.Errorf("conversations: check comment: %w", err)
	}
	if !exists {
		return nil, ErrNotFound
	}

	const query = `
		SELECT ` + bridgeColumns + `
		FROM bridges
		WHERE source_comment_id = $1 OR target_comment_id = $1
		ORDER BY created_at DESC, id DESC`

	return s.queryBridges(ctx, query, commentID)
}

// ListBridgesForStory returns every bridge whose source comment belongs to a
// version of the story, newest first, or ErrNotFound when no story has the
// given id.
//
// Both comments of a bridge are always on the same version (the target is
// created on the source's version), so joining on the source comment is enough
// to place the bridge in a story.
func (s *PostgresStore) ListBridgesForStory(ctx context.Context, storyID string) ([]Bridge, error) {
	if !isUUID(storyID) {
		return nil, ErrNotFound
	}

	var exists bool
	if err := s.pool.QueryRow(
		ctx,
		`SELECT EXISTS (SELECT 1 FROM stories WHERE id = $1)`,
		storyID,
	).Scan(&exists); err != nil {
		return nil, fmt.Errorf("conversations: check story: %w", err)
	}
	if !exists {
		return nil, ErrNotFound
	}

	const query = `
		SELECT b.id, b.source_comment_id, b.target_comment_id, b.author_id, b.target_language, b.adaptation_note, b.created_at
		FROM bridges b
		JOIN comments c ON c.id = b.source_comment_id
		JOIN story_versions v ON v.id = c.version_id
		WHERE v.story_id = $1
		ORDER BY b.created_at DESC, b.id DESC`

	return s.queryBridges(ctx, query, storyID)
}

// queryBridges runs a bridge SELECT and scans every row.
func (s *PostgresStore) queryBridges(ctx context.Context, query string, args ...any) ([]Bridge, error) {
	rows, err := s.pool.Query(ctx, query, args...)
	if err != nil {
		return nil, fmt.Errorf("conversations: list bridges: %w", err)
	}
	defer rows.Close()

	bridges := make([]Bridge, 0)
	for rows.Next() {
		bridge, err := scanBridge(rows)
		if err != nil {
			return nil, fmt.Errorf("conversations: list bridges: %w", err)
		}
		bridges = append(bridges, bridge)
	}
	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("conversations: list bridges: %w", err)
	}

	return bridges, nil
}

// rowScanner is the subset of pgx.Row and pgx.Rows that the scanners need.
type rowScanner interface {
	Scan(dest ...any) error
}

// scanComment reads one row of commentColumns into a Comment. It returns the
// driver error unwrapped so callers can classify it.
func scanComment(row rowScanner) (Comment, error) {
	var comment Comment

	err := row.Scan(
		&comment.ID,
		&comment.VersionID,
		&comment.AuthorID,
		&comment.Language,
		&comment.Body,
		&comment.CreatedAt,
		&comment.UpdatedAt,
	)
	if err != nil {
		return Comment{}, err
	}

	return comment, nil
}

// scanBridge reads one row of bridgeColumns into a Bridge. It returns the driver
// error unwrapped so callers can classify it.
func scanBridge(row rowScanner) (Bridge, error) {
	var (
		bridge Bridge
		note   *string
	)

	err := row.Scan(
		&bridge.ID,
		&bridge.SourceCommentID,
		&bridge.TargetCommentID,
		&bridge.AuthorID,
		&bridge.TargetLanguage,
		&note,
		&bridge.CreatedAt,
	)
	if err != nil {
		return Bridge{}, err
	}

	if note != nil {
		bridge.AdaptationNote = *note
	}

	return bridge, nil
}

// isForeignKeyViolation reports whether err is a foreign key violation on the
// named constraint.
func isForeignKeyViolation(err error, constraint string) bool {
	var pgErr *pgconn.PgError
	return errors.As(err, &pgErr) && pgErr.Code == foreignKeyViolation && pgErr.ConstraintName == constraint
}

// isUniqueViolation reports whether err is a unique violation on the named index
// or constraint.
func isUniqueViolation(err error, index string) bool {
	var pgErr *pgconn.PgError
	return errors.As(err, &pgErr) && pgErr.Code == uniqueViolation && pgErr.ConstraintName == index
}

// nullIfEmpty maps "" onto SQL NULL, which is what the nullable columns expect.
func nullIfEmpty(value string) *string {
	if value == "" {
		return nil
	}
	return &value
}
