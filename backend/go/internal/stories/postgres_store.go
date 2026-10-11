package stories

import (
	"context"
	"errors"
	"fmt"
	"strings"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"

	"github.com/knot/backend/internal/moderation"
)

// storyColumns is the canonical SELECT column list for a story read together
// with its root version. The language, title, and body come from the root version
// (aliased v), which is why every read joins story_versions. It is a constant so
// every query in this file stays consistent with scanStory, and its order is the
// order scanStory reads.
const storyColumns = `s.id, s.author_id, s.root_version_id, s.pillar, v.language, v.title, v.body, s.approximate_location, s.latitude, s.longitude, s.place_country, s.media_urls, s.sensitive, s.created_at, s.updated_at`

// storyFrom resolves each story's root version content. root_version_id is
// NOT NULL and unique, so the join is always one row and never drops a story.
const storyFrom = ` FROM stories s JOIN story_versions v ON v.id = s.root_version_id`

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

// CreateStory inserts a story together with its root version and returns the
// stored row, including the ids and timestamps PostgreSQL generated.
//
// The two rows reference each other: stories.root_version_id points at the
// version and story_versions.story_id points back. With an immediate foreign key
// neither row can be inserted first, so both are written by a single statement
// with two data-modifying CTEs. PostgreSQL checks the constraint after the whole
// statement, by which point both rows exist, so the write is atomic without a
// deferrable constraint.
func (s *PostgresStore) CreateStory(ctx context.Context, story Story) (Story, error) {
	// approximate_location_lower is a normalised copy of approximate_location,
	// maintained here so discovery can group by place without treating casing or
	// surrounding space as a different place. The expression matches the one in
	// migration 0006, so an existing row and a newly written row normalise alike.
	const query = `
		WITH new_story AS (
			INSERT INTO stories (
				author_id, pillar, approximate_location, approximate_location_lower, media_urls, sensitive,
				latitude, longitude, place_country, root_version_id
			)
			VALUES ($1, $2, $3, lower(trim($3)), $4, $5, $9, $10, $11, gen_random_uuid())
			RETURNING id, author_id, root_version_id, pillar, approximate_location, latitude, longitude, place_country, media_urls, sensitive, created_at, updated_at
		), new_version AS (
			INSERT INTO story_versions (
				id, story_id, parent_version_id, author_id, language, title, body
			)
			SELECT root_version_id, id, NULL, author_id, $6, $7, $8 FROM new_story
			RETURNING language, title, body
		)
		SELECT
			s.id, s.author_id, s.root_version_id, s.pillar, v.language, v.title, v.body,
			s.approximate_location, s.latitude, s.longitude, s.place_country,
			s.media_urls, s.sensitive, s.created_at, s.updated_at
		FROM new_story s, new_version v`

	row := s.pool.QueryRow(
		ctx,
		query,
		story.AuthorID,
		string(story.Pillar),
		nullIfEmpty(story.ApproximateLocation),
		nonNilURLs(story.MediaURLs),
		story.Sensitive,
		story.Language,
		story.Title,
		story.Body,
		story.Latitude,
		story.Longitude,
		story.PlaceCountry,
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

	const query = `SELECT ` + storyColumns + storyFrom + ` WHERE s.id = $1`

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
	query := `SELECT ` + storyColumns + storyFrom
	args := make([]any, 0, 4)
	conditions := make([]string, 0, 2)

	// Hide stories authored by anyone on either side of a block with the viewer.
	// An anonymous reader carries no set, so the predicate is simply absent.
	if excluded := moderation.ExcludedAuthors(ctx); len(excluded) > 0 {
		args = append(args, excluded)
		conditions = append(conditions, fmt.Sprintf("s.author_id <> ALL($%d::uuid[])", len(args)))
	}

	if cursor != nil {
		// The explicit casts are required because a row comparison does not
		// reliably infer the parameter types, and they are safe because
		// DecodeCursor has already proven the id is canonical UUID text.
		args = append(args, cursor.CreatedAt(), cursor.ID())
		conditions = append(conditions, fmt.Sprintf("(s.created_at, s.id) < ($%d::timestamptz, $%d::uuid)", len(args)-1, len(args)))
	}

	if len(conditions) > 0 {
		query += " WHERE " + strings.Join(conditions, " AND ")
	}

	args = append(args, limit+1)
	query += fmt.Sprintf(` ORDER BY s.created_at DESC, s.id DESC LIMIT $%d`, len(args))

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
		country  *string
		media    []string
	)

	err := row.Scan(
		&story.ID,
		&story.AuthorID,
		&story.RootVersionID,
		&pillar,
		&story.Language,
		&story.Title,
		&story.Body,
		&location,
		&story.Latitude,
		&story.Longitude,
		&country,
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
	story.PlaceCountry = country
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
