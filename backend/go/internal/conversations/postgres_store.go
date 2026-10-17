package conversations

import (
	"context"
	"errors"
	"fmt"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgconn"
	"github.com/jackc/pgx/v5/pgxpool"

	"github.com/knot/backend/internal/moderation"
)

// PostgreSQL SQLSTATEs the store classifies. A foreign key violation is how an
// unknown version or comment is recognised; a check violation is how the schema
// refuses a row that contradicts itself, such as a comment parenting itself.
const (
	foreignKeyViolation = "23503"
	checkViolation      = "23514"
	commentsVersionFK   = "comments_version_id_fkey"
	commentsParentFK    = "comments_parent_comment_id_fkey"
)

// commentColumns is the canonical SELECT/RETURNING column list. It is a constant
// so every query stays consistent with the scanner, and its order is the order
// the scanner reads.
const commentColumns = `id, version_id, author_id, language, body, parent_comment_id, created_at, updated_at`

// PostgresStore is the pgx-backed implementation of CommentStore.
//
// It holds an injected pool and no other state, so it is safe for concurrent use.
type PostgresStore struct {
	pool *pgxpool.Pool
}

// Compile-time proof that the store satisfies the domain contract.
var _ CommentStore = (*PostgresStore)(nil)

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
// than as a raw driver error, so the handler can answer 404. The same is true of a
// parent that has vanished between the service's lookup and this insert — a race
// with a delete that the product does not offer yet.
func (s *PostgresStore) CreateComment(ctx context.Context, comment Comment) (Comment, error) {
	const query = `
		INSERT INTO comments (version_id, author_id, language, body, parent_comment_id)
		VALUES ($1, $2, $3, $4, $5)
		RETURNING ` + commentColumns

	created, err := scanComment(s.pool.QueryRow(ctx, query, comment.VersionID, comment.AuthorID, comment.Language, comment.Body, comment.ParentCommentID))
	if err != nil {
		if isForeignKeyViolation(err, commentsVersionFK) || isForeignKeyViolation(err, commentsParentFK) {
			return Comment{}, ErrNotFound
		}
		return Comment{}, fmt.Errorf("conversations: create comment: %w", err)
	}

	return created, nil
}

// GetComment returns the comment with the given id, or ErrNotFound, together
// with the id of the story its version belongs to (Comment.StoryID).
//
// A comment row does not name its story — its version does — so the story is
// resolved by joining story_versions in the same query. The join cannot drop a
// row: comments.version_id is a foreign key, so every comment has a version.
//
// A malformed id is treated as not-found rather than as a database error, which
// keeps the id column's index usable instead of casting it to text in SQL.
func (s *PostgresStore) GetComment(ctx context.Context, id string) (Comment, error) {
	if !isUUID(id) {
		return Comment{}, ErrNotFound
	}

	const query = `
		SELECT c.id, c.version_id, c.author_id, c.language, c.body, c.parent_comment_id, c.created_at, c.updated_at, v.story_id
		FROM comments c
		JOIN story_versions v ON v.id = c.version_id
		WHERE c.id = $1`

	comment, err := scanCommentWithStory(s.pool.QueryRow(ctx, query, id))
	if err != nil {
		if errors.Is(err, pgx.ErrNoRows) {
			return Comment{}, ErrNotFound
		}
		return Comment{}, fmt.Errorf("conversations: get comment: %w", err)
	}

	return comment, nil
}

// VersionAuthor returns the author of the version with the given id, or
// ErrNotFound when no such version exists.
//
// A malformed id is treated as not-found, for the same reason as GetComment.
func (s *PostgresStore) VersionAuthor(ctx context.Context, versionID string) (string, error) {
	if !isUUID(versionID) {
		return "", ErrNotFound
	}

	const query = `SELECT author_id FROM story_versions WHERE id = $1`

	var authorID string
	if err := s.pool.QueryRow(ctx, query, versionID).Scan(&authorID); err != nil {
		if errors.Is(err, pgx.ErrNoRows) {
			return "", ErrNotFound
		}
		return "", fmt.Errorf("conversations: version author: %w", err)
	}

	return authorID, nil
}

// ListComments returns one page of a version's top-level comments, newest first,
// plus the cursor that resumes after it (nil when this page is the last one).
//
// Replies are not returned here: they are fetched per page by ListReplies, so a
// page is a page of threads rather than a slice of a flat stream that could cut a
// reply off from its parent. Pagination therefore counts top-level comments, and
// the cursor still seeks on the version's own index
// `comments_version_id_created_at_idx` (version_id, created_at DESC, id DESC) — the
// next page is everything strictly "before" the last parent of this page in that
// order (KNOT-ADR-047).
//
// It returns ErrNotFound when the version does not exist, so the caller can tell
// "no such version" from "a version with no comments".
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

	query := `SELECT ` + commentColumns + ` FROM comments WHERE version_id = $1 AND parent_comment_id IS NULL`
	args := []any{versionID}

	// Hide comments authored by anyone on either side of a block with the viewer.
	if excluded := moderation.ExcludedAuthors(ctx); len(excluded) > 0 {
		args = append(args, excluded)
		query += fmt.Sprintf(` AND author_id <> ALL($%d::uuid[])`, len(args))
	}

	if cursor != nil {
		// The explicit casts are required because a row comparison does not
		// reliably infer the parameter types, and they are safe because
		// DecodeCursor has already proven the id is canonical UUID text.
		args = append(args, cursor.CreatedAt(), cursor.ID())
		query += fmt.Sprintf(` AND (created_at, id) < ($%d::timestamptz, $%d::uuid)`, len(args)-1, len(args))
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

// ListReplies returns every reply to the given parent comments, oldest first
// within each parent, and an empty slice when there are none.
//
// It takes a whole page's parents, so assembling a page of a thread is one extra
// query whatever the number of comments on it. The ids come from rows this store
// just read, so they are canonical UUID text and the cast to uuid[] cannot fail.
func (s *PostgresStore) ListReplies(ctx context.Context, parentIDs []string) ([]Comment, error) {
	if len(parentIDs) == 0 {
		return []Comment{}, nil
	}

	query := `
		SELECT ` + commentColumns + `
		FROM comments
		WHERE parent_comment_id = ANY($1::uuid[])`
	args := []any{parentIDs}

	// Hide replies authored by anyone on either side of a block with the viewer.
	if excluded := moderation.ExcludedAuthors(ctx); len(excluded) > 0 {
		args = append(args, excluded)
		query += fmt.Sprintf(` AND author_id <> ALL($%d::uuid[])`, len(args))
	}

	query += ` ORDER BY created_at ASC, id ASC`

	rows, err := s.pool.Query(ctx, query, args...)
	if err != nil {
		return nil, fmt.Errorf("conversations: list replies: %w", err)
	}
	defer rows.Close()

	replies := make([]Comment, 0)
	for rows.Next() {
		comment, err := scanComment(rows)
		if err != nil {
			return nil, fmt.Errorf("conversations: list replies: %w", err)
		}
		replies = append(replies, comment)
	}
	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("conversations: list replies: %w", err)
	}

	return replies, nil
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
		&comment.ParentCommentID,
		&comment.CreatedAt,
		&comment.UpdatedAt,
	)
	if err != nil {
		return Comment{}, err
	}

	return comment, nil
}

// scanCommentWithStory reads one row of GetComment's join (the comment columns
// plus the story id of the comment's version) into a Comment with StoryID set.
// It returns the driver error unwrapped so callers can classify it.
func scanCommentWithStory(row rowScanner) (Comment, error) {
	var comment Comment

	err := row.Scan(
		&comment.ID,
		&comment.VersionID,
		&comment.AuthorID,
		&comment.Language,
		&comment.Body,
		&comment.ParentCommentID,
		&comment.CreatedAt,
		&comment.UpdatedAt,
		&comment.StoryID,
	)
	if err != nil {
		return Comment{}, err
	}

	return comment, nil
}

// isForeignKeyViolation reports whether err is a foreign key violation on the
// named constraint.
func isForeignKeyViolation(err error, constraint string) bool {
	var pgErr *pgconn.PgError
	return errors.As(err, &pgErr) && pgErr.Code == foreignKeyViolation && pgErr.ConstraintName == constraint
}

// nullIfEmpty maps "" onto SQL NULL, which is what the nullable columns expect.
func nullIfEmpty(value string) *string {
	if value == "" {
		return nil
	}
	return &value
}
