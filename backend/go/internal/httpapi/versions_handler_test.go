package httpapi

import (
	"context"
	"errors"
	"fmt"
	"io"
	"log/slog"
	"net/http"
	"strings"
	"testing"
	"time"

	"github.com/knot/backend/internal/versions"
)

// Version ids used by the handler tests. They are canonical UUID text so the
// service's UUID validation accepts them.
const (
	testStoryID             = "33333333-3333-4333-8333-333333333333"
	testOtherStoryID        = "33333333-3333-4333-8333-333333333334"
	testRootVersionID       = "44444444-4444-4444-8444-444444444444"
	testAdaptationVersionID = "55555555-5555-4555-8555-555555555555"
)

// fakeVersionsService returns canned values, so the failure paths a real service
// cannot produce (an infrastructure error) can still be tested.
type fakeVersionsService struct {
	createResult versions.StoryVersion
	createErr    error
	getResult    versions.StoryVersion
	getErr       error
	treeResult   []versions.StoryVersion
	treeErr      error
}

func (f *fakeVersionsService) CreateAdaptation(_ context.Context, _ versions.CreateAdaptationInput) (versions.StoryVersion, error) {
	return f.createResult, f.createErr
}

func (f *fakeVersionsService) GetVersion(_ context.Context, _ string) (versions.StoryVersion, error) {
	return f.getResult, f.getErr
}

func (f *fakeVersionsService) GetTree(_ context.Context, _ string) ([]versions.StoryVersion, error) {
	return f.treeResult, f.treeErr
}

// memoryVersionStore is an in-memory versions.VersionStore. The handler tests run
// the real versions service over it rather than a stub service, so the tests
// cover the actual validation and parent-checking behaviour while staying free of
// a database.
type memoryVersionStore struct {
	versions []versions.StoryVersion
	// stories is the set of story ids that exist, so ListByStory can report "no
	// such story" the way PostgresStore does.
	stories map[string]bool
	err     error

	created     versions.StoryVersion
	createCalls int
	listCalls   int
}

func newMemoryVersionStore() *memoryVersionStore {
	return &memoryVersionStore{stories: make(map[string]bool)}
}

// seed inserts a version directly, as the migration's backfill and earlier
// requests would have left it.
func (m *memoryVersionStore) seed(version versions.StoryVersion) {
	m.versions = append(m.versions, version)
	m.stories[version.StoryID] = true
}

func (m *memoryVersionStore) CreateVersion(_ context.Context, version versions.StoryVersion) (versions.StoryVersion, error) {
	m.createCalls++
	m.created = version
	if m.err != nil {
		return versions.StoryVersion{}, m.err
	}

	created := version
	created.ID = fmt.Sprintf("00000000-0000-4000-a000-%012d", len(m.versions)+1)
	created.CreatedAt = testNow.Add(time.Duration(len(m.versions)) * time.Second)
	created.UpdatedAt = created.CreatedAt
	m.versions = append(m.versions, created)
	m.stories[created.StoryID] = true

	return created, nil
}

func (m *memoryVersionStore) GetVersion(_ context.Context, id string) (versions.StoryVersion, error) {
	for _, version := range m.versions {
		if version.ID == id {
			return version, nil
		}
	}
	return versions.StoryVersion{}, versions.ErrNotFound
}

func (m *memoryVersionStore) ListByStory(_ context.Context, storyID string) ([]versions.StoryVersion, error) {
	m.listCalls++
	if m.err != nil {
		return nil, m.err
	}
	if !m.stories[storyID] {
		return nil, versions.ErrNotFound
	}

	out := make([]versions.StoryVersion, 0)
	for _, version := range m.versions {
		if version.StoryID == storyID {
			out = append(out, version)
		}
	}
	return out, nil
}

// rootVersion is a well-formed root version for testStoryID.
func rootVersion() versions.StoryVersion {
	return versions.StoryVersion{
		ID:       testRootVersionID,
		StoryID:  testStoryID,
		AuthorID: testUserID,
		Language: "en",
		Title:    "The first rain",
		Body:     "Grandmother said the first rain remembers every name.",
	}
}

// newVersionsHandler returns the composed router with the real versions service
// over store.
func newVersionsHandler(t *testing.T, store versions.VersionStore) http.Handler {
	t.Helper()

	logger := slog.New(slog.NewTextHandler(io.Discard, nil))

	service, err := versions.NewService(store)
	if err != nil {
		t.Fatalf("versions.NewService() error = %v, want nil", err)
	}

	return newVersionsRouter(t, logger, service)
}

// newVersionsHandlerWithService is newVersionsHandler for a handler service that
// is not the real one.
func newVersionsHandlerWithService(t *testing.T, service VersionsService) http.Handler {
	t.Helper()
	return newVersionsRouter(t, slog.New(slog.NewTextHandler(io.Discard, nil)), service)
}

// newVersionsRouter assembles the full router the way cmd/knot does, so the tests
// exercise the real middleware chain and route table.
func newVersionsRouter(t *testing.T, logger *slog.Logger, service VersionsService) http.Handler {
	t.Helper()

	authHandler, err := NewAuthHandler(&fakeAuthService{}, logger)
	if err != nil {
		t.Fatalf("NewAuthHandler() error = %v, want nil", err)
	}

	storiesHandler, err := NewStoriesHandler(&fakeStoriesService{}, &fakeRootedService{}, logger)
	if err != nil {
		t.Fatalf("NewStoriesHandler() error = %v, want nil", err)
	}

	versionsHandler, err := NewVersionsHandler(service, &fakeRootedService{}, logger)
	if err != nil {
		t.Fatalf("NewVersionsHandler() error = %v, want nil", err)
	}

	conversationsHandler, err := NewConversationsHandler(&fakeConversationsService{}, &fakeRootedService{}, logger)
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

	router, err := NewRouter(authHandler, storiesHandler, versionsHandler, conversationsHandler, rootedHandler, authMiddleware, "0.1.0", logger)
	if err != nil {
		t.Fatalf("NewRouter() error = %v, want nil", err)
	}

	return router.Handler()
}

// validAdaptationBody is a request body that passes every validation rule,
// adapting the root version of testStoryID.
func validAdaptationBody() string {
	return `{
		"parent_version_id": "` + testRootVersionID + `",
		"language": "fr",
		"title": "La première pluie",
		"body": "Grand-mère disait que la première pluie se souvient de chaque nom.",
		"adaptation_note": "Rendered for French-speaking listeners."
	}`
}

func TestAdaptRequiresAuthentication(t *testing.T) {
	store := newMemoryVersionStore()
	handler := newVersionsHandler(t, store)

	recorder := doStoryRequest(handler, http.MethodPost, "/stories/"+testStoryID+"/adapt", validAdaptationBody(), "")

	if recorder.Code != http.StatusUnauthorized {
		t.Fatalf("status = %d, want %d", recorder.Code, http.StatusUnauthorized)
	}
	if code := decodedErrorCode(t, recorder); code != codeUnauthorized {
		t.Errorf("error code = %q, want %q", code, codeUnauthorized)
	}
	if store.createCalls != 0 {
		t.Errorf("store received %d create calls, want 0 — an unauthenticated request must not adapt", store.createCalls)
	}
}

func TestAdaptHappyPath(t *testing.T) {
	store := newMemoryVersionStore()
	store.seed(rootVersion())
	handler := newVersionsHandler(t, store)

	recorder := doStoryRequest(handler, http.MethodPost, "/stories/"+testStoryID+"/adapt", validAdaptationBody(), testAccessToken)

	if recorder.Code != http.StatusCreated {
		t.Fatalf("status = %d, want %d (body %s)", recorder.Code, http.StatusCreated, recorder.Body.String())
	}

	var body versionEnvelope
	decodeBody(t, recorder, &body)

	if body.Version.ID == "" {
		t.Error("id is empty, want the stored id")
	}
	if body.Version.StoryID != testStoryID {
		t.Errorf("story id = %q, want %q", body.Version.StoryID, testStoryID)
	}
	if body.Version.AuthorID != testUserID {
		t.Errorf("author id = %q, want the authenticated user %q", body.Version.AuthorID, testUserID)
	}
	if body.Version.ParentVersionID == nil || *body.Version.ParentVersionID != testRootVersionID {
		t.Errorf("parent version id = %v, want %q", body.Version.ParentVersionID, testRootVersionID)
	}
	if body.Version.Language != "fr" {
		t.Errorf("language = %q, want %q", body.Version.Language, "fr")
	}
	if body.Version.AdaptationNote == nil {
		t.Error("adaptation note = null, want the submitted note")
	}
	if store.created.AuthorID != testUserID {
		t.Errorf("store author id = %q, want %q", store.created.AuthorID, testUserID)
	}
}

func TestAdaptRejectsAuthorIDInBody(t *testing.T) {
	// The adapter comes from the token. A client that tries to set it explicitly
	// must be rejected rather than silently ignored.
	store := newMemoryVersionStore()
	store.seed(rootVersion())
	handler := newVersionsHandler(t, store)

	body := `{"parent_version_id":"` + testRootVersionID + `","language":"fr","title":"T","body":"B","author_id":"` + testUserID + `"}`
	recorder := doStoryRequest(handler, http.MethodPost, "/stories/"+testStoryID+"/adapt", body, testAccessToken)

	if recorder.Code != http.StatusBadRequest {
		t.Fatalf("status = %d, want %d", recorder.Code, http.StatusBadRequest)
	}
	if code := decodedErrorCode(t, recorder); code != codeInvalidRequest {
		t.Errorf("error code = %q, want %q", code, codeInvalidRequest)
	}
	if store.createCalls != 0 {
		t.Error("the store was called, want the body rejected before the service runs")
	}
}

func TestAdaptValidationIsBadRequest(t *testing.T) {
	tests := []struct {
		name string
		body string
	}{
		{
			name: "missing parent version id",
			body: `{"language":"fr","title":"T","body":"B"}`,
		},
		{
			name: "parent version id is not a uuid",
			body: `{"parent_version_id":"not-a-uuid","language":"fr","title":"T","body":"B"}`,
		},
		{
			name: "language too short",
			body: `{"parent_version_id":"` + testRootVersionID + `","language":"f","title":"T","body":"B"}`,
		},
		{
			name: "empty title",
			body: `{"parent_version_id":"` + testRootVersionID + `","language":"fr","title":"  ","body":"B"}`,
		},
		{
			name: "empty body",
			body: `{"parent_version_id":"` + testRootVersionID + `","language":"fr","title":"T","body":"   "}`,
		},
	}

	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			store := newMemoryVersionStore()
			store.seed(rootVersion())
			handler := newVersionsHandler(t, store)

			recorder := doStoryRequest(handler, http.MethodPost, "/stories/"+testStoryID+"/adapt", test.body, testAccessToken)

			if recorder.Code != http.StatusBadRequest {
				t.Fatalf("status = %d, want %d (body %s)", recorder.Code, http.StatusBadRequest, recorder.Body.String())
			}
			if code := decodedErrorCode(t, recorder); code != codeValidation {
				t.Errorf("error code = %q, want %q", code, codeValidation)
			}
			if store.createCalls != 0 {
				t.Errorf("store received %d create calls, want 0 — invalid input must not reach the store", store.createCalls)
			}
		})
	}
}

func TestAdaptRejectsUnreadableBodies(t *testing.T) {
	tests := []struct {
		name     string
		body     string
		wantCode int
	}{
		{name: "malformed json", body: `{"parent_version_id":`, wantCode: http.StatusBadRequest},
		{name: "empty body", body: "", wantCode: http.StatusBadRequest},
		{name: "oversized body", body: `{"title":"` + strings.Repeat("x", maxRequestBodyBytes) + `"}`, wantCode: http.StatusRequestEntityTooLarge},
	}

	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			handler := newVersionsHandler(t, newMemoryVersionStore())

			recorder := doStoryRequest(handler, http.MethodPost, "/stories/"+testStoryID+"/adapt", test.body, testAccessToken)

			if recorder.Code != test.wantCode {
				t.Fatalf("status = %d, want %d", recorder.Code, test.wantCode)
			}
		})
	}
}

func TestAdaptParentVersionNotFound(t *testing.T) {
	// A well-formed parent id that names no version is a 404, not a 400.
	store := newMemoryVersionStore()
	store.stories[testStoryID] = true
	handler := newVersionsHandler(t, store)

	body := `{"parent_version_id":"99999999-9999-4999-8999-999999999999","language":"fr","title":"T","body":"B"}`
	recorder := doStoryRequest(handler, http.MethodPost, "/stories/"+testStoryID+"/adapt", body, testAccessToken)

	if recorder.Code != http.StatusNotFound {
		t.Fatalf("status = %d, want %d (body %s)", recorder.Code, http.StatusNotFound, recorder.Body.String())
	}
	if code := decodedErrorCode(t, recorder); code != codeNotFound {
		t.Errorf("error code = %q, want %q", code, codeNotFound)
	}
}

func TestAdaptParentFromAnotherStoryIsBadRequest(t *testing.T) {
	// The parent must belong to the story in the path. A version of a different
	// story is a validation failure, not a 404.
	store := newMemoryVersionStore()
	store.seed(rootVersion()) // root belongs to testStoryID
	handler := newVersionsHandler(t, store)

	// Adapt testOtherStoryID, but name testStoryID's root as the parent.
	store.stories[testOtherStoryID] = true
	recorder := doStoryRequest(handler, http.MethodPost, "/stories/"+testOtherStoryID+"/adapt", validAdaptationBody(), testAccessToken)

	if recorder.Code != http.StatusBadRequest {
		t.Fatalf("status = %d, want %d (body %s)", recorder.Code, http.StatusBadRequest, recorder.Body.String())
	}
	if code := decodedErrorCode(t, recorder); code != codeValidation {
		t.Errorf("error code = %q, want %q", code, codeValidation)
	}
	if store.createCalls != 0 {
		t.Errorf("store received %d create calls, want 0 — a cross-story parent must not be stored", store.createCalls)
	}
}

func TestAdaptUnexpectedFailureIsInternalError(t *testing.T) {
	handler := newVersionsHandlerWithService(t, &fakeVersionsService{createErr: errors.New("connection reset")})

	recorder := doStoryRequest(handler, http.MethodPost, "/stories/"+testStoryID+"/adapt", validAdaptationBody(), testAccessToken)

	if recorder.Code != http.StatusInternalServerError {
		t.Fatalf("status = %d, want %d", recorder.Code, http.StatusInternalServerError)
	}
	if code := decodedErrorCode(t, recorder); code != codeInternal {
		t.Errorf("error code = %q, want %q", code, codeInternal)
	}
	if strings.Contains(recorder.Body.String(), "connection reset") {
		t.Error("the response echoes the internal error, want a generic message")
	}
}

func TestTreeHappyPath(t *testing.T) {
	store := newMemoryVersionStore()
	store.seed(rootVersion())
	store.seed(versions.StoryVersion{
		ID:              testAdaptationVersionID,
		StoryID:         testStoryID,
		ParentVersionID: testRootVersionID,
		AuthorID:        testUserID,
		Language:        "fr",
		Title:           "La première pluie",
		Body:            "Grand-mère disait que la première pluie se souvient de chaque nom.",
	})
	handler := newVersionsHandler(t, store)

	recorder := doStoryRequest(handler, http.MethodGet, "/stories/"+testStoryID+"/tree", "", "")

	if recorder.Code != http.StatusOK {
		t.Fatalf("status = %d, want %d (body %s)", recorder.Code, http.StatusOK, recorder.Body.String())
	}

	var body treeResponse
	decodeBody(t, recorder, &body)

	if body.StoryID != testStoryID {
		t.Errorf("story id = %q, want %q", body.StoryID, testStoryID)
	}
	if len(body.Versions) != 2 {
		t.Fatalf("len(versions) = %d, want 2", len(body.Versions))
	}
	if body.Versions[0].ParentVersionID != nil {
		t.Error("the root version has a parent, want null")
	}
	if body.Versions[1].ParentVersionID == nil || *body.Versions[1].ParentVersionID != testRootVersionID {
		t.Errorf("the adaptation's parent = %v, want %q", body.Versions[1].ParentVersionID, testRootVersionID)
	}
}

func TestTreeStoryNotFound(t *testing.T) {
	handler := newVersionsHandler(t, newMemoryVersionStore())

	recorder := doStoryRequest(handler, http.MethodGet, "/stories/"+testStoryID+"/tree", "", "")

	if recorder.Code != http.StatusNotFound {
		t.Fatalf("status = %d, want %d", recorder.Code, http.StatusNotFound)
	}
	if code := decodedErrorCode(t, recorder); code != codeNotFound {
		t.Errorf("error code = %q, want %q", code, codeNotFound)
	}
}

func TestTreeMalformedStoryIDIsNotFound(t *testing.T) {
	store := newMemoryVersionStore()
	handler := newVersionsHandler(t, store)

	recorder := doStoryRequest(handler, http.MethodGet, "/stories/not-a-uuid/tree", "", "")

	if recorder.Code != http.StatusNotFound {
		t.Errorf("status = %d, want %d", recorder.Code, http.StatusNotFound)
	}
	if store.listCalls != 0 {
		t.Errorf("store received %d list calls, want 0 — a malformed id cannot name a story", store.listCalls)
	}
}

func TestTreeUnexpectedFailureIsInternalError(t *testing.T) {
	handler := newVersionsHandlerWithService(t, &fakeVersionsService{treeErr: errors.New("connection reset")})

	recorder := doStoryRequest(handler, http.MethodGet, "/stories/"+testStoryID+"/tree", "", "")

	if recorder.Code != http.StatusInternalServerError {
		t.Fatalf("status = %d, want %d", recorder.Code, http.StatusInternalServerError)
	}
	if code := decodedErrorCode(t, recorder); code != codeInternal {
		t.Errorf("error code = %q, want %q", code, codeInternal)
	}
}

func TestGetVersionHappyPath(t *testing.T) {
	store := newMemoryVersionStore()
	store.seed(rootVersion())
	handler := newVersionsHandler(t, store)

	recorder := doStoryRequest(handler, http.MethodGet, "/versions/"+testRootVersionID, "", "")

	if recorder.Code != http.StatusOK {
		t.Fatalf("status = %d, want %d (body %s)", recorder.Code, http.StatusOK, recorder.Body.String())
	}

	var body versionEnvelope
	decodeBody(t, recorder, &body)

	if body.Version.ID != testRootVersionID {
		t.Errorf("id = %q, want %q", body.Version.ID, testRootVersionID)
	}
	if body.Version.ParentVersionID != nil {
		t.Error("the root version has a parent, want null")
	}
	if body.Version.Body == "" {
		t.Error("body is empty, want the stored story text")
	}
}

func TestGetVersionNotFound(t *testing.T) {
	handler := newVersionsHandler(t, newMemoryVersionStore())

	recorder := doStoryRequest(handler, http.MethodGet, "/versions/99999999-9999-4999-8999-999999999999", "", "")

	if recorder.Code != http.StatusNotFound {
		t.Fatalf("status = %d, want %d", recorder.Code, http.StatusNotFound)
	}
	if code := decodedErrorCode(t, recorder); code != codeNotFound {
		t.Errorf("error code = %q, want %q", code, codeNotFound)
	}
}

func TestGetVersionUnexpectedFailureIsInternalError(t *testing.T) {
	handler := newVersionsHandlerWithService(t, &fakeVersionsService{getErr: errors.New("connection reset")})

	recorder := doStoryRequest(handler, http.MethodGet, "/versions/"+testRootVersionID, "", "")

	if recorder.Code != http.StatusInternalServerError {
		t.Fatalf("status = %d, want %d", recorder.Code, http.StatusInternalServerError)
	}
	if code := decodedErrorCode(t, recorder); code != codeInternal {
		t.Errorf("error code = %q, want %q", code, codeInternal)
	}
}

func TestNewVersionsHandlerRejectsMissingDependencies(t *testing.T) {
	logger := slog.New(slog.NewTextHandler(io.Discard, nil))

	if _, err := NewVersionsHandler(nil, &fakeRootedService{}, logger); err == nil {
		t.Error("NewVersionsHandler(nil, rooted, logger) error = nil, want an error")
	}
	if _, err := NewVersionsHandler(&fakeVersionsService{}, &fakeRootedService{}, nil); err == nil {
		t.Error("NewVersionsHandler(service, rooted, nil) error = nil, want an error")
	}
	if _, err := NewVersionsHandler(&fakeVersionsService{}, nil, logger); err == nil {
		t.Error("NewVersionsHandler(service, nil, logger) error = nil, want an error")
	}
}

// ensure the fake satisfies the handler's contract at compile time.
var _ VersionsService = (*fakeVersionsService)(nil)

// ensure the in-memory store satisfies the domain contract at compile time.
var _ versions.VersionStore = (*memoryVersionStore)(nil)
