package httpapi

import (
	"context"
	"encoding/json"
	"io"
	"log/slog"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/knot/backend/internal/conversations"
	"github.com/knot/backend/internal/reactions"
	"github.com/knot/backend/internal/stories"
	"github.com/knot/backend/internal/versions"
)

// Fixture ids for the reactions tests. They are canonical UUIDs so the domain
// validation accepts them.
const (
	testReactionUserID    = "22222222-2222-4222-8222-222222222222"
	testReactionEntityID  = "33333333-3333-4333-8333-333333333333"
	testReactionOtherID   = "44444444-4444-4444-8444-444444444444"
	testReactionAuthorID  = "55555555-5555-4555-8555-555555555555"
	testReactionCommentID = "66666666-6666-4666-8666-666666666666"
)

// fakeReactionsService is a stub ReactionsService, so handler behaviour can be
// tested without a database.
type fakeReactionsService struct {
	summary   reactions.Summary
	active    bool
	toggleErr error

	listResult []reactions.ReactionWithActor
	listErr    error

	gotToggle    reactions.ToggleInput
	toggleCalls  int
	gotListType  reactions.EntityType
	gotListID    string
	listCalls    int
	listReturned []reactions.ReactionWithActor
}

func (f *fakeReactionsService) Toggle(_ context.Context, in reactions.ToggleInput) (reactions.Summary, bool, error) {
	f.toggleCalls++
	f.gotToggle = in
	return f.summary, f.active, f.toggleErr
}

func (f *fakeReactionsService) ListForEntity(_ context.Context, entityType reactions.EntityType, entityID string) ([]reactions.ReactionWithActor, error) {
	f.listCalls++
	f.gotListType = entityType
	f.gotListID = entityID
	f.listReturned = f.listResult
	return f.listResult, f.listErr
}

// fakeReactionsNotifier records the notification it was asked to fire.
type fakeReactionsNotifier struct {
	calls        int
	gotRecipient string
	gotActor     string
	gotEntity    string
	gotReaction  string
	gotEntityID  string
}

func (f *fakeReactionsNotifier) NotifyReactionCreated(_ context.Context, recipientID, actorID, entityType, reactionType, entityID string) error {
	f.calls++
	f.gotRecipient = recipientID
	f.gotActor = actorID
	f.gotEntity = entityType
	f.gotReaction = reactionType
	f.gotEntityID = entityID
	return nil
}

// fakeReactionsLookup is the batched enrichment stub. It records what it was
// asked for, so a test can prove a page is enriched in one deduplicated call.
type fakeReactionsLookup struct {
	summaries map[string]reactions.Summary
	mine      map[string][]reactions.ReactionType
	err       error

	gotEntityType reactions.EntityType
	gotEntityIDs  []string
	gotUserID     string
	summaryCalls  int
	mineCalls     int
}

func (f *fakeReactionsLookup) BatchSummaries(_ context.Context, entityType reactions.EntityType, entityIDs []string) (map[string]reactions.Summary, error) {
	f.summaryCalls++
	f.gotEntityType = entityType
	f.gotEntityIDs = append(f.gotEntityIDs, entityIDs...)
	if f.err != nil {
		return nil, f.err
	}
	return f.summaries, nil
}

func (f *fakeReactionsLookup) BatchMyReactions(_ context.Context, userID string, entityType reactions.EntityType, entityIDs []string) (map[string][]reactions.ReactionType, error) {
	f.mineCalls++
	f.gotUserID = userID
	f.gotEntityType = entityType
	f.gotEntityIDs = append(f.gotEntityIDs, entityIDs...)
	if f.err != nil {
		return nil, f.err
	}
	return f.mine, nil
}

// newTestReactionsHandler builds a handler over the given stubs, defaulting every
// dependency so a test only sets what it exercises.
func newTestReactionsHandler(t *testing.T, logger *slog.Logger, service ReactionsService, storyService StoriesService, versionService VersionsService, conversationService ConversationsService, notifier ReactionsNotifier) *ReactionsHandler {
	t.Helper()

	if storyService == nil {
		storyService = &fakeStoriesService{}
	}
	if versionService == nil {
		versionService = &fakeVersionsService{}
	}
	if conversationService == nil {
		conversationService = &fakeConversationsService{}
	}
	if notifier == nil {
		notifier = &fakeReactionsNotifier{}
	}
	if service == nil {
		service = &fakeReactionsService{}
	}

	handler, err := NewReactionsHandler(service, storyService, versionService, conversationService, notifier, logger)
	if err != nil {
		t.Fatalf("NewReactionsHandler() error = %v, want nil", err)
	}

	return handler
}

// newTestReactionsRouterHandler is newTestReactionsHandler in the shape the router
// builders want: a fully-stubbed handler for route-table tests that do not hit
// the reaction routes.
func newTestReactionsRouterHandler(t *testing.T, logger *slog.Logger) *ReactionsHandler {
	t.Helper()
	return newTestReactionsHandler(t, logger, nil, nil, nil, nil, nil)
}

// doReactionRequest calls a reactions handler method directly, with the entity id
// set as the {id} path value and the given user placed on the context.
func doReactionRequest(handler func(http.ResponseWriter, *http.Request), method, path, entityID, userID, body string) *httptest.ResponseRecorder {
	var reader io.Reader
	if body != "" {
		reader = strings.NewReader(body)
	}

	request := httptest.NewRequest(method, path, reader)
	request.Header.Set("Content-Type", "application/json")
	request.SetPathValue("id", entityID)
	if userID != "" {
		request = request.WithContext(withUserID(request.Context(), userID))
	}

	recorder := httptest.NewRecorder()
	handler(recorder, request)
	return recorder
}

func TestToggleStoryReactionCreatesAndNotifies(t *testing.T) {
	logger := slog.New(slog.NewTextHandler(io.Discard, nil))
	service := &fakeReactionsService{summary: reactions.Summary{RingsTrue: 1}, active: true}
	notifier := &fakeReactionsNotifier{}
	storiesService := &fakeStoriesService{getResult: stories.Story{ID: testReactionEntityID, AuthorID: testReactionAuthorID}}

	handler := newTestReactionsHandler(t, logger, service, storiesService, nil, nil, notifier)

	recorder := doReactionRequest(handler.ToggleStory, http.MethodPost, "/stories/x/reactions", testReactionEntityID, testReactionUserID, `{"reaction_type":"rings_true"}`)

	if recorder.Code != http.StatusOK {
		t.Fatalf("status = %d, want %d (body %s)", recorder.Code, http.StatusOK, recorder.Body.String())
	}

	var body reactionCountsEnvelope
	if err := json.Unmarshal(recorder.Body.Bytes(), &body); err != nil {
		t.Fatalf("decode response: %v", err)
	}
	if body.Reactions.RingsTrue != 1 {
		t.Errorf("rings_true = %d, want 1", body.Reactions.RingsTrue)
	}

	if service.gotToggle.EntityType != reactions.EntityStory || service.gotToggle.ReactionType != reactions.RingsTrue {
		t.Errorf("toggle input = %+v, want story/rings_true", service.gotToggle)
	}
	if service.gotToggle.UserID != testReactionUserID {
		t.Errorf("toggle user = %q, want %q", service.gotToggle.UserID, testReactionUserID)
	}

	if notifier.calls != 1 {
		t.Fatalf("notifier calls = %d, want 1", notifier.calls)
	}
	if notifier.gotRecipient != testReactionAuthorID || notifier.gotActor != testReactionUserID {
		t.Errorf("notification recipient/actor = %q/%q, want %q/%q", notifier.gotRecipient, notifier.gotActor, testReactionAuthorID, testReactionUserID)
	}
	if notifier.gotEntity != "story" || notifier.gotEntityID != testReactionEntityID {
		t.Errorf("notification entity = %q/%q, want story/%q", notifier.gotEntity, notifier.gotEntityID, testReactionEntityID)
	}
}

func TestToggleReactionOnOwnContentDoesNotNotify(t *testing.T) {
	logger := slog.New(slog.NewTextHandler(io.Discard, nil))
	service := &fakeReactionsService{active: true}
	notifier := &fakeReactionsNotifier{}
	// The story's author is the user doing the reacting.
	storiesService := &fakeStoriesService{getResult: stories.Story{ID: testReactionEntityID, AuthorID: testReactionUserID}}

	handler := newTestReactionsHandler(t, logger, service, storiesService, nil, nil, notifier)

	recorder := doReactionRequest(handler.ToggleStory, http.MethodPost, "/stories/x/reactions", testReactionEntityID, testReactionUserID, `{"reaction_type":"rings_true"}`)

	if recorder.Code != http.StatusOK {
		t.Fatalf("status = %d, want %d", recorder.Code, http.StatusOK)
	}
	if notifier.calls != 0 {
		t.Errorf("notifier calls = %d, want 0 for a reaction on your own content", notifier.calls)
	}
}

func TestToggleReactionOffDoesNotNotify(t *testing.T) {
	logger := slog.New(slog.NewTextHandler(io.Discard, nil))
	service := &fakeReactionsService{active: false}
	notifier := &fakeReactionsNotifier{}
	storiesService := &fakeStoriesService{getResult: stories.Story{ID: testReactionEntityID, AuthorID: testReactionAuthorID}}

	handler := newTestReactionsHandler(t, logger, service, storiesService, nil, nil, notifier)

	recorder := doReactionRequest(handler.ToggleStory, http.MethodPost, "/stories/x/reactions", testReactionEntityID, testReactionUserID, `{"reaction_type":"rings_true"}`)

	if recorder.Code != http.StatusOK {
		t.Fatalf("status = %d, want %d", recorder.Code, http.StatusOK)
	}
	if notifier.calls != 0 {
		t.Errorf("notifier calls = %d, want 0 when a reaction is removed", notifier.calls)
	}
}

func TestToggleReactionRejectsInvalidReactionType(t *testing.T) {
	logger := slog.New(slog.NewTextHandler(io.Discard, nil))
	service := &fakeReactionsService{}

	handler := newTestReactionsHandler(t, logger, service, nil, nil, nil, nil)

	recorder := doReactionRequest(handler.ToggleStory, http.MethodPost, "/stories/x/reactions", testReactionEntityID, testReactionUserID, `{"reaction_type":"like"}`)

	if recorder.Code != http.StatusBadRequest {
		t.Fatalf("status = %d, want %d", recorder.Code, http.StatusBadRequest)
	}
	if code := decodedErrorCode(t, recorder); code != codeValidation {
		t.Errorf("error code = %q, want %q", code, codeValidation)
	}
	if service.toggleCalls != 0 {
		t.Errorf("toggle calls = %d, want 0: an invalid type never reaches the service", service.toggleCalls)
	}
}

func TestToggleReactionUnknownEntityIsNotFound(t *testing.T) {
	logger := slog.New(slog.NewTextHandler(io.Discard, nil))
	service := &fakeReactionsService{}
	storiesService := &fakeStoriesService{getErr: stories.ErrNotFound}

	handler := newTestReactionsHandler(t, logger, service, storiesService, nil, nil, nil)

	recorder := doReactionRequest(handler.ToggleStory, http.MethodPost, "/stories/x/reactions", testReactionEntityID, testReactionUserID, `{"reaction_type":"rings_true"}`)

	if recorder.Code != http.StatusNotFound {
		t.Fatalf("status = %d, want %d", recorder.Code, http.StatusNotFound)
	}
	if code := decodedErrorCode(t, recorder); code != codeNotFound {
		t.Errorf("error code = %q, want %q", code, codeNotFound)
	}
	if service.toggleCalls != 0 {
		t.Errorf("toggle calls = %d, want 0: an unknown entity never reaches the service", service.toggleCalls)
	}
}

func TestToggleReactionOnReplyIsRejected(t *testing.T) {
	logger := slog.New(slog.NewTextHandler(io.Discard, nil))
	service := &fakeReactionsService{}
	parent := testReactionEntityID
	conversationsService := &fakeConversationsService{getCommentResult: conversations.Comment{
		ID:              testReactionCommentID,
		AuthorID:        testReactionAuthorID,
		ParentCommentID: &parent,
	}}

	handler := newTestReactionsHandler(t, logger, service, nil, nil, conversationsService, nil)

	recorder := doReactionRequest(handler.ToggleComment, http.MethodPost, "/comments/x/reactions", testReactionCommentID, testReactionUserID, `{"reaction_type":"rings_true"}`)

	if recorder.Code != http.StatusBadRequest {
		t.Fatalf("status = %d, want %d (body %s)", recorder.Code, http.StatusBadRequest, recorder.Body.String())
	}
	if service.toggleCalls != 0 {
		t.Errorf("toggle calls = %d, want 0: a reply cannot be reacted to", service.toggleCalls)
	}
}

func TestToggleReactionUnauthenticatedIsUnauthorized(t *testing.T) {
	logger := slog.New(slog.NewTextHandler(io.Discard, nil))
	handler := newTestReactionsHandler(t, logger, nil, nil, nil, nil, nil)

	recorder := doReactionRequest(handler.ToggleStory, http.MethodPost, "/stories/x/reactions", testReactionEntityID, "", `{"reaction_type":"rings_true"}`)

	if recorder.Code != http.StatusUnauthorized {
		t.Fatalf("status = %d, want %d", recorder.Code, http.StatusUnauthorized)
	}
}

func TestToggleCommentReactionUsesCommentEntity(t *testing.T) {
	logger := slog.New(slog.NewTextHandler(io.Discard, nil))
	service := &fakeReactionsService{active: true}
	conversationsService := &fakeConversationsService{getCommentResult: conversations.Comment{
		ID:       testReactionCommentID,
		AuthorID: testReactionAuthorID,
	}}

	handler := newTestReactionsHandler(t, logger, service, nil, nil, conversationsService, nil)

	recorder := doReactionRequest(handler.ToggleComment, http.MethodPost, "/comments/x/reactions", testReactionCommentID, testReactionUserID, `{"reaction_type":"needs_a_source"}`)

	if recorder.Code != http.StatusOK {
		t.Fatalf("status = %d, want %d", recorder.Code, http.StatusOK)
	}
	if service.gotToggle.EntityType != reactions.EntityComment {
		t.Errorf("entity type = %q, want comment", service.gotToggle.EntityType)
	}
}

func TestSwitchToggleVersionAndBridgeEntityTypes(t *testing.T) {
	logger := slog.New(slog.NewTextHandler(io.Discard, nil))

	versionService := &fakeVersionsService{getResult: versions.StoryVersion{ID: testReactionEntityID, AuthorID: testReactionAuthorID}}
	bridgeService := &fakeConversationsService{getBridgeResult: conversations.Bridge{ID: testReactionEntityID, AuthorID: testReactionAuthorID}}

	versionHandler := newTestReactionsHandler(t, logger, &fakeReactionsService{active: true}, nil, versionService, nil, nil)
	versionRecorder := doReactionRequest(versionHandler.ToggleVersion, http.MethodPost, "/versions/x/reactions", testReactionEntityID, testReactionUserID, `{"reaction_type":"rings_true"}`)
	if versionRecorder.Code != http.StatusOK {
		t.Fatalf("version status = %d, want %d", versionRecorder.Code, http.StatusOK)
	}

	bridgeHandler := newTestReactionsHandler(t, logger, &fakeReactionsService{active: true}, nil, nil, bridgeService, nil)
	bridgeRecorder := doReactionRequest(bridgeHandler.ToggleBridge, http.MethodPost, "/bridges/x/reactions", testReactionEntityID, testReactionUserID, `{"reaction_type":"rings_true"}`)
	if bridgeRecorder.Code != http.StatusOK {
		t.Fatalf("bridge status = %d, want %d", bridgeRecorder.Code, http.StatusOK)
	}
}

func TestListReactionsReturnsActors(t *testing.T) {
	logger := slog.New(slog.NewTextHandler(io.Discard, nil))
	service := &fakeReactionsService{listResult: []reactions.ReactionWithActor{
		{
			Reaction: reactions.Reaction{ID: testReactionEntityID, UserID: testReactionUserID, ReactionType: reactions.RingsTrue},
			Actor:    reactions.Actor{ID: testReactionUserID, DisplayName: "Ada Lovelace", AvatarURL: "/users/x/avatar?v=a.png"},
		},
	}}

	handler := newTestReactionsHandler(t, logger, service, nil, nil, nil, nil)

	recorder := doReactionRequest(handler.ListStory, http.MethodGet, "/stories/x/reactions", testReactionEntityID, "", "")

	if recorder.Code != http.StatusOK {
		t.Fatalf("status = %d, want %d", recorder.Code, http.StatusOK)
	}

	var body reactionListResponse
	if err := json.Unmarshal(recorder.Body.Bytes(), &body); err != nil {
		t.Fatalf("decode response: %v", err)
	}
	if len(body.Reactions) != 1 {
		t.Fatalf("reactions length = %d, want 1", len(body.Reactions))
	}
	if body.Reactions[0].User.DisplayName != "Ada Lovelace" {
		t.Errorf("actor display name = %q, want %q", body.Reactions[0].User.DisplayName, "Ada Lovelace")
	}
	if body.Reactions[0].ReactionType != "rings_true" {
		t.Errorf("reaction type = %q, want rings_true", body.Reactions[0].ReactionType)
	}
	if service.gotListType != reactions.EntityStory {
		t.Errorf("list entity type = %q, want story", service.gotListType)
	}
}

func TestListReactionsEmptyIsAnArray(t *testing.T) {
	logger := slog.New(slog.NewTextHandler(io.Discard, nil))
	handler := newTestReactionsHandler(t, logger, &fakeReactionsService{}, nil, nil, nil, nil)

	recorder := doReactionRequest(handler.ListBridge, http.MethodGet, "/bridges/x/reactions", testReactionEntityID, "", "")

	if recorder.Code != http.StatusOK {
		t.Fatalf("status = %d, want %d", recorder.Code, http.StatusOK)
	}
	if !strings.Contains(recorder.Body.String(), `"reactions":[]`) {
		t.Errorf("body = %s, want an empty array rather than null", recorder.Body.String())
	}
}

func TestNewReactionsHandlerRejectsMissingDependencies(t *testing.T) {
	logger := slog.New(slog.NewTextHandler(io.Discard, nil))

	if _, err := NewReactionsHandler(nil, &fakeStoriesService{}, &fakeVersionsService{}, &fakeConversationsService{}, &fakeReactionsNotifier{}, logger); err == nil {
		t.Error("NewReactionsHandler(nil service) error = nil, want an error")
	}
	if _, err := NewReactionsHandler(&fakeReactionsService{}, nil, &fakeVersionsService{}, &fakeConversationsService{}, &fakeReactionsNotifier{}, logger); err == nil {
		t.Error("NewReactionsHandler(nil stories) error = nil, want an error")
	}
	if _, err := NewReactionsHandler(&fakeReactionsService{}, &fakeStoriesService{}, nil, &fakeConversationsService{}, &fakeReactionsNotifier{}, logger); err == nil {
		t.Error("NewReactionsHandler(nil versions) error = nil, want an error")
	}
	if _, err := NewReactionsHandler(&fakeReactionsService{}, &fakeStoriesService{}, &fakeVersionsService{}, nil, &fakeReactionsNotifier{}, logger); err == nil {
		t.Error("NewReactionsHandler(nil conversations) error = nil, want an error")
	}
	if _, err := NewReactionsHandler(&fakeReactionsService{}, &fakeStoriesService{}, &fakeVersionsService{}, &fakeConversationsService{}, nil, logger); err == nil {
		t.Error("NewReactionsHandler(nil notifier) error = nil, want an error")
	}
	if _, err := NewReactionsHandler(&fakeReactionsService{}, &fakeStoriesService{}, &fakeVersionsService{}, &fakeConversationsService{}, &fakeReactionsNotifier{}, nil); err == nil {
		t.Error("NewReactionsHandler(nil logger) error = nil, want an error")
	}
}
