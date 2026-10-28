package httpapi

import (
	"context"
	"encoding/json"
	"errors"
	"io"
	"log/slog"
	"net/http"
	"net/http/httptest"
	"reflect"
	"sync"
	"testing"
	"time"

	"github.com/knot/backend/internal/feedcache"
	"github.com/knot/backend/internal/stories"
	"github.com/knot/backend/internal/storystats"
)

// errCacheDown stands in for a Redis failure.
var errCacheDown = errors.New("feed cache unavailable")

// The precomputed feed's HTTP behaviour (KNOT-036).
//
// The cache is a recorder rather than a real Redis, so every branch of the read
// path — hit, miss, failure, anonymous, paginated — is testable, and so the
// background fan-out can be observed without a server. It is mutex-guarded
// because the handler calls it from goroutines while the test reads it.

// fanoutCall is one recorded fan-out call.
type fanoutCall struct {
	kind      feedcache.Kind
	id        string
	authorID  string
	language  string
	createdAt time.Time
}

// feedCacheState is a consistent snapshot of the recorder.
type feedCacheState struct {
	readCalls  int
	readUserID string
	readLimit  int

	seedCalls   int
	seedUserID  string
	seedEntries []feedcache.Entry

	fanoutCalls []fanoutCall
}

// feedCacheRecorder is a FeedCache that returns canned values and records
// everything it was asked to do.
type feedCacheRecorder struct {
	mu sync.Mutex

	items   []feedcache.Item
	readErr error

	seedErr   error
	fanoutErr error

	state feedCacheState
}

func (f *feedCacheRecorder) Read(_ context.Context, userID string, limit int) ([]feedcache.Item, error) {
	f.mu.Lock()
	defer f.mu.Unlock()

	f.state.readCalls++
	f.state.readUserID = userID
	f.state.readLimit = limit

	return f.items, f.readErr
}

func (f *feedCacheRecorder) FanoutStory(_ context.Context, storyID, authorID, language string, createdAt time.Time) (feedcache.Result, error) {
	f.record(feedcache.KindStory, storyID, authorID, language, createdAt)
	return feedcache.Result{Recipients: 1}, f.fanoutErr
}

func (f *feedCacheRecorder) FanoutVersion(_ context.Context, versionID, authorID, language string, createdAt time.Time) (feedcache.Result, error) {
	f.record(feedcache.KindVersion, versionID, authorID, language, createdAt)
	return feedcache.Result{Recipients: 1}, f.fanoutErr
}

func (f *feedCacheRecorder) record(kind feedcache.Kind, id, authorID, language string, createdAt time.Time) {
	f.mu.Lock()
	defer f.mu.Unlock()

	f.state.fanoutCalls = append(f.state.fanoutCalls, fanoutCall{
		kind:      kind,
		id:        id,
		authorID:  authorID,
		language:  language,
		createdAt: createdAt,
	})
}

func (f *feedCacheRecorder) Seed(_ context.Context, userID string, entries []feedcache.Entry) error {
	f.mu.Lock()
	defer f.mu.Unlock()

	f.state.seedCalls++
	f.state.seedUserID = userID
	f.state.seedEntries = entries

	return f.seedErr
}

// snapshot returns a copy of what the recorder has seen so far.
func (f *feedCacheRecorder) snapshot() feedCacheState {
	f.mu.Lock()
	defer f.mu.Unlock()

	return f.state
}

// waitFor polls cond until it holds, so a test can observe work the handler
// deliberately runs off the request path.
func waitFor(t *testing.T, what string, cond func() bool) {
	t.Helper()

	deadline := time.Now().Add(2 * time.Second)
	for time.Now().Before(deadline) {
		if cond() {
			return
		}
		time.Sleep(2 * time.Millisecond)
	}

	t.Fatalf("timed out waiting for %s", what)
}

// newStoriesHandlerWithCaches composes the router with the real stories service
// over store and the given Redis read caches.
func newStoriesHandlerWithCaches(t *testing.T, store stories.StoryStore, caches Caches) http.Handler {
	t.Helper()
	return newStoriesHandlerWithCachesAndStats(t, store, caches, &fakeStoryStatsLookup{})
}

// newStoriesHandlerWithCachesAndStats is newStoriesHandlerWithCaches with an
// explicit aggregate-counts lookup, so a test can assert the numbers a cached page
// carries against the SQL page's (KNOT-036-fix5).
func newStoriesHandlerWithCachesAndStats(t *testing.T, store stories.StoryStore, caches Caches, stats StoryStatsLookup) http.Handler {
	t.Helper()

	logger := slog.New(slog.NewTextHandler(io.Discard, nil))

	service, err := stories.NewService(store)
	if err != nil {
		t.Fatalf("stories.NewService() error = %v, want nil", err)
	}

	return newRouterWithFeedLookups(t, logger, service, feedLookups{
		authors:      &fakeAuthorService{},
		rooted:       &fakeRootedService{},
		media:        &fakeStoryMediaLookup{},
		reactions:    &fakeReactionsLookup{},
		stats:        stats,
		chains:       &fakeVersionChainLookup{},
		interactions: &fakeInteractionsLookup{},
		caches:       caches,
	})
}

// newStoriesHandlerWithFeed is newStoriesHandlerWithCaches for the feed cache
// alone, which is what most of these tests need.
func newStoriesHandlerWithFeed(t *testing.T, store stories.StoryStore, feed FeedCache) http.Handler {
	t.Helper()
	return newStoriesHandlerWithCaches(t, store, Caches{Feed: feed})
}

// twoStoryStore seeds a store with two stories whose ids are easy to read.
func twoStoryStore() (*memoryStoryStore, [2]string) {
	firstID := "0a000000-0000-4000-8000-000000000001"
	secondID := "0b000000-0000-4000-8000-000000000002"

	first := storyFixture(firstID, testUserID)
	second := storyFixture(secondID, testUserID)
	// The SQL feed is newest-first, so the seed must be too.
	second.CreatedAt = testNow.Add(time.Minute)
	second.UpdatedAt = second.CreatedAt

	return &memoryStoryStore{stories: []stories.Story{first, second}}, [2]string{firstID, secondID}
}

func TestFeedIsServedFromTheCache(t *testing.T) {
	store, ids := twoStoryStore()
	feed := &feedCacheRecorder{items: []feedcache.Item{{ID: ids[1], Kind: feedcache.KindStory}}}
	handler := newStoriesHandlerWithFeed(t, store, feed)

	recorder := doStoryRequest(handler, http.MethodGet, "/stories", "", testAccessToken)
	if recorder.Code != http.StatusOK {
		t.Fatalf("status = %d, want %d (body %s)", recorder.Code, http.StatusOK, recorder.Body.String())
	}

	var page feedPage
	decodeBody(t, recorder, &page)

	if len(page.Items) != 1 {
		t.Fatalf("items = %d, want the 1 cached entry", len(page.Items))
	}
	entries := feedStories(t, page)
	if entries[0].ID != ids[1] {
		t.Errorf("story id = %q, want the cached %q", entries[0].ID, ids[1])
	}
	if page.NextCursor != "" {
		t.Errorf("next cursor = %q, want empty (the cached path has no cursor)", page.NextCursor)
	}

	// The whole point: the SQL feed was not queried, and the ids were resolved in
	// one by-id read.
	if store.listCalls != 0 {
		t.Errorf("ListFeed calls = %d, want 0 when the cache answers", store.listCalls)
	}
	if store.byIDCalls != 1 {
		t.Errorf("StoriesByIDs calls = %d, want exactly 1", store.byIDCalls)
	}

	state := feed.snapshot()
	if state.readCalls != 1 {
		t.Fatalf("cache Read calls = %d, want 1", state.readCalls)
	}
	if state.readUserID != testUserID {
		t.Errorf("cache read for %q, want the authenticated %q", state.readUserID, testUserID)
	}
	if state.readLimit != stories.DefaultListLimit {
		t.Errorf("cache limit = %d, want the default %d", state.readLimit, stories.DefaultListLimit)
	}
	// A cache hit must not be seeded back into the cache.
	waitFor(t, "the seed goroutine to stay quiet", func() bool { return true })
	if state := feed.snapshot(); state.seedCalls != 0 {
		t.Errorf("Seed calls = %d, want 0 for a cache hit", state.seedCalls)
	}
}

// TestCachedFeedCarriesTheSameCommentCountsAsTheSQLFeed proves the two read paths
// agree on the one number this task fixes (KNOT-036-fix5). The cached page resolves
// its ids through the same branches the SQL feed uses and runs the same enrichment,
// so a story entry reports its root version's count and an adaptation reports its
// own -- identically on both paths.
func TestCachedFeedCarriesTheSameCommentCountsAsTheSQLFeed(t *testing.T) {
	const (
		storyID   = "0d000000-0000-4000-8000-00000000000a"
		rootID    = "0d000000-0000-4000-8000-00000000000b"
		versionID = "0d000000-0000-4000-8000-00000000000c"
	)

	story := storyFixture(storyID, testUserID)
	story.RootVersionID = rootID
	version := stories.VersionFeedItem{
		ID:              versionID,
		StoryID:         storyID,
		ParentVersionID: rootID,
		AuthorID:        testUserID,
		Language:        "zul",
		Title:           "Imvula yokuqala",
		Body:            "Ugogo wathi imvula yokuqala ikhumbula wonke amagama.",
		Pillar:          stories.PillarWonder,
		CreatedAt:       testNow.Add(time.Minute),
		UpdatedAt:       testNow.Add(time.Minute),
	}

	stats := &fakeStoryStatsLookup{byStory: map[string]storystats.Counts{
		storyID: {
			Versions:              2,
			Languages:             2,
			RootComments:          2,
			Comments:              5,
			CommentCountByVersion: map[string]int{rootID: 2, versionID: 1},
		},
	}}

	feed := &feedCacheRecorder{items: []feedcache.Item{
		{ID: versionID, Kind: feedcache.KindVersion},
		{ID: storyID, Kind: feedcache.KindStory},
	}}
	store := &memoryStoryStore{
		stories:  []stories.Story{story},
		versions: []stories.VersionFeedItem{version},
	}
	handler := newStoriesHandlerWithCachesAndStats(t, store, Caches{Feed: feed}, stats)

	// The SQL path: an anonymous reader never touches the feed cache.
	sqlRecorder := doStoryRequest(handler, http.MethodGet, "/stories", "", "")
	if sqlRecorder.Code != http.StatusOK {
		t.Fatalf("sql status = %d, want %d (body %s)", sqlRecorder.Code, http.StatusOK, sqlRecorder.Body.String())
	}

	// The cached path: the same page, resolved from cached ids.
	cachedRecorder := doStoryRequest(handler, http.MethodGet, "/stories", "", testAccessToken)
	if cachedRecorder.Code != http.StatusOK {
		t.Fatalf("cached status = %d, want %d (body %s)", cachedRecorder.Code, http.StatusOK, cachedRecorder.Body.String())
	}

	sqlCounts := commentCountsByKind(t, sqlRecorder)
	cachedCounts := commentCountsByKind(t, cachedRecorder)

	if sqlCounts[feedKindStory] != 2 || sqlCounts[feedKindVersion] != 1 {
		t.Errorf("sql comment counts = %v, want story 2 / version 1", sqlCounts)
	}
	if !reflect.DeepEqual(cachedCounts, sqlCounts) {
		t.Errorf("cached comment counts = %v, want the sql page's %v", cachedCounts, sqlCounts)
	}

	// The cached page really was served from the cache, so this compared the two
	// paths rather than the same path twice.
	if store.listCalls != 1 {
		t.Errorf("ListFeed calls = %d, want 1 (only the anonymous request)", store.listCalls)
	}
	if state := feed.snapshot(); state.readCalls != 1 {
		t.Errorf("cache reads = %d, want 1 for the signed-in request", state.readCalls)
	}
}

// commentCountsByKind projects a feed page's comment_count per entry kind, failing
// when a kind is missing so a comparison cannot pass on two empty maps.
func commentCountsByKind(t *testing.T, recorder *httptest.ResponseRecorder) map[string]int {
	t.Helper()

	var page feedPage
	if err := json.Unmarshal(recorder.Body.Bytes(), &page); err != nil {
		t.Fatalf("decode feed page: %v", err)
	}

	counts := make(map[string]int, 2)
	for _, raw := range page.Items {
		kind := feedKindOf(t, raw)

		var entry struct {
			CommentCount int `json:"comment_count"`
		}
		if err := json.Unmarshal(raw, &entry); err != nil {
			t.Fatalf("decode %s entry: %v", kind, err)
		}
		counts[kind] = entry.CommentCount
	}

	if len(counts) != 2 {
		t.Fatalf("comment counts = %v, want one entry of each kind", counts)
	}

	return counts
}

func TestFeedFallsBackToPostgresAndSeedsTheCache(t *testing.T) {
	store, ids := twoStoryStore()
	feed := &feedCacheRecorder{} // an empty feed: a cold start
	handler := newStoriesHandlerWithFeed(t, store, feed)

	recorder := doStoryRequest(handler, http.MethodGet, "/stories", "", testAccessToken)
	if recorder.Code != http.StatusOK {
		t.Fatalf("status = %d, want %d (body %s)", recorder.Code, http.StatusOK, recorder.Body.String())
	}

	var page feedPage
	decodeBody(t, recorder, &page)
	if len(page.Items) != 2 {
		t.Fatalf("items = %d, want the 2 stories PostgreSQL returned", len(page.Items))
	}

	if store.listCalls != 1 {
		t.Errorf("ListFeed calls = %d, want 1 (the fallback)", store.listCalls)
	}
	if store.byIDCalls != 0 {
		t.Errorf("StoriesByIDs calls = %d, want 0 on a miss", store.byIDCalls)
	}

	// The page just served is written back, so the reader's next read is a hit.
	waitFor(t, "the cache to be seeded", func() bool { return feed.snapshot().seedCalls > 0 })

	state := feed.snapshot()
	if state.seedUserID != testUserID {
		t.Errorf("seed user = %q, want %q", state.seedUserID, testUserID)
	}
	if len(state.seedEntries) != 2 {
		t.Fatalf("seeded %d entries, want 2", len(state.seedEntries))
	}
	if state.seedEntries[0].ID != ids[1] {
		t.Errorf("first seeded id = %q, want the newest %q", state.seedEntries[0].ID, ids[1])
	}
	if !state.seedEntries[0].CreatedAt.Equal(testNow.Add(time.Minute)) {
		t.Errorf("seeded created_at = %v, want the story's own timestamp", state.seedEntries[0].CreatedAt)
	}
}

func TestFeedFallsBackWhenTheCacheReadFails(t *testing.T) {
	store, _ := twoStoryStore()
	feed := &feedCacheRecorder{readErr: errCacheDown}
	handler := newStoriesHandlerWithFeed(t, store, feed)

	recorder := doStoryRequest(handler, http.MethodGet, "/stories", "", testAccessToken)
	if recorder.Code != http.StatusOK {
		t.Fatalf("status = %d, want %d: a broken cache must not break the feed (body %s)", recorder.Code, http.StatusOK, recorder.Body.String())
	}

	var page feedPage
	decodeBody(t, recorder, &page)
	if len(page.Items) != 2 {
		t.Errorf("items = %d, want the SQL page", len(page.Items))
	}
	if store.listCalls != 1 {
		t.Errorf("ListFeed calls = %d, want 1", store.listCalls)
	}
}

func TestAnonymousFeedNeverTouchesTheCache(t *testing.T) {
	store, _ := twoStoryStore()
	feed := &feedCacheRecorder{items: []feedcache.Item{{ID: "0a000000-0000-4000-8000-000000000001", Kind: feedcache.KindStory}}}
	handler := newStoriesHandlerWithFeed(t, store, feed)

	recorder := doStoryRequest(handler, http.MethodGet, "/stories", "", "")
	if recorder.Code != http.StatusOK {
		t.Fatalf("status = %d, want %d", recorder.Code, http.StatusOK)
	}

	if state := feed.snapshot(); state.readCalls != 0 {
		t.Errorf("cache Read calls = %d, want 0 for an anonymous reader", state.readCalls)
	}
	if store.listCalls != 1 {
		t.Errorf("ListFeed calls = %d, want 1", store.listCalls)
	}

	// An anonymous reader has no feed to seed; the fallback must not create one.
	waitFor(t, "the seed goroutine to stay quiet", func() bool { return true })
	if state := feed.snapshot(); state.seedCalls != 0 {
		t.Errorf("Seed calls = %d, want 0 for an anonymous reader", state.seedCalls)
	}
}

func TestPaginatedFeedSkipsTheCache(t *testing.T) {
	store, _ := twoStoryStore()
	feed := &feedCacheRecorder{items: []feedcache.Item{{ID: "0a000000-0000-4000-8000-000000000001", Kind: feedcache.KindStory}}}
	handler := newStoriesHandlerWithFeed(t, store, feed)

	// A cursor means page two or later, which the cached path (first page only)
	// cannot answer. The bogus value is enough: what matters is that the SQL path
	// was chosen, and it rejects it.
	recorder := doStoryRequest(handler, http.MethodGet, "/stories?cursor=bogus", "", testAccessToken)
	if recorder.Code != http.StatusBadRequest {
		t.Fatalf("status = %d, want %d (the SQL path answered and rejected the cursor)", recorder.Code, http.StatusBadRequest)
	}

	if state := feed.snapshot(); state.readCalls != 0 {
		t.Errorf("cache Read calls = %d, want 0 for a paginated request", state.readCalls)
	}
}

func TestForYouFeedSkipsTheCache(t *testing.T) {
	store, _ := twoStoryStore()
	feed := &feedCacheRecorder{items: []feedcache.Item{{ID: "0a000000-0000-4000-8000-000000000001", Kind: feedcache.KindStory}}}
	handler := newStoriesHandlerWithFeed(t, store, feed)

	// The ranked feed is out of scope for the cache (KNOT-ADR-093). This router is
	// wired without ranking dependencies, so `for_you` answers 500 — the point is
	// that it never reached the cache.
	recorder := doStoryRequest(handler, http.MethodGet, "/stories?feed_type=for_you", "", testAccessToken)
	if recorder.Code != http.StatusInternalServerError {
		t.Fatalf("status = %d, want %d without ranking dependencies", recorder.Code, http.StatusInternalServerError)
	}

	if state := feed.snapshot(); state.readCalls != 0 {
		t.Errorf("cache Read calls = %d, want 0 for the ranked feed", state.readCalls)
	}

	// Nor is a ranked page ever a seed source: its order is not the chronological
	// feed's, so writing it into the cache would poison the default feed.
	waitFor(t, "the seed goroutine to stay quiet", func() bool { return true })
	if state := feed.snapshot(); state.seedCalls != 0 {
		t.Errorf("Seed calls = %d, want 0 for the ranked feed", state.seedCalls)
	}
}

func TestCreateStoryFansOutOffTheRequestPath(t *testing.T) {
	store := &memoryStoryStore{}
	feed := &feedCacheRecorder{}
	handler := newStoriesHandlerWithFeed(t, store, feed)

	recorder := doStoryRequest(handler, http.MethodPost, "/stories", validStoryBody(), testAccessToken)
	if recorder.Code != http.StatusCreated {
		t.Fatalf("status = %d, want %d (body %s)", recorder.Code, http.StatusCreated, recorder.Body.String())
	}

	var envelope storyEnvelope
	decodeBody(t, recorder, &envelope)

	waitFor(t, "the fan-out goroutine", func() bool { return len(feed.snapshot().fanoutCalls) > 0 })

	state := feed.snapshot()
	if len(state.fanoutCalls) != 1 {
		t.Fatalf("fan-out calls = %d, want 1", len(state.fanoutCalls))
	}
	call := state.fanoutCalls[0]
	if call.kind != feedcache.KindStory {
		t.Errorf("fanned out kind = %q, want %q", call.kind, feedcache.KindStory)
	}
	if call.id != envelope.Story.ID {
		t.Errorf("fanned out id = %q, want the created %q", call.id, envelope.Story.ID)
	}
	if call.authorID != testUserID {
		t.Errorf("fanned out author = %q, want %q", call.authorID, testUserID)
	}
	if call.language != "eng" {
		t.Errorf("fanned out language = %q, want eng", call.language)
	}
	if call.createdAt.IsZero() {
		t.Error("fanned out created_at is zero, want the story's timestamp")
	}
}

func TestCreateStorySurvivesAFailedFanout(t *testing.T) {
	store := &memoryStoryStore{}
	feed := &feedCacheRecorder{fanoutErr: errCacheDown}
	handler := newStoriesHandlerWithFeed(t, store, feed)

	recorder := doStoryRequest(handler, http.MethodPost, "/stories", validStoryBody(), testAccessToken)
	if recorder.Code != http.StatusCreated {
		t.Fatalf("status = %d, want %d: a cache failure must not fail a publish", recorder.Code, http.StatusCreated)
	}

	// The failure is logged, not returned — so the only observable effect is that
	// the attempt happened.
	waitFor(t, "the fan-out attempt", func() bool { return len(feed.snapshot().fanoutCalls) == 1 })
}

// TestSeedEntriesKeepsBothKinds is the parity half of the cold-start repair: a
// seeded page must carry the adaptations it was served, each into its own set.
// Before KNOT-036-fix only story entries were seeded.
func TestSeedEntriesKeepsBothKinds(t *testing.T) {
	page := []stories.FeedItem{
		{Kind: stories.FeedKindVersion, Version: stories.VersionFeedItem{
			ID:        "11111111-1111-4111-8111-111111111111",
			CreatedAt: testNow.Add(time.Minute),
		}},
		{Kind: stories.FeedKindStory, Story: storyFixture("22222222-2222-4222-8222-222222222222", testUserID)},
	}

	entries := seedEntries(page)
	if len(entries) != 2 {
		t.Fatalf("entries = %d, want 2 (both kinds are seeded)", len(entries))
	}
	if entries[0].Kind != feedcache.KindVersion || entries[0].ID != "11111111-1111-4111-8111-111111111111" {
		t.Errorf("entry 0 = %+v, want the version", entries[0])
	}
	if !entries[0].CreatedAt.Equal(testNow.Add(time.Minute)) {
		t.Errorf("entry 0 created_at = %v, want the version's own timestamp", entries[0].CreatedAt)
	}
	if entries[1].Kind != feedcache.KindStory || entries[1].ID != "22222222-2222-4222-8222-222222222222" {
		t.Errorf("entry 1 = %+v, want the story", entries[1])
	}
}

// TestFeedRefsMapsKinds proves the cache's kind vocabulary is translated into the
// domain's, so an unknown kind can never become a version by accident.
func TestFeedRefsMapsKinds(t *testing.T) {
	refs := feedRefs([]feedcache.Item{
		{ID: "11111111-1111-4111-8111-111111111111", Kind: feedcache.KindStory},
		{ID: "22222222-2222-4222-8222-222222222222", Kind: feedcache.KindVersion},
	})

	if len(refs) != 2 {
		t.Fatalf("refs = %v, want 2", refs)
	}
	if refs[0].Kind != stories.FeedKindStory || refs[1].Kind != stories.FeedKindVersion {
		t.Errorf("refs = %v, want [story version]", refs)
	}
}

// TestCachedPageCarriesAdaptationEntries is the parity proof at the handler
// boundary: a cached page containing a version id is rendered as an adaptation
// entry — not dropped, and not served as a story (KNOT-036-fix).
func TestCachedPageCarriesAdaptationEntries(t *testing.T) {
	store, ids := twoStoryStore()
	versionID := "0c000000-0000-4000-8000-000000000003"
	store.versions = []stories.VersionFeedItem{{
		ID:        versionID,
		StoryID:   ids[1],
		AuthorID:  testUserID,
		Language:  "zul",
		Title:     "The first rain, in Zulu",
		Body:      "Adapted body.",
		Pillar:    stories.PillarWonder,
		CreatedAt: testNow.Add(2 * time.Minute),
	}}

	feed := &feedCacheRecorder{items: []feedcache.Item{
		{ID: versionID, Kind: feedcache.KindVersion},
		{ID: ids[1], Kind: feedcache.KindStory},
	}}
	handler := newStoriesHandlerWithFeed(t, store, feed)

	recorder := doStoryRequest(handler, http.MethodGet, "/stories", "", testAccessToken)
	if recorder.Code != http.StatusOK {
		t.Fatalf("status = %d, want %d (body %s)", recorder.Code, http.StatusOK, recorder.Body.String())
	}

	var page feedPage
	decodeBody(t, recorder, &page)

	if got := feedKinds(t, page); len(got) != 2 || got[0] != feedKindVersion || got[1] != feedKindStory {
		t.Fatalf("kinds = %v, want [version story] (the cache's order is preserved)", got)
	}

	versions := feedVersions(t, page)
	if len(versions) != 1 || versions[0].ID != versionID {
		t.Fatalf("adaptation entries = %v, want the cached version", versions)
	}

	// One call resolves both kinds, each through its own id space.
	if store.byIDCalls != 1 {
		t.Errorf("FeedItemsByIDs calls = %d, want 1", store.byIDCalls)
	}
	if len(store.gotRefs) != 2 {
		t.Fatalf("refs = %v, want both entries", store.gotRefs)
	}

	// The adaptation's card fields come from the same enrichment the SQL page uses.
	if versions[0].Language != "zul" || versions[0].StoryID != ids[1] {
		t.Errorf("version = %+v, want its own language and the parent story id", versions[0])
	}
	if versions[0].RecentInteractions == nil {
		t.Error("recent_interactions = nil, want an empty array (it must never be null)")
	}
}
