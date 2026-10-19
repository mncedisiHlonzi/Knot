package httpapi

import (
	"context"
	"encoding/json"
	"errors"
	"io"
	"log/slog"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"

	"github.com/knot/backend/internal/identity"
	"github.com/knot/backend/internal/moderation"
	"github.com/knot/backend/internal/rooted"
	"github.com/knot/backend/internal/search"
)

// errUnexpected stands in for a failure the handler does not recognise, so the
// 500 path can be exercised.
var errUnexpected = errors.New("search: boom")

// fakeSearchService records the input and context it received and returns canned
// results, so handler behaviour can be tested without a database.
type fakeSearchService struct {
	results []search.Result
	next    string
	err     error

	gotInput search.SearchInput
	gotCtx   context.Context
	calls    int
}

func (f *fakeSearchService) Search(ctx context.Context, in search.SearchInput) ([]search.Result, string, error) {
	f.calls++
	f.gotInput = in
	f.gotCtx = ctx
	return f.results, f.next, f.err
}

// fakeBlockLookup satisfies the auth middleware's BlockLookup with a fixed set, so
// a test can prove the block set reaches the search handler's service call.
type fakeBlockLookup struct {
	ids []string
	err error
}

func (f fakeBlockLookup) BlockedUserIDs(context.Context, string) ([]string, error) {
	return f.ids, f.err
}

// newSearchHandlerWith builds a search handler over the given service and
// enrichment lookups.
func newSearchHandlerWith(t *testing.T, logger *slog.Logger, service SearchService, authors AuthorLookup, rootedLookup RootedLookup) *SearchHandler {
	t.Helper()

	handler, err := NewSearchHandler(service, authors, rootedLookup, logger)
	if err != nil {
		t.Fatalf("NewSearchHandler() error = %v, want nil", err)
	}

	return handler
}

// newTestSearchHandler builds a search handler over empty fakes, for the shared
// router the other handler tests assemble.
func newTestSearchHandler(t *testing.T, logger *slog.Logger) *SearchHandler {
	t.Helper()

	return newSearchHandlerWith(t, logger, &fakeSearchService{}, &fakeAuthorService{}, &fakeRootedService{})
}

// newSearchRouter assembles the full router the way cmd/knot does, with the search
// route backed by service and the auth middleware backed by blocks, so the
// optional-auth path can be exercised end to end.
func newSearchRouter(t *testing.T, logger *slog.Logger, service SearchService, blocks BlockLookup) http.Handler {
	t.Helper()

	authHandler, err := NewAuthHandler(&fakeAuthService{}, logger)
	if err != nil {
		t.Fatalf("NewAuthHandler() error = %v, want nil", err)
	}

	storiesHandler, err := NewStoriesHandler(&fakeStoriesService{}, &fakeAuthorService{}, &fakeRootedService{}, &fakeStoryMediaLookup{}, &fakeReactionsLookup{}, &fakeStoryStatsLookup{}, &fakeVersionChainLookup{}, &fakeInteractionsLookup{}, logger)
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

	authMiddleware, err := NewAuthMiddleware(&fakeTokenParser{subject: testUserID}, logger, WithBlockLookup(blocks))
	if err != nil {
		t.Fatalf("NewAuthMiddleware() error = %v, want nil", err)
	}

	router, err := NewRouter(
		authHandler,
		storiesHandler,
		versionsHandler,
		conversationsHandler,
		rootedHandler,
		discoveryHandler,
		newTestAvatarHandler(t, logger),
		newTestStoryMediaHandler(t, logger),
		newTestNotificationsHandler(t, logger),
		newTestProfileHandler(t, logger),
		newTestReactionsRouterHandler(t, logger),
		newTestInquiriesHandler(t, logger),
		newTestModerationHandler(t, logger),
		newSearchHandlerWith(t, logger, service, &fakeAuthorService{}, &fakeRootedService{}),
		authMiddleware,
		"0.1.0",
		logger,
	)
	if err != nil {
		t.Fatalf("NewRouter() error = %v, want nil", err)
	}

	return router.Handler()
}

// decodeSearchResponse reads a search response body.
func decodeSearchResponse(t *testing.T, recorder *httptest.ResponseRecorder) searchResponse {
	t.Helper()

	var body searchResponse
	if err := json.NewDecoder(recorder.Body).Decode(&body); err != nil {
		t.Fatalf("decode search response: %v", err)
	}

	return body
}

// TestSearchHandlerReturnsEnrichedResults proves the endpoint projects a domain
// result onto the wire, attaches attribution and the Rooted badge, and passes the
// query parameters through to the service.
func TestSearchHandlerReturnsEnrichedResults(t *testing.T) {
	logger := slog.New(slog.NewTextHandler(io.Discard, nil))

	const (
		storyID  = "22222222-2222-4222-8222-222222222222"
		authorID = "33333333-3333-4333-8333-333333333333"
	)
	place := "Manguzi"
	pillar := "wonder"
	created := time.Date(2026, 10, 1, 9, 0, 0, 0, time.UTC)

	service := &fakeSearchService{
		results: []search.Result{{
			Kind:      search.KindStory,
			ID:        storyID,
			StoryID:   ptr(storyID),
			Title:     "Cattle at dusk",
			Preview:   "The herd comes home at the same hour",
			Language:  "eng",
			Pillar:    &pillar,
			Place:     &place,
			AuthorID:  authorID,
			CreatedAt: created,
			Rank:      0.6,
		}},
		next: "next-token",
	}

	authors := &fakeAuthorService{users: map[string]*identity.User{
		authorID: {ID: authorID, DisplayName: "Ada Lovelace"},
	}}
	rootedService := &fakeRootedService{batchResult: map[string]*rooted.Signal{
		authorID: {Place: "Manguzi"},
	}}

	handler := newSearchHandlerWith(t, logger, service, authors, rootedService)

	recorder := httptest.NewRecorder()
	handler.Search(recorder, httptest.NewRequest(http.MethodGet, "/search?q=manguzi", nil))

	if recorder.Code != http.StatusOK {
		t.Fatalf("status = %d, want %d", recorder.Code, http.StatusOK)
	}

	body := decodeSearchResponse(t, recorder)
	if len(body.Results) != 1 {
		t.Fatalf("results = %d, want 1", len(body.Results))
	}
	if body.NextCursor != "next-token" {
		t.Errorf("next_cursor = %q, want %q", body.NextCursor, "next-token")
	}

	got := body.Results[0]
	if got.Kind != search.KindStory {
		t.Errorf("kind = %q, want %q", got.Kind, search.KindStory)
	}
	if got.ID != storyID {
		t.Errorf("id = %q, want %q", got.ID, storyID)
	}
	if got.StoryID == nil || *got.StoryID != storyID {
		t.Errorf("story_id = %v, want %q", got.StoryID, storyID)
	}
	if got.Title != "Cattle at dusk" {
		t.Errorf("title = %q, want %q", got.Title, "Cattle at dusk")
	}
	if got.Preview != "The herd comes home at the same hour" {
		t.Errorf("preview = %q, want the body preview", got.Preview)
	}
	if got.Language != "eng" {
		t.Errorf("language = %q, want %q", got.Language, "eng")
	}
	if got.Pillar == nil || *got.Pillar != pillar {
		t.Errorf("pillar = %v, want %q", got.Pillar, pillar)
	}
	if got.Place == nil || *got.Place != place {
		t.Errorf("place = %v, want %q", got.Place, place)
	}
	if got.AuthorID != authorID {
		t.Errorf("author_id = %q, want %q", got.AuthorID, authorID)
	}
	if got.AuthorDisplayName != "Ada Lovelace" {
		t.Errorf("author_display_name = %q, want %q", got.AuthorDisplayName, "Ada Lovelace")
	}
	if got.AuthorAvatarURL != nil {
		t.Errorf("author_avatar_url = %v, want nil", *got.AuthorAvatarURL)
	}
	if got.AuthorRooted == nil || got.AuthorRooted.Place != "Manguzi" {
		t.Errorf("author_rooted = %+v, want the Manguzi summary", got.AuthorRooted)
	}
	if !got.CreatedAt.Equal(created) {
		t.Errorf("created_at = %v, want %v", got.CreatedAt, created)
	}

	if service.gotInput.Query != "manguzi" {
		t.Errorf("service query = %q, want %q", service.gotInput.Query, "manguzi")
	}
	if service.gotInput.Kind != "" {
		t.Errorf("service type = %q, want empty", service.gotInput.Kind)
	}
	if service.gotInput.Limit != search.DefaultLimit {
		t.Errorf("service limit = %d, want %d", service.gotInput.Limit, search.DefaultLimit)
	}
}

// TestSearchHandlerMapsValidationErrorTo400 proves a domain validation failure is
// reported as a client error with the machine-readable code and the field-named
// message, not as a 500.
func TestSearchHandlerMapsValidationErrorTo400(t *testing.T) {
	logger := slog.New(slog.NewTextHandler(io.Discard, nil))

	service := &fakeSearchService{err: &search.ValidationError{
		Field:   "type",
		Message: "must be one of story, version, inquiry",
	}}

	handler := newSearchHandlerWith(t, logger, service, &fakeAuthorService{}, &fakeRootedService{})

	recorder := httptest.NewRecorder()
	handler.Search(recorder, httptest.NewRequest(http.MethodGet, "/search?type=nope", nil))

	if recorder.Code != http.StatusBadRequest {
		t.Fatalf("status = %d, want %d", recorder.Code, http.StatusBadRequest)
	}
	if code := decodedErrorCode(t, recorder); code != codeValidation {
		t.Errorf("error code = %q, want %q", code, codeValidation)
	}
}

// TestSearchHandlerMapsUnexpectedErrorTo500 proves an unrecognised failure is not
// dressed up as a client error.
func TestSearchHandlerMapsUnexpectedErrorTo500(t *testing.T) {
	logger := slog.New(slog.NewTextHandler(io.Discard, nil))

	service := &fakeSearchService{err: errUnexpected}
	handler := newSearchHandlerWith(t, logger, service, &fakeAuthorService{}, &fakeRootedService{})

	recorder := httptest.NewRecorder()
	handler.Search(recorder, httptest.NewRequest(http.MethodGet, "/search?q=manguzi", nil))

	if recorder.Code != http.StatusInternalServerError {
		t.Fatalf("status = %d, want %d", recorder.Code, http.StatusInternalServerError)
	}
	if code := decodedErrorCode(t, recorder); code != codeInternal {
		t.Errorf("error code = %q, want %q", code, codeInternal)
	}
}

// TestSearchHandlerPassesFiltersAndCursor proves every filter and the cursor reach
// the service verbatim, and that the filter names are the documented ones.
func TestSearchHandlerPassesFiltersAndCursor(t *testing.T) {
	logger := slog.New(slog.NewTextHandler(io.Discard, nil))

	const authorID = "44444444-4444-4444-8444-444444444444"

	service := &fakeSearchService{}
	handler := newSearchHandlerWith(t, logger, service, &fakeAuthorService{}, &fakeRootedService{})

	recorder := httptest.NewRecorder()
	handler.Search(recorder, httptest.NewRequest(
		http.MethodGet,
		"/search?q=manguzi&type=version&pillar=heritage&language=zul&author_id="+authorID+"&place=Durban&cursor=abc&limit=5",
		nil,
	))

	if recorder.Code != http.StatusOK {
		t.Fatalf("status = %d, want %d", recorder.Code, http.StatusOK)
	}

	got := service.gotInput
	if got.Query != "manguzi" {
		t.Errorf("Query = %q, want %q", got.Query, "manguzi")
	}
	if got.Kind != search.KindVersion {
		t.Errorf("Kind = %q, want %q", got.Kind, search.KindVersion)
	}
	if got.Pillar != "heritage" {
		t.Errorf("Pillar = %q, want %q", got.Pillar, "heritage")
	}
	if got.Language != "zul" {
		t.Errorf("Language = %q, want %q", got.Language, "zul")
	}
	if got.AuthorID != authorID {
		t.Errorf("AuthorID = %q, want %q", got.AuthorID, authorID)
	}
	if got.Place != "Durban" {
		t.Errorf("Place = %q, want %q", got.Place, "Durban")
	}
	if got.Cursor != "abc" {
		t.Errorf("Cursor = %q, want %q", got.Cursor, "abc")
	}
	if got.Limit != 5 {
		t.Errorf("Limit = %d, want 5", got.Limit)
	}

	body := decodeSearchResponse(t, recorder)
	if body.NextCursor != "" {
		t.Errorf("next_cursor = %q, want empty on the last page", body.NextCursor)
	}
}

// TestParseSearchLimit proves the limit is defaulted, clamped, and rejected the way
// every other list endpoint behaves.
func TestParseSearchLimit(t *testing.T) {
	tests := []struct {
		name     string
		raw      string
		want     int
		wantErrs bool
	}{
		{name: "absent uses the default", raw: "", want: search.DefaultLimit},
		{name: "in range is kept", raw: "7", want: 7},
		{name: "above the cap is clamped", raw: "999", want: search.MaxLimit},
		{name: "the cap itself is kept", raw: "50", want: search.MaxLimit},
		{name: "zero is rejected", raw: "0", wantErrs: true},
		{name: "negative is rejected", raw: "-3", wantErrs: true},
		{name: "not a number is rejected", raw: "abc", wantErrs: true},
	}

	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			got, err := parseSearchLimit(test.raw)
			if test.wantErrs {
				if err == nil {
					t.Fatalf("parseSearchLimit(%q) error = nil, want an error", test.raw)
				}
				return
			}

			if err != nil {
				t.Fatalf("parseSearchLimit(%q) error = %v, want nil", test.raw, err)
			}
			if got != test.want {
				t.Errorf("parseSearchLimit(%q) = %d, want %d", test.raw, got, test.want)
			}
		})
	}
}

// TestSearchRouteIsOptionalAuthWithBlockFilter proves GET /search is reachable
// without a credential, that a valid credential resolves the viewer's block set
// onto the context the handler passes to the service, and that an anonymous
// request carries none.
func TestSearchRouteIsOptionalAuthWithBlockFilter(t *testing.T) {
	logger := slog.New(slog.NewTextHandler(io.Discard, nil))

	const blockedID = "55555555-5555-4555-8555-555555555555"

	service := &fakeSearchService{}
	router := newSearchRouter(t, logger, service, fakeBlockLookup{ids: []string{blockedID}})

	authenticated := httptest.NewRecorder()
	request := httptest.NewRequest(http.MethodGet, "/search?q=manguzi", nil)
	request.Header.Set("Authorization", "Bearer "+testAccessToken)
	router.ServeHTTP(authenticated, request)

	if authenticated.Code != http.StatusOK {
		t.Fatalf("authenticated status = %d, want %d", authenticated.Code, http.StatusOK)
	}
	if got := moderation.ExcludedAuthors(service.gotCtx); len(got) != 1 || got[0] != blockedID {
		t.Errorf("block set reaching the service = %v, want [%s]", got, blockedID)
	}

	anonymous := httptest.NewRecorder()
	router.ServeHTTP(anonymous, httptest.NewRequest(http.MethodGet, "/search?q=manguzi", nil))

	if anonymous.Code != http.StatusOK {
		t.Fatalf("anonymous status = %d, want %d", anonymous.Code, http.StatusOK)
	}
	if got := moderation.ExcludedAuthors(service.gotCtx); got != nil {
		t.Errorf("anonymous block set = %v, want nil", got)
	}
}

// ptr returns a pointer to a copy of value, for the pointer fields on a Result.
func ptr(value string) *string { return &value }
