package httpapi

import (
	"bytes"
	"context"
	"log/slog"
	"net/http"
	"strings"
	"testing"

	"github.com/knot/backend/internal/conversations"
	"github.com/knot/backend/internal/inquiries"
	"github.com/knot/backend/internal/reactions"
	"github.com/knot/backend/internal/stories"
	"github.com/knot/backend/internal/versions"
)

// The invalidation rules reactions and comments apply to the detail cache
// (KNOT-036-fix2).
//
// Every test drives the real handler over the fake detail cache the KNOT-036-fix
// tests introduced (detailCacheRecorder), so the assertions are about the keys a
// write deletes rather than about a mock's internal bookkeeping.

const (
	testInvalidationStoryID   = "a1000000-0000-4000-8000-000000000001"
	testInvalidationVersionID = "a2000000-0000-4000-8000-000000000002"
	testInvalidationCommentID = "a3000000-0000-4000-8000-000000000003"
)

// newReactionsHandlerWithDetails builds a reactions handler whose detail cache is
// the recorder, defaulting every other dependency so a test sets only what it
// exercises.
func newReactionsHandlerWithDetails(t *testing.T, logger *slog.Logger, details DetailCache, service ReactionsService, storyService StoriesService, versionService VersionsService, conversationService ConversationsService, inquiryService InquiriesService) *ReactionsHandler {
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
	if inquiryService == nil {
		inquiryService = &fakeInquiriesService{getErr: inquiries.ErrNotFound}
	}
	if service == nil {
		service = &fakeReactionsService{}
	}

	handler, err := NewReactionsHandlerWithCaches(
		service, storyService, versionService, conversationService, inquiryService,
		&fakeReactionsNotifier{}, logger, nil, Caches{Details: details},
	)
	if err != nil {
		t.Fatalf("NewReactionsHandlerWithCaches() error = %v, want nil", err)
	}

	return handler
}

// TestToggleStoryReactionInvalidatesTheStoryDetail is the simplest rule: the story
// detail's reaction counts changed, so its own key goes.
func TestToggleStoryReactionInvalidatesTheStoryDetail(t *testing.T) {
	details := newDetailCacheRecorder()
	service := &fakeReactionsService{summary: reactions.Summary{RingsTrue: 1}, active: true}
	storiesService := &fakeStoriesService{getResult: stories.Story{
		ID:       testReactionEntityID,
		AuthorID: testReactionAuthorID,
	}}

	handler := newReactionsHandlerWithDetails(t, discardLogger(), details, service, storiesService, nil, nil, nil)

	recorder := doReactionRequest(handler.ToggleStory, http.MethodPost, "/stories/x/reactions", testReactionEntityID, testReactionUserID, `{"reaction_type":"rings_true"}`)
	if recorder.Code != http.StatusOK {
		t.Fatalf("status = %d, want %d (body %s)", recorder.Code, http.StatusOK, recorder.Body.String())
	}

	state := details.snapshot()
	if len(state.invalidatedStories) != 1 || state.invalidatedStories[0] != testReactionEntityID {
		t.Fatalf("invalidated stories = %v, want [%s]", state.invalidatedStories, testReactionEntityID)
	}
	if len(state.invalidatedVersions) != 0 {
		t.Errorf("invalidated versions = %v, want none", state.invalidatedVersions)
	}
}

// TestToggleVersionReactionInvalidatesTheVersionAndItsStory proves the two-key
// rule: the version's own reaction count is on its detail, and the story detail's
// counts include its versions.
func TestToggleVersionReactionInvalidatesTheVersionAndItsStory(t *testing.T) {
	details := newDetailCacheRecorder()
	service := &fakeReactionsService{summary: reactions.Summary{AddsSomethingNew: 1}, active: true}
	versionsService := &fakeVersionsService{getResult: versions.StoryVersion{
		ID:       testInvalidationVersionID,
		StoryID:  testInvalidationStoryID,
		AuthorID: testReactionAuthorID,
	}}

	handler := newReactionsHandlerWithDetails(t, discardLogger(), details, service, nil, versionsService, nil, nil)

	recorder := doReactionRequest(handler.ToggleVersion, http.MethodPost, "/versions/x/reactions", testInvalidationVersionID, testReactionUserID, `{"reaction_type":"adds_something_new"}`)
	if recorder.Code != http.StatusOK {
		t.Fatalf("status = %d, want %d (body %s)", recorder.Code, http.StatusOK, recorder.Body.String())
	}

	state := details.snapshot()
	if len(state.invalidatedVersions) != 1 || state.invalidatedVersions[0] != testInvalidationVersionID {
		t.Fatalf("invalidated versions = %v, want [%s]", state.invalidatedVersions, testInvalidationVersionID)
	}
	if len(state.invalidatedStories) != 1 || state.invalidatedStories[0] != testInvalidationStoryID {
		t.Fatalf("invalidated stories = %v, want the parent [%s]", state.invalidatedStories, testInvalidationStoryID)
	}
}

// TestToggleCommentReactionIsRejectedAndInvalidatesNothing pins the current
// contract: reactions on comments are refused before anything is written
// (KNOT-ADR-053), so there is no count to invalidate. The comment rule itself is
// covered by the invalidator tests below, where the dispatch can be called.
func TestToggleCommentReactionIsRejectedAndInvalidatesNothing(t *testing.T) {
	details := newDetailCacheRecorder()
	handler := newReactionsHandlerWithDetails(t, discardLogger(), details, nil, nil, nil, nil, nil)

	recorder := doReactionRequest(handler.ToggleComment, http.MethodPost, "/comments/x/reactions", testInvalidationCommentID, testReactionUserID, `{"reaction_type":"rings_true"}`)
	if recorder.Code != http.StatusBadRequest {
		t.Fatalf("status = %d, want %d for a comment reaction", recorder.Code, http.StatusBadRequest)
	}

	state := details.snapshot()
	if len(state.invalidatedStories) != 0 || len(state.invalidatedVersions) != 0 {
		t.Errorf("invalidated = %v/%v, want nothing for a rejected toggle", state.invalidatedStories, state.invalidatedVersions)
	}
}

// TestDetailInvalidatorInvalidatesACommentThreadAndItsStory is the comment rule
// the dispatcher applies: the version detail shows the thread, and the story detail
// carries the comment count.
func TestDetailInvalidatorInvalidatesACommentThreadAndItsStory(t *testing.T) {
	details := newDetailCacheRecorder()
	comments := &fakeConversationsService{getCommentResult: conversations.Comment{
		ID:        testInvalidationCommentID,
		VersionID: testInvalidationVersionID,
		StoryID:   testInvalidationStoryID,
	}}

	invalidator := NewDetailInvalidator(details, &fakeVersionsService{}, comments, discardLogger())
	invalidator.InvalidateForEntity(context.Background(), string(reactions.EntityComment), testInvalidationCommentID)

	state := details.snapshot()
	if len(state.invalidatedVersions) != 1 || state.invalidatedVersions[0] != testInvalidationVersionID {
		t.Fatalf("invalidated versions = %v, want [%s]", state.invalidatedVersions, testInvalidationVersionID)
	}
	if len(state.invalidatedStories) != 1 || state.invalidatedStories[0] != testInvalidationStoryID {
		t.Fatalf("invalidated stories = %v, want [%s]", state.invalidatedStories, testInvalidationStoryID)
	}
}

// TestDetailInvalidatorResolvesTheStoryWhenACommentCarriesOnlyItsVersion covers the
// create path: CreateComment returns a comment whose StoryID is empty, so the story
// is resolved through the version.
func TestDetailInvalidatorResolvesTheStoryWhenACommentCarriesOnlyItsVersion(t *testing.T) {
	details := newDetailCacheRecorder()
	comments := &fakeConversationsService{getCommentResult: conversations.Comment{
		ID:        testInvalidationCommentID,
		VersionID: testInvalidationVersionID,
	}}
	versionsService := &fakeVersionsService{getResult: versions.StoryVersion{
		ID:      testInvalidationVersionID,
		StoryID: testInvalidationStoryID,
	}}

	invalidator := NewDetailInvalidator(details, versionsService, comments, discardLogger())
	invalidator.InvalidateForComment(context.Background(), testInvalidationCommentID)

	state := details.snapshot()
	if len(state.invalidatedVersions) != 1 || state.invalidatedVersions[0] != testInvalidationVersionID {
		t.Fatalf("invalidated versions = %v, want [%s]", state.invalidatedVersions, testInvalidationVersionID)
	}
	if len(state.invalidatedStories) != 1 || state.invalidatedStories[0] != testInvalidationStoryID {
		t.Fatalf("invalidated stories = %v, want the resolved parent [%s]", state.invalidatedStories, testInvalidationStoryID)
	}
}

// TestCreateCommentInvalidatesTheVersionAndItsStory is the write-path test: a new
// comment clears the version detail (its thread) and the story detail (its
// comment_count).
func TestCreateCommentInvalidatesTheVersionAndItsStory(t *testing.T) {
	details := newDetailCacheRecorder()

	handler, err := NewConversationsHandlerWithCaches(
		&fakeConversationsService{createCommentResult: conversations.Comment{
			ID:        testInvalidationCommentID,
			VersionID: testInvalidationVersionID,
			AuthorID:  testUserID,
			Language:  "eng",
			Body:      "A comment.",
			CreatedAt: testNow,
			UpdatedAt: testNow,
		}},
		&fakeAuthorService{},
		&fakeRootedService{},
		&fakeReactionsLookup{},
		&fakeVersionsService{getResult: versions.StoryVersion{
			ID:      testInvalidationVersionID,
			StoryID: testInvalidationStoryID,
		}},
		discardLogger(),
		nil,
		Caches{Details: details},
	)
	if err != nil {
		t.Fatalf("NewConversationsHandlerWithCaches() error = %v, want nil", err)
	}

	recorder := doReactionRequest(handler.CreateComment, http.MethodPost, "/versions/x/comments", testInvalidationVersionID, testUserID, `{"body":"A comment.","language":"eng"}`)
	if recorder.Code != http.StatusCreated {
		t.Fatalf("status = %d, want %d (body %s)", recorder.Code, http.StatusCreated, recorder.Body.String())
	}

	state := details.snapshot()
	if len(state.invalidatedVersions) != 1 || state.invalidatedVersions[0] != testInvalidationVersionID {
		t.Fatalf("invalidated versions = %v, want [%s]", state.invalidatedVersions, testInvalidationVersionID)
	}
	if len(state.invalidatedStories) != 1 || state.invalidatedStories[0] != testInvalidationStoryID {
		t.Fatalf("invalidated stories = %v, want [%s]", state.invalidatedStories, testInvalidationStoryID)
	}
}

// TestReactionInvalidationFailureStillSucceeds is the best-effort contract: a Redis
// failure is a logged warning, never a failed toggle, and the updated counts are
// still returned.
func TestReactionInvalidationFailureStillSucceeds(t *testing.T) {
	var logs bytes.Buffer
	logger := slog.New(slog.NewTextHandler(&logs, &slog.HandlerOptions{Level: slog.LevelDebug}))

	details := newDetailCacheRecorder()
	details.invalidateErr = errCacheDown

	service := &fakeReactionsService{summary: reactions.Summary{RingsTrue: 3}, active: true}
	storiesService := &fakeStoriesService{getResult: stories.Story{
		ID:       testReactionEntityID,
		AuthorID: testReactionAuthorID,
	}}

	handler := newReactionsHandlerWithDetails(t, logger, details, service, storiesService, nil, nil, nil)

	recorder := doReactionRequest(handler.ToggleStory, http.MethodPost, "/stories/x/reactions", testReactionEntityID, testReactionUserID, `{"reaction_type":"rings_true"}`)
	if recorder.Code != http.StatusOK {
		t.Fatalf("status = %d, want %d: a failed invalidation must not fail the toggle (body %s)",
			recorder.Code, http.StatusOK, recorder.Body.String())
	}
	if !strings.Contains(recorder.Body.String(), `"rings_true":3`) {
		t.Errorf("body = %s, want the updated counts", recorder.Body.String())
	}

	output := logs.String()
	if !strings.Contains(output, "level=WARN") {
		t.Errorf("logs = %q, want a WARN line", output)
	}
	if !strings.Contains(output, "story detail cache invalidation failed") {
		t.Errorf("logs = %q, want the invalidation warning", output)
	}
}

// TestToggleInquiryReactionInvalidatesNothing proves the dispatcher leaves alone a
// kind the detail cache does not hold: nothing caches an inquiry.
func TestToggleInquiryReactionInvalidatesNothing(t *testing.T) {
	details := newDetailCacheRecorder()
	service := &fakeReactionsService{summary: reactions.Summary{NeedsASource: 1}, active: true}
	inquiryService := &fakeInquiriesService{getResult: inquiries.Inquiry{
		ID:       testReactionEntityID,
		AuthorID: testReactionAuthorID,
	}}

	handler := newReactionsHandlerWithDetails(t, discardLogger(), details, service, nil, nil, nil, inquiryService)

	recorder := doReactionRequest(handler.ToggleInquiry, http.MethodPost, "/inquiries/x/reactions", testReactionEntityID, testReactionUserID, `{"reaction_type":"needs_a_source"}`)
	if recorder.Code != http.StatusOK {
		t.Fatalf("status = %d, want %d (body %s)", recorder.Code, http.StatusOK, recorder.Body.String())
	}

	state := details.snapshot()
	if len(state.invalidatedStories) != 0 || len(state.invalidatedVersions) != 0 {
		t.Errorf("invalidated = %v/%v, want nothing for an inquiry", state.invalidatedStories, state.invalidatedVersions)
	}
}
