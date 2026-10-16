package stories

import (
	"context"
	"errors"
	"fmt"
	"os"
	"strings"
	"testing"
	"time"

	"github.com/jackc/pgx/v5/pgxpool"

	"github.com/knot/backend/internal/moderation"
)

// integrationStore is what the integration tests work with: the store under test
// plus the pool, so a test can insert the author a story requires.
type integrationStore struct {
	store  *PostgresStore
	pool   *pgxpool.Pool
	prefix string
	author string
}

// integrationSetup connects to the database named by KNOT_POSTGRES_DSN and
// creates an author to attach stories to.
//
// The test is skipped, not failed, when KNOT_POSTGRES_DSN is unset, when the
// database is unreachable, or when the tables have not been migrated yet — none
// of those is a defect in the code under test.
func integrationSetup(t *testing.T) integrationStore {
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
	if err := pool.QueryRow(ctx, "SELECT to_regclass('public.stories') IS NOT NULL").Scan(&migrated); err != nil {
		pool.Close()
		t.Skipf("could not inspect the schema: %v", err)
	}
	if !migrated {
		pool.Close()
		t.Skip("the stories table does not exist; run `go run ./cmd/knot migrate up` first")
	}

	store, err := NewPostgresStore(pool)
	if err != nil {
		pool.Close()
		t.Fatalf("NewPostgresStore() error = %v, want nil", err)
	}

	prefix := fmt.Sprintf("knot-it-%d-", time.Now().UnixNano())

	var author string
	err = pool.QueryRow(
		ctx,
		`INSERT INTO users (email, password_hash, display_name, preferred_languages)
		 VALUES ($1, $2, $3, $4)
		 RETURNING id`,
		prefix+"author@example.test",
		"$argon2id$v=19$m=65536,t=1,p=4$c2FsdA$a2V5",
		"Integration Author",
		[]string{"eng"},
	).Scan(&author)
	if err != nil {
		pool.Close()
		t.Fatalf("could not create the integration author: %v", err)
	}

	t.Cleanup(func() {
		cleanupCtx, cancelCleanup := context.WithTimeout(context.Background(), 10*time.Second)
		defer cancelCleanup()

		// Stories reference the author, so they go first even though the foreign
		// key cascades.
		if _, err := pool.Exec(cleanupCtx, "DELETE FROM stories WHERE author_id = $1", author); err != nil {
			t.Logf("cleanup: could not delete test stories: %v", err)
		}
		if _, err := pool.Exec(cleanupCtx, "DELETE FROM users WHERE email LIKE $1", prefix+"%"); err != nil {
			t.Logf("cleanup: could not delete test users: %v", err)
		}
		pool.Close()
	})

	return integrationStore{store: store, pool: pool, prefix: prefix, author: author}
}

// newIntegrationStory builds a story ready to insert, named so a failure names
// the case that produced the row.
func newIntegrationStory(author, name string) Story {
	return Story{
		AuthorID:            author,
		Pillar:              PillarHeritage,
		Language:            "eng",
		Title:               "Integration " + name,
		Body:                "Body for " + name,
		ApproximateLocation: "Cape Town",
		MediaURLs:           []string{"https://example.test/" + name + ".jpg"},
	}
}

// TestPostgresStoreListFeedHidesExcludedAuthors proves the block filter: a
// story by an author on the viewer's block set is absent from the page, while a
// story by anyone else is still present.
func TestPostgresStoreListFeedHidesExcludedAuthors(t *testing.T) {
	env := integrationSetup(t)
	ctx := context.Background()

	// A second author, whose story the viewer will hide. The test users share the
	// setup prefix, so the cleanup deletes them (and cascades to their stories).
	var other string
	if err := env.pool.QueryRow(
		ctx,
		`INSERT INTO users (email, password_hash, display_name, preferred_languages)
		 VALUES ($1, $2, $3, $4)
		 RETURNING id`,
		env.prefix+"other@example.test",
		"$argon2id$v=19$m=65536,t=1,p=4$c2FsdA$a2V5",
		"Hidden Author",
		[]string{"eng"},
	).Scan(&other); err != nil {
		t.Fatalf("could not create the second author: %v", err)
	}

	hidden, err := env.store.CreateStory(ctx, newIntegrationStory(other, "hidden"))
	if err != nil {
		t.Fatalf("CreateStory(hidden) error = %v, want nil", err)
	}
	visible, err := env.store.CreateStory(ctx, newIntegrationStory(env.author, "visible"))
	if err != nil {
		t.Fatalf("CreateStory(visible) error = %v, want nil", err)
	}

	// Unfiltered: both are present (the hidden one is newest, so it leads).
	unfiltered, _, err := env.store.ListFeed(ctx, nil, 50)
	if err != nil {
		t.Fatalf("ListFeed() error = %v, want nil", err)
	}
	if !feedHasStory(unfiltered, hidden.ID) || !feedHasStory(unfiltered, visible.ID) {
		t.Fatalf("unfiltered page did not contain both stories")
	}

	// Filtered: the hidden author's story is gone; the other remains.
	filtered, _, err := env.store.ListFeed(moderation.WithExcludedAuthors(ctx, []string{other}), nil, 50)
	if err != nil {
		t.Fatalf("ListFeed(filtered) error = %v, want nil", err)
	}
	if feedHasStory(filtered, hidden.ID) {
		t.Error("filtered page contains the blocked author's story")
	}
	if !feedHasStory(filtered, visible.ID) {
		t.Error("filtered page is missing the unblocked author's story")
	}
}

// feedHasStory reports whether a page contains a STORY entry with the given id.
// An adaptation entry is keyed by its own id, so it does not answer this question.
func feedHasStory(page []FeedItem, id string) bool {
	for _, entry := range page {
		if entry.Kind == FeedKindStory && entry.Story.ID == id {
			return true
		}
	}
	return false
}

// feedHasVersion reports whether a page contains an ADAPTATION entry with the
// given version id.
func feedHasVersion(page []FeedItem, id string) bool {
	for _, entry := range page {
		if entry.Kind == FeedKindVersion && entry.Version.ID == id {
			return true
		}
	}
	return false
}

func TestPostgresStoreCreateStoryRoundTrip(t *testing.T) {
	env := integrationSetup(t)
	ctx := context.Background()

	created, err := env.store.CreateStory(ctx, newIntegrationStory(env.author, "create"))
	if err != nil {
		t.Fatalf("CreateStory() error = %v, want nil", err)
	}

	if !isUUID(created.ID) {
		t.Errorf("id = %q, want a canonical uuid generated by the database", created.ID)
	}
	if created.AuthorID != env.author {
		t.Errorf("author id = %q, want %q", created.AuthorID, env.author)
	}
	if created.Pillar != PillarHeritage {
		t.Errorf("pillar = %q, want %q", created.Pillar, PillarHeritage)
	}
	if created.ApproximateLocation != "Cape Town" {
		t.Errorf("approximate location = %q, want %q", created.ApproximateLocation, "Cape Town")
	}
	if len(created.MediaURLs) != 1 {
		t.Errorf("media urls = %v, want one entry", created.MediaURLs)
	}
	if created.Sensitive {
		t.Error("sensitive = true, want the column default false")
	}
	if created.CreatedAt.IsZero() || created.UpdatedAt.IsZero() {
		t.Error("timestamps are zero, want database-generated values")
	}
	if !isUUID(created.RootVersionID) {
		t.Errorf("root version id = %q, want a canonical uuid", created.RootVersionID)
	}

	// The story's root version must really exist, parentless, carrying the
	// submitted content. Read with an independent query so the assertion does
	// not lean on the same join the fetch uses.
	var (
		versionID string
		parent    *string
		language  string
		title     string
		body      string
	)
	if err := env.pool.QueryRow(
		ctx,
		`SELECT id, parent_version_id, language, title, body FROM story_versions WHERE id = $1`,
		created.RootVersionID,
	).Scan(&versionID, &parent, &language, &title, &body); err != nil {
		t.Fatalf("could not read the root version: %v", err)
	}
	if versionID != created.RootVersionID {
		t.Errorf("root version id = %q, want %q", versionID, created.RootVersionID)
	}
	if parent != nil {
		t.Errorf("root version parent = %q, want NULL", *parent)
	}
	if language != created.Language || title != created.Title || body != created.Body {
		t.Errorf("root version content = (%q, %q, %q), want the submitted content", language, title, body)
	}

	fetched, err := env.store.GetStory(ctx, created.ID)
	if err != nil {
		t.Fatalf("GetStory() error = %v, want nil", err)
	}
	if fetched.ID != created.ID || fetched.Title != created.Title || fetched.Body != created.Body {
		t.Errorf("fetched = %+v, want it to match the created story %+v", fetched, created)
	}
	if !fetched.CreatedAt.Equal(created.CreatedAt) {
		t.Errorf("created at = %v, want %v", fetched.CreatedAt, created.CreatedAt)
	}
}

func TestPostgresStoreCreateStoryWithoutOptionalFields(t *testing.T) {
	env := integrationSetup(t)
	ctx := context.Background()

	story := newIntegrationStory(env.author, "sparse")
	story.ApproximateLocation = ""
	story.MediaURLs = nil

	created, err := env.store.CreateStory(ctx, story)
	if err != nil {
		t.Fatalf("CreateStory() error = %v, want nil", err)
	}

	if created.ApproximateLocation != "" {
		t.Errorf("approximate location = %q, want empty for a NULL column", created.ApproximateLocation)
	}
	if created.MediaURLs == nil {
		t.Error("media urls = nil, want an empty slice for a TEXT[] NOT NULL column")
	}
	if len(created.MediaURLs) != 0 {
		t.Errorf("media urls = %v, want no entries", created.MediaURLs)
	}
}

func TestPostgresStoreGetStoryUnknownId(t *testing.T) {
	env := integrationSetup(t)

	_, err := env.store.GetStory(context.Background(), "99999999-9999-4999-8999-999999999999")
	if !errors.Is(err, ErrNotFound) {
		t.Errorf("errors.Is(err, ErrNotFound) = false, want true (err = %v)", err)
	}
}

func TestPostgresStoreGetStoryMalformedId(t *testing.T) {
	env := integrationSetup(t)

	_, err := env.store.GetStory(context.Background(), "not-a-uuid")
	if !errors.Is(err, ErrNotFound) {
		t.Errorf("errors.Is(err, ErrNotFound) = false, want true (err = %v)", err)
	}
}

// authoredIDs returns this test's story ids in feed order, read with an
// independent query so the pagination assertions are not circular.
func authoredIDs(t *testing.T, env integrationStore) []string {
	t.Helper()

	rows, err := env.pool.Query(
		context.Background(),
		`SELECT id FROM stories WHERE author_id = $1 ORDER BY created_at DESC, id DESC`,
		env.author,
	)
	if err != nil {
		t.Fatalf("could not read the expected order: %v", err)
	}
	defer rows.Close()

	var ids []string
	for rows.Next() {
		var id string
		if err := rows.Scan(&id); err != nil {
			t.Fatalf("could not scan an id: %v", err)
		}
		ids = append(ids, id)
	}
	if err := rows.Err(); err != nil {
		t.Fatalf("could not read the expected order: %v", err)
	}
	return ids
}

func TestPostgresStoreListFeedPaginatesWithoutGapsOrRepeats(t *testing.T) {
	env := integrationSetup(t)
	ctx := context.Background()

	const total = 5
	for i := 0; i < total; i++ {
		if _, err := env.store.CreateStory(ctx, newIntegrationStory(env.author, fmt.Sprintf("page-%d", i))); err != nil {
			t.Fatalf("CreateStory(%d) error = %v, want nil", i, err)
		}
	}

	// The pages must tile the feed exactly: no row repeated, none skipped. Only
	// this test's rows are compared, because the feed is global, so the walk is
	// bounded rather than run to the end of a table it does not own.
	want := authoredIDs(t, env)
	mine := make(map[string]bool, len(want))
	for _, id := range want {
		mine[id] = true
	}

	var (
		seen   []string
		cursor *Cursor
		pages  int
	)

	for pages = 0; pages < 50 && len(seen) < len(want); pages++ {
		page, next, err := env.store.ListFeed(ctx, cursor, 2)
		if err != nil {
			t.Fatalf("ListFeed() error = %v, want nil", err)
		}

		if len(page) > 2 {
			t.Fatalf("page %d has %d rows, want at most 2", pages+1, len(page))
		}
		for _, entry := range page {
			// The feed is a merged stream, so other authors' adaptations are
			// interleaved with this author's stories; only the stories are compared.
			if entry.Kind == FeedKindStory && mine[entry.Story.ID] {
				seen = append(seen, entry.Story.ID)
			}
		}

		if next == nil {
			break
		}
		cursor = next
	}

	if len(seen) != len(want) {
		t.Fatalf("collected %d of this author's stories across %d pages, want %d", len(seen), pages, len(want))
	}
	for i := range want {
		if seen[i] != want[i] {
			t.Fatalf("collected position %d = %s, want %s (pages must tile the feed in created_at DESC, id DESC order)", i, seen[i], want[i])
		}
	}
}

func TestPostgresStoreListFeedStopsWithoutACursor(t *testing.T) {
	env := integrationSetup(t)
	ctx := context.Background()

	const created = 3
	for i := 0; i < created; i++ {
		if _, err := env.store.CreateStory(ctx, newIntegrationStory(env.author, fmt.Sprintf("last-%d", i))); err != nil {
			t.Fatalf("CreateStory(%d) error = %v, want nil", i, err)
		}
	}

	// The feed is global, and other packages' integration tests run against the
	// same database at the same time, so a row count read here could change
	// before the next read. The assertions therefore use the contract rather
	// than a count: a page never exceeds the limit, and a cursor is returned
	// exactly when the page is full.
	for _, limit := range []int{created, 10} {
		page, next, err := env.store.ListFeed(ctx, nil, limit)
		if err != nil {
			t.Fatalf("ListFeed(limit=%d) error = %v, want nil", limit, err)
		}

		if len(page) > limit {
			t.Errorf("ListFeed(limit=%d) returned %d rows, want at most %d", limit, len(page), limit)
		}
		if next != nil && len(page) != limit {
			t.Errorf("ListFeed(limit=%d) returned a cursor with %d rows, want a cursor only when the page is full", limit, len(page))
		}
	}

	// The end of the feed must be reported explicitly rather than by an empty
	// page: a limit larger than the feed returns every row and no cursor.
	page, next, err := env.store.ListFeed(ctx, nil, 1000)
	if err != nil {
		t.Fatalf("ListFeed(limit=1000) error = %v, want nil", err)
	}
	if next != nil {
		t.Errorf("ListFeed(limit=1000) returned a cursor %+v, want nil at the end of the feed", next)
	}
	if len(page) < created {
		t.Errorf("ListFeed(limit=1000) returned %d rows, want at least the %d this test created", len(page), created)
	}
}

func TestPostgresStoreListFeedCursorIsExclusive(t *testing.T) {
	env := integrationSetup(t)
	ctx := context.Background()

	for i := 0; i < 3; i++ {
		if _, err := env.store.CreateStory(ctx, newIntegrationStory(env.author, fmt.Sprintf("excl-%d", i))); err != nil {
			t.Fatalf("CreateStory(%d) error = %v, want nil", i, err)
		}
	}

	first, next, err := env.store.ListFeed(ctx, nil, 1)
	if err != nil {
		t.Fatalf("ListFeed() error = %v, want nil", err)
	}
	if len(first) != 1 || next == nil {
		t.Fatalf("first page = %d rows with cursor %+v, want 1 row and a cursor", len(first), next)
	}

	second, _, err := env.store.ListFeed(ctx, next, 1)
	if err != nil {
		t.Fatalf("ListFeed() error = %v, want nil", err)
	}
	if len(second) != 1 {
		t.Fatalf("len(second) = %d, want 1", len(second))
	}

	if second[0].SortID() == first[0].SortID() {
		t.Error("the cursor row was returned again, want the comparison to be strictly exclusive")
	}

	// The resumed page must continue in feed order, not merely differ. Both kinds
	// sort by their own timestamp, which is what SortTime answers for either.
	if second[0].SortTime().After(first[0].SortTime()) {
		t.Errorf("second page created at %v is newer than the first page %v, want older", second[0].SortTime(), first[0].SortTime())
	}
	if second[0].SortTime().Equal(first[0].SortTime()) && second[0].SortID() >= first[0].SortID() {
		t.Errorf("second page id %s does not sort below %s, want the id tiebreaker to advance", second[0].SortID(), first[0].SortID())
	}
}

func TestPostgresStoreRejectsNilPool(t *testing.T) {
	if _, err := NewPostgresStore(nil); err == nil {
		t.Error("NewPostgresStore(nil) error = nil, want an error")
	}
}

// TestPostgresStoreListFeedMergesStoriesAndAdaptations is the central test of
// KNOT-022: one page carries the story and the version somebody wrote of it, the
// version is attributed to its own author, and the story's ROOT version is not an
// entry of its own — a story is its root, so repeating it would show every story
// twice.
func TestPostgresStoreListFeedMergesStoriesAndAdaptations(t *testing.T) {
	env := integrationSetup(t)
	ctx := context.Background()

	adapter := createIntegrationAuthor(t, env, "adapter@example.test")

	story, err := env.store.CreateStory(ctx, newIntegrationStory(env.author, "merged"))
	if err != nil {
		t.Fatalf("CreateStory() error = %v, want nil", err)
	}

	// Dated one second after the story, so the adaptation leads the page and the
	// ordering assertion below cannot pass by accident.
	adaptationID := insertIntegrationVersion(t, env, story.ID, story.RootVersionID, adapter, "zul", story.CreatedAt.Add(time.Second))

	page, _, err := env.store.ListFeed(ctx, nil, 50)
	if err != nil {
		t.Fatalf("ListFeed() error = %v, want nil", err)
	}

	// Only this test's rows are inspected: the feed is global, so other rows may
	// be interleaved.
	mine := make([]FeedItem, 0, 2)
	for _, entry := range page {
		if entry.Kind == FeedKindStory && entry.Story.ID == story.ID {
			mine = append(mine, entry)
			continue
		}
		if entry.Kind == FeedKindVersion && entry.Version.ID == adaptationID {
			mine = append(mine, entry)
		}
	}

	if len(mine) != 2 {
		t.Fatalf("this story contributed %d entries, want exactly 2 (the story and its adaptation)", len(mine))
	}
	for _, entry := range mine {
		if entry.Kind == FeedKindVersion && entry.Version.ID == story.RootVersionID {
			t.Error("the root version is an entry of its own, want the story to carry it")
		}
	}

	if mine[0].Kind != FeedKindVersion {
		t.Fatalf("first entry kind = %q, want the newer entry (%q) to lead", mine[0].Kind, FeedKindVersion)
	}

	adaptation := mine[0].Version
	if adaptation.StoryID != story.ID {
		t.Errorf("adaptation story id = %q, want %q", adaptation.StoryID, story.ID)
	}
	if adaptation.ParentVersionID != story.RootVersionID {
		t.Errorf("adaptation parent version id = %q, want the story's root %q", adaptation.ParentVersionID, story.RootVersionID)
	}
	if adaptation.AuthorID != adapter {
		t.Errorf("adaptation author id = %q, want the adapter %q rather than the story's author", adaptation.AuthorID, adapter)
	}
	if adaptation.Language != "zul" {
		t.Errorf("adaptation language = %q, want %q", adaptation.Language, "zul")
	}
	// The framing an adaptation inherits from the story it adapts, because the
	// version row carries no pillar, place, or media of its own.
	if adaptation.Pillar != story.Pillar {
		t.Errorf("adaptation pillar = %q, want the story's %q", adaptation.Pillar, story.Pillar)
	}
	if adaptation.ApproximateLocation != story.ApproximateLocation {
		t.Errorf("adaptation location = %q, want the story's %q", adaptation.ApproximateLocation, story.ApproximateLocation)
	}
	if len(adaptation.MediaURLs) != 1 {
		t.Errorf("adaptation media urls = %v, want the story's single item", adaptation.MediaURLs)
	}

	// The story still leads its own content, and it sorts by its own timestamp.
	if mine[1].Kind != FeedKindStory {
		t.Fatalf("second entry kind = %q, want %q", mine[1].Kind, FeedKindStory)
	}
	if mine[1].Story.Title != story.Title {
		t.Errorf("story title = %q, want %q", mine[1].Story.Title, story.Title)
	}
	if !mine[0].SortTime().Equal(adaptation.CreatedAt) {
		t.Errorf("adaptation sort time = %v, want its own created_at %v", mine[0].SortTime(), adaptation.CreatedAt)
	}
}

// TestPostgresStoreListFeedBlockFilterAppliesPerAuthor pins the semantics of the
// block filter on a merged stream: a block hides an AUTHOR, not a lineage. An
// adaptation written by somebody the viewer can see stays visible even when the
// story it adapts is hidden, and an adaptation by a blocked author disappears
// even when its story stays.
func TestPostgresStoreListFeedBlockFilterAppliesPerAuthor(t *testing.T) {
	env := integrationSetup(t)
	ctx := context.Background()

	adapter := createIntegrationAuthor(t, env, "blocked-adapter@example.test")

	story, err := env.store.CreateStory(ctx, newIntegrationStory(env.author, "blocked-merge"))
	if err != nil {
		t.Fatalf("CreateStory() error = %v, want nil", err)
	}
	adaptationID := insertIntegrationVersion(t, env, story.ID, story.RootVersionID, adapter, "ssw", story.CreatedAt.Add(time.Second))

	// The viewer blocks the ADAPTER: the story stays, the adaptation goes.
	blockingAdapter, _, err := env.store.ListFeed(moderation.WithExcludedAuthors(ctx, []string{adapter}), nil, 50)
	if err != nil {
		t.Fatalf("ListFeed(blocking the adapter) error = %v, want nil", err)
	}
	if !feedHasStory(blockingAdapter, story.ID) {
		t.Error("the story is missing, want a block on the adapter to leave the original visible")
	}
	if feedHasVersion(blockingAdapter, adaptationID) {
		t.Error("the blocked adapter's version is still present")
	}

	// The viewer blocks the STORY's AUTHOR: the story goes, the adaptation stays,
	// because its own author is not blocked.
	blockingAuthor, _, err := env.store.ListFeed(moderation.WithExcludedAuthors(ctx, []string{env.author}), nil, 50)
	if err != nil {
		t.Fatalf("ListFeed(blocking the author) error = %v, want nil", err)
	}
	if feedHasStory(blockingAuthor, story.ID) {
		t.Error("the blocked author's story is still present")
	}
	if !feedHasVersion(blockingAuthor, adaptationID) {
		t.Error("the adaptation is missing, want a block on the story's author to leave another author's version visible")
	}
}

// TestPostgresStoreListFeedPaginatesAcrossBothKinds proves the cursor walks the
// MERGED stream: with a page size of one, the entries of this test's two lineages
// come back one at a time, in (created_at DESC, id DESC) order over both kinds,
// with nothing repeated and nothing skipped.
func TestPostgresStoreListFeedPaginatesAcrossBothKinds(t *testing.T) {
	env := integrationSetup(t)
	ctx := context.Background()

	adapter := createIntegrationAuthor(t, env, "paging-adapter@example.test")

	// Two lineages, with explicit timestamps so the expected order is exact rather
	// than merely "probably": newest first, the two kinds alternate.
	base := time.Now().UTC().Truncate(time.Second)

	want := make([]string, 0, 4)
	for i := 0; i < 2; i++ {
		story, err := env.store.CreateStory(ctx, newIntegrationStory(env.author, fmt.Sprintf("paging-%d", i)))
		if err != nil {
			t.Fatalf("CreateStory(%d) error = %v, want nil", i, err)
		}

		// Story 0 at -4s, story 1 at -2s; each adaptation a second later.
		storyAt := base.Add(-time.Duration(4-2*i) * time.Second)
		setStoryCreatedAt(t, env, story.ID, storyAt)
		versionEntryID := insertIntegrationVersion(t, env, story.ID, story.RootVersionID, adapter, "zul", storyAt.Add(time.Second))

		want = append([]string{versionEntryID, story.ID}, want...)
	}

	mine := make(map[string]bool, len(want))
	for _, id := range want {
		mine[id] = true
	}

	var (
		seen   []string
		cursor *Cursor
	)

	for pages := 0; pages < 50 && len(seen) < len(want); pages++ {
		page, next, err := env.store.ListFeed(ctx, cursor, 1)
		if err != nil {
			t.Fatalf("ListFeed() error = %v, want nil", err)
		}
		if len(page) > 1 {
			t.Fatalf("page %d has %d entries, want at most 1", pages+1, len(page))
		}

		for _, entry := range page {
			if mine[entry.SortID()] {
				seen = append(seen, entry.SortID())
			}
		}

		if next == nil {
			break
		}
		cursor = next
	}

	if len(seen) != len(want) {
		t.Fatalf("collected %d of this test's %d entries, want all of them", len(seen), len(want))
	}
	for i := range want {
		if seen[i] != want[i] {
			t.Fatalf("collected position %d = %s, want %s (pages must tile the merged feed in created_at DESC, id DESC order)", i, seen[i], want[i])
		}
	}
}

// TestPostgresStoreListFeedShowsARootOnlyStoryOnce is a guard on the version
// branch's parent filter: a story whose root version is the only version it has
// contributes exactly one entry, so publishing a story never shows up twice.
func TestPostgresStoreListFeedShowsARootOnlyStoryOnce(t *testing.T) {
	env := integrationSetup(t)
	ctx := context.Background()

	story, err := env.store.CreateStory(ctx, newIntegrationStory(env.author, "root-only"))
	if err != nil {
		t.Fatalf("CreateStory() error = %v, want nil", err)
	}

	page, _, err := env.store.ListFeed(ctx, nil, 50)
	if err != nil {
		t.Fatalf("ListFeed() error = %v, want nil", err)
	}

	mine := 0
	for _, entry := range page {
		if entry.Kind == FeedKindStory && entry.Story.ID == story.ID {
			mine++
		}
		if entry.Kind == FeedKindVersion && entry.Version.StoryID == story.ID {
			mine++
			t.Errorf("entry %s is an adaptation of a story with no adaptations", entry.Version.ID)
		}
	}

	if mine != 1 {
		t.Errorf("the story contributed %d entries, want exactly 1", mine)
	}
}

// createIntegrationAuthor inserts an extra user in the test's namespace, so a
// cleanup that deletes by prefix removes it (and cascades to their versions).
func createIntegrationAuthor(t *testing.T, env integrationStore, email string) string {
	t.Helper()

	var id string
	if err := env.pool.QueryRow(
		context.Background(),
		`INSERT INTO users (email, password_hash, display_name, preferred_languages)
		 VALUES ($1, $2, $3, $4)
		 RETURNING id`,
		env.prefix+email,
		"$argon2id$v=19$m=65536,t=1,p=4$c2FsdA$a2V5",
		"Integration Adapter",
		[]string{"eng"},
	).Scan(&id); err != nil {
		t.Fatalf("could not create the adapter author: %v", err)
	}

	return id
}

// setStoryCreatedAt backdates a story's created_at, which is the key the story
// branch of the merged feed sorts by. It exists so a paging test can fix the
// order of the entries it creates instead of hoping the database clock separates
// them.
func setStoryCreatedAt(t *testing.T, env integrationStore, storyID string, createdAt time.Time) {
	t.Helper()

	if _, err := env.pool.Exec(
		context.Background(),
		`UPDATE stories SET created_at = $2, updated_at = $2 WHERE id = $1`,
		storyID,
		createdAt,
	); err != nil {
		t.Fatalf("could not backdate the story: %v", err)
	}
}

// insertIntegrationVersion inserts one version row directly and returns its id.
// The feed tests build lineages by hand so they can pin the exact timestamp and
// author of each version, which is what makes the merged order and the block
// semantics assertable. The versions package owns the real insert path; here only
// the rows matter.
func insertIntegrationVersion(t *testing.T, env integrationStore, storyID, parentVersionID, authorID, language string, createdAt time.Time) string {
	t.Helper()

	var id string
	if err := env.pool.QueryRow(
		context.Background(),
		`INSERT INTO story_versions (
			story_id, parent_version_id, author_id, language, title, body, created_at, updated_at
		 )
		 VALUES ($1, $2, $3, $4, $5, $6, $7, $7)
		 RETURNING id`,
		storyID,
		nullIfEmpty(parentVersionID),
		authorID,
		language,
		"Adaptation in "+language,
		"Body of the adaptation.",
		createdAt,
	).Scan(&id); err != nil {
		t.Fatalf("could not insert an adaptation: %v", err)
	}

	return id
}

// countAuthorStories counts the stories a single author owns, read with an
// independent query so the rollback assertion is not circular.
func countAuthorStories(t *testing.T, env integrationStore) int {
	t.Helper()

	var total int
	if err := env.pool.QueryRow(
		context.Background(),
		"SELECT count(*) FROM stories WHERE author_id = $1",
		env.author,
	).Scan(&total); err != nil {
		t.Fatalf("could not count stories: %v", err)
	}
	return total
}

func TestPostgresStoreCreateStoryRollsBackWhenVersionInsertFails(t *testing.T) {
	// A story and its root version are written together, so a failure while
	// writing the version must leave no story behind. A NUL byte is valid in a Go
	// string but cannot be stored in a PostgreSQL text column, so putting one in
	// the body makes only the version insert fail: the body column lives on
	// story_versions, not on stories.
	env := integrationSetup(t)
	ctx := context.Background()

	before := countAuthorStories(t, env)

	_, err := env.store.CreateStory(ctx, Story{
		AuthorID:  env.author,
		Pillar:    PillarHeritage,
		Language:  "eng",
		Title:     "Rollback candidate",
		Body:      "this body contains a NUL byte: \x00",
		MediaURLs: []string{},
	})
	if err == nil {
		t.Fatal("CreateStory() error = nil, want the root version insert to fail on the NUL byte")
	}

	if after := countAuthorStories(t, env); after != before {
		t.Errorf("stories for the author = %d, want %d — the story must roll back when its version fails", after, before)
	}
}
