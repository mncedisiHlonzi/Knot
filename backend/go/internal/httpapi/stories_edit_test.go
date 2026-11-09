package httpapi

import (
	"context"
	"io"
	"log/slog"
	"net/http"
	"testing"

	"github.com/knot/backend/internal/stories"
	"github.com/knot/backend/internal/storymedia"
)

// The identities used by this file's fixtures. They are canonical UUID text so
// the real domain validators accept them.
const (
	editStoryID   = "33333333-3333-4333-8333-333333333333"
	editRootID    = "55555555-5555-4555-8555-555555555555"
	editOtherUser = "44444444-4444-4444-8444-444444444444"
)

// editStory is a live story owned by the test's authenticated user.
func editStory() stories.Story {
	return stories.Story{
		ID:            editStoryID,
		AuthorID:      testUserID,
		RootVersionID: editRootID,
		Pillar:        stories.PillarHeritage,
		Language:      "zap",
		Title:         "How my grandmother counted the days",
		Body:          "She kept a string of knots.",
		CreatedAt:     testNow,
		UpdatedAt:     testNow,
	}
}

// fakeMediaPurger records the stories whose media it was asked to empty, so the
// permanent-removal handler's ordering can be asserted (KNOT-054).
type fakeMediaPurger struct {
	err   error
	calls []string
}

func (f *fakeMediaPurger) PurgeStoryMedia(_ context.Context, storyID string) error {
	f.calls = append(f.calls, storyID)
	return f.err
}

// newStoriesHandlerWithPurger assembles the full router with the real stories
// service over store and an explicit media purger.
func newStoriesHandlerWithPurger(t *testing.T, store stories.StoryStore, purger StoryMediaPurger) http.Handler {
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
		stats:        &fakeStoryStatsLookup{},
		chains:       &fakeVersionChainLookup{},
		interactions: &fakeInteractionsLookup{},
		purger:       purger,
	})
}

// binStoryStore is a store holding one story owned by the test user, live or in
// the bin depending on markDeleted.
func binStoryStore(story stories.Story, markDeleted bool) *memoryStoryStore {
	store := &memoryStoryStore{stories: []stories.Story{story}}
	if markDeleted {
		store.deleted = map[string]bool{story.ID: true}
	}
	return store
}

func TestUpdateStoryHandlerRewritesTheAuthorsText(t *testing.T) {
	store := binStoryStore(editStory(), false)
	handler := newStoriesHandler(t, store)

	recorder := doStoryRequest(
		handler,
		http.MethodPut,
		"/stories/"+editStoryID,
		`{"title":"  A new title  ","body":"A revised body."}`,
		testAccessToken,
	)

	if recorder.Code != http.StatusOK {
		t.Fatalf("status = %d, want %d (body %s)", recorder.Code, http.StatusOK, recorder.Body.String())
	}

	var envelope storyEnvelope
	decodeBody(t, recorder, &envelope)
	// The title is trimmed, the body stored verbatim -- the same rules creation
	// applies.
	if envelope.Story.Title != "A new title" {
		t.Errorf("title = %q, want %q", envelope.Story.Title, "A new title")
	}
	if envelope.Story.Body != "A revised body." {
		t.Errorf("body = %q, want %q", envelope.Story.Body, "A revised body.")
	}
	if got := store.stories[0].Title; got != "A new title" {
		t.Errorf("stored title = %q, want %q", got, "A new title")
	}
}

func TestUpdateStoryHandlerValidation(t *testing.T) {
	store := binStoryStore(editStory(), false)
	handler := newStoriesHandler(t, store)

	recorder := doStoryRequest(
		handler,
		http.MethodPut,
		"/stories/"+editStoryID,
		`{"title":"","body":"A revised body."}`,
		testAccessToken,
	)

	if recorder.Code != http.StatusBadRequest {
		t.Fatalf("status = %d, want %d", recorder.Code, http.StatusBadRequest)
	}
	if code := decodedErrorCode(t, recorder); code != codeValidation {
		t.Errorf("error code = %q, want %q", code, codeValidation)
	}
}

func TestUpdateStoryHandlerRefusesAnotherAuthor(t *testing.T) {
	other := editStory()
	other.AuthorID = editOtherUser
	handler := newStoriesHandler(t, binStoryStore(other, false))

	recorder := doStoryRequest(
		handler,
		http.MethodPut,
		"/stories/"+editStoryID,
		`{"title":"Mine now","body":"Body."}`,
		testAccessToken,
	)

	if recorder.Code != http.StatusForbidden {
		t.Fatalf("status = %d, want %d", recorder.Code, http.StatusForbidden)
	}
}

func TestUpdateStoryHandlerTreatsADeletedStoryAsMissing(t *testing.T) {
	handler := newStoriesHandler(t, binStoryStore(editStory(), true))

	recorder := doStoryRequest(
		handler,
		http.MethodPut,
		"/stories/"+editStoryID,
		`{"title":"Back","body":"Body."}`,
		testAccessToken,
	)

	if recorder.Code != http.StatusNotFound {
		t.Fatalf("status = %d, want %d", recorder.Code, http.StatusNotFound)
	}
}

func TestPurgeStoryHandlerEmptiesMediaThenDeletesTheRow(t *testing.T) {
	store := binStoryStore(editStory(), true)
	purger := &fakeMediaPurger{}
	handler := newStoriesHandlerWithPurger(t, store, purger)

	recorder := doStoryRequest(handler, http.MethodDelete, "/stories/"+editStoryID+"/permanent", "", testAccessToken)

	if recorder.Code != http.StatusNoContent {
		t.Fatalf("status = %d, want %d (body %s)", recorder.Code, http.StatusNoContent, recorder.Body.String())
	}
	if len(purger.calls) != 1 || purger.calls[0] != editStoryID {
		t.Errorf("purger calls = %v, want [%s]", purger.calls, editStoryID)
	}
	if len(store.stories) != 0 {
		t.Errorf("stories remaining = %d, want 0", len(store.stories))
	}
}

func TestPurgeStoryHandlerRefusesALiveStory(t *testing.T) {
	store := binStoryStore(editStory(), false)
	purger := &fakeMediaPurger{}
	handler := newStoriesHandlerWithPurger(t, store, purger)

	recorder := doStoryRequest(handler, http.MethodDelete, "/stories/"+editStoryID+"/permanent", "", testAccessToken)

	if recorder.Code != http.StatusConflict {
		t.Fatalf("status = %d, want %d", recorder.Code, http.StatusConflict)
	}
	if code := decodedErrorCode(t, recorder); code != codeConflict {
		t.Errorf("error code = %q, want %q", code, codeConflict)
	}
	// Nothing was touched: a live story can never be lost to a stray request.
	if len(purger.calls) != 0 {
		t.Errorf("purger calls = %v, want none", purger.calls)
	}
	if len(store.stories) != 1 {
		t.Errorf("stories remaining = %d, want 1", len(store.stories))
	}
}

func TestPurgeStoryHandlerRefusesAnotherAuthor(t *testing.T) {
	other := editStory()
	other.AuthorID = editOtherUser
	store := binStoryStore(other, true)
	purger := &fakeMediaPurger{}
	handler := newStoriesHandlerWithPurger(t, store, purger)

	recorder := doStoryRequest(handler, http.MethodDelete, "/stories/"+editStoryID+"/permanent", "", testAccessToken)

	if recorder.Code != http.StatusForbidden {
		t.Fatalf("status = %d, want %d", recorder.Code, http.StatusForbidden)
	}
	if len(purger.calls) != 0 {
		t.Errorf("purger calls = %v, want none", purger.calls)
	}
}

func TestPurgeStoryHandlerKeepsTheStoryWhenMediaCannotBeEmptied(t *testing.T) {
	store := binStoryStore(editStory(), true)
	purger := &fakeMediaPurger{err: context.DeadlineExceeded}
	handler := newStoriesHandlerWithPurger(t, store, purger)

	recorder := doStoryRequest(handler, http.MethodDelete, "/stories/"+editStoryID+"/permanent", "", testAccessToken)

	if recorder.Code != http.StatusInternalServerError {
		t.Fatalf("status = %d, want %d", recorder.Code, http.StatusInternalServerError)
	}
	// The story is still in the bin, so the author can retry.
	if len(store.stories) != 1 {
		t.Errorf("stories remaining = %d, want 1", len(store.stories))
	}
}

func TestPurgeStoryHandlerFailsClosedWithoutAPurger(t *testing.T) {
	// The plain router has no purger: a permanent removal must not silently leave
	// the bucket contents behind.
	handler := newStoriesHandler(t, binStoryStore(editStory(), true))

	recorder := doStoryRequest(handler, http.MethodDelete, "/stories/"+editStoryID+"/permanent", "", testAccessToken)

	if recorder.Code != http.StatusInternalServerError {
		t.Fatalf("status = %d, want %d", recorder.Code, http.StatusInternalServerError)
	}
}

func TestDeletedStoriesHandlerReturnsOnlyTheCallersBin(t *testing.T) {
	deletedStory := editStory()
	strangerStory := editStory()
	strangerStory.ID = "66666666-6666-4666-8666-666666666666"
	strangerStory.AuthorID = editOtherUser

	store := &memoryStoryStore{
		stories: []stories.Story{deletedStory, strangerStory},
		deleted: map[string]bool{deletedStory.ID: true, strangerStory.ID: true},
	}

	media := &fakeStoryMediaLookup{
		first: map[string]storymedia.StoryMedia{
			deletedStory.ID: {
				ID:           "77777777-7777-4777-8777-777777777777",
				StoryID:      deletedStory.ID,
				MediaType:    storymedia.MediaTypeImage,
				MimeType:     "image/jpeg",
				StorageKey:   "story-media/x/display.avif",
				ThumbnailKey: stringPtr("story-media/x/thumb.webp"),
				Status:       storymedia.StatusReady,
				CreatedAt:    testNow,
			},
		},
	}

	logger := slog.New(slog.NewTextHandler(io.Discard, nil))
	service, err := stories.NewService(store)
	if err != nil {
		t.Fatalf("stories.NewService() error = %v, want nil", err)
	}
	handler := newRouterWithFeedLookups(t, logger, service, feedLookups{
		authors:      &fakeAuthorService{},
		rooted:       &fakeRootedService{},
		media:        media,
		reactions:    &fakeReactionsLookup{},
		stats:        &fakeStoryStatsLookup{},
		chains:       &fakeVersionChainLookup{},
		interactions: &fakeInteractionsLookup{},
		purger:       &fakeMediaPurger{},
	})

	recorder := doStoryRequest(handler, http.MethodGet, "/stories/deleted", "", testAccessToken)

	if recorder.Code != http.StatusOK {
		t.Fatalf("status = %d, want %d (body %s)", recorder.Code, http.StatusOK, recorder.Body.String())
	}

	var body deletedStoriesResponse
	decodeBody(t, recorder, &body)

	if len(body.Stories) != 1 {
		t.Fatalf("stories = %d, want 1 (only the caller's own deletion)", len(body.Stories))
	}
	if body.Stories[0].ID != deletedStory.ID {
		t.Errorf("story id = %q, want %q", body.Stories[0].ID, deletedStory.ID)
	}
	// A bin row draws the story's first still.
	if body.Stories[0].FirstMediaType == nil || *body.Stories[0].FirstMediaType != "image" {
		t.Errorf("first_media_type = %v, want image", body.Stories[0].FirstMediaType)
	}
	if body.Stories[0].FirstMediaURL == nil || *body.Stories[0].FirstMediaURL == "" {
		t.Errorf("first_media_url = %v, want a thumbnail path", body.Stories[0].FirstMediaURL)
	}
}

func TestDeletedStoriesHandlerReturnsAnEmptyArray(t *testing.T) {
	handler := newStoriesHandler(t, &memoryStoryStore{})

	recorder := doStoryRequest(handler, http.MethodGet, "/stories/deleted", "", testAccessToken)

	if recorder.Code != http.StatusOK {
		t.Fatalf("status = %d, want %d", recorder.Code, http.StatusOK)
	}

	var body deletedStoriesResponse
	decodeBody(t, recorder, &body)
	if body.Stories == nil {
		t.Fatal("stories = null, want []")
	}
	if len(body.Stories) != 0 {
		t.Errorf("stories = %d, want 0", len(body.Stories))
	}
}

// stringPtr returns a pointer to s, for the fixtures above.
func stringPtr(s string) *string {
	return &s
}
