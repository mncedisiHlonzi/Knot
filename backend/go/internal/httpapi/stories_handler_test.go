package httpapi

import (
	"context"
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

	"github.com/knot/backend/internal/stories"
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
	err     error

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

func (m *memoryStoryStore) ListStories(_ context.Context, cursor *stories.Cursor, limit int) ([]stories.Story, *stories.Cursor, error) {
	m.listCalls++
	m.gotCursor = cursor
	m.gotLimit = limit
	if m.err != nil {
		return nil, nil, m.err
	}

	ordered := make([]stories.Story, len(m.stories))
	copy(ordered, m.stories)
	sort.SliceStable(ordered, func(i, j int) bool {
		if !ordered[i].CreatedAt.Equal(ordered[j].CreatedAt) {
			return ordered[i].CreatedAt.After(ordered[j].CreatedAt)
		}
		return ordered[i].ID > ordered[j].ID
	})

	start := 0
	if cursor != nil {
		for start < len(ordered) && !beforeCursor(ordered[start], *cursor) {
			start++
		}
	}

	remaining := ordered[start:]
	if len(remaining) <= limit {
		return remaining, nil, nil
	}

	last := remaining[limit-1]
	next := stories.NewCursor(last.CreatedAt, last.ID)

	return remaining[:limit], &next, nil
}

// beforeCursor reports whether story sorts strictly after cursor in the feed
// order (created_at DESC, id DESC), i.e. whether it belongs on a later page.
func beforeCursor(story stories.Story, cursor stories.Cursor) bool {
	if story.CreatedAt.Before(cursor.CreatedAt()) {
		return true
	}
	if story.CreatedAt.After(cursor.CreatedAt()) {
		return false
	}
	return story.ID < cursor.ID()
}

// fakeStoriesService returns canned values, so the failure paths a real service
// cannot produce (an infrastructure error) can still be tested.
type fakeStoriesService struct {
	createResult stories.Story
	createErr    error
	getResult    stories.Story
	getErr       error
	listResult   []stories.Story
	listNext     string
	listErr      error
}

func (f *fakeStoriesService) CreateStory(_ context.Context, _ stories.CreateStoryInput) (stories.Story, error) {
	return f.createResult, f.createErr
}

func (f *fakeStoriesService) GetStory(_ context.Context, _ string) (stories.Story, error) {
	return f.getResult, f.getErr
}

func (f *fakeStoriesService) ListStories(_ context.Context, _ string, _ int) ([]stories.Story, string, error) {
	return f.listResult, f.listNext, f.listErr
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
// exercise the real middleware chain and route table.
func newRouter(t *testing.T, logger *slog.Logger, service StoriesService) http.Handler {
	t.Helper()

	authHandler, err := NewAuthHandler(&fakeAuthService{}, logger)
	if err != nil {
		t.Fatalf("NewAuthHandler() error = %v, want nil", err)
	}

	storiesHandler, err := NewStoriesHandler(service, logger)
	if err != nil {
		t.Fatalf("NewStoriesHandler() error = %v, want nil", err)
	}

	versionsHandler, err := NewVersionsHandler(&fakeVersionsService{}, logger)
	if err != nil {
		t.Fatalf("NewVersionsHandler() error = %v, want nil", err)
	}

	conversationsHandler, err := NewConversationsHandler(&fakeConversationsService{}, logger)
	if err != nil {
		t.Fatalf("NewConversationsHandler() error = %v, want nil", err)
	}

	authMiddleware, err := NewAuthMiddleware(&fakeTokenParser{subject: testUserID}, logger)
	if err != nil {
		t.Fatalf("NewAuthMiddleware() error = %v, want nil", err)
	}

	router, err := NewRouter(authHandler, storiesHandler, versionsHandler, conversationsHandler, authMiddleware, "0.1.0", logger)
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
		"language": "en",
		"title": "The first rain",
		"body": "Grandmother said the first rain remembers every name.",
		"approximate_location": "Cape Town",
		"media_urls": ["https://example.test/rain.jpg"],
		"sensitive": false
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

	body := `{"pillar":"wonder","language":"en","title":"T","body":"B","author_id":"99999999-9999-4999-8999-999999999999"}`
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
		{name: "missing title", body: `{"pillar":"wonder","language":"en","body":"B"}`},
		{name: "blank body", body: `{"pillar":"wonder","language":"en","title":"T","body":"  "}`},
		{name: "unknown pillar", body: `{"pillar":"chaos","language":"en","title":"T","body":"B"}`},
		{name: "bad language", body: `{"pillar":"wonder","language":"e","title":"T","body":"B"}`},
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

	var body listStoriesResponse
	decodeBody(t, recorder, &body)

	if body.Stories == nil {
		t.Error("stories = null, want an empty array")
	}
	if len(body.Stories) != 0 {
		t.Errorf("len(stories) = %d, want 0", len(body.Stories))
	}
	if body.NextCursor != "" {
		t.Errorf("next cursor = %q, want an empty string on the last page", body.NextCursor)
	}
	if !strings.Contains(recorder.Body.String(), `"stories":[]`) {
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

	var first listStoriesResponse
	decodeBody(t, recorder, &first)

	if len(first.Stories) != 2 {
		t.Fatalf("len(stories) = %d, want 2", len(first.Stories))
	}
	if first.NextCursor == "" {
		t.Fatal("next cursor is empty, want a cursor for the second page")
	}

	// Second page: the remaining row, and no further cursor.
	recorder = doStoryRequest(handler, http.MethodGet, "/stories?limit=2&cursor="+first.NextCursor, "", "")
	if recorder.Code != http.StatusOK {
		t.Fatalf("status = %d, want %d (body %s)", recorder.Code, http.StatusOK, recorder.Body.String())
	}

	var second listStoriesResponse
	decodeBody(t, recorder, &second)

	if len(second.Stories) != 1 {
		t.Fatalf("len(stories) = %d, want 1", len(second.Stories))
	}
	if second.NextCursor != "" {
		t.Errorf("next cursor = %q, want an empty string on the last page", second.NextCursor)
	}

	if second.Stories[0].ID == first.Stories[0].ID || second.Stories[0].ID == first.Stories[1].ID {
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

	if _, err := NewStoriesHandler(nil, logger); err == nil {
		t.Error("NewStoriesHandler(nil, logger) error = nil, want an error")
	}
	if _, err := NewStoriesHandler(&fakeStoriesService{}, nil); err == nil {
		t.Error("NewStoriesHandler(service, nil) error = nil, want an error")
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

// ensure the fake satisfies the handler's contract at compile time.
var _ StoriesService = (*fakeStoriesService)(nil)

// ensure the in-memory store satisfies the domain contract at compile time.
var _ stories.StoryStore = (*memoryStoryStore)(nil)
