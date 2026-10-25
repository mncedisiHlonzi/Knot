package httpapi

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"log/slog"
	"net/http"
	"net/http/httptest"
	"sort"
	"strings"
	"testing"
	"time"

	"github.com/knot/backend/internal/interactions"
	"github.com/knot/backend/internal/stories"
	"github.com/knot/backend/internal/storymedia"
	"github.com/knot/backend/internal/versions"
)

// testUserID is the authenticated author id the fake token parser reports.
const testUserID = "11111111-1111-4111-8111-111111111111"

// testAccessToken is the only bearer token the fake parser accepts.
const testAccessToken = "valid-access-token"

// testNow is the fixed clock the in-memory store stamps stories with.
var testNow = time.Date(2026, 10, 7, 12, 0, 0, 0, time.UTC)

// memoryStoryStore is an in-memory stories.StoryStore.
//
// The handler tests run the real stories service over this store rather than a
// stub service, so the tests cover the actual validation, cursor, and pagination
// behaviour of the domain while staying free of a database. Only the SQL is
// replaced; the keyset semantics match PostgresStore.
type memoryStoryStore struct {
	stories []stories.Story
	// versions are adaptation entries the merged feed returns alongside the
	// stories, so a handler test can drive a page that mixes both kinds without a
	// database (KNOT-022). The real store takes them from story_versions; here a
	// test seeds them directly.
	versions []stories.VersionFeedItem
	err      error

	gotCreate  stories.Story
	gotCursor  *stories.Cursor
	gotLimit   int
	createCall int
	getCalls   int
	listCalls  int
}

func (m *memoryStoryStore) CreateStory(_ context.Context, story stories.Story) (stories.Story, error) {
	m.createCall++
	m.gotCreate = story
	if m.err != nil {
		return stories.Story{}, m.err
	}

	created := story
	created.ID = fmt.Sprintf("00000000-0000-4000-8000-%012d", len(m.stories)+1)
	created.RootVersionID = fmt.Sprintf("00000000-0000-4000-9000-%012d", len(m.stories)+1)
	created.CreatedAt = testNow.Add(time.Duration(len(m.stories)) * time.Second)
	created.UpdatedAt = created.CreatedAt
	m.stories = append(m.stories, created)

	return created, nil
}

func (m *memoryStoryStore) GetStory(_ context.Context, id string) (stories.Story, error) {
	m.getCalls++
	for _, story := range m.stories {
		if story.ID == id {
			return story, nil
		}
	}
	return stories.Story{}, stories.ErrNotFound
}

func (m *memoryStoryStore) ListFeed(_ context.Context, cursor *stories.Cursor, limit int) ([]stories.FeedItem, *stories.Cursor, error) {
	m.listCalls++
	m.gotCursor = cursor
	m.gotLimit = limit
	if m.err != nil {
		return nil, nil, m.err
	}

	ordered := make([]stories.FeedItem, 0, len(m.stories)+len(m.versions))
	for _, story := range m.stories {
		ordered = append(ordered, stories.FeedItem{Kind: stories.FeedKindStory, Story: story})
	}
	for _, version := range m.versions {
		ordered = append(ordered, stories.FeedItem{Kind: stories.FeedKindVersion, Version: version})
	}

	// The merged stream is ordered by each entry's own sort key, exactly as the
	// union does: an adaptation sorts where it was written, not where its story
	// was (KNOT-022).
	sort.SliceStable(ordered, func(i, j int) bool {
		if !ordered[i].SortTime().Equal(ordered[j].SortTime()) {
			return ordered[i].SortTime().After(ordered[j].SortTime())
		}
		return ordered[i].SortID() > ordered[j].SortID()
	})

	start := 0
	if cursor != nil {
		for start < len(ordered) && !beforeCursor(ordered[start].SortTime(), ordered[start].SortID(), *cursor) {
			start++
		}
	}

	remaining := ordered[start:]
	if len(remaining) <= limit {
		return remaining, nil, nil
	}

	last := remaining[limit-1]
	next := stories.NewCursor(last.SortTime(), last.SortID())

	return remaining[:limit], &next, nil
}

// beforeCursor reports whether an entry with this sort key sorts strictly after
// cursor in the feed order (created_at DESC, id DESC), i.e. whether it belongs on
// a later page. It takes the key rather than the entry so the story store's
// discovery page and the merged feed can share it.
func beforeCursor(createdAt time.Time, id string, cursor stories.Cursor) bool {
	if createdAt.Before(cursor.CreatedAt()) {
		return true
	}
	if createdAt.After(cursor.CreatedAt()) {
		return false
	}
	return id < cursor.ID()
}

// fakeStoriesService returns canned values, so the failure paths a real service
// cannot produce (an infrastructure error) can still be tested.
type fakeStoriesService struct {
	createResult stories.Story
	createErr    error
	getResult    stories.Story
	getErr       error
	listResult   []stories.FeedItem
	listNext     string
	listErr      error
}

func (f *fakeStoriesService) CreateStory(_ context.Context, _ stories.CreateStoryInput) (stories.Story, error) {
	return f.createResult, f.createErr
}

func (f *fakeStoriesService) GetStory(_ context.Context, _ string) (stories.Story, error) {
	return f.getResult, f.getErr
}

func (f *fakeStoriesService) ListFeed(_ context.Context, _ string, _ int) ([]stories.FeedItem, string, error) {
	return f.listResult, f.listNext, f.listErr
}

// feedOf wraps stories as story entries of the merged feed, so a test that cares
// about story fields does not have to spell out the discriminator (KNOT-022).
func feedOf(storiesList ...stories.Story) []stories.FeedItem {
	items := make([]stories.FeedItem, 0, len(storiesList))
	for _, story := range storiesList {
		items = append(items, stories.FeedItem{Kind: stories.FeedKindStory, Story: story})
	}
	return items
}

// fakeVersionChainLookup returns canned lineages, so the attribution a feed page
// attaches to its adaptations can be asserted without the versions package.
type fakeVersionChainLookup struct {
	chains map[string]versions.Chain
	err    error

	calls  int
	gotIDs []string
}

func (f *fakeVersionChainLookup) ChainsFor(_ context.Context, versionIDs []string) (map[string]versions.Chain, error) {
	f.calls++
	f.gotIDs = versionIDs
	if f.err != nil {
		return nil, f.err
	}
	return f.chains, nil
}

// fakeInteractionsLookup returns canned social proof, so a feed test can assert
// what the page's one interaction read cost and what each entry carries
// (KNOT-028).
type fakeInteractionsLookup struct {
	latest map[string][]interactions.Interaction
	err    error

	calls      int
	gotTargets []interactions.Target
}

func (f *fakeInteractionsLookup) Latest(_ context.Context, targets []interactions.Target) (map[string][]interactions.Interaction, error) {
	f.calls++
	f.gotTargets = targets
	if f.err != nil {
		return nil, f.err
	}
	return f.latest, nil
}

// feedPage is the GET /stories body as a test reads it: the entries kept raw, so
// a test can decode either kind from the same page and can also assert the order
// the two kinds appear in (KNOT-022).
type feedPage struct {
	Items      []json.RawMessage `json:"items"`
	NextCursor string            `json:"next_cursor"`
}

// feedStories decodes the story entries of a page, in page order.
func feedStories(t *testing.T, page feedPage) []storyResponse {
	t.Helper()

	out := make([]storyResponse, 0, len(page.Items))
	for i, raw := range page.Items {
		if kind := feedKindOf(t, raw); kind != feedKindStory {
			continue
		}
		var story storyResponse
		if err := json.Unmarshal(raw, &story); err != nil {
			t.Fatalf("decode item %d as a story: %v", i, err)
		}
		out = append(out, story)
	}

	return out
}

// feedVersions decodes the adaptation entries of a page, in page order.
func feedVersions(t *testing.T, page feedPage) []feedVersionItem {
	t.Helper()

	out := make([]feedVersionItem, 0, len(page.Items))
	for i, raw := range page.Items {
		if kind := feedKindOf(t, raw); kind != feedKindVersion {
			continue
		}
		var version feedVersionItem
		if err := json.Unmarshal(raw, &version); err != nil {
			t.Fatalf("decode item %d as an adaptation: %v", i, err)
		}
		out = append(out, version)
	}

	return out
}

// feedKinds returns each entry's discriminator in page order, which is how a test
// asserts that the merged stream interleaves as the store returned it.
func feedKinds(t *testing.T, page feedPage) []string {
	t.Helper()

	kinds := make([]string, 0, len(page.Items))
	for _, raw := range page.Items {
		kinds = append(kinds, feedKindOf(t, raw))
	}

	return kinds
}

// feedKindOf reads one entry's discriminator, failing the test when the entry
// carries none: an entry a client cannot classify is a bug in the response.
func feedKindOf(t *testing.T, raw json.RawMessage) string {
	t.Helper()

	var envelope struct {
		Kind string `json:"kind"`
	}
	if err := json.Unmarshal(raw, &envelope); err != nil {
		t.Fatalf("decode entry kind: %v", err)
	}
	if envelope.Kind == "" {
		t.Fatalf("entry %s has no kind, want %q or %q", raw, feedKindStory, feedKindVersion)
	}

	return envelope.Kind
}

// newStoriesHandler returns the composed router with the real stories service
// over store, plus the store so a test can inspect what reached it.
func newStoriesHandler(t *testing.T, store stories.StoryStore) http.Handler {
	t.Helper()

	logger := slog.New(slog.NewTextHandler(io.Discard, nil))

	service, err := stories.NewService(store)
	if err != nil {
		t.Fatalf("stories.NewService() error = %v, want nil", err)
	}

	return newRouter(t, logger, service)
}

// newStoriesHandlerWithService is newStoriesHandler for a handler service that
// is not the real one.
func newStoriesHandlerWithService(t *testing.T, service StoriesService) http.Handler {
	t.Helper()
	return newRouter(t, slog.New(slog.NewTextHandler(io.Discard, nil)), service)
}

// newRouter assembles the full router the way cmd/knot does, so the tests
// exercise the real middleware chain and route table. The stories handler's media
// lookup is inert.
func newRouter(t *testing.T, logger *slog.Logger, service StoriesService) http.Handler {
	t.Helper()
	return newRouterWithMediaLookup(t, logger, service, &fakeStoryMediaLookup{})
}

// newRouterWithMediaLookup is newRouter with an explicit media lookup, so the
// media array attached to story responses can be asserted. Its stats lookup is
// inert.
func newRouterWithMediaLookup(t *testing.T, logger *slog.Logger, service StoriesService, lookup StoryMediaLookup) http.Handler {
	t.Helper()
	return newRouterWithLookups(t, logger, service, lookup, &fakeStoryStatsLookup{})
}

// newRouterWithLookups is newRouterWithMediaLookup with explicit media and stats
// lookups, so a test can assert both the media array and the aggregate counts a
// story response carries.
func newRouterWithLookups(t *testing.T, logger *slog.Logger, service StoriesService, lookup StoryMediaLookup, stats StoryStatsLookup) http.Handler {
	t.Helper()

	return newRouterWithFeedLookups(t, logger, service, feedLookups{
		authors:      &fakeAuthorService{},
		rooted:       &fakeRootedService{},
		media:        lookup,
		reactions:    &fakeReactionsLookup{},
		stats:        stats,
		chains:       &fakeVersionChainLookup{},
		interactions: &fakeInteractionsLookup{},
	})
}

// feedLookups are the enrichment dependencies the feed tests supply, so a test
// can seed what a page's entries are decorated with and assert what each
// decoration cost.
type feedLookups struct {
	authors      AuthorLookup
	rooted       RootedLookup
	media        StoryMediaLookup
	reactions    ReactionsLookup
	stats        StoryStatsLookup
	chains       VersionChainLookup
	interactions InteractionLookup
}

// newRouterWithFeedLookups assembles the full router with explicit feed
// enrichment dependencies, so the merged feed can be exercised end to end.
func newRouterWithFeedLookups(t *testing.T, logger *slog.Logger, service StoriesService, lookups feedLookups) http.Handler {
	t.Helper()

	authHandler, err := NewAuthHandler(&fakeAuthService{}, newTestLimiter(t), logger)
	if err != nil {
		t.Fatalf("NewAuthHandler() error = %v, want nil", err)
	}

	storiesHandler, err := NewStoriesHandler(service, lookups.authors, lookups.rooted, lookups.media, lookups.reactions, lookups.stats, lookups.chains, lookups.interactions, logger)
	if err != nil {
		t.Fatalf("NewStoriesHandler() error = %v, want nil", err)
	}

	versionsHandler, err := NewVersionsHandler(&fakeVersionsService{}, &fakeAuthorService{}, &fakeRootedService{}, &fakeReactionsLookup{}, logger)
	if err != nil {
		t.Fatalf("NewVersionsHandler() error = %v, want nil", err)
	}

	conversationsHandler, err := NewConversationsHandler(&fakeConversationsService{}, &fakeAuthorService{}, &fakeRootedService{}, &fakeReactionsLookup{}, logger)
	if err != nil {
		t.Fatalf("NewConversationsHandler() error = %v, want nil", err)
	}

	rootedHandler, err := NewRootedHandler(&fakeRootedService{}, logger)
	if err != nil {
		t.Fatalf("NewRootedHandler() error = %v, want nil", err)
	}

	discoveryHandler := newTestDiscoveryHandler(t, logger, fakeDiscoveryService{})

	authMiddleware, err := NewAuthMiddleware(&fakeTokenParser{subject: testUserID, sessionID: testSessionID}, logger)
	if err != nil {
		t.Fatalf("NewAuthMiddleware() error = %v, want nil", err)
	}

	router, err := NewRouter(authHandler, storiesHandler, versionsHandler, conversationsHandler, rootedHandler, discoveryHandler, newTestAvatarHandler(t, logger), newTestStoryMediaHandler(t, logger), newTestNotificationsHandler(t, logger), newTestProfileHandler(t, logger), newTestReactionsRouterHandler(t, logger), newTestInquiriesHandler(t, logger), newTestModerationHandler(t, logger), newTestSearchHandler(t, logger), authMiddleware, "0.1.0", logger)
	if err != nil {
		t.Fatalf("NewRouter() error = %v, want nil", err)
	}

	return router.Handler()
}

// doStoryRequest issues a request with an optional bearer token.
func doStoryRequest(handler http.Handler, method, path, body, token string) *httptest.ResponseRecorder {
	var reader io.Reader
	if body != "" {
		reader = strings.NewReader(body)
	}

	request := httptest.NewRequest(method, path, reader)
	request.Header.Set("Content-Type", "application/json")
	if token != "" {
		request.Header.Set("Authorization", "Bearer "+token)
	}

	recorder := httptest.NewRecorder()
	handler.ServeHTTP(recorder, request)

	return recorder
}

// validStoryBody is a request body that passes every validation rule.
func validStoryBody() string {
	return `{
		"pillar": "wonder",
		"language": "eng",
		"title": "The first rain",
		"body": "Grandmother said the first rain remembers every name.",
		"approximate_location": "Cape Town",
		"media_urls": ["https://example.test/rain.jpg"]
	}`
}

func TestCreateStoryHappyPath(t *testing.T) {
	store := &memoryStoryStore{}
	handler := newStoriesHandler(t, store)

	recorder := doStoryRequest(handler, http.MethodPost, "/stories", validStoryBody(), testAccessToken)

	if recorder.Code != http.StatusCreated {
		t.Fatalf("status = %d, want %d (body %s)", recorder.Code, http.StatusCreated, recorder.Body.String())
	}
	if contentType := recorder.Header().Get("Content-Type"); !strings.HasPrefix(contentType, "application/json") {
		t.Errorf("Content-Type = %q, want an application/json media type", contentType)
	}

	var body storyEnvelope
	decodeBody(t, recorder, &body)

	if body.Story.ID == "" {
		t.Error("id is empty, want the stored id")
	}
	if body.Story.AuthorID != testUserID {
		t.Errorf("author id = %q, want the authenticated user %q", body.Story.AuthorID, testUserID)
	}
	if body.Story.RootVersionID == "" {
		t.Error("root version id is empty, want the id of the story's root version")
	}
	if body.Story.Pillar != stories.PillarWonder {
		t.Errorf("pillar = %q, want %q", body.Story.Pillar, stories.PillarWonder)
	}
	if body.Story.Title != "The first rain" {
		t.Errorf("title = %q, want %q", body.Story.Title, "The first rain")
	}
	if body.Story.CreatedAt.IsZero() {
		t.Error("created at is zero, want a timestamp")
	}
	if store.gotCreate.AuthorID != testUserID {
		t.Errorf("store author id = %q, want %q", store.gotCreate.AuthorID, testUserID)
	}
}

func TestCreateStoryWithCoordinates(t *testing.T) {
	store := &memoryStoryStore{}
	handler := newStoriesHandler(t, store)

	body := `{"pillar":"wonder","language":"eng","title":"T","body":"B","approximate_location":"Manguzi","latitude":-26.9998,"longitude":32.7489,"place_country":"South Africa"}`
	recorder := doStoryRequest(handler, http.MethodPost, "/stories", body, testAccessToken)

	if recorder.Code != http.StatusCreated {
		t.Fatalf("status = %d, want %d (body %s)", recorder.Code, http.StatusCreated, recorder.Body.String())
	}

	var envelope storyEnvelope
	decodeBody(t, recorder, &envelope)

	if envelope.Story.Latitude == nil || *envelope.Story.Latitude != -26.9998 {
		t.Errorf("latitude = %v, want -26.9998", envelope.Story.Latitude)
	}
	if envelope.Story.Longitude == nil || *envelope.Story.Longitude != 32.7489 {
		t.Errorf("longitude = %v, want 32.7489", envelope.Story.Longitude)
	}
	if envelope.Story.PlaceCountry == nil || *envelope.Story.PlaceCountry != "South Africa" {
		t.Errorf("place country = %v, want South Africa", envelope.Story.PlaceCountry)
	}
	if store.gotCreate.Latitude == nil || *store.gotCreate.Latitude != -26.9998 {
		t.Errorf("store latitude = %v, want -26.9998", store.gotCreate.Latitude)
	}
}

func TestCreateStoryWithoutCoordinatesIsNull(t *testing.T) {
	handler := newStoriesHandler(t, &memoryStoryStore{})

	recorder := doStoryRequest(handler, http.MethodPost, "/stories", validStoryBody(), testAccessToken)
	if recorder.Code != http.StatusCreated {
		t.Fatalf("status = %d, want %d", recorder.Code, http.StatusCreated)
	}

	var envelope storyEnvelope
	decodeBody(t, recorder, &envelope)
	if envelope.Story.Latitude != nil || envelope.Story.Longitude != nil || envelope.Story.PlaceCountry != nil {
		t.Errorf("structured place = (%v, %v, %v), want nils", envelope.Story.Latitude, envelope.Story.Longitude, envelope.Story.PlaceCountry)
	}
	raw := recorder.Body.String()
	if !strings.Contains(raw, `"latitude":null`) || !strings.Contains(raw, `"longitude":null`) || !strings.Contains(raw, `"place_country":null`) {
		t.Errorf("body = %s, want explicit null coordinates", raw)
	}
}

func TestCreateStoryRequiresAuthentication(t *testing.T) {
	store := &memoryStoryStore{}
	handler := newStoriesHandler(t, store)

	recorder := doStoryRequest(handler, http.MethodPost, "/stories", validStoryBody(), "")

	if recorder.Code != http.StatusUnauthorized {
		t.Fatalf("status = %d, want %d", recorder.Code, http.StatusUnauthorized)
	}
	if code := decodedErrorCode(t, recorder); code != codeUnauthorized {
		t.Errorf("error code = %q, want %q", code, codeUnauthorized)
	}

	var body errorResponse
	decodeBody(t, recorder, &body)
	if body.Error.Message != "authentication required" {
		t.Errorf("message = %q, want %q", body.Error.Message, "authentication required")
	}
	if store.createCall != 0 {
		t.Errorf("store received %d create calls, want 0 — an unauthenticated request must not publish", store.createCall)
	}
}

func TestCreateStoryRejectsInvalidToken(t *testing.T) {
	handler := newStoriesHandler(t, &memoryStoryStore{})

	recorder := doStoryRequest(handler, http.MethodPost, "/stories", validStoryBody(), "not-a-real-token")

	if recorder.Code != http.StatusUnauthorized {
		t.Errorf("status = %d, want %d", recorder.Code, http.StatusUnauthorized)
	}
}

func TestCreateStoryRejectsAuthorIDInBody(t *testing.T) {
	// The author comes from the token. A client that tries to set it explicitly
	// must be rejected rather than silently ignored, so there is no doubt about
	// which value was used.
	store := &memoryStoryStore{}
	handler := newStoriesHandler(t, store)

	body := `{"pillar":"wonder","language":"eng","title":"T","body":"B","author_id":"99999999-9999-4999-8999-999999999999"}`
	recorder := doStoryRequest(handler, http.MethodPost, "/stories", body, testAccessToken)

	if recorder.Code != http.StatusBadRequest {
		t.Fatalf("status = %d, want %d", recorder.Code, http.StatusBadRequest)
	}
	if code := decodedErrorCode(t, recorder); code != codeInvalidRequest {
		t.Errorf("error code = %q, want %q", code, codeInvalidRequest)
	}
	if store.createCall != 0 {
		t.Error("the store was called, want the body rejected before the service runs")
	}
}

func TestCreateStoryValidationIsBadRequest(t *testing.T) {
	tests := []struct {
		name string
		body string
	}{
		{name: "missing title", body: `{"pillar":"wonder","language":"eng","body":"B"}`},
		{name: "blank body", body: `{"pillar":"wonder","language":"eng","title":"T","body":"  "}`},
		{name: "unknown pillar", body: `{"pillar":"chaos","language":"eng","title":"T","body":"B"}`},
		{name: "bad language", body: `{"pillar":"wonder","language":"e","title":"T","body":"B"}`},
		{name: "two letter language", body: `{"pillar":"wonder","language":"en","title":"T","body":"B"}`},
		{name: "latitude without longitude", body: `{"pillar":"wonder","language":"eng","title":"T","body":"B","latitude":-26.9}`},
		{name: "longitude without latitude", body: `{"pillar":"wonder","language":"eng","title":"T","body":"B","longitude":32.7}`},
		{name: "latitude out of range", body: `{"pillar":"wonder","language":"eng","title":"T","body":"B","latitude":91,"longitude":0}`},
		{name: "longitude out of range", body: `{"pillar":"wonder","language":"eng","title":"T","body":"B","latitude":0,"longitude":181}`},
	}

	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			handler := newStoriesHandler(t, &memoryStoryStore{})

			recorder := doStoryRequest(handler, http.MethodPost, "/stories", test.body, testAccessToken)

			if recorder.Code != http.StatusBadRequest {
				t.Fatalf("status = %d, want %d (body %s)", recorder.Code, http.StatusBadRequest, recorder.Body.String())
			}
			if code := decodedErrorCode(t, recorder); code != codeValidation {
				t.Errorf("error code = %q, want %q", code, codeValidation)
			}
		})
	}
}

func TestCreateStoryRejectsUnreadableBodies(t *testing.T) {
	tests := []struct {
		name     string
		body     string
		wantCode int
	}{
		{name: "malformed json", body: `{"pillar":`, wantCode: http.StatusBadRequest},
		{name: "empty body", body: "", wantCode: http.StatusBadRequest},
		{name: "oversized body", body: `{"title":"` + strings.Repeat("x", maxRequestBodyBytes) + `"}`, wantCode: http.StatusRequestEntityTooLarge},
	}

	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			handler := newStoriesHandler(t, &memoryStoryStore{})

			recorder := doStoryRequest(handler, http.MethodPost, "/stories", test.body, testAccessToken)

			if recorder.Code != test.wantCode {
				t.Fatalf("status = %d, want %d", recorder.Code, test.wantCode)
			}
		})
	}
}

func TestCreateStoryUnexpectedFailureIsInternalError(t *testing.T) {
	service := &fakeStoriesService{createErr: errors.New("connection reset")}
	handler := newStoriesHandlerWithService(t, service)

	recorder := doStoryRequest(handler, http.MethodPost, "/stories", validStoryBody(), testAccessToken)

	if recorder.Code != http.StatusInternalServerError {
		t.Fatalf("status = %d, want %d", recorder.Code, http.StatusInternalServerError)
	}
	if code := decodedErrorCode(t, recorder); code != codeInternal {
		t.Errorf("error code = %q, want %q", code, codeInternal)
	}
	if strings.Contains(recorder.Body.String(), "connection reset") {
		t.Error("the response echoes the internal error, want a generic message")
	}
}

func TestGetStoryHappyPath(t *testing.T) {
	store := &memoryStoryStore{}
	handler := newStoriesHandler(t, store)

	created := createTestStory(t, handler)
	recorder := doStoryRequest(handler, http.MethodGet, "/stories/"+created.ID, "", "")

	if recorder.Code != http.StatusOK {
		t.Fatalf("status = %d, want %d (body %s)", recorder.Code, http.StatusOK, recorder.Body.String())
	}

	var body storyEnvelope
	decodeBody(t, recorder, &body)

	if body.Story.ID != created.ID {
		t.Errorf("id = %q, want %q", body.Story.ID, created.ID)
	}
	if body.Story.Body != created.Body {
		t.Errorf("body = %q, want %q", body.Story.Body, created.Body)
	}
}

func TestGetStoryNotFound(t *testing.T) {
	store := &memoryStoryStore{}
	handler := newStoriesHandler(t, store)

	recorder := doStoryRequest(handler, http.MethodGet, "/stories/99999999-9999-4999-8999-999999999999", "", "")

	if recorder.Code != http.StatusNotFound {
		t.Fatalf("status = %d, want %d", recorder.Code, http.StatusNotFound)
	}

	var body errorResponse
	decodeBody(t, recorder, &body)

	if body.Error.Code != codeNotFound {
		t.Errorf("error code = %q, want %q", body.Error.Code, codeNotFound)
	}
	if body.Error.Message != "story not found" {
		t.Errorf("message = %q, want %q", body.Error.Message, "story not found")
	}
}

func TestGetStoryMalformedIDIsNotFound(t *testing.T) {
	store := &memoryStoryStore{}
	handler := newStoriesHandler(t, store)

	recorder := doStoryRequest(handler, http.MethodGet, "/stories/not-a-uuid", "", "")

	if recorder.Code != http.StatusNotFound {
		t.Errorf("status = %d, want %d", recorder.Code, http.StatusNotFound)
	}
	if store.getCalls != 0 {
		t.Errorf("store received %d get calls, want 0 — a malformed id cannot match a row", store.getCalls)
	}
}

func TestGetStoryUnexpectedFailureIsInternalError(t *testing.T) {
	service := &fakeStoriesService{getErr: errors.New("connection reset")}
	handler := newStoriesHandlerWithService(t, service)

	recorder := doStoryRequest(handler, http.MethodGet, "/stories/99999999-9999-4999-8999-999999999999", "", "")

	if recorder.Code != http.StatusInternalServerError {
		t.Fatalf("status = %d, want %d", recorder.Code, http.StatusInternalServerError)
	}
	if code := decodedErrorCode(t, recorder); code != codeInternal {
		t.Errorf("error code = %q, want %q", code, codeInternal)
	}
}

func TestStoriesTrailingSlashDoesNotMatchStoryID(t *testing.T) {
	// "/stories/" must not be treated as a story id. The handler is specifically
	// not reached: a route that matched with an empty id would let a request for
	// the collection be answered as though it named a resource.
	store := &memoryStoryStore{}
	handler := newStoriesHandler(t, store)

	recorder := doStoryRequest(handler, http.MethodGet, "/stories/", "", "")

	if recorder.Code != http.StatusNotFound {
		t.Errorf("status = %d, want %d", recorder.Code, http.StatusNotFound)
	}
	if store.getCalls != 0 {
		t.Error("the story handler ran for /stories/, want the route not to match GET /stories/{id}")
	}
}

func TestListStoriesEmptyFeedIsAnEmptyArray(t *testing.T) {
	handler := newStoriesHandler(t, &memoryStoryStore{})

	recorder := doStoryRequest(handler, http.MethodGet, "/stories", "", "")

	if recorder.Code != http.StatusOK {
		t.Fatalf("status = %d, want %d", recorder.Code, http.StatusOK)
	}

	var body feedPage
	decodeBody(t, recorder, &body)

	if body.Items == nil {
		t.Error("items = null, want an empty array")
	}
	if len(body.Items) != 0 {
		t.Errorf("len(items) = %d, want 0", len(body.Items))
	}
	if body.NextCursor != "" {
		t.Errorf("next cursor = %q, want an empty string on the last page", body.NextCursor)
	}
	if !strings.Contains(recorder.Body.String(), `"items":[]`) {
		t.Errorf("body = %s, want it to contain an empty array", recorder.Body.String())
	}
}

func TestListStoriesDefaultsAndClampsLimit(t *testing.T) {
	tests := []struct {
		name      string
		query     string
		wantLimit int
	}{
		{name: "absent", query: "", wantLimit: stories.DefaultListLimit},
		{name: "explicit", query: "?limit=5", wantLimit: 5},
		{name: "at the maximum", query: fmt.Sprintf("?limit=%d", stories.MaxListLimit), wantLimit: stories.MaxListLimit},
		{name: "above the maximum is clamped", query: "?limit=5000", wantLimit: stories.MaxListLimit},
	}

	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			store := &memoryStoryStore{}
			handler := newStoriesHandler(t, store)

			recorder := doStoryRequest(handler, http.MethodGet, "/stories"+test.query, "", "")

			if recorder.Code != http.StatusOK {
				t.Fatalf("status = %d, want %d", recorder.Code, http.StatusOK)
			}
			if store.gotLimit != test.wantLimit {
				t.Errorf("store limit = %d, want %d", store.gotLimit, test.wantLimit)
			}
		})
	}
}

func TestListStoriesRejectsBadLimit(t *testing.T) {
	tests := []struct {
		name  string
		query string
	}{
		{name: "zero", query: "?limit=0"},
		{name: "negative", query: "?limit=-3"},
		{name: "not a number", query: "?limit=ten"},
		{name: "fractional", query: "?limit=1.5"},
	}

	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			store := &memoryStoryStore{}
			handler := newStoriesHandler(t, store)

			recorder := doStoryRequest(handler, http.MethodGet, "/stories"+test.query, "", "")

			if recorder.Code != http.StatusBadRequest {
				t.Fatalf("status = %d, want %d", recorder.Code, http.StatusBadRequest)
			}
			if code := decodedErrorCode(t, recorder); code != codeValidation {
				t.Errorf("error code = %q, want %q", code, codeValidation)
			}
			if store.listCalls != 0 {
				t.Errorf("store received %d list calls, want 0", store.listCalls)
			}
		})
	}
}

func TestListStoriesRejectsMalformedCursor(t *testing.T) {
	store := &memoryStoryStore{}
	handler := newStoriesHandler(t, store)

	recorder := doStoryRequest(handler, http.MethodGet, "/stories?cursor=not-a-cursor", "", "")

	if recorder.Code != http.StatusBadRequest {
		t.Fatalf("status = %d, want %d", recorder.Code, http.StatusBadRequest)
	}
	if code := decodedErrorCode(t, recorder); code != codeValidation {
		t.Errorf("error code = %q, want %q", code, codeValidation)
	}
	if store.listCalls != 0 {
		t.Errorf("store received %d list calls, want 0 — an unreadable cursor must not reach the database", store.listCalls)
	}
}

func TestListStoriesPaginatesThroughTheFeed(t *testing.T) {
	store := &memoryStoryStore{}
	handler := newStoriesHandler(t, store)

	for i := 0; i < 3; i++ {
		if recorder := doStoryRequest(handler, http.MethodPost, "/stories", validStoryBody(), testAccessToken); recorder.Code != http.StatusCreated {
			t.Fatalf("seed status = %d, want %d", recorder.Code, http.StatusCreated)
		}
	}

	// First page: two rows and a cursor to resume from.
	recorder := doStoryRequest(handler, http.MethodGet, "/stories?limit=2", "", "")
	if recorder.Code != http.StatusOK {
		t.Fatalf("status = %d, want %d", recorder.Code, http.StatusOK)
	}

	var first feedPage
	decodeBody(t, recorder, &first)

	if len(first.Items) != 2 {
		t.Fatalf("len(items) = %d, want 2", len(first.Items))
	}
	if first.NextCursor == "" {
		t.Fatal("next cursor is empty, want a cursor for the second page")
	}

	// Second page: the remaining row, and no further cursor.
	recorder = doStoryRequest(handler, http.MethodGet, "/stories?limit=2&cursor="+first.NextCursor, "", "")
	if recorder.Code != http.StatusOK {
		t.Fatalf("status = %d, want %d (body %s)", recorder.Code, http.StatusOK, recorder.Body.String())
	}

	var second feedPage
	decodeBody(t, recorder, &second)

	if len(second.Items) != 1 {
		t.Fatalf("len(items) = %d, want 1", len(second.Items))
	}
	if second.NextCursor != "" {
		t.Errorf("next cursor = %q, want an empty string on the last page", second.NextCursor)
	}

	firstPage := feedStories(t, first)
	secondPage := feedStories(t, second)
	if len(firstPage) != 2 || len(secondPage) != 1 {
		t.Fatalf("decoded stories = %d then %d, want 2 then 1", len(firstPage), len(secondPage))
	}
	if secondPage[0].ID == firstPage[0].ID || secondPage[0].ID == firstPage[1].ID {
		t.Error("the second page repeated a row from the first page")
	}
}

func TestListStoriesUnexpectedFailureIsInternalError(t *testing.T) {
	service := &fakeStoriesService{listErr: errors.New("connection reset")}
	handler := newStoriesHandlerWithService(t, service)

	recorder := doStoryRequest(handler, http.MethodGet, "/stories", "", "")

	if recorder.Code != http.StatusInternalServerError {
		t.Fatalf("status = %d, want %d", recorder.Code, http.StatusInternalServerError)
	}
	if code := decodedErrorCode(t, recorder); code != codeInternal {
		t.Errorf("error code = %q, want %q", code, codeInternal)
	}
}

func TestNewStoriesHandlerRejectsMissingDependencies(t *testing.T) {
	logger := slog.New(slog.NewTextHandler(io.Discard, nil))

	if _, err := NewStoriesHandler(nil, &fakeAuthorService{}, &fakeRootedService{}, &fakeStoryMediaLookup{}, &fakeReactionsLookup{}, &fakeStoryStatsLookup{}, &fakeVersionChainLookup{}, &fakeInteractionsLookup{}, logger); err == nil {
		t.Error("NewStoriesHandler(nil service) error = nil, want an error")
	}
	if _, err := NewStoriesHandler(&fakeStoriesService{}, nil, &fakeRootedService{}, &fakeStoryMediaLookup{}, &fakeReactionsLookup{}, &fakeStoryStatsLookup{}, &fakeVersionChainLookup{}, &fakeInteractionsLookup{}, logger); err == nil {
		t.Error("NewStoriesHandler(nil authors) error = nil, want an error")
	}
	if _, err := NewStoriesHandler(&fakeStoriesService{}, &fakeAuthorService{}, nil, &fakeStoryMediaLookup{}, &fakeReactionsLookup{}, &fakeStoryStatsLookup{}, &fakeVersionChainLookup{}, &fakeInteractionsLookup{}, logger); err == nil {
		t.Error("NewStoriesHandler(nil rooted) error = nil, want an error")
	}
	if _, err := NewStoriesHandler(&fakeStoriesService{}, &fakeAuthorService{}, &fakeRootedService{}, nil, &fakeReactionsLookup{}, &fakeStoryStatsLookup{}, &fakeVersionChainLookup{}, &fakeInteractionsLookup{}, logger); err == nil {
		t.Error("NewStoriesHandler(nil media) error = nil, want an error")
	}
	if _, err := NewStoriesHandler(&fakeStoriesService{}, &fakeAuthorService{}, &fakeRootedService{}, &fakeStoryMediaLookup{}, nil, &fakeStoryStatsLookup{}, &fakeVersionChainLookup{}, &fakeInteractionsLookup{}, logger); err == nil {
		t.Error("NewStoriesHandler(nil reactions) error = nil, want an error")
	}
	if _, err := NewStoriesHandler(&fakeStoriesService{}, &fakeAuthorService{}, &fakeRootedService{}, &fakeStoryMediaLookup{}, &fakeReactionsLookup{}, nil, &fakeVersionChainLookup{}, &fakeInteractionsLookup{}, logger); err == nil {
		t.Error("NewStoriesHandler(nil stats) error = nil, want an error")
	}
	// The chain lookup is what attributes an adaptation in the feed, so a handler
	// without one would serve entries it cannot attribute (KNOT-022).
	if _, err := NewStoriesHandler(&fakeStoriesService{}, &fakeAuthorService{}, &fakeRootedService{}, &fakeStoryMediaLookup{}, &fakeReactionsLookup{}, &fakeStoryStatsLookup{}, nil, &fakeInteractionsLookup{}, logger); err == nil {
		t.Error("NewStoriesHandler(nil chains) error = nil, want an error")
	}
	// The interaction lookup is the social proof header, so a handler without one
	// would silently serve every entry without it (KNOT-028).
	if _, err := NewStoriesHandler(&fakeStoriesService{}, &fakeAuthorService{}, &fakeRootedService{}, &fakeStoryMediaLookup{}, &fakeReactionsLookup{}, &fakeStoryStatsLookup{}, &fakeVersionChainLookup{}, nil, logger); err == nil {
		t.Error("NewStoriesHandler(nil interactions) error = nil, want an error")
	}
	if _, err := NewStoriesHandler(&fakeStoriesService{}, &fakeAuthorService{}, &fakeRootedService{}, &fakeStoryMediaLookup{}, &fakeReactionsLookup{}, &fakeStoryStatsLookup{}, &fakeVersionChainLookup{}, &fakeInteractionsLookup{}, nil); err == nil {
		t.Error("NewStoriesHandler(nil logger) error = nil, want an error")
	}
}

// createTestStory publishes a story through the API and returns the stored story.
func createTestStory(t *testing.T, handler http.Handler) storyResponse {
	t.Helper()

	recorder := doStoryRequest(handler, http.MethodPost, "/stories", validStoryBody(), testAccessToken)
	if recorder.Code != http.StatusCreated {
		t.Fatalf("status = %d, want %d (body %s)", recorder.Code, http.StatusCreated, recorder.Body.String())
	}

	var body storyEnvelope
	decodeBody(t, recorder, &body)

	return body.Story
}

// TestStoryDetailIncludesAttribution pins that the story detail states its own
// authorship: a story IS its root version, so its chain is a single link and its
// depth is 0 (KNOT-021). The router's author fake is empty, so the name falls
// back to the generic phrase, which also proves the fallback is wired.
func TestStoryDetailIncludesAttribution(t *testing.T) {
	store := &memoryStoryStore{}
	handler := newStoriesHandler(t, store)

	created := createTestStory(t, handler)
	recorder := doStoryRequest(handler, http.MethodGet, "/stories/"+created.ID, "", "")

	if recorder.Code != http.StatusOK {
		t.Fatalf("status = %d, want %d (body %s)", recorder.Code, http.StatusOK, recorder.Body.String())
	}

	var body storyEnvelope
	decodeBody(t, recorder, &body)

	if body.Story.VersionDepth == nil || *body.Story.VersionDepth != 0 {
		t.Errorf("version_depth = %v, want 0", body.Story.VersionDepth)
	}
	if body.Story.AttributionLine == nil {
		t.Fatal("attribution_line is absent, want the original's line")
	}
	want := "Original story by another author in English."
	if *body.Story.AttributionLine != want {
		t.Errorf("attribution_line = %q, want %q", *body.Story.AttributionLine, want)
	}
}

// TestStoryFeedOmitsAttribution pins the boundary with KNOT-022: the feed must
// not carry the attribution fields yet, so they are absent from its JSON rather
// than present and empty. They are pointers with omitempty precisely so the feed
// response is byte-identical to what it was before this task.
func TestStoryFeedOmitsAttribution(t *testing.T) {
	store := &memoryStoryStore{}
	handler := newStoriesHandler(t, store)

	createTestStory(t, handler)
	recorder := doStoryRequest(handler, http.MethodGet, "/stories", "", "")

	if recorder.Code != http.StatusOK {
		t.Fatalf("status = %d, want %d (body %s)", recorder.Code, http.StatusOK, recorder.Body.String())
	}

	raw := recorder.Body.String()
	if strings.Contains(raw, "attribution_line") || strings.Contains(raw, "version_depth") {
		t.Errorf("feed body = %s, want no attribution fields on the feed", raw)
	}
}

// ensure the fake satisfies the handler's contract at compile time.
var _ StoriesService = (*fakeStoriesService)(nil)

// ensure the in-memory store satisfies the domain contract at compile time.
var _ stories.StoryStore = (*memoryStoryStore)(nil)

// sampleMedia builds a storymedia item for the media-enrichment tests.
func sampleMedia(storyID, mediaID string) storymedia.StoryMedia {
	return storymedia.StoryMedia{
		ID:           mediaID,
		StoryID:      storyID,
		UploaderID:   testUserID,
		StorageKey:   "story-media/" + storyID + "/" + mediaID + ".jpg",
		MediaType:    storymedia.MediaTypeImage,
		MimeType:     "image/jpeg",
		Source:       storymedia.MediaSourceCamera,
		SizeBytes:    2048,
		DisplayOrder: 0,
		CreatedAt:    testNow,
	}
}

// TestStoryDetailIncludesMedia pins that GET /stories/{id} carries the story's
// full media list, each item with a relative content URL.
func TestStoryDetailIncludesMedia(t *testing.T) {
	logger := slog.New(slog.NewTextHandler(io.Discard, nil))

	store := &memoryStoryStore{}
	created, err := store.CreateStory(context.Background(), stories.Story{
		AuthorID: testUserID,
		Pillar:   stories.PillarWonder,
		Language: "eng",
		Title:    "The first rain",
		Body:     "Grandmother said the first rain remembers every name.",
	})
	if err != nil {
		t.Fatalf("CreateStory() error = %v, want nil", err)
	}

	const mediaID = "00000000-0000-4000-8000-000000009999"
	lookup := &fakeStoryMediaLookup{
		byStory: map[string][]storymedia.StoryMedia{created.ID: {sampleMedia(created.ID, mediaID)}},
	}

	service, err := stories.NewService(store)
	if err != nil {
		t.Fatalf("stories.NewService() error = %v, want nil", err)
	}
	handler := newRouterWithMediaLookup(t, logger, service, lookup)

	recorder := doRequest(handler, http.MethodGet, "/stories/"+created.ID, "")
	if recorder.Code != http.StatusOK {
		t.Fatalf("status = %d, want %d (body %s)", recorder.Code, http.StatusOK, recorder.Body.String())
	}

	var body storyEnvelope
	decodeBody(t, recorder, &body)

	if len(body.Story.Media) != 1 {
		t.Fatalf("media count = %d, want 1", len(body.Story.Media))
	}
	item := body.Story.Media[0]
	if item.ID != mediaID {
		t.Errorf("media id = %q, want %q", item.ID, mediaID)
	}
	if item.Source != "camera" {
		t.Errorf("media source = %q, want camera", item.Source)
	}
	if want := "/stories/" + created.ID + "/media/" + mediaID + "/content"; item.URL != want {
		t.Errorf("media url = %q, want %q", item.URL, want)
	}
}

// TestStoryDetailEmitsEmptyMediaArrayNotNull pins that the field is always an
// array, so a client never has to special-case null.
func TestStoryDetailEmitsEmptyMediaArrayNotNull(t *testing.T) {
	logger := slog.New(slog.NewTextHandler(io.Discard, nil))

	store := &memoryStoryStore{}
	created, err := store.CreateStory(context.Background(), stories.Story{
		AuthorID: testUserID,
		Pillar:   stories.PillarWonder,
		Language: "eng",
		Title:    "No media here",
		Body:     "Body",
	})
	if err != nil {
		t.Fatalf("CreateStory() error = %v, want nil", err)
	}

	service, err := stories.NewService(store)
	if err != nil {
		t.Fatalf("stories.NewService() error = %v, want nil", err)
	}
	handler := newRouterWithMediaLookup(t, logger, service, &fakeStoryMediaLookup{})

	recorder := doRequest(handler, http.MethodGet, "/stories/"+created.ID, "")
	if !strings.Contains(recorder.Body.String(), `"media":[]`) {
		t.Errorf("body = %s, want an empty media array rather than null", recorder.Body.String())
	}
}

// TestStoryFeedIncludesMediaPreview pins that the feed carries a single-item
// preview per story, and an empty array for a story with no media.
func TestStoryFeedIncludesMediaPreview(t *testing.T) {
	logger := slog.New(slog.NewTextHandler(io.Discard, nil))

	store := &memoryStoryStore{}
	first, err := store.CreateStory(context.Background(), stories.Story{
		AuthorID: testUserID,
		Pillar:   stories.PillarWonder,
		Language: "eng",
		Title:    "Has media",
		Body:     "Body",
	})
	if err != nil {
		t.Fatalf("CreateStory() error = %v, want nil", err)
	}
	second, err := store.CreateStory(context.Background(), stories.Story{
		AuthorID: testUserID,
		Pillar:   stories.PillarWonder,
		Language: "eng",
		Title:    "No media",
		Body:     "Body",
	})
	if err != nil {
		t.Fatalf("CreateStory() error = %v, want nil", err)
	}

	const mediaID = "00000000-0000-4000-8000-000000008888"
	lookup := &fakeStoryMediaLookup{
		first: map[string]storymedia.StoryMedia{first.ID: sampleMedia(first.ID, mediaID)},
	}

	service, err := stories.NewService(store)
	if err != nil {
		t.Fatalf("stories.NewService() error = %v, want nil", err)
	}
	handler := newRouterWithMediaLookup(t, logger, service, lookup)

	recorder := doRequest(handler, http.MethodGet, "/stories", "")
	if recorder.Code != http.StatusOK {
		t.Fatalf("status = %d, want %d (body %s)", recorder.Code, http.StatusOK, recorder.Body.String())
	}

	var body feedPage
	decodeBody(t, recorder, &body)

	storiesInPage := feedStories(t, body)
	byID := make(map[string][]storyMediaResponse, len(storiesInPage))
	for _, story := range storiesInPage {
		byID[story.ID] = story.Media
	}

	if got := byID[first.ID]; len(got) != 1 || got[0].ID != mediaID {
		t.Errorf("story with media = %+v, want one preview item %q", got, mediaID)
	}
	if got := byID[second.ID]; len(got) != 0 {
		t.Errorf("story without media = %+v, want an empty preview list", got)
	}
}

// TestStoryFeedMarksALongBodyAsATruncatedPreview pins the flag a feed entry's
// "read more" link keys off. The feed carries the whole body — clamping it to
// three lines is the client's job — so the server answers the only question it
// can: is this body longer than the preview window? A body exactly at the window
// is not marked, so the flag never claims there is more to read than there is
// (KNOT-020g-fix12).
func TestStoryFeedMarksALongBodyAsATruncatedPreview(t *testing.T) {
	logger := slog.New(slog.NewTextHandler(io.Discard, nil))

	store := &memoryStoryStore{}
	bodies := map[string]string{
		"short": "A body that fits in the preview.",
		"exact": strings.Repeat("a", previewWindowChars),
		"long":  strings.Repeat("b", previewWindowChars+1),
	}

	ids := make(map[string]string, len(bodies))
	for name, body := range bodies {
		created, err := store.CreateStory(context.Background(), stories.Story{
			AuthorID: testUserID,
			Pillar:   stories.PillarWonder,
			Language: "eng",
			Title:    name,
			Body:     body,
		})
		if err != nil {
			t.Fatalf("CreateStory(%s) error = %v, want nil", name, err)
		}
		ids[name] = created.ID
	}

	service, err := stories.NewService(store)
	if err != nil {
		t.Fatalf("stories.NewService() error = %v, want nil", err)
	}
	handler := newRouter(t, logger, service)

	recorder := doRequest(handler, http.MethodGet, "/stories", "")
	if recorder.Code != http.StatusOK {
		t.Fatalf("status = %d, want %d (body %s)", recorder.Code, http.StatusOK, recorder.Body.String())
	}

	var body feedPage
	decodeBody(t, recorder, &body)

	got := make(map[string]bool, len(body.Items))
	for _, story := range feedStories(t, body) {
		got[story.ID] = story.PreviewTruncated
	}

	if !got[ids["long"]] {
		t.Error("one character past the window: preview_truncated = false, want true")
	}
	if got[ids["exact"]] {
		t.Error("exactly the window: preview_truncated = true, want false, the body is whole")
	}
	if got[ids["short"]] {
		t.Error("short body: preview_truncated = true, want false")
	}
	// The field is not `omitempty`: a client reads it without having to tell absent
	// from false.
	if !strings.Contains(recorder.Body.String(), `"preview_truncated":false`) {
		t.Errorf("body = %s, want preview_truncated present on every story", recorder.Body.String())
	}
}

// TestStoryMediaEnrichmentFailureIsSwallowed pins that a media lookup error does
// not make a story unreadable: the media is supplementary.
func TestStoryMediaEnrichmentFailureIsSwallowed(t *testing.T) {
	logger := slog.New(slog.NewTextHandler(io.Discard, nil))

	store := &memoryStoryStore{}
	created, err := store.CreateStory(context.Background(), stories.Story{
		AuthorID: testUserID,
		Pillar:   stories.PillarWonder,
		Language: "eng",
		Title:    "Readable anyway",
		Body:     "Body",
	})
	if err != nil {
		t.Fatalf("CreateStory() error = %v, want nil", err)
	}

	service, err := stories.NewService(store)
	if err != nil {
		t.Fatalf("stories.NewService() error = %v, want nil", err)
	}
	handler := newRouterWithMediaLookup(t, logger, service, &fakeStoryMediaLookup{err: errors.New("media read failed")})

	recorder := doRequest(handler, http.MethodGet, "/stories/"+created.ID, "")
	if recorder.Code != http.StatusOK {
		t.Fatalf("status = %d, want %d even when media enrichment fails", recorder.Code, http.StatusOK)
	}
	if !strings.Contains(recorder.Body.String(), `"media":[]`) {
		t.Errorf("body = %s, want an empty media array on enrichment failure", recorder.Body.String())
	}
}
