package httpapi

import (
	"errors"
	"io"
	"log/slog"
	"net/http"
	"strings"
	"testing"

	"github.com/knot/backend/internal/conversations"
	"github.com/knot/backend/internal/rooted"
	"github.com/knot/backend/internal/stories"
	"github.com/knot/backend/internal/versions"
)

// Author ids used by the enrichment tests.
const (
	authorA = "aaaaaaaa-aaaa-4aaa-8aaa-aaaaaaaaaaaa"
	authorB = "bbbbbbbb-bbbb-4bbb-8bbb-bbbbbbbbbbbb"
)

func discardLogger() *slog.Logger {
	return slog.New(slog.NewTextHandler(io.Discard, nil))
}

// rootedSignals builds a batch result mapping each author id to a public signal in
// the given place.
func rootedSignals(places map[string]string) map[string]*rooted.Signal {
	out := make(map[string]*rooted.Signal, len(places))
	for userID, place := range places {
		out[userID] = &rooted.Signal{
			UserID:         userID,
			Place:          place,
			DurationBucket: rooted.DurationLifelong,
			IsPublic:       true,
			IsPrimary:      true,
		}
	}
	return out
}

func storyFixture(id, authorID string) stories.Story {
	return stories.Story{
		ID:            id,
		AuthorID:      authorID,
		RootVersionID: "0f6c2b1a-9e2d-4c7b-8a31-6d5e4f3c2b1a",
		Pillar:        stories.PillarWonder,
		Language:      "en",
		Title:         "The first rain",
		Body:          "Grandmother said the first rain remembers every name.",
		MediaURLs:     []string{},
		CreatedAt:     testNow,
		UpdatedAt:     testNow,
	}
}

func versionFixture(id, authorID string) versions.StoryVersion {
	return versions.StoryVersion{
		ID:        id,
		StoryID:   testStoryID,
		AuthorID:  authorID,
		Language:  "en",
		Title:     "The first rain",
		Body:      "Grandmother said the first rain remembers every name.",
		CreatedAt: testNow,
		UpdatedAt: testNow,
	}
}

func commentFixture(id, authorID string) conversations.Comment {
	return conversations.Comment{
		ID:        id,
		VersionID: testVersionID,
		AuthorID:  authorID,
		Language:  "en",
		Body:      "The first rain remembers every name.",
		CreatedAt: testNow,
		UpdatedAt: testNow,
	}
}

func bridgeFixture(id, authorID string) conversations.Bridge {
	return conversations.Bridge{
		ID:              id,
		SourceCommentID: testSourceCommentID,
		TargetCommentID: testTargetCommentID,
		AuthorID:        authorID,
		TargetLanguage:  "fr",
		CreatedAt:       testNow,
	}
}

func TestStoryDetailAttachesAuthorRooted(t *testing.T) {
	service := &fakeRootedService{batchResult: rootedSignals(map[string]string{authorA: "Cape Town"})}
	handler := newRouterWithRooted(t, discardLogger(), service,
		&fakeStoriesService{getResult: storyFixture("d6b53a2c-2e2f-4a4d-9b0f-3f6f4e0f1a2b", authorA)},
		&fakeVersionsService{}, &fakeConversationsService{})

	recorder := doStoryRequest(handler, http.MethodGet, "/stories/d6b53a2c-2e2f-4a4d-9b0f-3f6f4e0f1a2b", "", "")

	if recorder.Code != http.StatusOK {
		t.Fatalf("status = %d, want %d (body %s)", recorder.Code, http.StatusOK, recorder.Body.String())
	}

	var body storyEnvelope
	decodeBody(t, recorder, &body)

	if body.Story.AuthorRooted == nil {
		t.Fatal("author_rooted = nil, want the author's inline summary")
	}
	if body.Story.AuthorRooted.Place != "Cape Town" {
		t.Errorf("place = %q, want %q", body.Story.AuthorRooted.Place, "Cape Town")
	}
	if body.Story.AuthorRooted.DurationBucket != rooted.DurationLifelong {
		t.Errorf("duration_bucket = %q, want %q", body.Story.AuthorRooted.DurationBucket, rooted.DurationLifelong)
	}
	if service.batchCalls != 1 {
		t.Errorf("batch calls = %d, want exactly 1 per response", service.batchCalls)
	}
}

func TestStoryDetailWithoutSignalIsNull(t *testing.T) {
	service := &fakeRootedService{}
	handler := newRouterWithRooted(t, discardLogger(), service,
		&fakeStoriesService{getResult: storyFixture("d6b53a2c-2e2f-4a4d-9b0f-3f6f4e0f1a2b", authorA)},
		&fakeVersionsService{}, &fakeConversationsService{})

	recorder := doStoryRequest(handler, http.MethodGet, "/stories/d6b53a2c-2e2f-4a4d-9b0f-3f6f4e0f1a2b", "", "")

	if recorder.Code != http.StatusOK {
		t.Fatalf("status = %d, want %d", recorder.Code, http.StatusOK)
	}
	if !strings.Contains(recorder.Body.String(), `"author_rooted":null`) {
		t.Errorf("body = %s, want author_rooted null", recorder.Body.String())
	}
	if service.batchCalls != 1 {
		t.Errorf("batch calls = %d, want exactly 1", service.batchCalls)
	}
}

func TestLanguageTreeBatchesAuthorsOnce(t *testing.T) {
	tree := []versions.StoryVersion{
		versionFixture("11111111-1111-4111-8111-111111111111", authorA),
		versionFixture("22222222-2222-4222-8222-222222222222", authorB),
		versionFixture("33333333-3333-4333-8333-333333333333", authorA),
	}
	service := &fakeRootedService{batchResult: rootedSignals(map[string]string{authorA: "Cape Town"})}
	handler := newRouterWithRooted(t, discardLogger(), service, &fakeStoriesService{},
		&fakeVersionsService{treeResult: tree}, &fakeConversationsService{})

	recorder := doStoryRequest(handler, http.MethodGet, "/stories/"+testStoryID+"/tree", "", "")

	if recorder.Code != http.StatusOK {
		t.Fatalf("status = %d, want %d (body %s)", recorder.Code, http.StatusOK, recorder.Body.String())
	}

	var body treeResponse
	decodeBody(t, recorder, &body)

	if len(body.Versions) != 3 {
		t.Fatalf("versions = %d, want 3", len(body.Versions))
	}
	if body.Versions[0].AuthorRooted == nil || body.Versions[0].AuthorRooted.Place != "Cape Town" {
		t.Errorf("version 0 author_rooted = %+v, want the author's summary", body.Versions[0].AuthorRooted)
	}
	if body.Versions[1].AuthorRooted != nil {
		t.Errorf("version 1 author_rooted = %+v, want null for an author with no signal", body.Versions[1].AuthorRooted)
	}
	if body.Versions[2].AuthorRooted == nil {
		t.Error("version 2 author_rooted = nil, want the summary for a repeated author")
	}

	if service.batchCalls != 1 {
		t.Errorf("batch calls = %d, want exactly 1 for the whole response", service.batchCalls)
	}
	if len(service.gotBatchIDs) != 2 {
		t.Errorf("batched ids = %v, want the 2 distinct authors", service.gotBatchIDs)
	}
}

func TestVersionGetAttachesAuthorRooted(t *testing.T) {
	service := &fakeRootedService{batchResult: rootedSignals(map[string]string{authorA: "Cape Town"})}
	handler := newRouterWithRooted(t, discardLogger(), service, &fakeStoriesService{},
		&fakeVersionsService{getResult: versionFixture("11111111-1111-4111-8111-111111111111", authorA)},
		&fakeConversationsService{})

	recorder := doStoryRequest(handler, http.MethodGet, "/versions/11111111-1111-4111-8111-111111111111", "", "")

	if recorder.Code != http.StatusOK {
		t.Fatalf("status = %d, want %d", recorder.Code, http.StatusOK)
	}

	var body versionEnvelope
	decodeBody(t, recorder, &body)

	if body.Version.AuthorRooted == nil || body.Version.AuthorRooted.Place != "Cape Town" {
		t.Errorf("author_rooted = %+v, want the author's summary", body.Version.AuthorRooted)
	}
	if service.batchCalls != 1 {
		t.Errorf("batch calls = %d, want 1", service.batchCalls)
	}
}

func TestAdaptResponseAttachesAuthorRooted(t *testing.T) {
	service := &fakeRootedService{batchResult: rootedSignals(map[string]string{testUserID: "Cape Town"})}
	handler := newRouterWithRooted(t, discardLogger(), service, &fakeStoriesService{},
		&fakeVersionsService{createResult: versionFixture("11111111-1111-4111-8111-111111111111", testUserID)},
		&fakeConversationsService{})

	body := `{"parent_version_id": "` + testRootVersionID + `", "language": "fr", "title": "t", "body": "b"}`
	recorder := doStoryRequest(handler, http.MethodPost, "/stories/"+testStoryID+"/adapt", body, testAccessToken)

	if recorder.Code != http.StatusCreated {
		t.Fatalf("status = %d, want %d (body %s)", recorder.Code, http.StatusCreated, recorder.Body.String())
	}

	var envelope versionEnvelope
	decodeBody(t, recorder, &envelope)

	if envelope.Version.AuthorRooted == nil {
		t.Fatal("author_rooted = nil, want the adapter's summary")
	}
	if service.batchCalls != 1 {
		t.Errorf("batch calls = %d, want 1", service.batchCalls)
	}
}

func TestCommentListBatchesAuthorsOnce(t *testing.T) {
	comments := []conversations.Comment{
		commentFixture("66666666-6666-4666-8666-666666666661", authorA),
		commentFixture("66666666-6666-4666-8666-666666666662", authorB),
	}
	service := &fakeRootedService{batchResult: rootedSignals(map[string]string{authorA: "Cape Town", authorB: "Johannesburg"})}
	handler := newRouterWithRooted(t, discardLogger(), service, &fakeStoriesService{}, &fakeVersionsService{},
		&fakeConversationsService{listCommentsResult: comments})

	recorder := doStoryRequest(handler, http.MethodGet, "/versions/"+testVersionID+"/comments", "", "")

	if recorder.Code != http.StatusOK {
		t.Fatalf("status = %d, want %d (body %s)", recorder.Code, http.StatusOK, recorder.Body.String())
	}

	var body commentListResponse
	decodeBody(t, recorder, &body)

	if len(body.Comments) != 2 {
		t.Fatalf("comments = %d, want 2", len(body.Comments))
	}
	if body.Comments[0].AuthorRooted == nil || body.Comments[0].AuthorRooted.Place != "Cape Town" {
		t.Errorf("comment 0 author_rooted = %+v, want Cape Town", body.Comments[0].AuthorRooted)
	}
	if body.Comments[1].AuthorRooted == nil || body.Comments[1].AuthorRooted.Place != "Johannesburg" {
		t.Errorf("comment 1 author_rooted = %+v, want Johannesburg", body.Comments[1].AuthorRooted)
	}
	if service.batchCalls != 1 {
		t.Errorf("batch calls = %d, want exactly 1", service.batchCalls)
	}
	if len(service.gotBatchIDs) != 2 {
		t.Errorf("batched ids = %v, want 2 distinct authors", service.gotBatchIDs)
	}
}

func TestCreateCommentAttachesAuthorRooted(t *testing.T) {
	service := &fakeRootedService{batchResult: rootedSignals(map[string]string{testUserID: "Cape Town"})}
	handler := newRouterWithRooted(t, discardLogger(), service, &fakeStoriesService{}, &fakeVersionsService{},
		&fakeConversationsService{createCommentResult: commentFixture("66666666-6666-4666-8666-666666666661", testUserID)})

	recorder := doStoryRequest(handler, http.MethodPost, "/versions/"+testVersionID+"/comments",
		`{"body": "hi", "language": "en"}`, testAccessToken)

	if recorder.Code != http.StatusCreated {
		t.Fatalf("status = %d, want %d (body %s)", recorder.Code, http.StatusCreated, recorder.Body.String())
	}

	var body commentEnvelope
	decodeBody(t, recorder, &body)

	if body.Comment.AuthorRooted == nil {
		t.Fatal("author_rooted = nil, want the commenter's summary")
	}
	if service.batchCalls != 1 {
		t.Errorf("batch calls = %d, want 1", service.batchCalls)
	}
}

func TestCreateBridgeAttachesToAllThreeEntities(t *testing.T) {
	service := &fakeRootedService{batchResult: rootedSignals(map[string]string{
		testUserID: "Cape Town", authorA: "Johannesburg", authorB: "Durban",
	})}
	handler := newRouterWithRooted(t, discardLogger(), service, &fakeStoriesService{}, &fakeVersionsService{},
		&fakeConversationsService{
			createBridgeBridge: bridgeFixture(testBridgeID, testUserID),
			createBridgeSource: commentFixture(testSourceCommentID, authorA),
			createBridgeTarget: commentFixture(testTargetCommentID, authorB),
		})

	recorder := doStoryRequest(handler, http.MethodPost, "/comments/"+testSourceCommentID+"/bridges",
		`{"target_language": "fr", "body": "hi"}`, testAccessToken)

	if recorder.Code != http.StatusCreated {
		t.Fatalf("status = %d, want %d (body %s)", recorder.Code, http.StatusCreated, recorder.Body.String())
	}

	var body createBridgeResponse
	decodeBody(t, recorder, &body)

	if body.Bridge.AuthorRooted == nil || body.Bridge.AuthorRooted.Place != "Cape Town" {
		t.Errorf("bridge author_rooted = %+v, want Cape Town", body.Bridge.AuthorRooted)
	}
	if body.SourceComment.AuthorRooted == nil || body.SourceComment.AuthorRooted.Place != "Johannesburg" {
		t.Errorf("source comment author_rooted = %+v, want Johannesburg", body.SourceComment.AuthorRooted)
	}
	if body.TargetComment.AuthorRooted == nil || body.TargetComment.AuthorRooted.Place != "Durban" {
		t.Errorf("target comment author_rooted = %+v, want Durban", body.TargetComment.AuthorRooted)
	}

	if service.batchCalls != 1 {
		t.Errorf("batch calls = %d, want exactly 1 for the whole response", service.batchCalls)
	}
	if len(service.gotBatchIDs) != 3 {
		t.Errorf("batched ids = %v, want the 3 distinct authors", service.gotBatchIDs)
	}
}

func TestBridgeListAttachesAuthorRooted(t *testing.T) {
	service := &fakeRootedService{batchResult: rootedSignals(map[string]string{authorA: "Cape Town"})}
	handler := newRouterWithRooted(t, discardLogger(), service, &fakeStoriesService{}, &fakeVersionsService{},
		&fakeConversationsService{listBridgesResult: []conversations.Bridge{bridgeFixture(testBridgeID, authorA)}})

	recorder := doStoryRequest(handler, http.MethodGet, "/comments/"+testSourceCommentID+"/bridges", "", "")

	if recorder.Code != http.StatusOK {
		t.Fatalf("status = %d, want %d", recorder.Code, http.StatusOK)
	}

	var body bridgeListResponse
	decodeBody(t, recorder, &body)

	if len(body.Bridges) != 1 || body.Bridges[0].AuthorRooted == nil {
		t.Fatalf("bridges = %+v, want one entry with author_rooted", body.Bridges)
	}
	if service.batchCalls != 1 {
		t.Errorf("batch calls = %d, want 1", service.batchCalls)
	}
}

func TestGetBridgeAttachesAuthorRooted(t *testing.T) {
	service := &fakeRootedService{batchResult: rootedSignals(map[string]string{authorA: "Cape Town"})}
	handler := newRouterWithRooted(t, discardLogger(), service, &fakeStoriesService{}, &fakeVersionsService{},
		&fakeConversationsService{getBridgeResult: bridgeFixture(testBridgeID, authorA)})

	recorder := doStoryRequest(handler, http.MethodGet, "/bridges/"+testBridgeID, "", "")

	if recorder.Code != http.StatusOK {
		t.Fatalf("status = %d, want %d", recorder.Code, http.StatusOK)
	}

	var body bridgeEnvelope
	decodeBody(t, recorder, &body)

	if body.Bridge.AuthorRooted == nil || body.Bridge.AuthorRooted.Place != "Cape Town" {
		t.Errorf("author_rooted = %+v, want the bridger's summary", body.Bridge.AuthorRooted)
	}
	if service.batchCalls != 1 {
		t.Errorf("batch calls = %d, want 1", service.batchCalls)
	}
}

func TestEnrichmentFailureDoesNotFailTheResponse(t *testing.T) {
	service := &fakeRootedService{batchErr: errors.New("rooted table is unavailable")}
	handler := newRouterWithRooted(t, discardLogger(), service,
		&fakeStoriesService{getResult: storyFixture("d6b53a2c-2e2f-4a4d-9b0f-3f6f4e0f1a2b", authorA)},
		&fakeVersionsService{}, &fakeConversationsService{})

	recorder := doStoryRequest(handler, http.MethodGet, "/stories/d6b53a2c-2e2f-4a4d-9b0f-3f6f4e0f1a2b", "", "")

	if recorder.Code != http.StatusOK {
		t.Fatalf("status = %d, want %d — enrichment is supplementary", recorder.Code, http.StatusOK)
	}
	if !strings.Contains(recorder.Body.String(), `"author_rooted":null`) {
		t.Errorf("body = %s, want the story with a null author_rooted", recorder.Body.String())
	}
}
