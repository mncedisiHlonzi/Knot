package httpapi

import (
	"context"
	"encoding/json"
	"io"
	"log/slog"
	"net/http"
	"sync"
	"testing"

	"github.com/knot/backend/internal/moderation"
	"github.com/knot/backend/internal/reactions"
	"github.com/knot/backend/internal/stories"
	"github.com/knot/backend/internal/versions"
)

// The story and version detail cache's HTTP behaviour (KNOT-036-fix).
//
// The recorder is a map plus counters, so every branch — hit, miss, failure,
// invalidation — is testable without Redis, and so a test can read the payload the
// handler stored and assert what it does NOT contain.

// detailCacheRecorder is a DetailCache backed by two maps.
type detailCacheRecorder struct {
	mu sync.Mutex

	stories  map[string]string
	versions map[string]string

	getErr        error
	setErr        error
	invalidateErr error

	getCalls         int
	setCalls         int
	invalidatedStory []string
	invalidatedVers  []string
}

func newDetailCacheRecorder() *detailCacheRecorder {
	return &detailCacheRecorder{
		stories:  map[string]string{},
		versions: map[string]string{},
	}
}

func (d *detailCacheRecorder) GetStoryJSON(_ context.Context, storyID string) (string, bool, error) {
	d.mu.Lock()
	defer d.mu.Unlock()

	d.getCalls++
	if d.getErr != nil {
		return "", false, d.getErr
	}
	payload, ok := d.stories[storyID]
	return payload, ok, nil
}

func (d *detailCacheRecorder) SetStoryJSON(_ context.Context, storyID, payload string) error {
	d.mu.Lock()
	defer d.mu.Unlock()

	d.setCalls++
	if d.setErr != nil {
		return d.setErr
	}
	d.stories[storyID] = payload
	return nil
}

func (d *detailCacheRecorder) GetVersionJSON(_ context.Context, versionID string) (string, bool, error) {
	d.mu.Lock()
	defer d.mu.Unlock()

	d.getCalls++
	if d.getErr != nil {
		return "", false, d.getErr
	}
	payload, ok := d.versions[versionID]
	return payload, ok, nil
}

func (d *detailCacheRecorder) SetVersionJSON(_ context.Context, versionID, payload string) error {
	d.mu.Lock()
	defer d.mu.Unlock()

	d.setCalls++
	if d.setErr != nil {
		return d.setErr
	}
	d.versions[versionID] = payload
	return nil
}

func (d *detailCacheRecorder) InvalidateStory(_ context.Context, storyID string) error {
	d.mu.Lock()
	defer d.mu.Unlock()

	d.invalidatedStory = append(d.invalidatedStory, storyID)
	delete(d.stories, storyID)
	return d.invalidateErr
}

func (d *detailCacheRecorder) InvalidateVersion(_ context.Context, versionID string) error {
	d.mu.Lock()
	defer d.mu.Unlock()

	d.invalidatedVers = append(d.invalidatedVers, versionID)
	delete(d.versions, versionID)
	return d.invalidateErr
}

// detailCacheState is a consistent snapshot of the recorder.
type detailCacheState struct {
	getCalls            int
	setCalls            int
	invalidatedStories  []string
	invalidatedVersions []string
}

// snapshot copies the recorder's state so an assertion cannot race a goroutine.
func (d *detailCacheRecorder) snapshot() detailCacheState {
	d.mu.Lock()
	defer d.mu.Unlock()

	return detailCacheState{
		getCalls:            d.getCalls,
		setCalls:            d.setCalls,
		invalidatedStories:  append([]string(nil), d.invalidatedStory...),
		invalidatedVersions: append([]string(nil), d.invalidatedVers...),
	}
}

// storedStory decodes the payload the handler cached for a story.
func (d *detailCacheRecorder) storedStory(t *testing.T, storyID string) storyResponse {
	t.Helper()

	d.mu.Lock()
	payload, ok := d.stories[storyID]
	d.mu.Unlock()

	if !ok {
		t.Fatalf("no cached payload for story %s", storyID)
	}

	var envelope storyEnvelope
	if err := json.Unmarshal([]byte(payload), &envelope); err != nil {
		t.Fatalf("cached payload is not a story envelope: %v", err)
	}
	return envelope.Story
}

// cachedStoryPayload builds the payload a cached story detail holds: the response
// as the handler writes it, with the reader-specific field blanked out.
func cachedStoryPayload(t *testing.T, response storyResponse) string {
	t.Helper()

	response.MyReactions = []string{}
	payload, err := json.Marshal(storyEnvelope{Story: response})
	if err != nil {
		t.Fatalf("marshal story payload: %v", err)
	}
	return string(payload)
}

func TestCachedStoryDetailIsServedWithoutTouchingTheDatabase(t *testing.T) {
	const storyID = "0a000000-0000-4000-8000-000000000001"

	details := newDetailCacheRecorder()
	details.stories[storyID] = cachedStoryPayload(t, storyResponse{
		ID:                storyID,
		AuthorID:          testUserID,
		AuthorDisplayName: "Cached Author",
		Title:             "From the cache",
		Body:              "Cached body.",
	})

	// An empty store: anything served here can only have come from the cache.
	handler := newStoriesHandlerWithCaches(t, &memoryStoryStore{}, Caches{Details: details})

	recorder := doStoryRequest(handler, http.MethodGet, "/stories/"+storyID, "", testAccessToken)
	if recorder.Code != http.StatusOK {
		t.Fatalf("status = %d, want %d (body %s)", recorder.Code, http.StatusOK, recorder.Body.String())
	}

	var envelope storyEnvelope
	decodeBody(t, recorder, &envelope)
	if envelope.Story.ID != storyID || envelope.Story.Title != "From the cache" {
		t.Errorf("story = %+v, want the cached payload", envelope.Story)
	}

	// A hit reads once and does not re-store what it just read.
	if state := details.snapshot(); state.getCalls != 1 || state.setCalls != 0 {
		t.Errorf("cache reads = %d, writes = %d, want 1 and 0", state.getCalls, state.setCalls)
	}
}

// TestStoryDetailCacheMissPopulatesTheAnonymousProjection proves what the cache
// stores: the response with `my_reactions` blanked, so one reader's signals can
// never be served to another.
func TestStoryDetailCacheMissPopulatesTheAnonymousProjection(t *testing.T) {
	const storyID = "0a000000-0000-4000-8000-000000000001"

	store := &memoryStoryStore{}
	store.stories = append(store.stories, storyFixture(storyID, testUserID))

	details := newDetailCacheRecorder()
	reactions := &fakeReactionsLookup{
		mine: map[string][]reactions.ReactionType{
			storyID: {reactions.RingsTrue},
		},
	}

	service, err := stories.NewService(store)
	if err != nil {
		t.Fatalf("stories.NewService() error = %v, want nil", err)
	}

	handler := newRouterWithFeedLookups(t, discardLogger(), service, feedLookups{
		authors:      &fakeAuthorService{},
		rooted:       &fakeRootedService{},
		media:        &fakeStoryMediaLookup{},
		reactions:    reactions,
		stats:        &fakeStoryStatsLookup{},
		chains:       &fakeVersionChainLookup{},
		interactions: &fakeInteractionsLookup{},
		caches:       Caches{Details: details},
	})

	recorder := doStoryRequest(handler, http.MethodGet, "/stories/"+storyID, "", testAccessToken)
	if recorder.Code != http.StatusOK {
		t.Fatalf("status = %d, want %d (body %s)", recorder.Code, http.StatusOK, recorder.Body.String())
	}

	// The response carries the reader's own signal...
	var envelope storyEnvelope
	decodeBody(t, recorder, &envelope)
	if len(envelope.Story.MyReactions) != 1 {
		t.Fatalf("my_reactions = %v, want the reader's own signal", envelope.Story.MyReactions)
	}

	// ...and the cached copy does not.
	cached := details.storedStory(t, storyID)
	if len(cached.MyReactions) != 0 {
		t.Errorf("cached my_reactions = %v, want empty (the anonymous projection)", cached.MyReactions)
	}
	if cached.ID != storyID {
		t.Errorf("cached id = %q, want %q", cached.ID, storyID)
	}
	if state := details.snapshot(); state.setCalls != 1 {
		t.Errorf("cache writes = %d, want 1", state.setCalls)
	}
}

// TestCachedStoryDetailHonoursABlock proves the one thing a cache hit must not
// bypass: a viewer who blocked the author still gets a 404, because visibility is
// decided per request rather than stored.
func TestCachedStoryDetailHonoursABlock(t *testing.T) {
	const storyID = "0a000000-0000-4000-8000-000000000001"
	const blockedAuthor = "33333333-3333-4333-8333-333333333333"

	details := newDetailCacheRecorder()
	details.stories[storyID] = cachedStoryPayload(t, storyResponse{ID: storyID, AuthorID: blockedAuthor})

	handler, err := NewStoriesHandlerWithCaches(
		&fakeStoriesService{},
		&fakeAuthorService{},
		&fakeRootedService{},
		&fakeStoryMediaLookup{},
		&fakeReactionsLookup{},
		&fakeStoryStatsLookup{},
		&fakeVersionChainLookup{},
		&fakeInteractionsLookup{},
		slog.New(slog.NewTextHandler(io.Discard, nil)),
		nil,
		FeedRanking{},
		Caches{Details: details},
	)
	if err != nil {
		t.Fatalf("NewStoriesHandlerWithCaches() error = %v, want nil", err)
	}

	blocked := moderation.WithExcludedAuthors(context.Background(), []string{blockedAuthor})
	if _, ok := handler.cachedStoryDetail(blocked, storyID); ok {
		t.Fatal("cachedStoryDetail() = ok for a blocked author, want the database path")
	}

	if _, ok := handler.cachedStoryDetail(context.Background(), storyID); !ok {
		t.Fatal("cachedStoryDetail() = not ok for an unblocked viewer, want the cached entry")
	}
}

func TestCachedVersionDetailIsServedWithoutTouchingTheDatabase(t *testing.T) {
	const versionID = "0c000000-0000-4000-8000-000000000003"

	details := newDetailCacheRecorder()
	// The stored payload is the detail envelope, comment count included: the count
	// is part of the response, so a cache hit serves the number it stored rather
	// than recomputing (or zeroing) it (KNOT-036-fix5).
	payload, err := json.Marshal(versionDetailEnvelope{Version: versionDetailResponse{
		versionResponse: versionResponse{
			ID:          versionID,
			StoryID:     "0b000000-0000-4000-8000-000000000002",
			AuthorID:    testUserID,
			Language:    "zul",
			Title:       "Cached adaptation",
			Body:        "Cached body.",
			MyReactions: []string{},
		},
		CommentCount: 4,
	}})
	if err != nil {
		t.Fatalf("marshal version payload: %v", err)
	}
	details.versions[versionID] = string(payload)

	versionsService := &fakeVersionsService{}
	stats := &fakeStoryStatsLookup{}
	handler := newRouterWithFeedLookups(t, discardLogger(), &fakeStoriesService{}, feedLookups{
		authors:      &fakeAuthorService{},
		rooted:       &fakeRootedService{},
		media:        &fakeStoryMediaLookup{},
		reactions:    &fakeReactionsLookup{},
		stats:        stats,
		chains:       &fakeVersionChainLookup{},
		interactions: &fakeInteractionsLookup{},
		versions:     versionsService,
		caches:       Caches{Details: details},
	})

	recorder := doStoryRequest(handler, http.MethodGet, "/versions/"+versionID, "", testAccessToken)
	if recorder.Code != http.StatusOK {
		t.Fatalf("status = %d, want %d (body %s)", recorder.Code, http.StatusOK, recorder.Body.String())
	}

	var envelope versionDetailBody
	decodeBody(t, recorder, &envelope)
	if envelope.Version.ID != versionID || envelope.Version.Title != "Cached adaptation" {
		t.Errorf("version = %+v, want the cached payload", envelope.Version)
	}
	if envelope.Version.CommentCount != 4 {
		t.Errorf("comment_count = %d, want the cached 4", envelope.Version.CommentCount)
	}
	if versionsService.getCalls != 0 {
		t.Errorf("versions service reads = %d, want 0 (the cache answered)", versionsService.getCalls)
	}
	if len(stats.gotVersionIDs) != 0 {
		t.Errorf("count reads = %v, want none on a cache hit", stats.gotVersionIDs)
	}
}

func TestDetailCacheFailureFallsBackToTheDatabase(t *testing.T) {
	store, ids := twoStoryStore()
	details := newDetailCacheRecorder()
	details.getErr = errCacheDown

	handler := newStoriesHandlerWithCaches(t, store, Caches{Details: details})

	recorder := doStoryRequest(handler, http.MethodGet, "/stories/"+ids[0], "", testAccessToken)
	if recorder.Code != http.StatusOK {
		t.Fatalf("status = %d, want %d: a broken cache must not break the read (body %s)",
			recorder.Code, http.StatusOK, recorder.Body.String())
	}

	var envelope storyEnvelope
	decodeBody(t, recorder, &envelope)
	if envelope.Story.ID != ids[0] {
		t.Errorf("story id = %q, want %q from the store", envelope.Story.ID, ids[0])
	}
}

// TestCreateStoryInvalidatesTheStoryDetail proves the write path clears the detail
// key, so a key that should not exist cannot linger from anything else.
func TestCreateStoryInvalidatesTheStoryDetail(t *testing.T) {
	details := newDetailCacheRecorder()
	handler := newStoriesHandlerWithCaches(t, &memoryStoryStore{}, Caches{Details: details})

	recorder := doStoryRequest(handler, http.MethodPost, "/stories", validStoryBody(), testAccessToken)
	if recorder.Code != http.StatusCreated {
		t.Fatalf("status = %d, want %d (body %s)", recorder.Code, http.StatusCreated, recorder.Body.String())
	}

	var envelope storyEnvelope
	decodeBody(t, recorder, &envelope)

	state := details.snapshot()
	if len(state.invalidatedStories) != 1 || state.invalidatedStories[0] != envelope.Story.ID {
		t.Fatalf("invalidated = %v, want [%s]", state.invalidatedStories, envelope.Story.ID)
	}
}

// TestAdaptInvalidatesTheParentStoryDetail is the required invalidation test: an
// adaptation changes the story's version_count, so the story's cached detail must
// go.
func TestAdaptInvalidatesTheParentStoryDetail(t *testing.T) {
	const parentStoryID = "0b000000-0000-4000-8000-000000000002"
	const newVersionID = "0c000000-0000-4000-8000-000000000003"

	details := newDetailCacheRecorder()
	// Seed the story's cached detail so the invalidation has something to remove.
	details.stories[parentStoryID] = cachedStoryPayload(t, storyResponse{ID: parentStoryID, AuthorID: testUserID})

	versionsService := &fakeVersionsService{createResult: versions.StoryVersion{
		ID:        newVersionID,
		StoryID:   parentStoryID,
		AuthorID:  testUserID,
		Language:  "zul",
		Title:     "An adaptation",
		Body:      "Body.",
		CreatedAt: testNow,
		UpdatedAt: testNow,
	}}

	handler := newRouterWithFeedLookups(t, discardLogger(), &fakeStoriesService{}, feedLookups{
		authors:      &fakeAuthorService{},
		rooted:       &fakeRootedService{},
		media:        &fakeStoryMediaLookup{},
		reactions:    &fakeReactionsLookup{},
		stats:        &fakeStoryStatsLookup{},
		chains:       &fakeVersionChainLookup{},
		interactions: &fakeInteractionsLookup{},
		versions:     versionsService,
		caches:       Caches{Details: details},
	})

	body := `{"parent_version_id":"0a000000-0000-4000-8000-000000000001","language":"zul","title":"An adaptation","body":"Body."}`
	recorder := doStoryRequest(handler, http.MethodPost, "/stories/"+parentStoryID+"/adapt", body, testAccessToken)
	if recorder.Code != http.StatusCreated {
		t.Fatalf("status = %d, want %d (body %s)", recorder.Code, http.StatusCreated, recorder.Body.String())
	}

	state := details.snapshot()
	if len(state.invalidatedStories) != 1 || state.invalidatedStories[0] != parentStoryID {
		t.Fatalf("invalidated stories = %v, want the parent %s", state.invalidatedStories, parentStoryID)
	}
	if len(state.invalidatedVersions) != 1 || state.invalidatedVersions[0] != newVersionID {
		t.Fatalf("invalidated versions = %v, want the new %s", state.invalidatedVersions, newVersionID)
	}

	if _, stillCached := details.stories[parentStoryID]; stillCached {
		t.Error("the parent story's cached detail survived the adaptation")
	}
}

// TestAdaptFansOutTheVersion proves a new adaptation is written into the version
// feed sets, which is what makes it appear in a reader's cached feed.
func TestAdaptFansOutTheVersion(t *testing.T) {
	const parentStoryID = "0b000000-0000-4000-8000-000000000002"
	const newVersionID = "0c000000-0000-4000-8000-000000000003"

	feed := &feedCacheRecorder{}
	versionsService := &fakeVersionsService{createResult: versions.StoryVersion{
		ID:        newVersionID,
		StoryID:   parentStoryID,
		AuthorID:  testUserID,
		Language:  "zul",
		Title:     "An adaptation",
		Body:      "Body.",
		CreatedAt: testNow,
		UpdatedAt: testNow,
	}}

	handler := newRouterWithFeedLookups(t, discardLogger(), &fakeStoriesService{}, feedLookups{
		authors:      &fakeAuthorService{},
		rooted:       &fakeRootedService{},
		media:        &fakeStoryMediaLookup{},
		reactions:    &fakeReactionsLookup{},
		stats:        &fakeStoryStatsLookup{},
		chains:       &fakeVersionChainLookup{},
		interactions: &fakeInteractionsLookup{},
		versions:     versionsService,
		caches:       Caches{Feed: feed},
	})

	body := `{"parent_version_id":"0a000000-0000-4000-8000-000000000001","language":"zul","title":"An adaptation","body":"Body."}`
	recorder := doStoryRequest(handler, http.MethodPost, "/stories/"+parentStoryID+"/adapt", body, testAccessToken)
	if recorder.Code != http.StatusCreated {
		t.Fatalf("status = %d, want %d (body %s)", recorder.Code, http.StatusCreated, recorder.Body.String())
	}

	waitFor(t, "the version fan-out goroutine", func() bool { return len(feed.snapshot().fanoutCalls) > 0 })

	state := feed.snapshot()
	if len(state.fanoutCalls) != 1 {
		t.Fatalf("fan-out calls = %d, want 1", len(state.fanoutCalls))
	}
	call := state.fanoutCalls[0]
	if call.kind != "version" || call.id != newVersionID {
		t.Errorf("fan-out = %+v, want the version %s", call, newVersionID)
	}
	if call.language != "zul" {
		t.Errorf("fanned out language = %q, want zul", call.language)
	}
}

var _ = io.Discard
