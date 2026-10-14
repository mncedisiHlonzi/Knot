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
	"github.com/knot/backend/internal/identity"
	"github.com/knot/backend/internal/reactions"
	"github.com/knot/backend/internal/stories"
	"github.com/knot/backend/internal/storymedia"
	"github.com/knot/backend/internal/storystats"
)

// fakeDiscoveryService is an inert DiscoveryService for the router-assembly
// tests in the other handler test files, which build the full route table but do
// not exercise discovery.
type fakeDiscoveryService struct{}

func (fakeDiscoveryService) ListClusters(context.Context, discovery.ClusterFilter) ([]discovery.PlaceCluster, error) {
	return []discovery.PlaceCluster{}, nil
}

func (fakeDiscoveryService) ListStoriesAtPlace(context.Context, string, string, string, string, int) ([]stories.Story, string, error) {
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

	gotFilter  discovery.ClusterFilter
	gotPlace   string
	gotCountry discovery.PlaceCountryFilter
	gotPillar  stories.Pillar
	gotCursor  *stories.Cursor
	gotLimit   int
	clusterGo  int
	listGo     int
}

func (m *memoryDiscoveryStore) ListClusters(_ context.Context, filter discovery.ClusterFilter) ([]discovery.PlaceCluster, error) {
	m.clusterGo++
	m.gotFilter = filter
	if m.clustersErr != nil {
		return nil, m.clustersErr
	}
	return m.clusters, nil
}

func (m *memoryDiscoveryStore) ListStoriesAtPlace(_ context.Context, placeLower string, country discovery.PlaceCountryFilter, pillar stories.Pillar, cursor *stories.Cursor, limit int) ([]stories.Story, *stories.Cursor, error) {
	m.listGo++
	m.gotPlace = placeLower
	m.gotCountry = country
	m.gotPillar = pillar
	m.gotCursor = cursor
	m.gotLimit = limit
	if m.listErr != nil {
		return nil, nil, m.listErr
	}

	// Mirror the SQL: match the normalised place name, then narrow by country when
	// the filter is enabled (Absent means "records no country") and by pillar when
	// one is asked for.
	filtered := make([]stories.Story, 0, len(m.stories))
	for _, story := range m.stories {
		if strings.ToLower(strings.TrimSpace(story.ApproximateLocation)) != placeLower {
			continue
		}
		if country.Absent && story.PlaceCountry != nil {
			continue
		}
		if !country.Absent && country.Name != "" && (story.PlaceCountry == nil || *story.PlaceCountry != country.Name) {
			continue
		}
		if pillar != "" && story.Pillar != pillar {
			continue
		}
		filtered = append(filtered, story)
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

// discoveryLookups bundles the enrichment dependencies the discovery handler
// needs, so a test can seed the one it asserts on and leave the rest inert.
type discoveryLookups struct {
	authors   AuthorLookup
	rooted    RootedLookup
	media     StoryMediaLookup
	reactions ReactionsLookup
	stats     StoryStatsLookup
}

// inertDiscoveryLookups returns lookups that add nothing, for the tests that
// only exercise routing, country filtering, or paging.
func inertDiscoveryLookups() discoveryLookups {
	return discoveryLookups{
		authors:   &fakeAuthorService{},
		rooted:    &fakeRootedService{},
		media:     &fakeStoryMediaLookup{},
		reactions: &fakeReactionsLookup{},
		stats:     &fakeStoryStatsLookup{},
	}
}

// newTestDiscoveryHandler builds a discovery handler with inert enrichment
// lookups, for the router-assembly tests in the other handler test files, which
// build the full route table but do not exercise discovery.
func newTestDiscoveryHandler(t *testing.T, logger *slog.Logger, service DiscoveryService) *DiscoveryHandler {
	t.Helper()

	handler, err := NewDiscoveryHandler(
		service,
		&fakeAuthorService{},
		&fakeRootedService{},
		&fakeStoryMediaLookup{},
		&fakeReactionsLookup{},
		&fakeStoryStatsLookup{},
		logger,
	)
	if err != nil {
		t.Fatalf("NewDiscoveryHandler() error = %v, want nil", err)
	}

	return handler
}

// newDiscoveryRouter assembles a mux with the two discovery routes and the real
// discovery service over store. It uses the same Go 1.22 method-qualified
// patterns as the real router, so the {place} wildcard and its URL decoding are
// exercised, without pulling in the other handlers' fakes.
func newDiscoveryRouter(t *testing.T, store discovery.DiscoveryStore) http.Handler {
	t.Helper()
	return newDiscoveryRouterWithLookups(t, store, inertDiscoveryLookups())
}

// newDiscoveryRouterWithLookups is newDiscoveryRouter over explicit enrichment
// lookups, so a test can prove the place page is decorated like the feed.
func newDiscoveryRouterWithLookups(t *testing.T, store discovery.DiscoveryStore, lookups discoveryLookups) http.Handler {
	t.Helper()

	logger := slog.New(slog.NewTextHandler(io.Discard, nil))

	service, err := discovery.NewService(store)
	if err != nil {
		t.Fatalf("discovery.NewService() error = %v, want nil", err)
	}

	handler, err := NewDiscoveryHandler(service, lookups.authors, lookups.rooted, lookups.media, lookups.reactions, lookups.stats, logger)
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

// discoveryStoryAt is discoveryStory with an explicit country, so a test can
// prove the ?country= filter.
func discoveryStoryAt(place string, country *string, index int) stories.Story {
	story := discoveryStory(place, index)
	story.PlaceCountry = country
	return story
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

// TestDiscoveryPlaceStoriesFiltersByCountry proves the ?country= parameter
// narrows a place page to one country, that its reserved "null" value selects the
// stories which record no country, and that an absent parameter lists the whole
// place — so a country-scoped cluster's page matches its story count
// (KNOT-020e-fix2).
func TestDiscoveryPlaceStoriesFiltersByCountry(t *testing.T) {
	china := "China"
	france := "France"

	store := &memoryDiscoveryStore{
		stories: []stories.Story{
			discoveryStoryAt("Beijing", &china, 1),
			discoveryStoryAt("Beijing", &china, 2),
			discoveryStoryAt("Beijing", nil, 3),
			discoveryStoryAt("Beijing", &france, 4),
		},
	}
	handler := newDiscoveryRouter(t, store)

	cases := []struct {
		name        string
		query       string
		want        int
		wantCountry discovery.PlaceCountryFilter
	}{
		{
			name:        "an absent country lists the whole place",
			query:       "",
			want:        4,
			wantCountry: discovery.PlaceCountryFilter{},
		},
		{
			name:        "a named country narrows the page",
			query:       "?country=China",
			want:        2,
			wantCountry: discovery.PlaceCountryFilter{Name: "China"},
		},
		{
			name:        "the null sentinel selects the country-less stories",
			query:       "?country=null",
			want:        1,
			wantCountry: discovery.PlaceCountryFilter{Absent: true},
		},
		{
			name:        "an unknown country matches nothing rather than failing",
			query:       "?country=Atlantis",
			want:        0,
			wantCountry: discovery.PlaceCountryFilter{Name: "Atlantis"},
		},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			recorder := doRequest(handler, http.MethodGet, "/discovery/places/Beijing"+tc.query, "")
			if recorder.Code != http.StatusOK {
				t.Fatalf("status = %d, want %d (body %s)", recorder.Code, http.StatusOK, recorder.Body.String())
			}

			var body placeStoriesResponse
			decodeBody(t, recorder, &body)
			if len(body.Stories) != tc.want {
				t.Fatalf("stories = %d, want %d", len(body.Stories), tc.want)
			}
			if store.gotCountry != tc.wantCountry {
				t.Errorf("store country filter = %+v, want %+v", store.gotCountry, tc.wantCountry)
			}
			if store.gotPlace != "beijing" {
				t.Errorf("store place = %q, want %q (normalised)", store.gotPlace, "beijing")
			}
		})
	}
}

// TestDiscoveryPlaceStoriesCarriesFeedEnrichment proves a place page's stories
// carry the same enrichment the feed's do — author attribution, the media
// preview, the aggregate counts, and the reactions — so a story reads identically
// in both places (KNOT-020e-fix2).
func TestDiscoveryPlaceStoriesCarriesFeedEnrichment(t *testing.T) {
	story := discoveryStory("Cape Town", 7)
	storyID := story.ID

	lookups := inertDiscoveryLookups()
	lookups.authors = &fakeAuthorService{users: map[string]*identity.User{
		testUserID: {ID: testUserID, DisplayName: "Ada Lovelace"},
	}}
	lookups.media = &fakeStoryMediaLookup{first: map[string]storymedia.StoryMedia{
		storyID: {
			ID:        "33333333-3333-4333-8333-333333333333",
			StoryID:   storyID,
			MediaType: storymedia.MediaTypeVideo,
		},
	}}
	lookups.stats = &fakeStoryStatsLookup{byStory: map[string]storystats.Counts{
		storyID: {Versions: 4, Languages: 3, Comments: 14, Media: 1},
	}}
	lookups.reactions = &fakeReactionsLookup{
		summaries: map[string]reactions.Summary{storyID: {RingsTrue: 2}},
	}

	handler := newDiscoveryRouterWithLookups(t, &memoryDiscoveryStore{stories: []stories.Story{story}}, lookups)

	recorder := doRequest(handler, http.MethodGet, "/discovery/places/Cape%20Town", "")
	if recorder.Code != http.StatusOK {
		t.Fatalf("status = %d, want %d (body %s)", recorder.Code, http.StatusOK, recorder.Body.String())
	}

	var body placeStoriesResponse
	decodeBody(t, recorder, &body)
	if len(body.Stories) != 1 {
		t.Fatalf("stories = %d, want 1", len(body.Stories))
	}

	got := body.Stories[0]
	if got.AuthorDisplayName != "Ada Lovelace" {
		t.Errorf("author_display_name = %q, want %q", got.AuthorDisplayName, "Ada Lovelace")
	}
	if got.AuthorAvatarURL != nil {
		t.Errorf("author_avatar_url = %v, want null (this author has no avatar)", got.AuthorAvatarURL)
	}
	if got.VersionCount != 4 || got.LanguageCount != 3 || got.CommentCount != 14 || got.MediaCount != 1 {
		t.Errorf(
			"counts = versions %d, languages %d, comments %d, media %d, want 4/3/14/1",
			got.VersionCount, got.LanguageCount, got.CommentCount, got.MediaCount,
		)
	}
	if got.FirstMediaType == nil || *got.FirstMediaType != "video" {
		t.Errorf("first_media_type = %v, want video", got.FirstMediaType)
	}
	if got.FirstMediaURL == nil || !strings.HasSuffix(*got.FirstMediaURL, "/content") {
		t.Errorf("first_media_url = %v, want the media content path", got.FirstMediaURL)
	}
	if got.Reactions.RingsTrue != 2 {
		t.Errorf("reactions.rings_true = %d, want 2", got.Reactions.RingsTrue)
	}
	if got.MyReactions == nil {
		t.Error("my_reactions = null, want an array")
	}
}

// TestDiscoveryPlaceStoriesFiltersByPillar proves the ?pillar= parameter narrows
// a place page to one storytelling lens, composes with ?country=, and rejects an
// unsupported pillar with a 400 before the store is consulted (KNOT-020e-fix6).
func TestDiscoveryPlaceStoriesFiltersByPillar(t *testing.T) {
	china := "China"

	wonder := discoveryStoryAt("Beijing", &china, 1)
	wonder.Pillar = stories.PillarWonder
	heritage := discoveryStoryAt("Beijing", &china, 2)
	heritage.Pillar = stories.PillarHeritage

	store := &memoryDiscoveryStore{stories: []stories.Story{wonder, heritage}}
	handler := newDiscoveryRouter(t, store)

	cases := []struct {
		name       string
		query      string
		want       int
		wantPillar stories.Pillar
	}{
		{"an absent pillar lists every pillar", "", 2, ""},
		{"wonder narrows the page", "?pillar=wonder", 1, stories.PillarWonder},
		{"heritage narrows the page", "?country=China&pillar=heritage", 1, stories.PillarHeritage},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			recorder := doRequest(handler, http.MethodGet, "/discovery/places/Beijing"+tc.query, "")
			if recorder.Code != http.StatusOK {
				t.Fatalf("status = %d, want %d (body %s)", recorder.Code, http.StatusOK, recorder.Body.String())
			}

			var body placeStoriesResponse
			decodeBody(t, recorder, &body)
			if len(body.Stories) != tc.want {
				t.Fatalf("stories = %d, want %d", len(body.Stories), tc.want)
			}
			if store.gotPillar != tc.wantPillar {
				t.Errorf("store pillar = %q, want %q", store.gotPillar, tc.wantPillar)
			}
		})
	}

	// An unsupported pillar is rejected by the domain, so the store is never
	// reached and the client gets a validation error rather than an empty list.
	before := store.listGo
	recorder := doRequest(handler, http.MethodGet, "/discovery/places/Beijing?pillar=news", "")
	if recorder.Code != http.StatusBadRequest {
		t.Fatalf("status = %d, want %d (body %s)", recorder.Code, http.StatusBadRequest, recorder.Body.String())
	}
	if code := decodedErrorCode(t, recorder); code != codeValidation {
		t.Errorf("error code = %q, want %q", code, codeValidation)
	}
	if store.listGo != before {
		t.Errorf("the store was consulted for an invalid pillar (%d calls)", store.listGo-before)
	}
}
