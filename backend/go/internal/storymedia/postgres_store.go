package storymedia

import (
	"context"
	"errors"
	"fmt"
	"strings"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"
)

// mediaColumns is the canonical SELECT/RETURNING column list. It is a constant
// so every query in this file stays consistent with scanMedia, and its order is
// the order scanMedia reads.
const mediaColumns = `id, story_id, uploader_id, storage_key, media_type, mime_type, source, width, height, duration_ms, size_bytes, display_order, created_at`

// PostgresStore is the pgx-backed implementation of StoryMediaStore.
//
// It holds an injected pool and no other state, so it is safe for concurrent use.
type PostgresStore struct {
	pool *pgxpool.Pool
}

// Compile-time proof that the store satisfies the domain contract.
var _ StoryMediaStore = (*PostgresStore)(nil)

// NewPostgresStore returns a store backed by pool. The pool is injected rather
// than reached for globally so that callers own its lifecycle.
func NewPostgresStore(pool *pgxpool.Pool) (*PostgresStore, error) {
	if pool == nil {
		return nil, fmt.Errorf("storymedia: postgres store requires a non-nil pool")
	}
	return &PostgresStore{pool: pool}, nil
}

// CreateMedia inserts a media row and returns the stored row.
//
// It runs in one transaction that first locks the story row and counts the
// story's media, so a story can never exceed MaxMediaPerStory even when two
// uploads arrive at once (KNOT-ADR-054). A count at the cap returns ErrMediaLimit
// and the transaction rolls back without writing.
//
// A DisplayOrder below zero on the incoming value asks the store to append the
// item after the story's current last one, computed in the same statement so two
// uploads cannot both read the same maximum. A non-negative value is honoured as
// given.
func (s *PostgresStore) CreateMedia(ctx context.Context, media StoryMedia) (StoryMedia, error) {
	tx, err := s.pool.Begin(ctx)
	if err != nil {
		return StoryMedia{}, fmt.Errorf("storymedia: begin create media: %w", err)
	}
	// A rollback after a successful commit is a no-op, so this covers every early
	// return.
	defer func() { _ = tx.Rollback(ctx) }()

	if err := checkMediaCap(ctx, tx, media.StoryID); err != nil {
		return StoryMedia{}, err
	}

	const query = `
		INSERT INTO story_media (
			story_id, uploader_id, storage_key, media_type, mime_type, source,
			width, height, duration_ms, size_bytes, display_order
		)
		VALUES (
			$1, $2, $3, $4, $5, $6, $7, $8, $9, $10,
			CASE
				WHEN $11 < 0 THEN (
					SELECT COALESCE(MAX(display_order) + 1, 0)
					FROM story_media
					WHERE story_id = $1
				)
				ELSE $11
			END
		)
		RETURNING ` + mediaColumns

	row := tx.QueryRow(
		ctx,
		query,
		media.StoryID,
		media.UploaderID,
		media.StorageKey,
		string(media.MediaType),
		media.MimeType,
		string(media.Source),
		media.Width,
		media.Height,
		media.DurationMS,
		media.SizeBytes,
		media.DisplayOrder,
	)

	created, err := scanMedia(row)
	if err != nil {
		return StoryMedia{}, fmt.Errorf("storymedia: create media: %w", err)
	}

	if err := tx.Commit(ctx); err != nil {
		return StoryMedia{}, fmt.Errorf("storymedia: commit create media: %w", err)
	}

	return created, nil
}

// checkMediaCap locks a story row and refuses the insert when the story already
// holds MaxMediaPerStory items.
//
// The lock is on stories.id — one stable row, and the entity whose media is being
// counted — not on story_media rows. It serialises every media writer for that
// story, so two concurrent uploads cannot both observe a count below the cap and
// both insert. The lock is released when the transaction commits or rolls back
// (KNOT-ADR-054).
func checkMediaCap(ctx context.Context, tx pgx.Tx, storyID string) error {
	var locked string
	if err := tx.QueryRow(ctx, `SELECT id FROM stories WHERE id = $1 FOR UPDATE`, storyID).Scan(&locked); err != nil {
		if errors.Is(err, pgx.ErrNoRows) {
			return ErrStoryNotFound
		}
		return fmt.Errorf("storymedia: lock story: %w", err)
	}

	var count int
	if err := tx.QueryRow(ctx, `SELECT COUNT(*) FROM story_media WHERE story_id = $1`, storyID).Scan(&count); err != nil {
		return fmt.Errorf("storymedia: count media: %w", err)
	}
	if count >= MaxMediaPerStory {
		return ErrMediaLimit
	}

	return nil
}

// CountMedia returns how many media items a story currently has.
//
// A malformed story id names no rows, so it counts zero rather than erroring.
func (s *PostgresStore) CountMedia(ctx context.Context, storyID string) (int, error) {
	if !isUUID(storyID) {
		return 0, nil
	}

	var count int
	if err := s.pool.QueryRow(
		ctx,
		`SELECT COUNT(*) FROM story_media WHERE story_id = $1`,
		storyID,
	).Scan(&count); err != nil {
		return 0, fmt.Errorf("storymedia: count media: %w", err)
	}

	return count, nil
}

// ListMedia returns every media item of a story in display order. The id
// tiebreaker makes the order deterministic when two items share a display_order.
func (s *PostgresStore) ListMedia(ctx context.Context, storyID string) ([]StoryMedia, error) {
	const query = `SELECT ` + mediaColumns + ` FROM story_media WHERE story_id = $1 ORDER BY display_order, created_at, id`

	rows, err := s.pool.Query(ctx, query, storyID)
	if err != nil {
		return nil, fmt.Errorf("storymedia: list media: %w", err)
	}
	defer rows.Close()

	page := make([]StoryMedia, 0)
	for rows.Next() {
		media, err := scanMedia(rows)
		if err != nil {
			return nil, fmt.Errorf("storymedia: list media: %w", err)
		}
		page = append(page, media)
	}
	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("storymedia: list media: %w", err)
	}

	return page, nil
}

// FirstMedia returns each story's first item, keyed by story id, for the listed
// stories that have any.
//
// It is a single query rather than one per story, so enriching a feed page does
// not fan out. DISTINCT ON keeps the first row of each story in the same order
// the detail view uses, so the preview and the detail agree on which item is
// first.
func (s *PostgresStore) FirstMedia(ctx context.Context, storyIDs []string) (map[string]StoryMedia, error) {
	if len(storyIDs) == 0 {
		return map[string]StoryMedia{}, nil
	}

	placeholders := make([]string, len(storyIDs))
	args := make([]any, len(storyIDs))
	for i, id := range storyIDs {
		placeholders[i] = fmt.Sprintf("$%d", i+1)
		args[i] = id
	}

	query := `SELECT DISTINCT ON (story_id) ` + mediaColumns +
		` FROM story_media WHERE story_id IN (` + strings.Join(placeholders, ", ") + `)` +
		` ORDER BY story_id, display_order, created_at, id`

	rows, err := s.pool.Query(ctx, query, args...)
	if err != nil {
		return nil, fmt.Errorf("storymedia: first media: %w", err)
	}
	defer rows.Close()

	first := make(map[string]StoryMedia, len(storyIDs))
	for rows.Next() {
		media, err := scanMedia(rows)
		if err != nil {
			return nil, fmt.Errorf("storymedia: first media: %w", err)
		}
		first[media.StoryID] = media
	}
	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("storymedia: first media: %w", err)
	}

	return first, nil
}

// GetMedia returns one media row, or ErrNotFound.
//
// A malformed id is treated as not-found rather than as a database error, which
// keeps the id column's index usable instead of casting it to text in SQL.
func (s *PostgresStore) GetMedia(ctx context.Context, id string) (StoryMedia, error) {
	if !isUUID(id) {
		return StoryMedia{}, ErrNotFound
	}

	const query = `SELECT ` + mediaColumns + ` FROM story_media WHERE id = $1`

	media, err := scanMedia(s.pool.QueryRow(ctx, query, id))
	if err != nil {
		if errors.Is(err, pgx.ErrNoRows) {
			return StoryMedia{}, ErrNotFound
		}
		return StoryMedia{}, fmt.Errorf("storymedia: get media: %w", err)
	}

	return media, nil
}

// DeleteMedia removes one media row, or reports ErrNotFound when no row matched.
func (s *PostgresStore) DeleteMedia(ctx context.Context, id string) error {
	if !isUUID(id) {
		return ErrNotFound
	}

	tag, err := s.pool.Exec(ctx, `DELETE FROM story_media WHERE id = $1`, id)
	if err != nil {
		return fmt.Errorf("storymedia: delete media: %w", err)
	}
	if tag.RowsAffected() == 0 {
		return ErrNotFound
	}

	return nil
}

// StoryAuthor returns the author id of a story, or ErrStoryNotFound.
func (s *PostgresStore) StoryAuthor(ctx context.Context, storyID string) (string, error) {
	if !isUUID(storyID) {
		return "", ErrStoryNotFound
	}

	var author string
	if err := s.pool.QueryRow(ctx, `SELECT author_id FROM stories WHERE id = $1`, storyID).Scan(&author); err != nil {
		if errors.Is(err, pgx.ErrNoRows) {
			return "", ErrStoryNotFound
		}
		return "", fmt.Errorf("storymedia: story author: %w", err)
	}

	return author, nil
}

// rowScanner is the subset of pgx.Row and pgx.Rows that scanMedia needs.
type rowScanner interface {
	Scan(dest ...any) error
}

// scanMedia reads one row of mediaColumns into a StoryMedia.
func scanMedia(row rowScanner) (StoryMedia, error) {
	var (
		media    StoryMedia
		kind     string
		source   string
		width    *int
		height   *int
		duration *int
	)

	err := row.Scan(
		&media.ID,
		&media.StoryID,
		&media.UploaderID,
		&media.StorageKey,
		&kind,
		&media.MimeType,
		&source,
		&width,
		&height,
		&duration,
		&media.SizeBytes,
		&media.DisplayOrder,
		&media.CreatedAt,
	)
	if err != nil {
		return StoryMedia{}, err
	}

	media.MediaType = MediaType(kind)
	media.Source = MediaSource(source)
	media.Width = width
	media.Height = height
	media.DurationMS = duration

	return media, nil
}
