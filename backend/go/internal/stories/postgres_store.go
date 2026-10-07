package stories

import (
	"context"
	"errors"
	"fmt"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"
)

// storyColumns is the canonical SELECT/RETURNING column list. It is a constant so
// every query in this file stays consistent with scanStory, and its order is the
// order scanStory reads.
const storyColumns = `id, author_id, pillar, language, title, body, approximate_location, media_urls, sensitive, created_at, updated_at`

// PostgresStore is the pgx-backed implementation of StoryStore.
//
// It holds an injected pool and no other state, so it is safe for concurrent use.
type PostgresStore struct {
	pool *pgxpool.Pool
}

// Compile-time proof that the store satisfies the domain contract.
var _ StoryStore = (*PostgresStore)(nil)

// NewPostgresStore returns a store backed by pool. The pool is injected rather
// than reached for globally so that callers own its lifecycle.
func NewPostgresStore(pool *pgxpool.Pool) (*PostgresStore, error) {
	if pool == nil {
		return nil, fmt.Errorf("stories: postgres store requires a non-nil pool")
	}
	return &PostgresStore{pool: pool}, nil
}

// CreateStory inserts a story and returns the stored row, including the id and
// timestamps PostgreSQL generated.
func (s *PostgresStore) CreateStory(ctx context.Context, story Story) (Story, error) {
	const query = `
		INSERT INTO stories (
			author_id, pillar, language, title, body, approximate_location, media_urls, sensitive
		)
		VALUES ($1, $2, $3, $4, $5, $6, $7, $8)
		RETURNING ` + storyColumns

	row := s.pool.QueryRow(
		ctx,
		query,
		story.AuthorID,
		string(story.Pillar),
		story.Language,
		story.Title,
		story.Body,
		nullIfEmpty(story.ApproximateLocation),
		nonNilURLs(story.MediaURLs),
		story.Sensitive,
	)

	created, err := scanStory(row)
	if err != nil {
		return Story{}, fmt.Errorf("stories: create story: %w", err)
	}

	return created, nil
}

// GetStory returns the story with the given id, or ErrNotFound.
//
// A malformed id is treated as not-found rather than as a database error, which
// keeps the id column's index usable instead of casting it to text in SQL.
func (s *PostgresStore) GetStory(ctx context.Context, id string) (Story, error) {
	if !isUUID(id) {
		return Story{}, ErrNotFound
	}

	const query = `SELECT ` + storyColumns + ` FROM stories WHERE id = $1`

	story, err := scanStory(s.pool.QueryRow(ctx, query, id))
	if err != nil {
		if errors.Is(err, pgx.ErrNoRows) {
			return Story{}, ErrNotFound
		}
		return Story{}, fmt.Errorf("stories: get story: %w", err)
	}

	return story, nil
}

// ListStories returns one page of stories, newest first, plus the cursor that
// resumes after it (nil when this page is the last one).
//
// Pagination is keyset rather than offset-based: the ORDER BY key is
// (created_at DESC, id DESC), so the next page is everything strictly "before"
// the last row of this page in that same order. The row comparison
// (created_at, id) < ($1, $2) is exactly that predicate, and the id tiebreaker
// is what makes it correct when two stories share a timestamp. This matches the
// stories_created_at_id_idx index added in 0002_stories.up.sql, so PostgreSQL
// can serve the page from the index rather than sorting the table.
//
// One extra row is fetched to decide whether more remain; that row is not
// returned, and the cursor is derived from the last row that is.
func (s *PostgresStore) ListStories(ctx context.Context, cursor *Cursor, limit int) ([]Story, *Cursor, error) {
	query := `SELECT ` + storyColumns + ` FROM stories`
	args := make([]any, 0, 3)

	if cursor != nil {
		// The explicit casts are required because a row comparison does not
		// reliably infer the parameter types, and they are safe because
		// DecodeCursor has already proven the id is canonical UUID text.
		query += ` WHERE (created_at, id) < ($1::timestamptz, $2::uuid)`
		args = append(args, cursor.CreatedAt(), cursor.ID())
	}

	query += fmt.Sprintf(` ORDER BY created_at DESC, id DESC LIMIT $%d`, len(args)+1)
	args = append(args, limit+1)

	rows, err := s.pool.Query(ctx, query, args...)
	if err != nil {
		return nil, nil, fmt.Errorf("stories: list stories: %w", err)
	}
	defer rows.Close()

	page := make([]Story, 0, limit)
	for rows.Next() {
		story, err := scanStory(rows)
		if err != nil {
			return nil, nil, fmt.Errorf("stories: list stories: %w", err)
		}
		page = append(page, story)
	}
	if err := rows.Err(); err != nil {
		return nil, nil, fmt.Errorf("stories: list stories: %w", err)
	}

	if len(page) <= limit {
		return page, nil, nil
	}

	last := page[limit-1]
	next := NewCursor(last.CreatedAt, last.ID)

	return page[:limit], &next, nil
}

// rowScanner is the subset of pgx.Row and pgx.Rows that scanStory needs.
type rowScanner interface {
	Scan(dest ...any) error
}

// scanStory reads one row of storyColumns into a Story. It returns the driver
// error unwrapped so callers can classify it (for example as pgx.ErrNoRows).
func scanStory(row rowScanner) (Story, error) {
	var (
		story    Story
		pillar   string
		location *string
		media    []string
	)

	err := row.Scan(
		&story.ID,
		&story.AuthorID,
		&pillar,
		&story.Language,
		&story.Title,
		&story.Body,
		&location,
		&media,
		&story.Sensitive,
		&story.CreatedAt,
		&story.UpdatedAt,
	)
	if err != nil {
		return Story{}, err
	}

	story.Pillar = Pillar(pillar)
	if location != nil {
		story.ApproximateLocation = *location
	}
	story.MediaURLs = nonNilURLs(media)

	return story, nil
}

// nullIfEmpty maps "" onto SQL NULL, which is what the nullable columns expect.
func nullIfEmpty(value string) *string {
	if value == "" {
		return nil
	}
	return &value
}

// nonNilURLs guarantees a non-nil slice so a TEXT[] NOT NULL column never
// receives NULL, and so a JSON response never contains null where a list belongs.
func nonNilURLs(urls []string) []string {
	if urls == nil {
		return []string{}
	}
	return urls
}
