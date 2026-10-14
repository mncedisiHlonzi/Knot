package discovery

import (
	"context"
	"fmt"
	"time"

	"github.com/jackc/pgx/v5/pgxpool"

	"github.com/knot/backend/internal/moderation"
	"github.com/knot/backend/internal/stories"
)

// storyColumns is the canonical SELECT column list for a story read together
// with its root version. It matches the stories store's own list, so a story
// returned from a place looks identical to one returned from the feed. The
// aliases differ only in the joined version (rv rather than v).
const storyColumns = `s.id, s.author_id, s.root_version_id, s.pillar, rv.language, rv.title, rv.body, s.approximate_location, s.latitude, s.longitude, s.place_country, s.media_urls, s.sensitive, s.created_at, s.updated_at`

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

// ListClusters aggregates stories by place and country.
//
// A place is identified by the normalised lower case form of its name *and* the
// country the geocoder reported, so "Cape Town" and "cape town" are one cluster
// while two "Beijing" stories far apart on the map are still one cluster. The
// country is part of the key rather than decoration: a story with no country is
// its own bucket, so a bare "Springfield" never merges with a "Springfield,
// United States". The displayed Place is the earliest member's original spelling,
// so the author's capitalisation survives.
//
// The cluster's Latitude and Longitude are the **average** of its members'
// coordinates (KNOT-020e-fix), so the marker sits among the stories rather than
// on whichever one a non-deterministic MIN happened to pick. A cluster whose
// members carry no coordinate at all averages to NULL, and the client falls back
// to its own lookup table exactly as a legacy story did before structured place
// data existed (KNOT-ADR-034).
//
// Rows with no place name are excluded, because they cannot belong to a place
// cluster.
//
// The language counts come from each story's **root version** only. Aggregating
// every version of every story would need a second, heavier join for a figure the
// map does not display; the root version's language is the story's primary
// language, and that is enough for the MVP. The language *filter* still matches
// any version, so filtering by a language finds a place where that language is
// spoken even if it is not the place's dominant one. Both behaviours are recorded
// in KNOT-ADR-020.
//
// Ordering is by story count descending, then by the cluster key ascending so
// equal counts are ordered deterministically rather than arbitrarily.
func (s *PostgresStore) ListClusters(ctx context.Context, filter ClusterFilter) ([]PlaceCluster, error) {
	query := `
		SELECT
			(array_agg(s.approximate_location ORDER BY s.created_at ASC, s.id ASC)
				FILTER (WHERE s.approximate_location IS NOT NULL))[1] AS place,
			s.place_country AS place_country,
			AVG(s.latitude) AS latitude,
			AVG(s.longitude) AS longitude,
			COUNT(*) AS story_count,
			COUNT(*) FILTER (WHERE s.pillar = 'wonder') AS wonder_count,
			COUNT(*) FILTER (WHERE s.pillar = 'heritage') AS heritage_count,
			array_agg(DISTINCT rv.language ORDER BY rv.language) AS languages,
			MAX(s.created_at) AS latest_story_at
		FROM stories s
		JOIN story_versions rv ON rv.id = s.root_version_id
		WHERE s.approximate_location_lower IS NOT NULL`

	args := make([]any, 0, 4)

	// Hide stories authored by anyone on either side of a block with the viewer.
	// The predicate is applied inside the WHERE clause, before the grouping, so a
	// cluster's story count and language set reflect only the stories the viewer
	// can see, and a cluster left with no visible stories disappears entirely. An
	// anonymous reader carries no set, so the predicate is simply absent and the
	// query is byte-for-byte what it was before.
	if excluded := moderation.ExcludedAuthors(ctx); len(excluded) > 0 {
		args = append(args, excluded)
		query += fmt.Sprintf(` AND s.author_id <> ALL($%d::uuid[])`, len(args))
	}

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
		GROUP BY s.approximate_location_lower, s.place_country
		ORDER BY story_count DESC, s.approximate_location_lower ASC, s.place_country ASC`

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
			place         *string
			placeCountry  *string
			latitude      *float64
			longitude     *float64
			storyCount    int64
			wonderCount   int64
			heritageCount int64
			languages     []string
			latest        time.Time
		)

		if err := rows.Scan(&place, &placeCountry, &latitude, &longitude, &storyCount, &wonderCount, &heritageCount, &languages, &latest); err != nil {
			return nil, fmt.Errorf("discovery: list clusters: %w", err)
		}

		display := ""
		if place != nil {
			display = *place
		}

		clusters = append(clusters, PlaceCluster{
			Place:        display,
			PlaceCountry: placeCountry,
			Latitude:     latitude,
			Longitude:    longitude,
			StoryCount:   int(storyCount),
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
// index directly. country narrows the page further, to the stories a cluster with
// that country holds; its zero value lists every story at the place, including
// the ones from a different country with the same name. The country predicate is
// appended after the cursor predicate so the cursor keeps its fixed $2/$3
// placeholders (KNOT-020e-fix2).
//
// Pagination is keyset, exactly as for the feed: the ORDER BY key is
// (created_at DESC, id DESC), the page predicate is the matching row comparison,
// and one extra row is fetched to decide whether more remain.
func (s *PostgresStore) ListStoriesAtPlace(ctx context.Context, placeLower string, country PlaceCountryFilter, cursor *stories.Cursor, limit int) ([]stories.Story, *stories.Cursor, error) {
	query := `SELECT ` + storyColumns + storyFrom + ` WHERE s.approximate_location_lower = $1`
	args := []any{placeLower}

	if cursor != nil {
		// The explicit casts are required because a row comparison does not
		// reliably infer the parameter types, and they are safe because
		// DecodeCursor has already proven the id is canonical UUID text.
		query += ` AND (s.created_at, s.id) < ($2::timestamptz, $3::uuid)`
		args = append(args, cursor.CreatedAt(), cursor.ID())
	}

	// Narrow to one country. A cluster with no country is keyed by a NULL country,
	// so "no country" is an IS NULL test rather than an equality one. The country
	// is matched verbatim: the client passes back exactly the string the cluster
	// reported.
	if country.Absent {
		query += ` AND s.place_country IS NULL`
	} else if country.Name != "" {
		args = append(args, country.Name)
		query += fmt.Sprintf(` AND s.place_country = $%d`, len(args))
	}

	// Hide stories authored by anyone on either side of a block with the viewer. It
	// is appended after the cursor predicate so the cursor keeps its fixed $2/$3
	// placeholders. An anonymous reader carries no set, so the predicate is absent
	// and the page is exactly what it was before.
	if excluded := moderation.ExcludedAuthors(ctx); len(excluded) > 0 {
		args = append(args, excluded)
		query += fmt.Sprintf(` AND s.author_id <> ALL($%d::uuid[])`, len(args))
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
		return stories.Story{}, err
	}

	story.Pillar = stories.Pillar(pillar)
	if location != nil {
		story.ApproximateLocation = *location
	}
	story.PlaceCountry = country
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
