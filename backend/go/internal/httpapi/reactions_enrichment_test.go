package httpapi

import (
	"encoding/json"
	"io"
	"log/slog"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/knot/backend/internal/conversations"
	"github.com/knot/backend/internal/reactions"
	"github.com/knot/backend/internal/stories"
	"github.com/knot/backend/internal/versions"
)

// testEnrichedEntityID is the entity id the enrichment tests react to.
const testEnrichedEntityID = "77777777-7777-4777-8777-777777777777"

// getWithOptionalUser builds a GET request whose {id} path value is set and, when
// userID is non-empty, whose context carries that authenticated user — the shape
// AuthMiddleware.Optional produces on a public route.
func getWithOptionalUser(method, path, id, userID string) *http.Request {
	request := httptest.NewRequest(method, path, nil)
	request.SetPathValue("id", id)
	if userID != "" {
		request = request.WithContext(withUserID(request.Context(), userID))
	}
	return request
}

func TestStoryDetailCarriesReactionsAndMyReactions(t *testing.T) {
	logger := slog.New(slog.NewTextHandler(io.Discard, nil))
	lookup := &fakeReactionsLookup{
		summaries: map[string]reactions.Summary{testEnrichedEntityID: {RingsTrue: 3, NeedsASource: 1}},
		mine:      map[string][]reactions.ReactionType{testEnrichedEntityID: {reactions.RingsTrue}},
	}
	storiesService := &fakeStoriesService{getResult: stories.Story{ID: testEnrichedEntityID, AuthorID: testReactionAuthorID}}

	handler, err := NewStoriesHandler(storiesService, &fakeAuthorService{}, &fakeRootedService{}, &fakeStoryMediaLookup{}, lookup, logger)
	if err != nil {
		t.Fatalf("NewStoriesHandler() error = %v, want nil", err)
	}

	request := getWithOptionalUser(http.MethodGet, "/stories/x", testEnrichedEntityID, testReactionUserID)
	recorder := httptest.NewRecorder()
	handler.Get(recorder, request)

	if recorder.Code != http.StatusOK {
		t.Fatalf("status = %d, want %d", recorder.Code, http.StatusOK)
	}

	var body storyEnvelope
	if err := json.Unmarshal(recorder.Body.Bytes(), &body); err != nil {
		t.Fatalf("decode response: %v", err)
	}
	if body.Story.Reactions.RingsTrue != 3 || body.Story.Reactions.NeedsASource != 1 {
		t.Errorf("reactions = %+v, want rings_true 3 / needs_a_source 1", body.Story.Reactions)
	}
	if len(body.Story.MyReactions) != 1 || body.Story.MyReactions[0] != "rings_true" {
		t.Errorf("my_reactions = %v, want [rings_true]", body.Story.MyReactions)
	}
	if lookup.gotUserID != testReactionUserID {
		t.Errorf("lookup user id = %q, want %q", lookup.gotUserID, testReactionUserID)
	}
}

func TestStoryDetailAnonymousHasEmptyMyReactions(t *testing.T) {
	logger := slog.New(slog.NewTextHandler(io.Discard, nil))
	lookup := &fakeReactionsLookup{summaries: map[string]reactions.Summary{testEnrichedEntityID: {RingsTrue: 2}}}
	storiesService := &fakeStoriesService{getResult: stories.Story{ID: testEnrichedEntityID, AuthorID: testReactionAuthorID}}

	handler, err := NewStoriesHandler(storiesService, &fakeAuthorService{}, &fakeRootedService{}, &fakeStoryMediaLookup{}, lookup, logger)
	if err != nil {
		t.Fatalf("NewStoriesHandler() error = %v, want nil", err)
	}

	// No user on the context: the public view.
	request := getWithOptionalUser(http.MethodGet, "/stories/x", testEnrichedEntityID, "")
	recorder := httptest.NewRecorder()
	handler.Get(recorder, request)

	if recorder.Code != http.StatusOK {
		t.Fatalf("status = %d, want %d", recorder.Code, http.StatusOK)
	}

	var body storyEnvelope
	if err := json.Unmarshal(recorder.Body.Bytes(), &body); err != nil {
		t.Fatalf("decode response: %v", err)
	}
	if body.Story.Reactions.RingsTrue != 2 {
		t.Errorf("reactions.rings_true = %d, want 2", body.Story.Reactions.RingsTrue)
	}
	if body.Story.MyReactions == nil || len(body.Story.MyReactions) != 0 {
		t.Errorf("my_reactions = %v, want an empty array for an anonymous reader", body.Story.MyReactions)
	}
	if lookup.mineCalls != 0 {
		t.Errorf("my-reactions lookup calls = %d, want 0 for an anonymous reader", lookup.mineCalls)
	}
}

func TestStoryFeedBatchesReactionsOnce(t *testing.T) {
	logger := slog.New(slog.NewTextHandler(io.Discard, nil))
	lookup := &fakeReactionsLookup{summaries: map[string]reactions.Summary{testEnrichedEntityID: {AddsSomethingNew: 1}}}
	storiesService := &fakeStoriesService{listResult: []stories.Story{{ID: testEnrichedEntityID, AuthorID: testReactionAuthorID}}}

	handler, err := NewStoriesHandler(storiesService, &fakeAuthorService{}, &fakeRootedService{}, &fakeStoryMediaLookup{}, lookup, logger)
	if err != nil {
		t.Fatalf("NewStoriesHandler() error = %v, want nil", err)
	}

	request := httptest.NewRequest(http.MethodGet, "/stories", nil)
	recorder := httptest.NewRecorder()
	handler.List(recorder, request)

	if recorder.Code != http.StatusOK {
		t.Fatalf("status = %d, want %d", recorder.Code, http.StatusOK)
	}

	var body listStoriesResponse
	if err := json.Unmarshal(recorder.Body.Bytes(), &body); err != nil {
		t.Fatalf("decode response: %v", err)
	}
	if len(body.Stories) != 1 || body.Stories[0].Reactions.AddsSomethingNew != 1 {
		t.Errorf("feed reactions = %+v, want adds_something_new 1", body.Stories)
	}
	if lookup.summaryCalls != 1 {
		t.Errorf("summary lookup calls = %d, want exactly 1 batched call", lookup.summaryCalls)
	}
}

func TestVersionDetailCarriesReactions(t *testing.T) {
	logger := slog.New(slog.NewTextHandler(io.Discard, nil))
	lookup := &fakeReactionsLookup{summaries: map[string]reactions.Summary{testEnrichedEntityID: {KnowItDifferently: 4}}}
	versionsService := &fakeVersionsService{getResult: versions.StoryVersion{ID: testEnrichedEntityID, AuthorID: testReactionAuthorID}}

	handler, err := NewVersionsHandler(versionsService, &fakeAuthorService{}, &fakeRootedService{}, lookup, logger)
	if err != nil {
		t.Fatalf("NewVersionsHandler() error = %v, want nil", err)
	}

	request := getWithOptionalUser(http.MethodGet, "/versions/x", testEnrichedEntityID, "")
	recorder := httptest.NewRecorder()
	handler.Get(recorder, request)

	if recorder.Code != http.StatusOK {
		t.Fatalf("status = %d, want %d", recorder.Code, http.StatusOK)
	}

	var body versionEnvelope
	if err := json.Unmarshal(recorder.Body.Bytes(), &body); err != nil {
		t.Fatalf("decode response: %v", err)
	}
	if body.Version.Reactions.KnowItDifferently != 4 {
		t.Errorf("reactions.know_it_differently = %d, want 4", body.Version.Reactions.KnowItDifferently)
	}
}

func TestCommentListCarriesReactions(t *testing.T) {
	logger := slog.New(slog.NewTextHandler(io.Discard, nil))
	lookup := &fakeReactionsLookup{
		summaries: map[string]reactions.Summary{testEnrichedEntityID: {RingsTrue: 1}},
		mine:      map[string][]reactions.ReactionType{testEnrichedEntityID: {reactions.RingsTrue, reactions.NeedsASource}},
	}
	conversationsService := &fakeConversationsService{listCommentsResult: []conversations.Comment{
		{ID: testEnrichedEntityID, VersionID: testReactionEntityID, AuthorID: testReactionAuthorID},
	}}

	handler, err := NewConversationsHandler(conversationsService, &fakeAuthorService{}, &fakeRootedService{}, lookup, logger)
	if err != nil {
		t.Fatalf("NewConversationsHandler() error = %v, want nil", err)
	}

	request := getWithOptionalUser(http.MethodGet, "/versions/x/comments", testReactionEntityID, testReactionUserID)
	recorder := httptest.NewRecorder()
	handler.ListComments(recorder, request)

	if recorder.Code != http.StatusOK {
		t.Fatalf("status = %d, want %d", recorder.Code, http.StatusOK)
	}

	var body commentListResponse
	if err := json.Unmarshal(recorder.Body.Bytes(), &body); err != nil {
		t.Fatalf("decode response: %v", err)
	}
	if len(body.Comments) != 1 {
		t.Fatalf("comments length = %d, want 1", len(body.Comments))
	}
	if body.Comments[0].Reactions.RingsTrue != 1 {
		t.Errorf("reactions.rings_true = %d, want 1", body.Comments[0].Reactions.RingsTrue)
	}
	if len(body.Comments[0].MyReactions) != 2 {
		t.Errorf("my_reactions = %v, want two signals", body.Comments[0].MyReactions)
	}
}

func TestBridgeDetailCarriesReactions(t *testing.T) {
	logger := slog.New(slog.NewTextHandler(io.Discard, nil))
	lookup := &fakeReactionsLookup{summaries: map[string]reactions.Summary{testEnrichedEntityID: {NeedsASource: 2}}}
	conversationsService := &fakeConversationsService{getBridgeResult: conversations.Bridge{ID: testEnrichedEntityID, AuthorID: testReactionAuthorID}}

	handler, err := NewConversationsHandler(conversationsService, &fakeAuthorService{}, &fakeRootedService{}, lookup, logger)
	if err != nil {
		t.Fatalf("NewConversationsHandler() error = %v, want nil", err)
	}

	request := getWithOptionalUser(http.MethodGet, "/bridges/x", testEnrichedEntityID, "")
	recorder := httptest.NewRecorder()
	handler.GetBridge(recorder, request)

	if recorder.Code != http.StatusOK {
		t.Fatalf("status = %d, want %d", recorder.Code, http.StatusOK)
	}

	var body bridgeEnvelope
	if err := json.Unmarshal(recorder.Body.Bytes(), &body); err != nil {
		t.Fatalf("decode response: %v", err)
	}
	if body.Bridge.Reactions.NeedsASource != 2 {
		t.Errorf("reactions.needs_a_source = %d, want 2", body.Bridge.Reactions.NeedsASource)
	}
}

func TestReactionEnrichmentSwallowsLookupFailure(t *testing.T) {
	logger := slog.New(slog.NewTextHandler(io.Discard, nil))
	lookup := &fakeReactionsLookup{err: io.ErrUnexpectedEOF}
	storiesService := &fakeStoriesService{getResult: stories.Story{ID: testEnrichedEntityID, AuthorID: testReactionAuthorID}}

	handler, err := NewStoriesHandler(storiesService, &fakeAuthorService{}, &fakeRootedService{}, &fakeStoryMediaLookup{}, lookup, logger)
	if err != nil {
		t.Fatalf("NewStoriesHandler() error = %v, want nil", err)
	}

	request := getWithOptionalUser(http.MethodGet, "/stories/x", testEnrichedEntityID, testReactionUserID)
	recorder := httptest.NewRecorder()
	handler.Get(recorder, request)

	// The story is still served, with zero counts.
	if recorder.Code != http.StatusOK {
		t.Fatalf("status = %d, want %d (enrichment failure must not fail the read)", recorder.Code, http.StatusOK)
	}

	var body storyEnvelope
	if err := json.Unmarshal(recorder.Body.Bytes(), &body); err != nil {
		t.Fatalf("decode response: %v", err)
	}
	if body.Story.Reactions.Total() != 0 {
		t.Errorf("reactions.Total() = %d, want 0 when the lookup fails", body.Story.Reactions.Total())
	}
}
