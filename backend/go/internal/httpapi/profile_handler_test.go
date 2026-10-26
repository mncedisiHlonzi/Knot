package httpapi

import (
	"context"
	"errors"
	"log/slog"
	"net/http"
	"strings"
	"testing"

	"github.com/knot/backend/internal/identity"
	"github.com/knot/backend/internal/profile"
	"github.com/knot/backend/internal/storymedia"
)

// fakeProfileService returns canned values, so the failure paths a real service
// cannot produce (an infrastructure error) can still be tested. It records the
// arguments it received so a test can prove the handler passed the cursor and
// clamped the limit.
type fakeProfileService struct {
	result profile.Profile
	next   string
	err    error

	gotUserID string
	gotCursor string
	gotLimit  int
	calls     int
}

func (f *fakeProfileService) GetProfile(_ context.Context, userID string, rawCursor string, limit int) (profile.Profile, string, error) {
	f.calls++
	f.gotUserID = userID
	f.gotCursor = rawCursor
	f.gotLimit = limit
	return f.result, f.next, f.err
}

// newTestProfileHandler returns a profile handler backed by an inert fake service,
// for the routers that only need the route to exist.
func newTestProfileHandler(t *testing.T, logger *slog.Logger) *ProfileHandler {
	t.Helper()

	handler, err := NewProfileHandler(&fakeProfileService{}, &fakeRootedService{}, &fakeStoryMediaLookup{}, logger)
	if err != nil {
		t.Fatalf("NewProfileHandler() error = %v, want nil", err)
	}
	return handler
}

// newProfileRouter assembles the full router the way cmd/knot does, with the
// profile handler backed by service, so the tests exercise the real route table
// and middleware chain.
func newProfileRouter(t *testing.T, service ProfileService, rooted RootedLookup) http.Handler {
	t.Helper()
	return newProfileRouterWithMedia(t, service, rooted, &fakeStoryMediaLookup{})
}

// newProfileRouterWithMedia is newProfileRouter with the story-media enrichment
// injected, so a test can prove the wall's cards carry a media preview.
func newProfileRouterWithMedia(t *testing.T, service ProfileService, rooted RootedLookup, media StoryMediaLookup) http.Handler {
	t.Helper()

	logger := discardLogger()

	authHandler, err := NewAuthHandler(&fakeAuthService{}, newTestLimiter(t), logger)
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

	profileHandler, err := NewProfileHandler(service, rooted, media, logger)
	if err != nil {
		t.Fatalf("NewProfileHandler() error = %v, want nil", err)
	}

	authMiddleware, err := NewAuthMiddleware(&fakeTokenParser{subject: testUserID, sessionID: testSessionID}, logger)
	if err != nil {
		t.Fatalf("NewAuthMiddleware() error = %v, want nil", err)
	}

	router, err := NewRouter(authHandler, storiesHandler, versionsHandler, conversationsHandler, rootedHandler, discoveryHandler, newTestAvatarHandler(t, logger), newTestStoryMediaHandler(t, logger), newTestNotificationsHandler(t, logger), profileHandler, newTestReactionsRouterHandler(t, logger), newTestInquiriesHandler(t, logger), newTestModerationHandler(t, logger), newTestSearchHandler(t, logger), newTestAccountHandler(t, logger), authMiddleware, "0.1.0", logger)
	if err != nil {
		t.Fatalf("NewRouter() error = %v, want nil", err)
	}

	return router.Handler()
}

// profileUserFixture is a stored account for the profile tests.
func profileUserFixture(id, displayName, avatarKey string) *identity.User {
	return &identity.User{
		ID:          id,
		DisplayName: displayName,
		AvatarURL:   avatarKey,
		CreatedAt:   testNow,
	}
}

// storyActivity is one story activity on a wall, with the payload the store
// produces for that kind.
func storyActivity() profile.Activity {
	return profile.Activity{
		Kind:      profile.KindStory,
		ID:        "d6b53a2c-2e2f-4a4d-9b0f-3f6f4e0f1a2b",
		CreatedAt: testNow,
		Payload: profile.Payload{
			Title:    "The first rain",
			Pillar:   "wonder",
			Language: "eng",
		},
	}
}

func TestGetProfileHappyPath(t *testing.T) {
	service := &fakeProfileService{
		result: profile.Profile{
			User:       profileUserFixture(testUserID, "Ada Lovelace", "avatars/"+testUserID+"/ada.png"),
			Activities: []profile.Activity{storyActivity()},
		},
	}
	rooted := &fakeRootedService{batchResult: rootedSignals(map[string]string{testUserID: "Cape Town"})}
	handler := newProfileRouter(t, service, rooted)

	recorder := doStoryRequest(handler, http.MethodGet, "/users/"+testUserID+"/profile", "", "")

	if recorder.Code != http.StatusOK {
		t.Fatalf("status = %d, want %d (body %s)", recorder.Code, http.StatusOK, recorder.Body.String())
	}

	var body profileResponse
	decodeBody(t, recorder, &body)

	if body.User.ID != testUserID || body.User.DisplayName != "Ada Lovelace" {
		t.Errorf("user = %+v, want the owner's id and name", body.User)
	}
	wantAvatar := "/users/" + testUserID + "/avatar?v=ada.png"
	if body.User.AvatarURL == nil || *body.User.AvatarURL != wantAvatar {
		t.Errorf("avatar_url = %v, want %q", body.User.AvatarURL, wantAvatar)
	}
	if body.User.Rooted == nil || body.User.Rooted.Place != "Cape Town" {
		t.Errorf("rooted = %+v, want the owner's inline summary", body.User.Rooted)
	}
	if body.User.JoinedAt.IsZero() {
		t.Error("joined_at is zero, want the account's creation time")
	}
	if len(body.Activities) != 1 || body.Activities[0].Kind != "story" {
		t.Fatalf("activities = %+v, want one story", body.Activities)
	}
	if body.NextCursor != "" {
		t.Errorf("next_cursor = %q, want empty on the last page", body.NextCursor)
	}
	if service.gotUserID != testUserID {
		t.Errorf("service user id = %q, want %q", service.gotUserID, testUserID)
	}
	if service.gotLimit != profile.DefaultListLimit {
		t.Errorf("service limit = %d, want the default %d", service.gotLimit, profile.DefaultListLimit)
	}
}

func TestGetProfileStoryPayloadIsMinimal(t *testing.T) {
	service := &fakeProfileService{
		result: profile.Profile{
			User:       profileUserFixture(testUserID, "Ada Lovelace", ""),
			Activities: []profile.Activity{storyActivity()},
		},
	}
	handler := newProfileRouter(t, service, &fakeRootedService{})

	recorder := doStoryRequest(handler, http.MethodGet, "/users/"+testUserID+"/profile", "", "")
	if recorder.Code != http.StatusOK {
		t.Fatalf("status = %d, want %d", recorder.Code, http.StatusOK)
	}

	body := recorder.Body.String()
	if !strings.Contains(body, `"payload":{"title":"The first rain","pillar":"wonder","language":"eng"}`) {
		t.Errorf("body = %s, want a story payload of exactly title, pillar, language", body)
	}
	if strings.Contains(body, `"story_id"`) {
		t.Errorf("body = %s, want no story_id on a story payload", body)
	}
	if !strings.Contains(body, `"avatar_url":null`) {
		t.Errorf("body = %s, want a null avatar_url for an author with no avatar", body)
	}
}

func TestGetProfileNotFound(t *testing.T) {
	service := &fakeProfileService{err: profile.ErrNotFound}
	handler := newProfileRouter(t, service, &fakeRootedService{})

	recorder := doStoryRequest(handler, http.MethodGet, "/users/"+testUserID+"/profile", "", "")

	if recorder.Code != http.StatusNotFound {
		t.Fatalf("status = %d, want %d", recorder.Code, http.StatusNotFound)
	}
	if code := decodedErrorCode(t, recorder); code != codeNotFound {
		t.Errorf("error code = %q, want %q", code, codeNotFound)
	}
}

func TestGetProfileForwardsCursorAndReturnsNext(t *testing.T) {
	nextCursor := "MjAyNi0xMC0wOVQxMjowMDowMFo=|d6b53a2c-2e2f-4a4d-9b0f-3f6f4e0f1a2b"
	service := &fakeProfileService{
		result: profile.Profile{
			User:       profileUserFixture(testUserID, "Ada Lovelace", ""),
			Activities: []profile.Activity{},
		},
		next: nextCursor,
	}
	handler := newProfileRouter(t, service, &fakeRootedService{})

	recorder := doStoryRequest(handler, http.MethodGet, "/users/"+testUserID+"/profile?cursor="+nextCursor+"&limit=2", "", "")

	if recorder.Code != http.StatusOK {
		t.Fatalf("status = %d, want %d (body %s)", recorder.Code, http.StatusOK, recorder.Body.String())
	}

	var body profileResponse
	decodeBody(t, recorder, &body)

	if service.gotCursor != nextCursor {
		t.Errorf("service cursor = %q, want %q", service.gotCursor, nextCursor)
	}
	if service.gotLimit != 2 {
		t.Errorf("service limit = %d, want 2", service.gotLimit)
	}
	if body.NextCursor != nextCursor {
		t.Errorf("next_cursor = %q, want %q", body.NextCursor, nextCursor)
	}
	if body.Activities == nil {
		t.Error("activities = nil, want an empty array")
	}
}

func TestGetProfileClampsLimit(t *testing.T) {
	service := &fakeProfileService{
		result: profile.Profile{User: profileUserFixture(testUserID, "Ada Lovelace", ""), Activities: []profile.Activity{}},
	}
	handler := newProfileRouter(t, service, &fakeRootedService{})

	recorder := doStoryRequest(handler, http.MethodGet, "/users/"+testUserID+"/profile?limit=9999", "", "")

	if recorder.Code != http.StatusOK {
		t.Fatalf("status = %d, want %d", recorder.Code, http.StatusOK)
	}
	if service.gotLimit != profile.MaxListLimit {
		t.Errorf("service limit = %d, want the clamped maximum %d", service.gotLimit, profile.MaxListLimit)
	}
}

func TestGetProfileInvalidLimitIsValidationError(t *testing.T) {
	handler := newProfileRouter(t, &fakeProfileService{}, &fakeRootedService{})

	recorder := doStoryRequest(handler, http.MethodGet, "/users/"+testUserID+"/profile?limit=zero", "", "")

	if recorder.Code != http.StatusBadRequest {
		t.Fatalf("status = %d, want %d", recorder.Code, http.StatusBadRequest)
	}
	if code := decodedErrorCode(t, recorder); code != codeValidation {
		t.Errorf("error code = %q, want %q", code, codeValidation)
	}
}

func TestGetProfileUnexpectedFailureIsInternalError(t *testing.T) {
	service := &fakeProfileService{err: errors.New("connection reset")}
	handler := newProfileRouter(t, service, &fakeRootedService{})

	recorder := doStoryRequest(handler, http.MethodGet, "/users/"+testUserID+"/profile", "", "")

	if recorder.Code != http.StatusInternalServerError {
		t.Fatalf("status = %d, want %d", recorder.Code, http.StatusInternalServerError)
	}
	if code := decodedErrorCode(t, recorder); code != codeInternal {
		t.Errorf("error code = %q, want %q", code, codeInternal)
	}
}

func TestGetProfileWithoutOwnerIsInternalError(t *testing.T) {
	// A service that returns no user and no error is a wiring mistake; the handler
	// must fail closed rather than panic.
	service := &fakeProfileService{result: profile.Profile{User: nil, Activities: []profile.Activity{}}}
	handler := newProfileRouter(t, service, &fakeRootedService{})

	recorder := doStoryRequest(handler, http.MethodGet, "/users/"+testUserID+"/profile", "", "")

	if recorder.Code != http.StatusInternalServerError {
		t.Fatalf("status = %d, want %d", recorder.Code, http.StatusInternalServerError)
	}
}

func TestNewProfileHandlerRejectsMissingDependencies(t *testing.T) {
	logger := discardLogger()

	if _, err := NewProfileHandler(nil, &fakeRootedService{}, &fakeStoryMediaLookup{}, logger); err == nil {
		t.Error("NewProfileHandler(nil, rooted, media, logger) error = nil, want an error")
	}
	if _, err := NewProfileHandler(&fakeProfileService{}, nil, &fakeStoryMediaLookup{}, logger); err == nil {
		t.Error("NewProfileHandler(service, nil, media, logger) error = nil, want an error")
	}
	if _, err := NewProfileHandler(&fakeProfileService{}, &fakeRootedService{}, nil, logger); err == nil {
		t.Error("NewProfileHandler(service, rooted, nil, logger) error = nil, want an error")
	}
	if _, err := NewProfileHandler(&fakeProfileService{}, &fakeRootedService{}, &fakeStoryMediaLookup{}, nil); err == nil {
		t.Error("NewProfileHandler(service, rooted, media, nil) error = nil, want an error")
	}
}

// TestGetProfileAttachesStoryMediaPreview proves the wall's story and version
// cards carry the story's first media item, attached in one batched read, and that
// a story with no media carries neither field (KNOT-020g-fix3). It also proves the
// dimensions travel with the preview, so a card frames the media at its natural
// ratio (KNOT-020g-fix5).
func TestGetProfileAttachesStoryMediaPreview(t *testing.T) {
	const (
		storyID     = "d6b53a2c-2e2f-4a4d-9b0f-3f6f4e0f1a2b"
		versionID   = "e7c64b3d-3f30-4b5e-8c1a-4a7f5f1a2b3c"
		bareStoryID = "11111111-1111-4111-8111-111111111111"
	)

	service := &fakeProfileService{
		result: profile.Profile{
			User: profileUserFixture(testUserID, "Ada Lovelace", ""),
			Activities: []profile.Activity{
				{
					Kind:      profile.KindStory,
					ID:        storyID,
					CreatedAt: testNow,
					Payload:   profile.Payload{Title: "The first rain", Pillar: "wonder", Language: "eng"},
				},
				{
					Kind:      profile.KindVersion,
					ID:        versionID,
					CreatedAt: testNow,
					Payload:   profile.Payload{StoryID: storyID, StoryTitle: "The first rain", Language: "fra"},
				},
				{
					Kind:      profile.KindStory,
					ID:        bareStoryID,
					CreatedAt: testNow,
					Payload:   profile.Payload{Title: "No media at all", Pillar: "heritage", Language: "eng"},
				},
			},
		},
	}
	media := &fakeStoryMediaLookup{first: map[string]storymedia.StoryMedia{
		storyID: {
			ID:        "33333333-3333-4333-8333-333333333333",
			StoryID:   storyID,
			MediaType: storymedia.MediaTypeImage,
			Width:     intPtr(900),
			Height:    intPtr(1600),
		},
	}}

	handler := newProfileRouterWithMedia(t, service, &fakeRootedService{}, media)
	recorder := doStoryRequest(handler, http.MethodGet, "/users/"+testUserID+"/profile", "", "")

	if recorder.Code != http.StatusOK {
		t.Fatalf("status = %d, want %d (body %s)", recorder.Code, http.StatusOK, recorder.Body.String())
	}

	// The wire shape names the new fields, so a client can rely on them.
	for _, want := range []string{
		`"media_preview_url":"/stories/` + storyID + `/media/`,
		`"first_media_type":"image"`,
		`"first_media_width":900`,
		`"first_media_height":1600`,
	} {
		if !strings.Contains(recorder.Body.String(), want) {
			t.Errorf("body = %s, want it to contain %s", recorder.Body.String(), want)
		}
	}

	var body profileResponse
	decodeBody(t, recorder, &body)
	if len(body.Activities) != 3 {
		t.Fatalf("activities = %d, want 3", len(body.Activities))
	}

	story := body.Activities[0].Payload
	if story.FirstMediaType != "image" || !strings.HasSuffix(story.MediaPreviewURL, "/content") {
		t.Errorf("story media = %q/%q, want the first item's image type and content path", story.FirstMediaType, story.MediaPreviewURL)
	}
	if story.FirstMediaWidth == nil || *story.FirstMediaWidth != 900 || story.FirstMediaHeight == nil || *story.FirstMediaHeight != 1600 {
		t.Errorf("story media dimensions = %v/%v, want 900/1600", story.FirstMediaWidth, story.FirstMediaHeight)
	}

	// A version has no media of its own, so it previews its story's — including its
	// dimensions, so the two cards agree on the frame.
	version := body.Activities[1].Payload
	if version.FirstMediaType != "image" || version.MediaPreviewURL != story.MediaPreviewURL {
		t.Errorf("version media = %q/%q, want its story's preview %q", version.FirstMediaType, version.MediaPreviewURL, story.MediaPreviewURL)
	}
	if version.FirstMediaWidth == nil || *version.FirstMediaWidth != 900 {
		t.Errorf("version media width = %v, want 900", version.FirstMediaWidth)
	}

	bare := body.Activities[2].Payload
	if bare.FirstMediaType != "" || bare.MediaPreviewURL != "" {
		t.Errorf("story without media = %q/%q, want both empty", bare.FirstMediaType, bare.MediaPreviewURL)
	}
	if bare.FirstMediaWidth != nil || bare.FirstMediaHeight != nil {
		t.Errorf("story without media dimensions = %v/%v, want nil/nil", bare.FirstMediaWidth, bare.FirstMediaHeight)
	}
}

// ensure the fake satisfies the handler contract at compile time.
var _ ProfileService = (*fakeProfileService)(nil)
