package stories

import (
	"context"
	"errors"
	"fmt"
	"strings"
	"time"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"

	"github.com/knot/backend/internal/moderation"
)

// storyColumns is the canonical SELECT column list for a story read together
// with its root version. The language, title, and body come from the root version
// (aliased v), which is why every read joins story_versions. It is a constant so
// every query in this file stays consistent with scanStory, and its order is the
// order scanStory reads.
const storyColumns = `s.id, s.author_id, s.root_version_id, s.pillar, v.language, v.title, v.body, s.approximate_location, s.latitude, s.longitude, s.place_country, s.media_urls, s.created_at, s.updated_at`

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
				author_id, pillar, approximate_location, approximate_location_lower, media_urls,
				latitude, longitude, place_country, root_version_id
			)
			VALUES ($1, $2, $3, lower(trim($3)), $4, $8, $9, $10, gen_random_uuid())
			RETURNING id, author_id, root_version_id, pillar, approximate_location, latitude, longitude, place_country, media_urls, created_at, updated_at
		), new_version AS (
			INSERT INTO story_versions (
				id, story_id, parent_version_id, author_id, language, title, body
			)
			SELECT root_version_id, id, NULL, author_id, $5, $6, $7 FROM new_story
			RETURNING language, title, body
		)
		SELECT
			s.id, s.author_id, s.root_version_id, s.pillar, v.language, v.title, v.body,
			s.approximate_location, s.latitude, s.longitude, s.place_country,
			s.media_urls, s.created_at, s.updated_at
		FROM new_story s, new_version v`

	row := s.pool.QueryRow(
		ctx,
		query,
		story.AuthorID,
		string(story.Pillar),
		nullIfEmpty(story.ApproximateLocation),
		nonNilURLs(story.MediaURLs),
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

// feedStoryBranch and feedVersionBranch are the two halves of the merged feed.
//
// They select the same eighteen columns in the same order, so the union can be
// ordered and scanned as one stream: the discriminator, the entry's own id, the
// parent story's id, the version's parent, the author, the content, the framing
// (pillar and place), the media, the timestamps, and the story's root version.
// A column that does not apply to a branch is selected as ” (or NULL) rather
// than being left out, because a union needs one shape.
//
// The uuid columns are cast to text and an absent value is the empty string, which
// is this package's convention for a nullable id (docs/DATA_MODEL.md), so nothing
// in the row is nullable-as-uuid except the two coordinate columns.
const feedStoryBranch = `
	SELECT
		'story' AS kind,
		s.id::text AS id,
		s.id::text AS story_id,
		''::text AS parent_version_id,
		s.author_id::text AS author_id,
		v.language, v.title, v.body,
		s.pillar::text AS pillar,
		s.approximate_location, s.latitude, s.longitude, s.place_country,
		s.media_urls,
		s.created_at, s.updated_at,
		s.root_version_id::text AS root_version_id
	FROM stories s
	JOIN story_versions v ON v.id = s.root_version_id`

const feedVersionBranch = `
	SELECT
		'version' AS kind,
		v.id::text AS id,
		v.story_id::text AS story_id,
		v.parent_version_id::text AS parent_version_id,
		v.author_id::text AS author_id,
		v.language, v.title, v.body,
		s.pillar::text AS pillar,
		s.approximate_location, s.latitude, s.longitude, s.place_country,
		s.media_urls,
		v.created_at, v.updated_at,
		''::text AS root_version_id
	FROM story_versions v
	JOIN stories s ON s.id = v.story_id`

// ListFeed returns one page of the merged feed, newest first, plus the cursor
// that resumes after it (nil when this page is the last one).
//
// The feed is a UNION ALL of stories and their adaptations -- `story_versions`
// rows with a parent. A **root version is never a feed entry of its own**: it is
// the story, and the story branch already carries its content, so repeating it
// would show every story twice (KNOT-022, KNOT-ADR-070).
//
// Pagination is the keyset comparison this feed has always used, applied to the
// merged stream: ORDER BY (created_at DESC, id DESC) with
// (created_at, id) < (cursor.created_at, cursor.id). Both sort keys come from the
// entry itself -- a version sorts by its own created_at, not its story's -- so an
// adaptation appears where it was written. The story branch is served from
// stories_created_at_id_idx and the version branch from
// story_versions_created_at_id_idx (migration 0017), which lets the planner merge
// two ordered index scans instead of sorting the union.
//
// The block filter is applied to each branch on **its own author**: a viewer who
// blocks A stops seeing A's story and stops seeing any version A wrote, while a
// version B wrote of A's story stays visible. A block hides an author, not a
// lineage -- the same rule the version endpoints follow (KNOT-ADR-060,
// KNOT-ADR-069).
//
// One extra row is fetched to decide whether more remain; that row is not
// returned, and the cursor is derived from the last row that is.
func (s *PostgresStore) ListFeed(ctx context.Context, cursor *Cursor, limit int) ([]FeedItem, *Cursor, error) {
	storyConditions := make([]string, 0, 2)
	versionConditions := []string{"v.parent_version_id IS NOT NULL"}
	args := make([]any, 0, 4)

	// Hide content authored by anyone on either side of a block with the viewer,
	// in both branches. An anonymous reader carries no set, so the predicate is
	// simply absent.
	if excluded := moderation.ExcludedAuthors(ctx); len(excluded) > 0 {
		args = append(args, excluded)
		storyConditions = append(storyConditions, fmt.Sprintf("s.author_id <> ALL($%d::uuid[])", len(args)))
		versionConditions = append(versionConditions, fmt.Sprintf("v.author_id <> ALL($%d::uuid[])", len(args)))
	}

	if cursor != nil {
		// The explicit casts are required because a row comparison does not
		// reliably infer the parameter types, and they are safe because
		// DecodeCursor has already proven the id is canonical UUID text. Both
		// branches share the two parameters, so the cursor costs nothing extra.
		args = append(args, cursor.CreatedAt(), cursor.ID())
		timestampParam, idParam := len(args)-1, len(args)
		storyConditions = append(storyConditions, fmt.Sprintf(
			"(s.created_at, s.id) < ($%d::timestamptz, $%d::uuid)", timestampParam, idParam,
		))
		versionConditions = append(versionConditions, fmt.Sprintf(
			"(v.created_at, v.id) < ($%d::timestamptz, $%d::uuid)", timestampParam, idParam,
		))
	}

	query := feedStoryBranch
	if len(storyConditions) > 0 {
		query += " WHERE " + strings.Join(storyConditions, " AND ")
	}
	query += " UNION ALL " + feedVersionBranch + " WHERE " + strings.Join(versionConditions, " AND ")

	args = append(args, limit+1)
	query += fmt.Sprintf(" ORDER BY created_at DESC, id DESC LIMIT $%d", len(args))

	rows, err := s.pool.Query(ctx, query, args...)
	if err != nil {
		return nil, nil, fmt.Errorf("stories: list feed: %w", err)
	}
	defer rows.Close()

	page := make([]FeedItem, 0, limit)
	for rows.Next() {
		item, err := scanFeedItem(rows)
		if err != nil {
			return nil, nil, fmt.Errorf("stories: list feed: %w", err)
		}
		page = append(page, item)
	}
	if err := rows.Err(); err != nil {
		return nil, nil, fmt.Errorf("stories: list feed: %w", err)
	}

	if len(page) <= limit {
		return page, nil, nil
	}

	// The cursor is built from the entry's own sort key, which SortTime and
	// SortID answer for both kinds (a version sorts by its own timestamp).
	last := page[limit-1]
	next := NewCursor(last.SortTime(), last.SortID())

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

// scanFeedItem reads one row of the merged feed into a FeedItem. It returns the
// driver error unwrapped so callers can classify it.
//
// The two branches select the same columns in the same order, so one scan serves
// both; `kind` decides which half of the FeedItem is filled.
func scanFeedItem(row rowScanner) (FeedItem, error) {
	var (
		kind            string
		id              string
		storyID         string
		parentVersionID string
		authorID        string
		language        string
		title           string
		body            string
		pillar          string
		location        *string
		latitude        *float64
		longitude       *float64
		country         *string
		media           []string
		createdAt       time.Time
		updatedAt       time.Time
		rootVersionID   string
	)

	err := row.Scan(
		&kind,
		&id,
		&storyID,
		&parentVersionID,
		&authorID,
		&language,
		&title,
		&body,
		&pillar,
		&location,
		&latitude,
		&longitude,
		&country,
		&media,
		&createdAt,
		&updatedAt,
		&rootVersionID,
	)
	if err != nil {
		return FeedItem{}, err
	}

	place := ""
	if location != nil {
		place = *location
	}

	if kind == string(FeedKindVersion) {
		return FeedItem{
			Kind: FeedKindVersion,
			Version: VersionFeedItem{
				ID:                  id,
				StoryID:             storyID,
				ParentVersionID:     parentVersionID,
				AuthorID:            authorID,
				Language:            language,
				Title:               title,
				Body:                body,
				CreatedAt:           createdAt,
				UpdatedAt:           updatedAt,
				Pillar:              Pillar(pillar),
				ApproximateLocation: place,
				Latitude:            latitude,
				Longitude:           longitude,
				PlaceCountry:        country,
				MediaURLs:           nonNilURLs(media),
			},
		}, nil
	}

	// Anything that is not the version discriminator is a story. The SQL hardcodes
	// both literals, so this branch is only reachable with 'story' in practice.
	return FeedItem{
		Kind: FeedKindStory,
		Story: Story{
			ID:                  storyID,
			AuthorID:            authorID,
			RootVersionID:       rootVersionID,
			Pillar:              Pillar(pillar),
			Language:            language,
			Title:               title,
			Body:                body,
			ApproximateLocation: place,
			Latitude:            latitude,
			Longitude:           longitude,
			PlaceCountry:        country,
			MediaURLs:           nonNilURLs(media),
			CreatedAt:           createdAt,
			UpdatedAt:           updatedAt,
		},
	}, nil
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
