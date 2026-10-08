package discovery

import (
	"context"
	"fmt"
	"time"

	"github.com/jackc/pgx/v5/pgxpool"

	"github.com/knot/backend/internal/stories"
)

// storyColumns is the canonical SELECT column list for a story read together
// with its root version. It matches the stories store's own list, so a story
// returned from a place looks identical to one returned from the feed. The
// aliases differ only in the joined version (rv rather than v).
const storyColumns = `s.id, s.author_id, s.root_version_id, s.pillar, rv.language, rv.title, rv.body, s.approximate_location, s.media_urls, s.sensitive, s.created_at, s.updated_at`

// storyFrom resolves each story's root version content. root_version_id is NOT
// NULL and unique, so the join is always one row and never drops a story.
const storyFrom = ` FROM stories s JOIN story_versions rv ON rv.id = s.root_version_id`

// PostgresStore is the pgx-backed implementation of DiscoveryStore.
//
// It holds an injected pool and no other state, so it is safe for concurrent use.
// It reads the stories and story_versions tables directly rather than going
// through the stories store: discovery is a different read shape over the same
// data, and keeping its query here avoids widening the stories interface for a
// concern the stories domain does not have.
type PostgresStore struct {
	pool *pgxpool.Pool
}

// Compile-time proof that the store satisfies the domain contract.
var _ DiscoveryStore = (*PostgresStore)(nil)

// NewPostgresStore returns a store backed by pool. The pool is injected rather
// than reached for globally so that callers own its lifecycle.
func NewPostgresStore(pool *pgxpool.Pool) (*PostgresStore, error) {
	if pool == nil {
		return nil, fmt.Errorf("discovery: postgres store requires a non-nil pool")
	}
	return &PostgresStore{pool: pool}, nil
}

// ListClusters aggregates stories by place.
//
// Grouping is by the normalised `approximate_location_lower`, so "Cape Town" and
// "cape town" are one cluster; the displayed Place is a representative original
// spelling from the group. Rows with no location are excluded, because a story
// with no place cannot belong to a place cluster.
//
// The language counts come from each story's **root version** only. Aggregating
// every version of every story would need a second, heavier join for a figure the
// map does not display; the root version's language is the story's primary
// language, and that is enough for the MVP. The language *filter* still matches
// any version, so filtering by a language finds a place where that language is
// spoken even if it is not the place's dominant one. Both behaviours are recorded
// in KNOT-ADR-020.
//
// Ordering is by story count descending, then by the normalised place ascending
// so equal counts are ordered deterministically rather than arbitrarily.
func (s *PostgresStore) ListClusters(ctx context.Context, filter ClusterFilter) ([]PlaceCluster, error) {
	query := `
		SELECT
			s.approximate_location_lower AS place_lower,
			MIN(s.approximate_location) AS place,
			COUNT(*) AS story_count,
			COUNT(*) FILTER (WHERE s.pillar = 'wonder') AS wonder_count,
			COUNT(*) FILTER (WHERE s.pillar = 'heritage') AS heritage_count,
			array_agg(DISTINCT rv.language ORDER BY rv.language) AS languages,
			MAX(s.created_at) AS latest_story_at
		FROM stories s
		JOIN story_versions rv ON rv.id = s.root_version_id
		WHERE s.approximate_location_lower IS NOT NULL`

	args := make([]any, 0, 3)

	if filter.Pillar != "" {
		args = append(args, string(filter.Pillar))
		query += fmt.Sprintf(` AND s.pillar = $%d`, len(args))
	}
	if filter.Language != "" {
		args = append(args, filter.Language)
		query += fmt.Sprintf(` AND EXISTS (
			SELECT 1 FROM story_versions sv
			WHERE sv.story_id = s.id AND sv.language = $%d
		)`, len(args))
	}

	query += `
		GROUP BY s.approximate_location_lower
		ORDER BY story_count DESC, s.approximate_location_lower ASC`

	// The limit is always the final placeholder.
	args = append(args, filter.Limit)
	query += fmt.Sprintf(` LIMIT $%d`, len(args))

	rows, err := s.pool.Query(ctx, query, args...)
	if err != nil {
		return nil, fmt.Errorf("discovery: list clusters: %w", err)
	}
	defer rows.Close()

	clusters := make([]PlaceCluster, 0, filter.Limit)
	for rows.Next() {
		var (
			placeLower    string
			place         *string
			storyCount    int64
			wonderCount   int64
			heritageCount int64
			languages     []string
			latest        time.Time
		)

		if err := rows.Scan(&placeLower, &place, &storyCount, &wonderCount, &heritageCount, &languages, &latest); err != nil {
			return nil, fmt.Errorf("discovery: list clusters: %w", err)
		}

		display := placeLower
		if place != nil && *place != "" {
			display = *place
		}

		clusters = append(clusters, PlaceCluster{
			Place:      display,
			StoryCount: int(storyCount),
			PillarCounts: map[stories.Pillar]int{
				stories.PillarWonder:   int(wonderCount),
				stories.PillarHeritage: int(heritageCount),
			},
			Languages:     nonNilStrings(languages),
			LatestStoryAt: latest,
		})
	}
	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("discovery: list clusters: %w", err)
	}

	return clusters, nil
}

// ListStoriesAtPlace returns one page of the stories at a place, newest first,
// plus the cursor that resumes after it (nil when this page is the last one).
//
// The place is expected already normalised (trimmed and lower-cased) by the
// service, so the equality test uses the `stories_approximate_location_lower_idx`
// index directly. Pagination is keyset, exactly as for the feed: the ORDER BY key
// is (created_at DESC, id DESC), the page predicate is the matching row
// comparison, and one extra row is fetched to decide whether more remain.
func (s *PostgresStore) ListStoriesAtPlace(ctx context.Context, placeLower string, cursor *stories.Cursor, limit int) ([]stories.Story, *stories.Cursor, error) {
	query := `SELECT ` + storyColumns + storyFrom + ` WHERE s.approximate_location_lower = $1`
	args := []any{placeLower}

	if cursor != nil {
		// The explicit casts are required because a row comparison does not
		// reliably infer the parameter types, and they are safe because
		// DecodeCursor has already proven the id is canonical UUID text.
		query += ` AND (s.created_at, s.id) < ($2::timestamptz, $3::uuid)`
		args = append(args, cursor.CreatedAt(), cursor.ID())
	}

	query += fmt.Sprintf(` ORDER BY s.created_at DESC, s.id DESC LIMIT $%d`, len(args)+1)
	args = append(args, limit+1)

	rows, err := s.pool.Query(ctx, query, args...)
	if err != nil {
		return nil, nil, fmt.Errorf("discovery: list stories at place: %w", err)
	}
	defer rows.Close()

	page := make([]stories.Story, 0, limit)
	for rows.Next() {
		story, err := scanStory(rows)
		if err != nil {
			return nil, nil, fmt.Errorf("discovery: list stories at place: %w", err)
		}
		page = append(page, story)
	}
	if err := rows.Err(); err != nil {
		return nil, nil, fmt.Errorf("discovery: list stories at place: %w", err)
	}

	if len(page) <= limit {
		return page, nil, nil
	}

	last := page[limit-1]
	next := stories.NewCursor(last.CreatedAt, last.ID)

	return page[:limit], &next, nil
}

// rowScanner is the subset of pgx.Row and pgx.Rows that scanStory needs.
type rowScanner interface {
	Scan(dest ...any) error
}

// scanStory reads one row of storyColumns into a stories.Story. It mirrors the
// stories store's own scanner; the two are kept separate because the column
// aliases differ, and a shared helper would have to live in one package and be
// exported from it for the other to use.
func scanStory(row rowScanner) (stories.Story, error) {
	var (
		story    stories.Story
		pillar   string
		location *string
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
		&media,
		&story.Sensitive,
		&story.CreatedAt,
		&story.UpdatedAt,
	)
	if err != nil {
		return stories.Story{}, err
	}

	story.Pillar = stories.Pillar(pillar)
	if location != nil {
		story.ApproximateLocation = *location
	}
	story.MediaURLs = nonNilStrings(media)

	return story, nil
}

// nonNilStrings guarantees a non-nil slice so a JSON response never contains
// null where a list belongs.
func nonNilStrings(values []string) []string {
	if values == nil {
		return []string{}
	}
	return values
}
