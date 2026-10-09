package httpapi

import (
	"context"
	"io"
	"log/slog"
	"net/http"
	"sort"
	"strings"
	"testing"
	"time"

	"github.com/knot/backend/internal/discovery"
	"github.com/knot/backend/internal/stories"
)

// fakeDiscoveryService is an inert DiscoveryService for the router-assembly
// tests in the other handler test files, which build the full route table but do
// not exercise discovery.
type fakeDiscoveryService struct{}

func (fakeDiscoveryService) ListClusters(context.Context, discovery.ClusterFilter) ([]discovery.PlaceCluster, error) {
	return []discovery.PlaceCluster{}, nil
}

func (fakeDiscoveryService) ListStoriesAtPlace(context.Context, string, string, int) ([]stories.Story, string, error) {
	return []stories.Story{}, "", nil
}

// memoryDiscoveryStore is an in-memory discovery.DiscoveryStore.
//
// The discovery handler tests run the real discovery service over this store
// rather than a stub service, so the tests cover the actual filter validation,
// limit policy, and cursor behaviour of the domain while staying free of a
// database. Only the SQL is replaced.
type memoryDiscoveryStore struct {
	clusters    []discovery.PlaceCluster
	clustersErr error

	stories []stories.Story
	listErr error

	gotFilter discovery.ClusterFilter
	gotPlace  string
	gotCursor *stories.Cursor
	gotLimit  int
	clusterGo int
	listGo    int
}

func (m *memoryDiscoveryStore) ListClusters(_ context.Context, filter discovery.ClusterFilter) ([]discovery.PlaceCluster, error) {
	m.clusterGo++
	m.gotFilter = filter
	if m.clustersErr != nil {
		return nil, m.clustersErr
	}
	return m.clusters, nil
}

func (m *memoryDiscoveryStore) ListStoriesAtPlace(_ context.Context, placeLower string, cursor *stories.Cursor, limit int) ([]stories.Story, *stories.Cursor, error) {
	m.listGo++
	m.gotPlace = placeLower
	m.gotCursor = cursor
	m.gotLimit = limit
	if m.listErr != nil {
		return nil, nil, m.listErr
	}

	filtered := make([]stories.Story, 0, len(m.stories))
	for _, story := range m.stories {
		if strings.ToLower(strings.TrimSpace(story.ApproximateLocation)) == placeLower {
			filtered = append(filtered, story)
		}
	}

	sort.SliceStable(filtered, func(i, j int) bool {
		if !filtered[i].CreatedAt.Equal(filtered[j].CreatedAt) {
			return filtered[i].CreatedAt.After(filtered[j].CreatedAt)
		}
		return filtered[i].ID > filtered[j].ID
	})

	start := 0
	if cursor != nil {
		for start < len(filtered) && !beforeCursor(filtered[start], *cursor) {
			start++
		}
	}

	remaining := filtered[start:]
	if len(remaining) <= limit {
		return remaining, nil, nil
	}

	last := remaining[limit-1]
	next := stories.NewCursor(last.CreatedAt, last.ID)

	return remaining[:limit], &next, nil
}

// newDiscoveryRouter assembles a mux with the two discovery routes and the real
// discovery service over store. It uses the same Go 1.22 method-qualified
// patterns as the real router, so the {place} wildcard and its URL decoding are
// exercised, without pulling in the other handlers' fakes.
func newDiscoveryRouter(t *testing.T, store discovery.DiscoveryStore) http.Handler {
	t.Helper()

	logger := slog.New(slog.NewTextHandler(io.Discard, nil))

	service, err := discovery.NewService(store)
	if err != nil {
		t.Fatalf("discovery.NewService() error = %v, want nil", err)
	}

	handler, err := NewDiscoveryHandler(service, &fakeRootedService{}, logger)
	if err != nil {
		t.Fatalf("NewDiscoveryHandler() error = %v, want nil", err)
	}

	mux := http.NewServeMux()
	mux.HandleFunc("GET /discovery/clusters", handler.Clusters)
	mux.HandleFunc("GET /discovery/places/{place}", handler.PlaceStories)

	return mux
}

// discoveryStory is a story at place, stamped so tests can order them predictably.
func discoveryStory(place string, index int) stories.Story {
	return stories.Story{
		ID:                  discoverableID(index),
		AuthorID:            testUserID,
		RootVersionID:       discoverableID(index + 100),
		Pillar:              stories.PillarWonder,
		Language:            "eng",
		Title:               "A story",
		Body:                "Body",
		ApproximateLocation: place,
		MediaURLs:           []string{},
		CreatedAt:           testNow.Add(time.Duration(index) * time.Second),
		UpdatedAt:           testNow.Add(time.Duration(index) * time.Second),
	}
}

// discoverableID renders a canonical UUID-shaped id from a sequence number.
func discoverableID(n int) string {
	const digits = "0123456789abcdef"
	hex := make([]byte, 12)
	for i := 11; i >= 0; i-- {
		hex[i] = digits[n&0xf]
		n >>= 4
	}
	return "00000000-0000-4000-8000-" + string(hex)
}

func TestDiscoveryClustersReturnsClusters(t *testing.T) {
	store := &memoryDiscoveryStore{
		clusters: []discovery.PlaceCluster{
			{
				Place:      "Cape Town",
				StoryCount: 3,
				PillarCounts: map[stories.Pillar]int{
					stories.PillarWonder:   2,
					stories.PillarHeritage: 1,
				},
				Languages:     []string{"afr", "eng"},
				LatestStoryAt: testNow,
			},
		},
	}
	handler := newDiscoveryRouter(t, store)

	recorder := doRequest(handler, http.MethodGet, "/discovery/clusters", "")

	if recorder.Code != http.StatusOK {
		t.Fatalf("status = %d, want %d (body %s)", recorder.Code, http.StatusOK, recorder.Body.String())
	}

	var body clustersResponse
	decodeBody(t, recorder, &body)
	if len(body.Clusters) != 1 {
		t.Fatalf("clusters = %d, want 1", len(body.Clusters))
	}
	cluster := body.Clusters[0]
	if cluster.Place != "Cape Town" {
		t.Errorf("place = %q, want %q", cluster.Place, "Cape Town")
	}
	if cluster.StoryCount != 3 {
		t.Errorf("story_count = %d, want 3", cluster.StoryCount)
	}
	if cluster.PillarCounts["wonder"] != 2 || cluster.PillarCounts["heritage"] != 1 {
		t.Errorf("pillar_counts = %v, want wonder:2 heritage:1", cluster.PillarCounts)
	}
	if len(cluster.Languages) != 2 {
		t.Errorf("languages = %v, want two entries", cluster.Languages)
	}
	if store.gotFilter.Limit != discovery.DefaultClusterLimit {
		t.Errorf("store limit = %d, want %d", store.gotFilter.Limit, discovery.DefaultClusterLimit)
	}
}

func TestDiscoveryClustersAppliesFiltersAndClampsLimit(t *testing.T) {
	store := &memoryDiscoveryStore{}
	handler := newDiscoveryRouter(t, store)

	recorder := doRequest(handler, http.MethodGet, "/discovery/clusters?pillar=heritage&language=fra&limit=999", "")

	if recorder.Code != http.StatusOK {
		t.Fatalf("status = %d, want %d (body %s)", recorder.Code, http.StatusOK, recorder.Body.String())
	}
	if store.gotFilter.Pillar != stories.PillarHeritage {
		t.Errorf("pillar = %q, want %q", store.gotFilter.Pillar, stories.PillarHeritage)
	}
	if store.gotFilter.Language != "fra" {
		t.Errorf("language = %q, want %q", store.gotFilter.Language, "fra")
	}
	if store.gotFilter.Limit != discovery.MaxClusterLimit {
		t.Errorf("limit = %d, want %d (clamped)", store.gotFilter.Limit, discovery.MaxClusterLimit)
	}
}

func TestDiscoveryClustersRejectsBadFilters(t *testing.T) {
	cases := []struct {
		name string
		path string
	}{
		{"unknown pillar", "/discovery/clusters?pillar=news"},
		{"unknown language code", "/discovery/clusters?language=fr1"},
		{"language in upper case", "/discovery/clusters?language=FR"},
		{"non-integer limit", "/discovery/clusters?limit=many"},
		{"zero limit", "/discovery/clusters?limit=0"},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			handler := newDiscoveryRouter(t, &memoryDiscoveryStore{})
			recorder := doRequest(handler, http.MethodGet, tc.path, "")

			if recorder.Code != http.StatusBadRequest {
				t.Fatalf("status = %d, want %d (body %s)", recorder.Code, http.StatusBadRequest, recorder.Body.String())
			}
			if code := decodedErrorCode(t, recorder); code != codeValidation {
				t.Errorf("error code = %q, want %q", code, codeValidation)
			}
		})
	}
}

func TestDiscoveryClustersStoreErrorIs500(t *testing.T) {
	store := &memoryDiscoveryStore{clustersErr: context.DeadlineExceeded}
	handler := newDiscoveryRouter(t, store)

	recorder := doRequest(handler, http.MethodGet, "/discovery/clusters", "")

	if recorder.Code != http.StatusInternalServerError {
		t.Fatalf("status = %d, want %d", recorder.Code, http.StatusInternalServerError)
	}
	if code := decodedErrorCode(t, recorder); code != codeInternal {
		t.Errorf("error code = %q, want %q", code, codeInternal)
	}
}

func TestDiscoveryPlaceStoriesReturnsDecodedPlace(t *testing.T) {
	store := &memoryDiscoveryStore{
		stories: []stories.Story{
			discoveryStory("Cape Town", 1),
			discoveryStory("Cape Town", 2),
		},
	}
	handler := newDiscoveryRouter(t, store)

	recorder := doRequest(handler, http.MethodGet, "/discovery/places/Cape%20Town", "")

	if recorder.Code != http.StatusOK {
		t.Fatalf("status = %d, want %d (body %s)", recorder.Code, http.StatusOK, recorder.Body.String())
	}
	var body placeStoriesResponse
	decodeBody(t, recorder, &body)
	if len(body.Stories) != 2 {
		t.Fatalf("stories = %d, want 2", len(body.Stories))
	}
	if body.NextCursor != "" {
		t.Errorf("next_cursor = %q, want empty", body.NextCursor)
	}
	if store.gotPlace != "cape town" {
		t.Errorf("store place = %q, want %q (normalised)", store.gotPlace, "cape town")
	}
}

func TestDiscoveryPlaceStoriesPaginates(t *testing.T) {
	store := &memoryDiscoveryStore{
		stories: []stories.Story{
			discoveryStory("Cape Town", 1),
			discoveryStory("Cape Town", 2),
			discoveryStory("Cape Town", 3),
		},
	}
	handler := newDiscoveryRouter(t, store)

	first := doRequest(handler, http.MethodGet, "/discovery/places/Cape%20Town?limit=2", "")
	if first.Code != http.StatusOK {
		t.Fatalf("first page status = %d, want %d", first.Code, http.StatusOK)
	}
	var firstBody placeStoriesResponse
	decodeBody(t, first, &firstBody)
	if len(firstBody.Stories) != 2 {
		t.Fatalf("first page stories = %d, want 2", len(firstBody.Stories))
	}
	if firstBody.NextCursor == "" {
		t.Fatal("first page next_cursor is empty, want a token")
	}

	second := doRequest(handler, http.MethodGet, "/discovery/places/Cape%20Town?limit=2&cursor="+firstBody.NextCursor, "")
	if second.Code != http.StatusOK {
		t.Fatalf("second page status = %d, want %d", second.Code, http.StatusOK)
	}
	var secondBody placeStoriesResponse
	decodeBody(t, second, &secondBody)
	if len(secondBody.Stories) != 1 {
		t.Fatalf("second page stories = %d, want 1", len(secondBody.Stories))
	}
	if secondBody.NextCursor != "" {
		t.Errorf("second page next_cursor = %q, want empty", secondBody.NextCursor)
	}
}

func TestDiscoveryPlaceStoriesUnknownPlaceIsEmpty(t *testing.T) {
	handler := newDiscoveryRouter(t, &memoryDiscoveryStore{})

	recorder := doRequest(handler, http.MethodGet, "/discovery/places/Nowhere", "")

	if recorder.Code != http.StatusOK {
		t.Fatalf("status = %d, want %d", recorder.Code, http.StatusOK)
	}
	var body placeStoriesResponse
	decodeBody(t, recorder, &body)
	if body.Stories == nil {
		t.Error("stories = null, want an empty array")
	}
	if len(body.Stories) != 0 {
		t.Errorf("stories = %d, want 0", len(body.Stories))
	}
	if body.NextCursor != "" {
		t.Errorf("next_cursor = %q, want empty", body.NextCursor)
	}
}

func TestDiscoveryPlaceStoriesClampsLimit(t *testing.T) {
	store := &memoryDiscoveryStore{}
	handler := newDiscoveryRouter(t, store)

	doRequest(handler, http.MethodGet, "/discovery/places/Cape%20Town?limit=999", "")

	if store.gotLimit != discovery.MaxPlaceLimit {
		t.Errorf("store limit = %d, want %d (clamped)", store.gotLimit, discovery.MaxPlaceLimit)
	}
}

func TestDiscoveryPlaceStoriesRejectsMalformedCursor(t *testing.T) {
	handler := newDiscoveryRouter(t, &memoryDiscoveryStore{})

	recorder := doRequest(handler, http.MethodGet, "/discovery/places/Cape%20Town?cursor=not-a-cursor", "")

	if recorder.Code != http.StatusBadRequest {
		t.Fatalf("status = %d, want %d (body %s)", recorder.Code, http.StatusBadRequest, recorder.Body.String())
	}
	if code := decodedErrorCode(t, recorder); code != codeValidation {
		t.Errorf("error code = %q, want %q", code, codeValidation)
	}
}

func TestDiscoveryPlaceStoriesStoreErrorIs500(t *testing.T) {
	store := &memoryDiscoveryStore{listErr: context.DeadlineExceeded}
	handler := newDiscoveryRouter(t, store)

	recorder := doRequest(handler, http.MethodGet, "/discovery/places/Cape%20Town", "")

	if recorder.Code != http.StatusInternalServerError {
		t.Fatalf("status = %d, want %d", recorder.Code, http.StatusInternalServerError)
	}
	if code := decodedErrorCode(t, recorder); code != codeInternal {
		t.Errorf("error code = %q, want %q", code, codeInternal)
	}
}
