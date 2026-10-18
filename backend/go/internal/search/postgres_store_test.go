package search

import (
	"context"
	"fmt"
	"os"
	"strings"
	"testing"
	"time"

	"github.com/jackc/pgx/v5/pgxpool"

	"github.com/knot/backend/internal/moderation"
)

// integrationSetup connects to the database named by KNOT_POSTGRES_DSN.
//
// The test is skipped, not failed, when KNOT_POSTGRES_DSN is unset, when the
// database is unreachable, or when migration 0016 has not been applied — none of
// those is a defect in the code under test.
//
// It returns the store, the pool, and a per-run email prefix that cleanup uses to
// delete exactly the users this test created. Deleting a user cascades to its
// stories, versions, and inquiries, so the search rows go with them.
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
			WHERE table_name = 'story_versions' AND column_name = 'search_vector'
		)`).Scan(&migrated); err != nil {
		pool.Close()
		t.Skipf("could not inspect the schema: %v", err)
	}
	if !migrated {
		pool.Close()
		t.Skip("story_versions.search_vector does not exist; run `go run ./cmd/knot migrate up` first")
	}

	store, err := NewPostgresStore(pool)
	if err != nil {
		pool.Close()
		t.Fatalf("NewPostgresStore() error = %v, want nil", err)
	}

	prefix := fmt.Sprintf("knot-search-it-%d-", time.Now().UnixNano())

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

// runToken returns a per-run token embedded in every fixture's text, so a search
// for it matches this run's rows and nothing that was already in the database
// (KNOT-ADR-063).
func runToken() string {
	return fmt.Sprintf("srch%d", time.Now().UnixNano())
}

// createUser inserts a user directly and returns its id. Content references
// users(id), so a test user has to exist before content can be written.
func createUser(t *testing.T, pool *pgxpool.Pool, prefix, name string) string {
	t.Helper()

	var id string
	err := pool.QueryRow(
		context.Background(),
		`INSERT INTO users (email, password_hash, display_name) VALUES ($1, $2, $3) RETURNING id`,
		prefix+name+"@example.test",
		"$argon2id$v=19$m=65536,t=1,p=4$c2FsdA$a2V5",
		"Search "+name,
	).Scan(&id)
	if err != nil {
		t.Fatalf("createUser() error = %v, want nil", err)
	}

	return id
}

// createStory inserts a story with its root version and returns the story id. The
// story's searchable text lives on the root version, exactly as the real write path
// stores it.
func createStory(t *testing.T, pool *pgxpool.Pool, authorID, title, body, language, place, pillar string, at time.Time) string {
	t.Helper()

	var storyID string
	err := pool.QueryRow(
		context.Background(),
		`WITH new_story AS (
			INSERT INTO stories (
				author_id, pillar, approximate_location, approximate_location_lower,
				media_urls, root_version_id, created_at, updated_at
			)
			VALUES ($1, $2, $3, lower(trim($3)), '{}', gen_random_uuid(), $4, $4)
			RETURNING id, root_version_id
		)
		INSERT INTO story_versions (
			id, story_id, parent_version_id, author_id, language, title, body, created_at, updated_at
		)
		SELECT root_version_id, id, NULL, $1, $5, $6, $7, $4, $4 FROM new_story
		RETURNING story_id`,
		authorID, pillar, place, at, language, title, body,
	).Scan(&storyID)
	if err != nil {
		t.Fatalf("createStory() error = %v, want nil", err)
	}

	return storyID
}

// createAdaptation appends a non-root version to a story and returns the version
// id. It is the row a "version" search result comes from.
func createAdaptation(t *testing.T, pool *pgxpool.Pool, authorID, storyID, title, body, language string, at time.Time) string {
	t.Helper()

	var versionID string
	err := pool.QueryRow(
		context.Background(),
		`INSERT INTO story_versions (
			story_id, parent_version_id, author_id, language, title, body, created_at, updated_at
		)
		SELECT $1, root_version_id, $2, $3, $4, $5, $6, $6 FROM stories WHERE id = $1
		RETURNING id`,
		storyID, authorID, language, title, body, at,
	).Scan(&versionID)
	if err != nil {
		t.Fatalf("createAdaptation() error = %v, want nil", err)
	}

	return versionID
}

// createInquiry inserts an inquiry and returns its id.
func createInquiry(t *testing.T, pool *pgxpool.Pool, authorID, title, body, language, place string, at time.Time) string {
	t.Helper()

	var inquiryID string
	err := pool.QueryRow(
		context.Background(),
		`INSERT INTO inquiries (author_id, title, body, language, place, created_at, updated_at)
		VALUES ($1, $2, $3, $4, $5, $6, $6)
		RETURNING id`,
		authorID, title, body, language, place, at,
	).Scan(&inquiryID)
	if err != nil {
		t.Fatalf("createInquiry() error = %v, want nil", err)
	}

	return inquiryID
}

// newQuery builds a store query with the default page size, so a test never
// accidentally asks for LIMIT 0.
func newQuery(phrase string) StoreQuery {
	return StoreQuery{Query: phrase, Limit: DefaultLimit}
}

// blockedCtx is the request context a signed-in viewer carries once the given
// authors are hidden from them: exactly what the auth middleware resolves from the
// viewer's mutual block set (KNOT-ADR-060).
func blockedCtx(authorIDs ...string) context.Context {
	return moderation.WithExcludedAuthors(context.Background(), authorIDs)
}

// byKind indexes results by kind, so a test can assert on each corpus at once.
func byKind(results []Result) map[Kind]Result {
	out := make(map[Kind]Result, len(results))
	for _, result := range results {
		out[result.Kind] = result
	}
	return out
}

// TestPostgresStoreSearchesAllThreeTypes proves one phrase finds a story (through
// its root version), an adaptation, and an inquiry together.
func TestPostgresStoreSearchesAllThreeTypes(t *testing.T) {
	store, pool, prefix := integrationSetup(t)
	token := runToken()
	base := time.Now().Add(-6 * time.Hour).UTC()

	author := createUser(t, pool, prefix, "author")
	storyID := createStory(t, pool, author, "Cattle "+token, "The herd comes home", "eng", "Manguzi "+token, "wonder", base)
	versionID := createAdaptation(t, pool, author, storyID, "Inkomo "+token, "Adapted body", "zul", base.Add(time.Minute))
	inquiryID := createInquiry(t, pool, author, "Why "+token, "Why do they come home?", "eng", "Manguzi "+token, base.Add(2*time.Minute))

	results, next, err := store.Search(context.Background(), newQuery(token))
	if err != nil {
		t.Fatalf("Search() error = %v, want nil", err)
	}
	if next != nil {
		t.Fatalf("Search() next = %v, want nil for a single page", next)
	}
	if len(results) != 3 {
		t.Fatalf("Search() = %d results, want 3: %+v", len(results), results)
	}

	got := byKind(results)

	story, ok := got[KindStory]
	if !ok {
		t.Fatalf("no story result in %+v", results)
	}
	if story.ID != storyID || story.StoryID == nil || *story.StoryID != storyID {
		t.Errorf("story result = %+v, want id and story_id %q", story, storyID)
	}
	if story.Title != "Cattle "+token {
		t.Errorf("story title = %q, want %q", story.Title, "Cattle "+token)
	}
	if story.Pillar == nil || *story.Pillar != "wonder" {
		t.Errorf("story pillar = %v, want wonder", story.Pillar)
	}
	if story.Language != "eng" {
		t.Errorf("story language = %q, want eng", story.Language)
	}

	version, ok := got[KindVersion]
	if !ok {
		t.Fatalf("no version result in %+v", results)
	}
	if version.ID != versionID {
		t.Errorf("version id = %q, want %q", version.ID, versionID)
	}
	if version.StoryID == nil || *version.StoryID != storyID {
		t.Errorf("version story_id = %v, want %q", version.StoryID, storyID)
	}
	if version.Language != "zul" {
		t.Errorf("version language = %q, want zul", version.Language)
	}

	inquiry, ok := got[KindInquiry]
	if !ok {
		t.Fatalf("no inquiry result in %+v", results)
	}
	if inquiry.ID != inquiryID {
		t.Errorf("inquiry id = %q, want %q", inquiry.ID, inquiryID)
	}
	if inquiry.StoryID != nil {
		t.Errorf("inquiry story_id = %v, want nil", *inquiry.StoryID)
	}
	if inquiry.Pillar != nil {
		t.Errorf("inquiry pillar = %v, want nil", *inquiry.Pillar)
	}
}

// TestPostgresStoreFiltersByType proves a type filter returns only that corpus.
func TestPostgresStoreFiltersByType(t *testing.T) {
	store, pool, prefix := integrationSetup(t)
	token := runToken()
	base := time.Now().Add(-5 * time.Hour).UTC()

	author := createUser(t, pool, prefix, "author")
	storyID := createStory(t, pool, author, "Cattle "+token, "Body", "eng", "Manguzi "+token, "wonder", base)
	createAdaptation(t, pool, author, storyID, "Inkomo "+token, "Body", "zul", base.Add(time.Minute))
	createInquiry(t, pool, author, "Why "+token, "Body", "eng", "Manguzi "+token, base.Add(2*time.Minute))

	tests := []struct {
		kind Kind
		want Kind
	}{
		{kind: KindStory, want: KindStory},
		{kind: KindVersion, want: KindVersion},
		{kind: KindInquiry, want: KindInquiry},
	}

	for _, test := range tests {
		query := newQuery(token)
		query.Kind = test.kind

		results, _, err := store.Search(context.Background(), query)
		if err != nil {
			t.Fatalf("Search(type=%s) error = %v, want nil", test.kind, err)
		}
		if len(results) != 1 {
			t.Fatalf("Search(type=%s) = %d results, want 1: %+v", test.kind, len(results), results)
		}
		if results[0].Kind != test.want {
			t.Errorf("Search(type=%s) kind = %q, want %q", test.kind, results[0].Kind, test.want)
		}
	}
}

// TestPostgresStoreFiltersByPillar proves a pillar filter keeps only that pillar's
// stories, and excludes inquiries, which have no pillar at all.
func TestPostgresStoreFiltersByPillar(t *testing.T) {
	store, pool, prefix := integrationSetup(t)
	token := runToken()
	base := time.Now().Add(-5 * time.Hour).UTC()

	author := createUser(t, pool, prefix, "author")
	wonderID := createStory(t, pool, author, "Cattle "+token, "Body", "eng", "Manguzi "+token, "wonder", base)
	createStory(t, pool, author, "Old "+token, "Body", "eng", "Durban "+token, "heritage", base.Add(time.Minute))
	createInquiry(t, pool, author, "Why "+token, "Body", "eng", "Manguzi "+token, base.Add(2*time.Minute))

	query := newQuery(token)
	query.Pillar = "wonder"

	results, _, err := store.Search(context.Background(), query)
	if err != nil {
		t.Fatalf("Search(pillar=wonder) error = %v, want nil", err)
	}
	if len(results) != 1 {
		t.Fatalf("Search(pillar=wonder) = %d results, want 1: %+v", len(results), results)
	}
	if results[0].ID != wonderID {
		t.Errorf("Search(pillar=wonder) id = %q, want the wonder story %q", results[0].ID, wonderID)
	}

	// With no pillar filter every corpus is eligible again.
	results, _, err = store.Search(context.Background(), newQuery(token))
	if err != nil {
		t.Fatalf("Search() error = %v, want nil", err)
	}
	if len(results) != 3 {
		t.Fatalf("unfiltered Search() = %d results, want 3: %+v", len(results), results)
	}
}

// TestPostgresStoreFiltersByLanguage proves the language filter is applied to the
// matched row's own language: a story's root version for a story, the version for an
// adaptation, the inquiry for a question.
func TestPostgresStoreFiltersByLanguage(t *testing.T) {
	store, pool, prefix := integrationSetup(t)
	token := runToken()
	base := time.Now().Add(-5 * time.Hour).UTC()

	author := createUser(t, pool, prefix, "author")
	storyID := createStory(t, pool, author, "Cattle "+token, "Body", "eng", "Manguzi "+token, "wonder", base)
	versionID := createAdaptation(t, pool, author, storyID, "Inkomo "+token, "Body", "zul", base.Add(time.Minute))
	createInquiry(t, pool, author, "Why "+token, "Body", "afr", "Manguzi "+token, base.Add(2*time.Minute))

	query := newQuery(token)
	query.Language = "zul"

	results, _, err := store.Search(context.Background(), query)
	if err != nil {
		t.Fatalf("Search(language=zul) error = %v, want nil", err)
	}
	if len(results) != 1 {
		t.Fatalf("Search(language=zul) = %d results, want 1: %+v", len(results), results)
	}
	if results[0].ID != versionID || results[0].Kind != KindVersion {
		t.Errorf("Search(language=zul) = %+v, want the zul adaptation %q", results[0], versionID)
	}
}

// TestPostgresStoreFiltersByAuthor proves the author filter matches the author of
// the matched row.
func TestPostgresStoreFiltersByAuthor(t *testing.T) {
	store, pool, prefix := integrationSetup(t)
	token := runToken()
	base := time.Now().Add(-5 * time.Hour).UTC()

	first := createUser(t, pool, prefix, "first")
	second := createUser(t, pool, prefix, "second")
	firstStoryID := createStory(t, pool, first, "Cattle "+token, "Body", "eng", "Manguzi "+token, "wonder", base)
	createStory(t, pool, second, "Sheep "+token, "Body", "eng", "Manguzi "+token, "wonder", base.Add(time.Minute))

	query := newQuery(token)
	query.AuthorID = first

	results, _, err := store.Search(context.Background(), query)
	if err != nil {
		t.Fatalf("Search(author_id) error = %v, want nil", err)
	}
	if len(results) != 1 {
		t.Fatalf("Search(author_id) = %d results, want 1: %+v", len(results), results)
	}
	if results[0].ID != firstStoryID {
		t.Errorf("Search(author_id) id = %q, want %q", results[0].ID, firstStoryID)
	}
	if results[0].AuthorID != first {
		t.Errorf("AuthorID = %q, want %q", results[0].AuthorID, first)
	}
}

// TestPostgresStoreFiltersByPlace proves the place filter matches the stored
// spelling on both stories and inquiries.
func TestPostgresStoreFiltersByPlace(t *testing.T) {
	store, pool, prefix := integrationSetup(t)
	token := runToken()
	base := time.Now().Add(-5 * time.Hour).UTC()

	place := "Manguzi " + token
	otherPlace := "Durban " + token

	author := createUser(t, pool, prefix, "author")
	storyID := createStory(t, pool, author, "Cattle "+token, "Body", "eng", place, "wonder", base)
	createStory(t, pool, author, "Sheep "+token, "Body", "eng", otherPlace, "wonder", base.Add(time.Minute))
	inquiryID := createInquiry(t, pool, author, "Why "+token, "Body", "eng", place, base.Add(2*time.Minute))

	query := newQuery(token)
	query.Place = place

	results, _, err := store.Search(context.Background(), query)
	if err != nil {
		t.Fatalf("Search(place) error = %v, want nil", err)
	}
	if len(results) != 2 {
		t.Fatalf("Search(place) = %d results, want 2: %+v", len(results), results)
	}

	ids := map[string]bool{}
	for _, result := range results {
		ids[result.ID] = true
		if result.Place == nil || *result.Place != place {
			t.Errorf("result place = %v, want %q", result.Place, place)
		}
	}
	if !ids[storyID] || !ids[inquiryID] {
		t.Errorf("Search(place) ids = %v, want the %q story and inquiry", ids, place)
	}
}

// TestPostgresStoreCombinesFilters proves the filters compose: each one narrows, and
// together they select exactly one row.
func TestPostgresStoreCombinesFilters(t *testing.T) {
	store, pool, prefix := integrationSetup(t)
	token := runToken()
	base := time.Now().Add(-5 * time.Hour).UTC()

	place := "Manguzi " + token

	author := createUser(t, pool, prefix, "author")
	other := createUser(t, pool, prefix, "other")
	want := createStory(t, pool, author, "Cattle "+token, "Body", "eng", place, "wonder", base)
	createStory(t, pool, other, "Sheep "+token, "Body", "eng", place, "wonder", base.Add(time.Minute))
	createStory(t, pool, author, "Old "+token, "Body", "eng", place, "heritage", base.Add(2*time.Minute))
	createStory(t, pool, author, "Kine "+token, "Body", "zul", place, "wonder", base.Add(3*time.Minute))

	query := newQuery(token)
	query.Kind = KindStory
	query.Pillar = "wonder"
	query.Language = "eng"
	query.AuthorID = author
	query.Place = place

	results, _, err := store.Search(context.Background(), query)
	if err != nil {
		t.Fatalf("Search(combined) error = %v, want nil", err)
	}
	if len(results) != 1 {
		t.Fatalf("Search(combined) = %d results, want 1: %+v", len(results), results)
	}
	if results[0].ID != want {
		t.Errorf("Search(combined) id = %q, want %q", results[0].ID, want)
	}
}

// TestPostgresStoreHidesBlockedAuthors proves the block filter removes a blocked
// author's rows from every corpus, and that an anonymous reader carries no filter
// and therefore sees them.
func TestPostgresStoreHidesBlockedAuthors(t *testing.T) {
	store, pool, prefix := integrationSetup(t)
	token := runToken()
	base := time.Now().Add(-5 * time.Hour).UTC()

	blocked := createUser(t, pool, prefix, "blocked")
	visible := createUser(t, pool, prefix, "visible")
	blockedStoryID := createStory(t, pool, blocked, "Cattle "+token, "Body", "eng", "Manguzi "+token, "wonder", base)
	visibleStoryID := createStory(t, pool, visible, "Sheep "+token, "Body", "eng", "Manguzi "+token, "wonder", base.Add(time.Minute))
	createInquiry(t, pool, blocked, "Why "+token, "Body", "eng", "Manguzi "+token, base.Add(2*time.Minute))

	filtered, _, err := store.Search(blockedCtx(blocked), newQuery(token))
	if err != nil {
		t.Fatalf("Search(blocked ctx) error = %v, want nil", err)
	}
	if len(filtered) != 1 {
		t.Fatalf("Search(blocked ctx) = %d results, want 1: %+v", len(filtered), filtered)
	}
	for _, result := range filtered {
		if result.AuthorID == blocked {
			t.Errorf("blocked author's result %+v reached the reader", result)
		}
	}
	if filtered[0].ID != visibleStoryID {
		t.Errorf("Search(blocked ctx) id = %q, want %q", filtered[0].ID, visibleStoryID)
	}

	anonymous, _, err := store.Search(context.Background(), newQuery(token))
	if err != nil {
		t.Fatalf("Search(anonymous) error = %v, want nil", err)
	}
	if len(anonymous) != 3 {
		t.Fatalf("Search(anonymous) = %d results, want 3: %+v", len(anonymous), anonymous)
	}

	blockedSeen := false
	for _, result := range anonymous {
		if result.ID == blockedStoryID {
			blockedSeen = true
		}
	}
	if !blockedSeen {
		t.Errorf("anonymous Search() = %+v, want the blocked author's story", anonymous)
	}
}

// TestPostgresStoreMatchesCaseInsensitivelyAndByPrefix proves the `simple`
// configuration folds case, and that a prefix term matches a longer word — the
// reason "mang" finds "Manguzi".
func TestPostgresStoreMatchesCaseInsensitivelyAndByPrefix(t *testing.T) {
	store, pool, prefix := integrationSetup(t)
	token := runToken()
	base := time.Now().Add(-4 * time.Hour).UTC()

	author := createUser(t, pool, prefix, "author")

	// The title is one word, so its lexeme is unique to this run and a search can
	// only match this row.
	title := "Manguziprobe" + token
	storyID := createStory(t, pool, author, title, "Body", "eng", "Manguzi "+token, "wonder", base)

	phrases := []struct {
		name   string
		phrase string
	}{
		{name: "lower case phrase", phrase: strings.ToLower(title)},
		{name: "upper case phrase", phrase: strings.ToUpper(title)},
		{name: "prefix of the word", phrase: title[:len("Manguziprobe")+4]},
	}

	for _, test := range phrases {
		t.Run(test.name, func(t *testing.T) {
			results, _, err := store.Search(context.Background(), newQuery(test.phrase))
			if err != nil {
				t.Fatalf("Search(%q) error = %v, want nil", test.phrase, err)
			}
			if len(results) != 1 {
				t.Fatalf("Search(%q) = %d results, want 1: %+v", test.phrase, len(results), results)
			}
			if results[0].ID != storyID {
				t.Errorf("Search(%q) id = %q, want %q", test.phrase, results[0].ID, storyID)
			}
			if results[0].Title != title {
				t.Errorf("Search(%q) title = %q, want %q", test.phrase, results[0].Title, title)
			}
		})
	}
}

// TestPostgresStorePaginatesExactlyOnce proves the keyset cursor walks every row
// once: each page is one result, the ids are distinct, and the walk ends with no
// cursor.
func TestPostgresStorePaginatesExactlyOnce(t *testing.T) {
	store, pool, prefix := integrationSetup(t)
	token := runToken()
	base := time.Now().Add(-4 * time.Hour).UTC()

	author := createUser(t, pool, prefix, "author")
	want := map[string]bool{
		createStory(t, pool, author, "One "+token, "Body", "eng", "A "+token, "wonder", base):                      true,
		createStory(t, pool, author, "Two "+token, "Body", "eng", "B "+token, "wonder", base.Add(time.Minute)):     true,
		createStory(t, pool, author, "Three "+token, "Body", "eng", "C "+token, "wonder", base.Add(2*time.Minute)): true,
	}

	seen := map[string]bool{}
	query := newQuery(token)
	query.Limit = 1

	for page := 0; page < 5; page++ {
		results, next, err := store.Search(context.Background(), query)
		if err != nil {
			t.Fatalf("Search(page %d) error = %v, want nil", page, err)
		}

		if len(results) == 0 {
			if next != nil {
				t.Fatalf("Search(page %d) returned no results but a cursor %v", page, next)
			}
			break
		}
		if len(results) != 1 {
			t.Fatalf("Search(page %d) = %d results, want 1", page, len(results))
		}
		if seen[results[0].ID] {
			t.Fatalf("Search(page %d) repeated id %q", page, results[0].ID)
		}
		seen[results[0].ID] = true

		if next == nil {
			break
		}
		query.Cursor = next
	}

	if len(seen) != len(want) {
		t.Fatalf("walked %d rows, want %d: %v", len(seen), len(want), seen)
	}
	for id := range want {
		if !seen[id] {
			t.Errorf("id %q was never returned", id)
		}
	}
}

// TestPostgresStoreFiltersOnlySearch proves a filter-only search (no phrase) is
// served: every row matching the filter comes back at rank 0.
func TestPostgresStoreFiltersOnlySearch(t *testing.T) {
	store, pool, prefix := integrationSetup(t)
	token := runToken()
	base := time.Now().Add(-4 * time.Hour).UTC()

	place := "Manguzi " + token
	author := createUser(t, pool, prefix, "author")
	storyID := createStory(t, pool, author, "Cattle", "Body", "eng", place, "wonder", base)

	query := newQuery("")
	query.Place = place

	results, _, err := store.Search(context.Background(), query)
	if err != nil {
		t.Fatalf("Search(filter only) error = %v, want nil", err)
	}
	if len(results) != 1 {
		t.Fatalf("Search(filter only) = %d results, want 1: %+v", len(results), results)
	}
	if results[0].ID != storyID {
		t.Errorf("Search(filter only) id = %q, want %q", results[0].ID, storyID)
	}
	if results[0].Rank != 0 {
		t.Errorf("Rank = %v, want 0 for a filter-only search", results[0].Rank)
	}
}
