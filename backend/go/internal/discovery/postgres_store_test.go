package discovery

import (
	"context"
	"fmt"
	"os"
	"strings"
	"testing"
	"time"

	"github.com/jackc/pgx/v5/pgxpool"

	"github.com/knot/backend/internal/stories"
)

// integrationSetup connects to the database named by KNOT_POSTGRES_DSN.
//
// The test is skipped, not failed, when KNOT_POSTGRES_DSN is unset, when the
// database is unreachable, or when the stories table has not been migrated to
// 0006 yet — none of those is a defect in the code under test.
//
// It returns the store, the pool, and a per-run email prefix that cleanup uses to
// delete exactly the users this test created. Deleting a user cascades to its
// stories and their versions, so the discovery rows go with them.
func integrationSetup(t *testing.T) (*PostgresStore, *pgxpool.Pool, string) {
	t.Helper()

	dsn := strings.TrimSpace(os.Getenv("KNOT_POSTGRES_DSN"))
	if dsn == "" {
		t.Skip("KNOT_POSTGRES_DSN is not set; skipping PostgreSQL integration test")
	}

	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()

	pool, err := pgxpool.New(ctx, dsn)
	if err != nil {
		t.Skipf("could not build a pool from KNOT_POSTGRES_DSN: %v", err)
	}
	if err := pool.Ping(ctx); err != nil {
		pool.Close()
		t.Skipf("PostgreSQL at KNOT_POSTGRES_DSN is not reachable: %v", err)
	}

	var migrated bool
	if err := pool.QueryRow(ctx, `
		SELECT EXISTS (
			SELECT 1 FROM information_schema.columns
			WHERE table_name = 'stories' AND column_name = 'approximate_location_lower'
		)`).Scan(&migrated); err != nil {
		pool.Close()
		t.Skipf("could not inspect the schema: %v", err)
	}
	if !migrated {
		pool.Close()
		t.Skip("stories.approximate_location_lower does not exist; run `go run ./cmd/knot migrate up` first")
	}

	store, err := NewPostgresStore(pool)
	if err != nil {
		pool.Close()
		t.Fatalf("NewPostgresStore() error = %v, want nil", err)
	}

	prefix := fmt.Sprintf("knot-discovery-it-%d-", time.Now().UnixNano())

	t.Cleanup(func() {
		cleanupCtx, cancelCleanup := context.WithTimeout(context.Background(), 10*time.Second)
		defer cancelCleanup()

		if _, err := pool.Exec(cleanupCtx, "DELETE FROM users WHERE email LIKE $1", prefix+"%"); err != nil {
			t.Logf("cleanup: could not delete test users: %v", err)
		}
		pool.Close()
	})

	return store, pool, prefix
}

// createUser inserts a user directly and returns its id. A story references
// users(id), so a test user has to exist before a story can be written.
func createUser(t *testing.T, pool *pgxpool.Pool, prefix, name string) string {
	t.Helper()

	var id string
	err := pool.QueryRow(
		context.Background(),
		`INSERT INTO users (email, password_hash, display_name) VALUES ($1, $2, $3) RETURNING id`,
		prefix+name+"@example.test",
		"$argon2id$v=19$m=65536,t=1,p=4$c2FsdA$a2V5",
		"Discovery "+name,
	).Scan(&id)
	if err != nil {
		t.Fatalf("createUser() error = %v, want nil", err)
	}

	return id
}

// createStoryAt inserts a story together with its root version, at a fixed
// creation time. It writes approximate_location_lower with the same
// lower(trim(...)) expression the application and migration 0006 use, so the
// stored row is normalised exactly as a real one would be.
func createStoryAt(t *testing.T, pool *pgxpool.Pool, authorID, place, pillar, language string, at time.Time) string {
	t.Helper()

	var storyID string
	err := pool.QueryRow(
		context.Background(),
		`WITH new_story AS (
			INSERT INTO stories (
				author_id, pillar, approximate_location, approximate_location_lower,
				media_urls, sensitive, root_version_id, created_at, updated_at
			)
			VALUES ($1, $2, $3, lower(trim($3)), '{}', false, gen_random_uuid(), $4, $4)
			RETURNING id, root_version_id
		)
		INSERT INTO story_versions (
			id, story_id, parent_version_id, author_id, language, title, body, created_at, updated_at
		)
		SELECT root_version_id, id, NULL, $1, $5, 'A discovery story', 'Body', $4, $4 FROM new_story
		RETURNING story_id`,
		authorID, pillar, place, at, language,
	).Scan(&storyID)
	if err != nil {
		t.Fatalf("createStoryAt() error = %v, want nil", err)
	}

	return storyID
}

// createStoryWithCoords inserts a story with (or without, when latitude is nil)
// structured place data, together with its root version.
func createStoryWithCoords(t *testing.T, pool *pgxpool.Pool, authorID, place string, latitude, longitude *float64, country *string, at time.Time) string {
	t.Helper()

	var storyID string
	err := pool.QueryRow(
		context.Background(),
		`WITH new_story AS (
			INSERT INTO stories (
				author_id, pillar, approximate_location, approximate_location_lower,
				latitude, longitude, place_country,
				media_urls, sensitive, root_version_id, created_at, updated_at
			)
			VALUES ($1, 'wonder', $2, lower(trim($2)), $3, $4, $5, '{}', false, gen_random_uuid(), $6, $6)
			RETURNING id, root_version_id
		)
		INSERT INTO story_versions (
			id, story_id, parent_version_id, author_id, language, title, body, created_at, updated_at
		)
		SELECT root_version_id, id, NULL, $1, 'en', 'A discovery story', 'Body', $6, $6 FROM new_story
		RETURNING story_id`,
		authorID, place, latitude, longitude, country, at,
	).Scan(&storyID)
	if err != nil {
		t.Fatalf("createStoryWithCoords() error = %v, want nil", err)
	}

	return storyID
}

// runToken returns a per-run token used to give places unique names, so the
// tests are unaffected by any other rows that happen to be in the database.
func runToken() string {
	return fmt.Sprintf("it%d", time.Now().UnixNano())
}

// ours filters clusters to the run's own places.
func ours(clusters []PlaceCluster, token string) []PlaceCluster {
	out := make([]PlaceCluster, 0, len(clusters))
	for _, cluster := range clusters {
		if strings.HasSuffix(cluster.Place, token) {
			out = append(out, cluster)
		}
	}
	return out
}

func TestPostgresStoreListClustersAggregates(t *testing.T) {
	store, pool, prefix := integrationSetup(t)
	ctx := context.Background()
	userID := createUser(t, pool, prefix, "clusters")
	token := runToken()

	capeTown := "Cape Town " + token
	nairobi := "Nairobi " + token
	lagos := "Lagos " + token

	base := time.Now().UTC().Truncate(time.Second).Add(-time.Hour)
	createStoryAt(t, pool, userID, capeTown, "wonder", "eng", base)
	createStoryAt(t, pool, userID, capeTown, "wonder", "eng", base.Add(time.Minute))
	createStoryAt(t, pool, userID, capeTown, "heritage", "afr", base.Add(2*time.Minute))
	createStoryAt(t, pool, userID, nairobi, "wonder", "eng", base.Add(3*time.Minute))
	createStoryAt(t, pool, userID, lagos, "heritage", "eng", base.Add(4*time.Minute))

	clusters, err := store.ListClusters(ctx, ClusterFilter{Limit: 100})
	if err != nil {
		t.Fatalf("ListClusters() error = %v, want nil", err)
	}

	group := ours(clusters, token)
	if len(group) != 3 {
		t.Fatalf("clusters for this run = %d, want 3: %+v", len(group), group)
	}

	// story_count descending: Cape Town (3) must come before the two singles.
	if group[0].Place != capeTown {
		t.Errorf("first cluster = %q, want %q", group[0].Place, capeTown)
	}
	if group[0].StoryCount != 3 {
		t.Errorf("Cape Town story_count = %d, want 3", group[0].StoryCount)
	}
	if group[0].PillarCounts[stories.PillarWonder] != 2 || group[0].PillarCounts[stories.PillarHeritage] != 1 {
		t.Errorf("Cape Town pillar_counts = %v, want wonder:2 heritage:1", group[0].PillarCounts)
	}
	if len(group[0].Languages) != 2 || group[0].Languages[0] != "afr" || group[0].Languages[1] != "eng" {
		t.Errorf("Cape Town languages = %v, want [afr eng]", group[0].Languages)
	}

	// latest_story_at is the newest story's creation time.
	wantLatest := base.Add(2 * time.Minute)
	if !group[0].LatestStoryAt.Equal(wantLatest) {
		t.Errorf("Cape Town latest_story_at = %v, want %v", group[0].LatestStoryAt, wantLatest)
	}
}

func TestPostgresStoreListClustersGroupsByCoordinates(t *testing.T) {
	store, pool, prefix := integrationSetup(t)
	ctx := context.Background()
	userID := createUser(t, pool, prefix, "coords")
	token := runToken()

	manguzi := "Manguzi " + token
	kwangwanase := "Kwangwanase " + token
	legacy := "Nowhere " + token

	latitude, longitude := -26.9998, 32.7489
	country := "South Africa"

	base := time.Now().UTC().Truncate(time.Second).Add(-time.Hour)
	// Two stories at the same coordinate but with different place names.
	createStoryWithCoords(t, pool, userID, manguzi, &latitude, &longitude, &country, base)
	createStoryWithCoords(t, pool, userID, kwangwanase, &latitude, &longitude, &country, base.Add(time.Minute))
	// A story with no coordinate still clusters, by its place name.
	createStoryWithCoords(t, pool, userID, legacy, nil, nil, nil, base.Add(2*time.Minute))

	clusters, err := store.ListClusters(ctx, ClusterFilter{Limit: 100})
	if err != nil {
		t.Fatalf("ListClusters() error = %v, want nil", err)
	}

	group := ours(clusters, token)
	if len(group) != 2 {
		t.Fatalf("clusters for this run = %d, want 2: %+v", len(group), group)
	}

	// story_count descending: the coordinate cluster (2) leads.
	coordinateCluster := group[0]
	if coordinateCluster.Latitude == nil || *coordinateCluster.Latitude != latitude {
		t.Errorf("latitude = %v, want %v", coordinateCluster.Latitude, latitude)
	}
	if coordinateCluster.Longitude == nil || *coordinateCluster.Longitude != longitude {
		t.Errorf("longitude = %v, want %v", coordinateCluster.Longitude, longitude)
	}
	if coordinateCluster.PlaceCountry == nil || *coordinateCluster.PlaceCountry != country {
		t.Errorf("place_country = %v, want %q", coordinateCluster.PlaceCountry, country)
	}
	if coordinateCluster.StoryCount != 2 {
		t.Errorf("coordinate cluster story_count = %d, want 2", coordinateCluster.StoryCount)
	}
}

func TestPostgresStoreListClustersLegacyRowsHaveNilCoordinates(t *testing.T) {
	store, pool, prefix := integrationSetup(t)
	ctx := context.Background()
	userID := createUser(t, pool, prefix, "legacy")
	token := runToken()

	place := "Smallville " + token
	base := time.Now().UTC().Truncate(time.Second).Add(-time.Hour)
	createStoryAt(t, pool, userID, place, "wonder", "eng", base)
	createStoryAt(t, pool, userID, place, "wonder", "eng", base.Add(time.Minute))

	clusters, err := store.ListClusters(ctx, ClusterFilter{Limit: 100})
	if err != nil {
		t.Fatalf("ListClusters() error = %v, want nil", err)
	}

	group := ours(clusters, token)
	if len(group) != 1 {
		t.Fatalf("clusters for this run = %d, want 1: %+v", len(group), group)
	}
	if group[0].StoryCount != 2 {
		t.Errorf("story_count = %d, want 2", group[0].StoryCount)
	}
	if group[0].Latitude != nil || group[0].Longitude != nil {
		t.Errorf("coordinates = (%v, %v), want nil for a text-only place", group[0].Latitude, group[0].Longitude)
	}
}

func TestPostgresStoreListClustersFilterByPillar(t *testing.T) {
	store, pool, prefix := integrationSetup(t)
	ctx := context.Background()
	userID := createUser(t, pool, prefix, "pillar")
	token := runToken()

	capeTown := "Cape Town " + token
	nairobi := "Nairobi " + token
	lagos := "Lagos " + token

	base := time.Now().UTC().Truncate(time.Second).Add(-time.Hour)
	createStoryAt(t, pool, userID, capeTown, "wonder", "eng", base)
	createStoryAt(t, pool, userID, capeTown, "heritage", "eng", base.Add(time.Minute))
	createStoryAt(t, pool, userID, nairobi, "wonder", "eng", base.Add(2*time.Minute))
	createStoryAt(t, pool, userID, lagos, "heritage", "eng", base.Add(3*time.Minute))

	clusters, err := store.ListClusters(ctx, ClusterFilter{Pillar: stories.PillarHeritage, Limit: 100})
	if err != nil {
		t.Fatalf("ListClusters() error = %v, want nil", err)
	}

	group := ours(clusters, token)
	if len(group) != 2 {
		t.Fatalf("heritage clusters for this run = %d, want 2: %+v", len(group), group)
	}
	for _, cluster := range group {
		if cluster.Place == nairobi {
			t.Errorf("Nairobi (wonder only) appeared in a heritage filter: %+v", cluster)
		}
		if cluster.StoryCount != 1 {
			t.Errorf("%s story_count = %d, want 1 (only heritage counted)", cluster.Place, cluster.StoryCount)
		}
	}
}

func TestPostgresStoreListClustersFilterByLanguage(t *testing.T) {
	store, pool, prefix := integrationSetup(t)
	ctx := context.Background()
	userID := createUser(t, pool, prefix, "language")
	token := runToken()

	capeTown := "Cape Town " + token
	nairobi := "Nairobi " + token

	base := time.Now().UTC().Truncate(time.Second).Add(-time.Hour)
	createStoryAt(t, pool, userID, capeTown, "wonder", "eng", base)
	createStoryAt(t, pool, userID, capeTown, "heritage", "afr", base.Add(time.Minute))
	createStoryAt(t, pool, userID, nairobi, "wonder", "eng", base.Add(2*time.Minute))

	clusters, err := store.ListClusters(ctx, ClusterFilter{Language: "afr", Limit: 100})
	if err != nil {
		t.Fatalf("ListClusters() error = %v, want nil", err)
	}

	group := ours(clusters, token)
	if len(group) != 1 {
		t.Fatalf("af clusters for this run = %d, want 1: %+v", len(group), group)
	}
	if group[0].Place != capeTown {
		t.Errorf("af cluster = %q, want %q", group[0].Place, capeTown)
	}
}

func TestPostgresStoreListStoriesAtPlaceMatchesNormalisedPlace(t *testing.T) {
	store, pool, prefix := integrationSetup(t)
	ctx := context.Background()
	userID := createUser(t, pool, prefix, "match")
	token := runToken()

	place := "Cape Town " + token
	base := time.Now().UTC().Truncate(time.Second).Add(-time.Hour)
	createStoryAt(t, pool, userID, place, "wonder", "eng", base)
	createStoryAt(t, pool, userID, place, "heritage", "afr", base.Add(time.Minute))

	// The store expects the already-normalised place: trimmed and lower-cased.
	page, next, err := store.ListStoriesAtPlace(ctx, strings.ToLower(place), nil, 10)
	if err != nil {
		t.Fatalf("ListStoriesAtPlace() error = %v, want nil", err)
	}
	if len(page) != 2 {
		t.Fatalf("stories = %d, want 2", len(page))
	}
	if next != nil {
		t.Errorf("next cursor = %v, want nil on the last page", next)
	}

	// A different place returns nothing.
	empty, _, err := store.ListStoriesAtPlace(ctx, strings.ToLower(place)+"x", nil, 10)
	if err != nil {
		t.Fatalf("ListStoriesAtPlace(unknown) error = %v, want nil", err)
	}
	if len(empty) != 0 {
		t.Errorf("unknown place stories = %d, want 0", len(empty))
	}
}

func TestPostgresStoreListStoriesAtPlacePaginates(t *testing.T) {
	store, pool, prefix := integrationSetup(t)
	ctx := context.Background()
	userID := createUser(t, pool, prefix, "paginate")
	token := runToken()

	place := "Pagination Town " + token
	base := time.Now().UTC().Truncate(time.Second).Add(-time.Hour)
	for i := 0; i < 5; i++ {
		createStoryAt(t, pool, userID, place, "wonder", "eng", base.Add(time.Duration(i)*time.Minute))
	}

	placeLower := strings.ToLower(place)
	var (
		collected []string
		cursor    *stories.Cursor
		pages     int
	)
	for {
		page, next, err := store.ListStoriesAtPlace(ctx, placeLower, cursor, 2)
		if err != nil {
			t.Fatalf("ListStoriesAtPlace() error = %v, want nil", err)
		}
		pages++
		for _, story := range page {
			collected = append(collected, story.ID)
		}
		if next == nil {
			break
		}
		cursor = next
		if pages > 5 {
			t.Fatal("pagination did not terminate")
		}
	}

	if pages != 3 {
		t.Errorf("pages = %d, want 3", pages)
	}
	if len(collected) != 5 {
		t.Fatalf("stories collected = %d, want 5", len(collected))
	}

	seen := make(map[string]bool, len(collected))
	for _, id := range collected {
		if seen[id] {
			t.Errorf("duplicate story id %s across pages", id)
		}
		seen[id] = true
	}
}

func TestPostgresStoreListStoriesAtPlaceEmptyPlace(t *testing.T) {
	store, pool, prefix := integrationSetup(t)
	ctx := context.Background()
	_ = createUser(t, pool, prefix, "empty")
	token := runToken()

	page, next, err := store.ListStoriesAtPlace(ctx, strings.ToLower("Nowhere "+token), nil, 10)
	if err != nil {
		t.Fatalf("ListStoriesAtPlace() error = %v, want nil", err)
	}
	if len(page) != 0 {
		t.Errorf("stories = %d, want 0", len(page))
	}
	if next != nil {
		t.Errorf("next cursor = %v, want nil", next)
	}
}
