package httpapi

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"log/slog"
	"net/http"
	"testing"
	"time"

	"github.com/knot/backend/internal/identity"
	"github.com/knot/backend/internal/stories"
)

// fakeProfileLookup answers the reader's profile for ranking.
type fakeProfileLookup struct {
	user *identity.User
	err  error
}

func (f *fakeProfileLookup) UserByID(_ context.Context, _ string) (*identity.User, error) {
	return f.user, f.err
}

// fakeEngagementLookup answers the reader's weighted engagement per pillar.
type fakeEngagementLookup struct {
	weights map[string]float64
	err     error
}

func (f *fakeEngagementLookup) PillarEngagement(_ context.Context, _ string) (map[string]float64, error) {
	return f.weights, f.err
}

// fakeEntityFeatureLookup returns canned read-model rows and records the batch it
// was asked for.
type fakeEntityFeatureLookup struct {
	features map[string]stories.EntityFeature
	err      error

	calls  int
	gotIDs []string
}

func (f *fakeEntityFeatureLookup) FeaturesFor(_ context.Context, entityIDs []string) (map[string]stories.EntityFeature, error) {
	f.calls++
	f.gotIDs = append([]string(nil), entityIDs...)
	return f.features, f.err
}

// feedTestStory builds one story entry for the ranking tests.
func feedTestStory(id, author, language string, createdAt time.Time) stories.Story {
	return stories.Story{
		ID:                  id,
		AuthorID:            author,
		RootVersionID:       id + "-root",
		Pillar:              stories.PillarHeritage,
		Language:            language,
		Title:               "Story " + id,
		Body:                "Body of " + id,
		ApproximateLocation: "Cape Town",
		CreatedAt:           createdAt,
		UpdatedAt:           createdAt,
	}
}

// newRankedFeedRouter assembles the real router with a stories handler that has
// ranking dependencies, mirroring cmd/knot's composition.
func newRankedFeedRouter(t *testing.T, service StoriesService, ranking FeedRanking) http.Handler {
	t.Helper()

	logger := slog.New(slog.NewTextHandler(io.Discard, nil))

	authHandler, err := NewAuthHandler(&fakeAuthService{}, logger)
	if err != nil {
		t.Fatalf("NewAuthHandler() error = %v, want nil", err)
	}

	storiesHandler, err := NewStoriesHandlerWithRanking(
		service,
		&fakeAuthorService{},
		&fakeRootedService{},
		&fakeStoryMediaLookup{},
		&fakeReactionsLookup{},
		&fakeStoryStatsLookup{},
		&fakeVersionChainLookup{},
		&fakeInteractionsLookup{},
		logger,
		nil,
		ranking,
	)
	if err != nil {
		t.Fatalf("NewStoriesHandlerWithRanking() error = %v, want nil", err)
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

	authMiddleware, err := NewAuthMiddleware(&fakeTokenParser{subject: testUserID}, logger)
	if err != nil {
		t.Fatalf("NewAuthMiddleware() error = %v, want nil", err)
	}

	router, err := NewRouter(authHandler, storiesHandler, versionsHandler, conversationsHandler, rootedHandler, newTestDiscoveryHandler(t, logger, fakeDiscoveryService{}), newTestAvatarHandler(t, logger), newTestStoryMediaHandler(t, logger), newTestNotificationsHandler(t, logger), newTestProfileHandler(t, logger), newTestReactionsRouterHandler(t, logger), newTestInquiriesHandler(t, logger), newTestModerationHandler(t, logger), newTestSearchHandler(t, logger), authMiddleware, "0.1.0", logger)
	if err != nil {
		t.Fatalf("NewRouter() error = %v, want nil", err)
	}

	return router.Handler()
}

// feedResponse is the part of the JSON body these tests inspect.
type feedResponse struct {
	Items []struct {
		ID   string `json:"id"`
		Kind string `json:"kind"`
	} `json:"items"`
	NextCursor string `json:"next_cursor"`
}

// decodeFeed parses a feed response body.
func decodeFeed(t *testing.T, body []byte) feedResponse {
	t.Helper()

	var response feedResponse
	if err := json.Unmarshal(body, &response); err != nil {
		t.Fatalf("could not decode the feed response: %v (%s)", err, body)
	}
	return response
}

// feedIDs lists the entry ids in the order the response carried them.
func feedIDs(response feedResponse) []string {
	ids := make([]string, 0, len(response.Items))
	for _, item := range response.Items {
		ids = append(ids, item.ID)
	}
	return ids
}

// rankedFeedFixture is the three-entry corpus the ranking tests share.
//
// Chronological order is english (newest), zulu, own (oldest). The reader wrote
// `own` and prefers zul, so the ranked order is own, zulu, english.
type rankedFeedFixture struct {
	service    *fakeStoriesService
	profiles   *fakeProfileLookup
	engagement *fakeEngagementLookup
	features   *fakeEntityFeatureLookup
	handler    http.Handler
}

func newRankedFeedFixture(t *testing.T) rankedFeedFixture {
	t.Helper()

	now := time.Now().UTC()
	own := feedTestStory("11111111-1111-4111-8111-111111111111", testUserID, "eng", now.Add(-10*time.Hour))
	zulu := feedTestStory("22222222-2222-4222-8222-222222222222", "bbbbbbbb-bbbb-4bbb-8bbb-bbbbbbbbbbbb", "zul", now.Add(-5*time.Hour))
	english := feedTestStory("33333333-3333-4333-8333-333333333333", "bbbbbbbb-bbbb-4bbb-8bbb-bbbbbbbbbbbb", "eng", now.Add(-1*time.Hour))

	service := &fakeStoriesService{
		listResult: []stories.FeedItem{
			{Kind: stories.FeedKindStory, Story: english},
			{Kind: stories.FeedKindStory, Story: zulu},
			{Kind: stories.FeedKindStory, Story: own},
		},
	}
	profiles := &fakeProfileLookup{user: &identity.User{
		ID:                 testUserID,
		PreferredLanguages: []string{"zul"},
	}}
	engagement := &fakeEngagementLookup{weights: map[string]float64{}}
	features := &fakeEntityFeatureLookup{features: map[string]stories.EntityFeature{
		zulu.ID: {
			EntityID:   zulu.ID,
			EntityType: "story",
			Language:   "zul",
			Pillar:     "heritage",
			PlaceLower: "cape town",
		},
	}}

	return rankedFeedFixture{
		service:    service,
		profiles:   profiles,
		engagement: engagement,
		features:   features,
		handler: newRankedFeedRouter(t, service, FeedRanking{
			Profiles:   profiles,
			Engagement: engagement,
			Features:   features,
		}),
	}
}

func TestFeedTypeRecentReturnsTheChronologicalOrder(t *testing.T) {
	fixture := newRankedFeedFixture(t)

	for _, path := range []string{"/stories", "/stories?feed_type=recent"} {
		t.Run(path, func(t *testing.T) {
			recorder := doStoryRequest(fixture.handler, http.MethodGet, path, "", testAccessToken)
			if recorder.Code != http.StatusOK {
				t.Fatalf("status = %d, want %d (%s)", recorder.Code, http.StatusOK, recorder.Body.String())
			}

			got := feedIDs(decodeFeed(t, recorder.Body.Bytes()))
			want := []string{
				"33333333-3333-4333-8333-333333333333",
				"22222222-2222-4222-8222-222222222222",
				"11111111-1111-4111-8111-111111111111",
			}
			if fmt.Sprint(got) != fmt.Sprint(want) {
				t.Errorf("feed order = %v, want %v", got, want)
			}
			// The chronological path must not touch the ranking dependencies.
			if fixture.features.calls != 0 {
				t.Errorf("feature lookups = %d, want 0 for the chronological feed", fixture.features.calls)
			}
		})
	}
}

func TestFeedTypeForYouReturnsTheRankedOrder(t *testing.T) {
	fixture := newRankedFeedFixture(t)

	recorder := doStoryRequest(fixture.handler, http.MethodGet, "/stories?feed_type=for_you", "", testAccessToken)
	if recorder.Code != http.StatusOK {
		t.Fatalf("status = %d, want %d (%s)", recorder.Code, http.StatusOK, recorder.Body.String())
	}

	got := feedIDs(decodeFeed(t, recorder.Body.Bytes()))
	// The reader's own story (+100) leads; zul (+50) precedes eng (no bonus).
	want := []string{
		"11111111-1111-4111-8111-111111111111",
		"22222222-2222-4222-8222-222222222222",
		"33333333-3333-4333-8333-333333333333",
	}
	if fmt.Sprint(got) != fmt.Sprint(want) {
		t.Errorf("ranked order = %v, want %v", got, want)
	}

	// The feature join is one batched read covering the whole window.
	if fixture.features.calls != 1 {
		t.Errorf("feature lookups = %d, want 1 (batched)", fixture.features.calls)
	}
	if len(fixture.features.gotIDs) != 3 {
		t.Errorf("feature batch = %v, want all three entity ids", fixture.features.gotIDs)
	}
}

func TestFeedTypeForYouRequiresAuthentication(t *testing.T) {
	fixture := newRankedFeedFixture(t)

	recorder := doStoryRequest(fixture.handler, http.MethodGet, "/stories?feed_type=for_you", "", "")
	if recorder.Code != http.StatusUnauthorized {
		t.Fatalf("status = %d, want %d", recorder.Code, http.StatusUnauthorized)
	}
}

func TestFeedTypeRejectsAnUnknownValue(t *testing.T) {
	fixture := newRankedFeedFixture(t)

	recorder := doStoryRequest(fixture.handler, http.MethodGet, "/stories?feed_type=trending", "", testAccessToken)
	if recorder.Code != http.StatusBadRequest {
		t.Fatalf("status = %d, want %d (%s)", recorder.Code, http.StatusBadRequest, recorder.Body.String())
	}
}

func TestRankedFeedCursorIsStableAcrossPages(t *testing.T) {
	fixture := newRankedFeedFixture(t)

	first := doStoryRequest(fixture.handler, http.MethodGet, "/stories?feed_type=for_you&limit=2", "", testAccessToken)
	if first.Code != http.StatusOK {
		t.Fatalf("first page status = %d, want %d (%s)", first.Code, http.StatusOK, first.Body.String())
	}
	firstBody := decodeFeed(t, first.Body.Bytes())
	if got := feedIDs(firstBody); len(got) != 2 {
		t.Fatalf("first page = %v, want two entries", got)
	}
	if firstBody.NextCursor == "" {
		t.Fatal("first page next_cursor is empty, want a cursor for the second page")
	}

	second := doStoryRequest(
		fixture.handler,
		http.MethodGet,
		"/stories?feed_type=for_you&limit=2&cursor="+firstBody.NextCursor,
		"",
		testAccessToken,
	)
	if second.Code != http.StatusOK {
		t.Fatalf("second page status = %d, want %d (%s)", second.Code, http.StatusOK, second.Body.String())
	}
	secondBody := decodeFeed(t, second.Body.Bytes())

	// The ranked order is own, zulu, english: page two must hold exactly the
	// remainder, with no repeat and no skip.
	if got := feedIDs(secondBody); len(got) != 1 || got[0] != "33333333-3333-4333-8333-333333333333" {
		t.Fatalf("second page = %v, want the final entry only", got)
	}
	if secondBody.NextCursor != "" {
		t.Errorf("second page next_cursor = %q, want empty", secondBody.NextCursor)
	}

	seen := append(feedIDs(firstBody), feedIDs(secondBody)...)
	unique := map[string]struct{}{}
	for _, id := range seen {
		if _, repeat := unique[id]; repeat {
			t.Errorf("entry %s appeared twice across pages", id)
		}
		unique[id] = struct{}{}
	}
	if len(unique) != 3 {
		t.Errorf("pages covered %d entries, want all 3", len(unique))
	}
}

func TestRankedFeedRejectsAMalformedCursor(t *testing.T) {
	fixture := newRankedFeedFixture(t)

	for _, cursor := range []string{"not-base64!", "bm90LWpzb24"} {
		t.Run(cursor, func(t *testing.T) {
			recorder := doStoryRequest(
				fixture.handler,
				http.MethodGet,
				"/stories?feed_type=for_you&cursor="+cursor,
				"",
				testAccessToken,
			)
			if recorder.Code != http.StatusBadRequest {
				t.Fatalf("status = %d, want %d (%s)", recorder.Code, http.StatusBadRequest, recorder.Body.String())
			}
		})
	}
}
