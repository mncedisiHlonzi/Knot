package httpapi

import (
	"context"
	"errors"
	"fmt"
	"io"
	"log/slog"
	"net/http"
	"sort"
	"strings"
	"testing"
	"time"

	"github.com/knot/backend/internal/conversations"
)

// Ids used by the conversation handler tests. They are canonical UUID text so
// the service's UUID validation accepts them.
const (
	testVersionID       = "44444444-4444-4444-8444-444444444444"
	testFrenchVersionID = "44444444-4444-4444-8444-444444444445"
	testSourceCommentID = "66666666-6666-4666-8666-666666666666"
	testReplyCommentID  = "66666666-6666-4666-8666-666666666667"
	testTargetCommentID = "88888888-8888-4888-8888-888888888888"
	testBridgeID        = "77777777-7777-4777-8777-777777777777"
)

// fakeConversationsService returns canned values, so the failure paths a real
// service cannot produce (an infrastructure error) can still be tested.
type fakeConversationsService struct {
	createCommentResult conversations.Comment
	createCommentErr    error

	listCommentsResult []conversations.Comment
	listCommentsNext   string
	listCommentsErr    error

	getCommentResult conversations.Comment
	getCommentErr    error

	createBridgeBridge conversations.Bridge
	createBridgeSource conversations.Comment
	createBridgeTarget conversations.Comment
	createBridgeErr    error

	getBridgeResult conversations.Bridge
	getBridgeErr    error

	listBridgesResult []conversations.Bridge
	listBridgesErr    error
}

func (f *fakeConversationsService) CreateComment(_ context.Context, _ conversations.CreateCommentInput) (conversations.Comment, error) {
	return f.createCommentResult, f.createCommentErr
}

func (f *fakeConversationsService) GetComment(_ context.Context, _ string) (conversations.Comment, error) {
	return f.getCommentResult, f.getCommentErr
}

func (f *fakeConversationsService) ListComments(_ context.Context, _ string, _ string, _ int) ([]conversations.Comment, string, error) {
	return f.listCommentsResult, f.listCommentsNext, f.listCommentsErr
}

func (f *fakeConversationsService) CreateBridge(_ context.Context, _ conversations.CreateBridgeInput) (conversations.Bridge, conversations.Comment, conversations.Comment, error) {
	return f.createBridgeBridge, f.createBridgeSource, f.createBridgeTarget, f.createBridgeErr
}

func (f *fakeConversationsService) GetBridge(_ context.Context, _ string) (conversations.Bridge, error) {
	return f.getBridgeResult, f.getBridgeErr
}

func (f *fakeConversationsService) ListBridgesForComment(_ context.Context, _ string) ([]conversations.Bridge, error) {
	return f.listBridgesResult, f.listBridgesErr
}

// memoryVersion is what the in-memory store knows about a story version: the
// story it belongs to and the language it is written in. It is what resolving a
// bridge's target version needs.
type memoryVersion struct {
	storyID  string
	language string
}

// memoryConversationsStore is an in-memory conversations.CommentStore and
// conversations.BridgeStore. The handler tests run the real conversations
// service over it rather than a stub service, so the tests cover the actual
// validation, the same-language rule, and target-version resolution while
// staying free of a database.
type memoryConversationsStore struct {
	comments []conversations.Comment
	bridges  []conversations.Bridge
	// versions is the set of version ids that exist, so comment creation and
	// listing can report "no such version" the way PostgresStore does.
	versions map[string]bool
	// versionMeta describes each seeded version enough to resolve a target
	// version. seedComment registers its comment's version with testStoryID.
	versionMeta map[string]memoryVersion
	// versionAuthors is the author of each seeded version, so the conversations
	// service can tell a version's author that someone commented on it.
	versionAuthors map[string]string
	err            error

	createCommentCalls int
}

func newMemoryConversationsStore() *memoryConversationsStore {
	return &memoryConversationsStore{
		versions:       make(map[string]bool),
		versionMeta:    make(map[string]memoryVersion),
		versionAuthors: make(map[string]string),
	}
}

// seedVersion registers a story version, so a bridge into its language can be
// resolved. It also marks the version as existing for comment operations.
func (m *memoryConversationsStore) seedVersion(versionID, storyID, language string) {
	m.versions[versionID] = true
	m.versionMeta[versionID] = memoryVersion{storyID: storyID, language: language}
}

// seedComment inserts a comment directly, as earlier requests would have left
// it, and registers its version on testStoryID so bridging it can be resolved.
func (m *memoryConversationsStore) seedComment(comment conversations.Comment) {
	m.comments = append(m.comments, comment)
	m.seedVersion(comment.VersionID, testStoryID, comment.Language)
}

// seedVersionAuthor records who authored a seeded version. A version with no
// recorded author is not found by VersionAuthor, which mirrors a version that
// does not exist.
func (m *memoryConversationsStore) seedVersionAuthor(versionID, authorID string) {
	m.versionAuthors[versionID] = authorID
}

// VersionAuthor returns the author of a seeded version, or ErrNotFound.
func (m *memoryConversationsStore) VersionAuthor(_ context.Context, versionID string) (string, error) {
	authorID, ok := m.versionAuthors[versionID]
	if !ok {
		return "", conversations.ErrNotFound
	}
	return authorID, nil
}

func (m *memoryConversationsStore) CreateComment(_ context.Context, comment conversations.Comment) (conversations.Comment, error) {
	m.createCommentCalls++
	if m.err != nil {
		return conversations.Comment{}, m.err
	}
	if !m.versions[comment.VersionID] {
		return conversations.Comment{}, conversations.ErrNotFound
	}

	created := comment
	created.ID = fmt.Sprintf("00000000-0000-4000-c000-%012d", len(m.comments)+1)
	created.CreatedAt = testNow.Add(time.Duration(len(m.comments)) * time.Second)
	created.UpdatedAt = created.CreatedAt
	m.comments = append(m.comments, created)

	return created, nil
}

func (m *memoryConversationsStore) GetComment(_ context.Context, id string) (conversations.Comment, error) {
	for _, comment := range m.comments {
		if comment.ID == id {
			// Resolve the story the comment's version belongs to, exactly as the
			// SQL store's JOIN does, so GET /comments/{id} can name it.
			if version, ok := m.versionMeta[comment.VersionID]; ok {
				comment.StoryID = version.storyID
			}
			return comment, nil
		}
	}
	return conversations.Comment{}, conversations.ErrNotFound
}

func (m *memoryConversationsStore) ListComments(_ context.Context, versionID string, cursor *conversations.Cursor, limit int) ([]conversations.Comment, *conversations.Cursor, error) {
	if m.err != nil {
		return nil, nil, m.err
	}
	if !m.versions[versionID] {
		return nil, nil, conversations.ErrNotFound
	}

	ordered := make([]conversations.Comment, 0)
	for _, comment := range m.comments {
		// Only top-level comments are paged; replies come from ListReplies, so a
		// page of threads is never missing a parent.
		if comment.VersionID == versionID && comment.ParentCommentID == nil {
			ordered = append(ordered, comment)
		}
	}
	sort.SliceStable(ordered, func(i, j int) bool {
		if !ordered[i].CreatedAt.Equal(ordered[j].CreatedAt) {
			return ordered[i].CreatedAt.After(ordered[j].CreatedAt)
		}
		return ordered[i].ID > ordered[j].ID
	})

	start := 0
	if cursor != nil {
		for start < len(ordered) && !beforeCommentCursor(ordered[start], *cursor) {
			start++
		}
	}

	remaining := ordered[start:]
	if len(remaining) <= limit {
		return remaining, nil, nil
	}

	last := remaining[limit-1]
	next := conversations.NewCursor(last.CreatedAt, last.ID)

	return remaining[:limit], &next, nil
}

// ListReplies returns the replies to the given parents, oldest first within each
// parent, mirroring the SQL store's ordering so the service assembles the same
// thread either way.
func (m *memoryConversationsStore) ListReplies(_ context.Context, parentIDs []string) ([]conversations.Comment, error) {
	if m.err != nil {
		return nil, m.err
	}

	wanted := make(map[string]bool, len(parentIDs))
	for _, id := range parentIDs {
		wanted[id] = true
	}

	replies := make([]conversations.Comment, 0)
	for _, comment := range m.comments {
		if comment.ParentCommentID != nil && wanted[*comment.ParentCommentID] {
			replies = append(replies, comment)
		}
	}
	sort.SliceStable(replies, func(i, j int) bool {
		if !replies[i].CreatedAt.Equal(replies[j].CreatedAt) {
			return replies[i].CreatedAt.Before(replies[j].CreatedAt)
		}
		return replies[i].ID < replies[j].ID
	})

	return replies, nil
}

// beforeCommentCursor reports whether comment sorts strictly after cursor in the
// thread order (created_at DESC, id DESC), i.e. whether it belongs on a later page.
func beforeCommentCursor(comment conversations.Comment, cursor conversations.Cursor) bool {
	if comment.CreatedAt.Before(cursor.CreatedAt()) {
		return true
	}
	if comment.CreatedAt.After(cursor.CreatedAt()) {
		return false
	}
	return comment.ID < cursor.ID()
}

func (m *memoryConversationsStore) CreateBridge(_ context.Context, target conversations.Comment, sourceCommentID, adaptationNote string) (conversations.Bridge, conversations.Comment, error) {
	if m.err != nil {
		return conversations.Bridge{}, conversations.Comment{}, m.err
	}
	for _, bridge := range m.bridges {
		if bridge.SourceCommentID == sourceCommentID && bridge.TargetLanguage == target.Language {
			return conversations.Bridge{}, conversations.Comment{}, conversations.ErrAlreadyBridged
		}
	}

	createdTarget := target
	createdTarget.ID = fmt.Sprintf("00000000-0000-4000-c000-%012d", len(m.comments)+1)
	createdTarget.CreatedAt = testNow.Add(time.Duration(len(m.comments)) * time.Second)
	createdTarget.UpdatedAt = createdTarget.CreatedAt
	m.comments = append(m.comments, createdTarget)

	bridge := conversations.Bridge{
		ID:              fmt.Sprintf("00000000-0000-4000-b000-%012d", len(m.bridges)+1),
		SourceCommentID: sourceCommentID,
		TargetCommentID: createdTarget.ID,
		AuthorID:        target.AuthorID,
		TargetLanguage:  target.Language,
		AdaptationNote:  adaptationNote,
		CreatedAt:       createdTarget.CreatedAt,
	}
	m.bridges = append(m.bridges, bridge)

	return bridge, createdTarget, nil
}

func (m *memoryConversationsStore) GetBridge(_ context.Context, id string) (conversations.Bridge, error) {
	for _, bridge := range m.bridges {
		if bridge.ID == id {
			return bridge, nil
		}
	}
	return conversations.Bridge{}, conversations.ErrNotFound
}

func (m *memoryConversationsStore) ListBridgesForComment(_ context.Context, commentID string) ([]conversations.Bridge, error) {
	found := false
	for _, comment := range m.comments {
		if comment.ID == commentID {
			found = true
			break
		}
	}
	if !found {
		return nil, conversations.ErrNotFound
	}

	out := make([]conversations.Bridge, 0)
	for _, bridge := range m.bridges {
		if bridge.SourceCommentID == commentID || bridge.TargetCommentID == commentID {
			out = append(out, bridge)
		}
	}
	return out, nil
}

func (m *memoryConversationsStore) ListBridgesForStory(_ context.Context, _ string) ([]conversations.Bridge, error) {
	return []conversations.Bridge{}, nil
}

// FindTargetVersion returns the version of the source version's story written in
// targetLanguage. When several match, the smallest id is returned, mirroring the
// SQL store's deterministic ORDER BY closely enough for the handler tests.
func (m *memoryConversationsStore) FindTargetVersion(_ context.Context, sourceVersionID, targetLanguage string) (string, error) {
	source, ok := m.versionMeta[sourceVersionID]
	if !ok {
		return "", conversations.ErrNotFound
	}

	matches := make([]string, 0)
	for versionID, version := range m.versionMeta {
		if version.storyID == source.storyID && version.language == targetLanguage {
			matches = append(matches, versionID)
		}
	}
	if len(matches) == 0 {
		return "", conversations.ErrNotFound
	}

	sort.Strings(matches)
	return matches[0], nil
}

// newConversationsHandler returns the composed router with the real
// conversations service over store.
func newConversationsHandler(t *testing.T, store *memoryConversationsStore) http.Handler {
	t.Helper()

	logger := slog.New(slog.NewTextHandler(io.Discard, nil))

	service, err := conversations.NewService(store, store, &fakeNotifier{})
	if err != nil {
		t.Fatalf("conversations.NewService() error = %v, want nil", err)
	}

	return newConversationsRouter(t, logger, service)
}

// newConversationsHandlerWithService is newConversationsHandler for a handler
// service that is not the real one.
func newConversationsHandlerWithService(t *testing.T, service ConversationsService) http.Handler {
	t.Helper()
	return newConversationsRouter(t, slog.New(slog.NewTextHandler(io.Discard, nil)), service)
}

// newConversationsRouter assembles the full router the way cmd/knot does, so the
// tests exercise the real middleware chain and route table.
func newConversationsRouter(t *testing.T, logger *slog.Logger, service ConversationsService) http.Handler {
	t.Helper()

	authHandler, err := NewAuthHandler(&fakeAuthService{}, logger)
	if err != nil {
		t.Fatalf("NewAuthHandler() error = %v, want nil", err)
	}

	storiesHandler, err := NewStoriesHandler(&fakeStoriesService{}, &fakeAuthorService{}, &fakeRootedService{}, &fakeStoryMediaLookup{}, logger)
	if err != nil {
		t.Fatalf("NewStoriesHandler() error = %v, want nil", err)
	}

	versionsHandler, err := NewVersionsHandler(&fakeVersionsService{}, &fakeAuthorService{}, &fakeRootedService{}, logger)
	if err != nil {
		t.Fatalf("NewVersionsHandler() error = %v, want nil", err)
	}

	conversationsHandler, err := NewConversationsHandler(service, &fakeAuthorService{}, &fakeRootedService{}, logger)
	if err != nil {
		t.Fatalf("NewConversationsHandler() error = %v, want nil", err)
	}

	rootedHandler, err := NewRootedHandler(&fakeRootedService{}, logger)
	if err != nil {
		t.Fatalf("NewRootedHandler() error = %v, want nil", err)
	}

	discoveryHandler, err := NewDiscoveryHandler(fakeDiscoveryService{}, &fakeRootedService{}, logger)
	if err != nil {
		t.Fatalf("NewDiscoveryHandler() error = %v, want nil", err)
	}

	authMiddleware, err := NewAuthMiddleware(&fakeTokenParser{subject: testUserID}, logger)
	if err != nil {
		t.Fatalf("NewAuthMiddleware() error = %v, want nil", err)
	}

	router, err := NewRouter(authHandler, storiesHandler, versionsHandler, conversationsHandler, rootedHandler, discoveryHandler, newTestAvatarHandler(t, logger), newTestStoryMediaHandler(t, logger), newTestNotificationsHandler(t, logger), newTestProfileHandler(t, logger), authMiddleware, "0.1.0", logger)
	if err != nil {
		t.Fatalf("NewRouter() error = %v, want nil", err)
	}

	return router.Handler()
}

// englishComment is a well-formed comment on testVersionID.
func englishComment() conversations.Comment {
	return conversations.Comment{
		ID:        testSourceCommentID,
		VersionID: testVersionID,
		AuthorID:  testUserID,
		Language:  "eng",
		Body:      "The first rain remembers every name.",
		CreatedAt: testNow,
		UpdatedAt: testNow,
	}
}

// validCommentBody is a request body that passes every comment validation rule.
func validCommentBody() string {
	return `{"body":"The first rain remembers every name.","language":"eng"}`
}

// validBridgeBody is a request body that passes every bridge validation rule.
func validBridgeBody() string {
	return `{
		"target_language": "fra",
		"body": "La première pluie se souvient de chaque nom.",
		"adaptation_note": "Rendered for French-speaking listeners."
	}`
}

func TestCreateCommentRequiresAuthentication(t *testing.T) {
	store := newMemoryConversationsStore()
	store.versions[testVersionID] = true
	handler := newConversationsHandler(t, store)

	recorder := doStoryRequest(handler, http.MethodPost, "/versions/"+testVersionID+"/comments", validCommentBody(), "")

	if recorder.Code != http.StatusUnauthorized {
		t.Fatalf("status = %d, want %d", recorder.Code, http.StatusUnauthorized)
	}
	if code := decodedErrorCode(t, recorder); code != codeUnauthorized {
		t.Errorf("error code = %q, want %q", code, codeUnauthorized)
	}
	if store.createCommentCalls != 0 {
		t.Errorf("store received %d create calls, want 0 — an unauthenticated request must not comment", store.createCommentCalls)
	}
}

func TestCreateCommentHappyPath(t *testing.T) {
	store := newMemoryConversationsStore()
	store.versions[testVersionID] = true
	handler := newConversationsHandler(t, store)

	recorder := doStoryRequest(handler, http.MethodPost, "/versions/"+testVersionID+"/comments", validCommentBody(), testAccessToken)

	if recorder.Code != http.StatusCreated {
		t.Fatalf("status = %d, want %d (body %s)", recorder.Code, http.StatusCreated, recorder.Body.String())
	}

	var body commentEnvelope
	decodeBody(t, recorder, &body)

	if body.Comment.ID == "" {
		t.Error("id is empty, want the stored id")
	}
	if body.Comment.VersionID != testVersionID {
		t.Errorf("version id = %q, want %q", body.Comment.VersionID, testVersionID)
	}
	if body.Comment.AuthorID != testUserID {
		t.Errorf("author id = %q, want the authenticated user %q", body.Comment.AuthorID, testUserID)
	}
	if body.Comment.Language != "eng" {
		t.Errorf("language = %q, want %q", body.Comment.Language, "eng")
	}
}

func TestCreateCommentRejectsAuthorIDInBody(t *testing.T) {
	store := newMemoryConversationsStore()
	store.versions[testVersionID] = true
	handler := newConversationsHandler(t, store)

	body := `{"body":"B","language":"eng","author_id":"` + testUserID + `"}`
	recorder := doStoryRequest(handler, http.MethodPost, "/versions/"+testVersionID+"/comments", body, testAccessToken)

	if recorder.Code != http.StatusBadRequest {
		t.Fatalf("status = %d, want %d", recorder.Code, http.StatusBadRequest)
	}
	if code := decodedErrorCode(t, recorder); code != codeInvalidRequest {
		t.Errorf("error code = %q, want %q", code, codeInvalidRequest)
	}
	if store.createCommentCalls != 0 {
		t.Error("the store was called, want the body rejected before the service runs")
	}
}

func TestCreateCommentValidationIsBadRequest(t *testing.T) {
	tests := []struct {
		name string
		body string
	}{
		{name: "empty body", body: `{"body":"   ","language":"eng"}`},
		{name: "missing language", body: `{"body":"B","language":""}`},
		{name: "bad language", body: `{"body":"B","language":"e"}`},
		{name: "two letter language", body: `{"body":"B","language":"en"}`},
	}

	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			store := newMemoryConversationsStore()
			store.versions[testVersionID] = true
			handler := newConversationsHandler(t, store)

			recorder := doStoryRequest(handler, http.MethodPost, "/versions/"+testVersionID+"/comments", test.body, testAccessToken)

			if recorder.Code != http.StatusBadRequest {
				t.Fatalf("status = %d, want %d (body %s)", recorder.Code, http.StatusBadRequest, recorder.Body.String())
			}
			if code := decodedErrorCode(t, recorder); code != codeValidation {
				t.Errorf("error code = %q, want %q", code, codeValidation)
			}
		})
	}
}

func TestCreateCommentVersionNotFound(t *testing.T) {
	// A well-formed version id that names no version is a 404, not a 400.
	handler := newConversationsHandler(t, newMemoryConversationsStore())

	recorder := doStoryRequest(handler, http.MethodPost, "/versions/"+testVersionID+"/comments", validCommentBody(), testAccessToken)

	if recorder.Code != http.StatusNotFound {
		t.Fatalf("status = %d, want %d (body %s)", recorder.Code, http.StatusNotFound, recorder.Body.String())
	}
	if code := decodedErrorCode(t, recorder); code != codeNotFound {
		t.Errorf("error code = %q, want %q", code, codeNotFound)
	}
}

func TestCreateCommentMalformedVersionIDIsNotFound(t *testing.T) {
	store := newMemoryConversationsStore()
	handler := newConversationsHandler(t, store)

	recorder := doStoryRequest(handler, http.MethodPost, "/versions/not-a-uuid/comments", validCommentBody(), testAccessToken)

	if recorder.Code != http.StatusNotFound {
		t.Errorf("status = %d, want %d", recorder.Code, http.StatusNotFound)
	}
	if store.createCommentCalls != 0 {
		t.Errorf("store received %d create calls, want 0 — a malformed id cannot name a version", store.createCommentCalls)
	}
}

func TestCreateCommentRejectsUnreadableBodies(t *testing.T) {
	tests := []struct {
		name     string
		body     string
		wantCode int
	}{
		{name: "malformed json", body: `{"body":`, wantCode: http.StatusBadRequest},
		{name: "empty body", body: "", wantCode: http.StatusBadRequest},
		{name: "oversized body", body: `{"body":"` + strings.Repeat("x", maxRequestBodyBytes) + `"}`, wantCode: http.StatusRequestEntityTooLarge},
	}

	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			handler := newConversationsHandler(t, newMemoryConversationsStore())

			recorder := doStoryRequest(handler, http.MethodPost, "/versions/"+testVersionID+"/comments", test.body, testAccessToken)

			if recorder.Code != test.wantCode {
				t.Fatalf("status = %d, want %d", recorder.Code, test.wantCode)
			}
		})
	}
}

func TestCreateCommentUnexpectedFailureIsInternalError(t *testing.T) {
	handler := newConversationsHandlerWithService(t, &fakeConversationsService{createCommentErr: errors.New("connection reset")})

	recorder := doStoryRequest(handler, http.MethodPost, "/versions/"+testVersionID+"/comments", validCommentBody(), testAccessToken)

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

func TestListCommentsHappyPath(t *testing.T) {
	store := newMemoryConversationsStore()
	store.seedComment(englishComment())
	handler := newConversationsHandler(t, store)

	recorder := doStoryRequest(handler, http.MethodGet, "/versions/"+testVersionID+"/comments", "", "")

	if recorder.Code != http.StatusOK {
		t.Fatalf("status = %d, want %d (body %s)", recorder.Code, http.StatusOK, recorder.Body.String())
	}

	var body commentListResponse
	decodeBody(t, recorder, &body)

	if len(body.Comments) != 1 {
		t.Fatalf("len(comments) = %d, want 1", len(body.Comments))
	}
	if body.Comments[0].ID != testSourceCommentID {
		t.Errorf("comment id = %q, want %q", body.Comments[0].ID, testSourceCommentID)
	}
	if body.NextCursor != "" {
		t.Errorf("next cursor = %q, want an empty string on the last page", body.NextCursor)
	}
}

func TestListCommentsEmptyThreadIsAnEmptyArray(t *testing.T) {
	store := newMemoryConversationsStore()
	store.versions[testVersionID] = true
	handler := newConversationsHandler(t, store)

	recorder := doStoryRequest(handler, http.MethodGet, "/versions/"+testVersionID+"/comments", "", "")

	if recorder.Code != http.StatusOK {
		t.Fatalf("status = %d, want %d", recorder.Code, http.StatusOK)
	}
	if !strings.Contains(recorder.Body.String(), `"comments":[]`) {
		t.Errorf("body = %s, want it to contain an empty array", recorder.Body.String())
	}
}

// replyCommentBody is a request body that replies to the comment with parentID.
func replyCommentBody(parentID string) string {
	return `{"body":"It does.","language":"eng","parent_comment_id":"` + parentID + `"}`
}

// commentParentID returns a pointer to id, for building a seeded reply.
func commentParentID(id string) *string {
	return &id
}

func TestCreateCommentReplyHappyPath(t *testing.T) {
	store := newMemoryConversationsStore()
	store.versions[testVersionID] = true
	store.seedComment(englishComment())
	handler := newConversationsHandler(t, store)

	recorder := doStoryRequest(handler, http.MethodPost, "/versions/"+testVersionID+"/comments",
		replyCommentBody(testSourceCommentID), testAccessToken)

	if recorder.Code != http.StatusCreated {
		t.Fatalf("status = %d, want %d (body %s)", recorder.Code, http.StatusCreated, recorder.Body.String())
	}

	var body commentEnvelope
	decodeBody(t, recorder, &body)

	if body.Comment.ParentCommentID == nil {
		t.Fatal("parent_comment_id = null, want the comment being replied to")
	}
	if *body.Comment.ParentCommentID != testSourceCommentID {
		t.Errorf("parent_comment_id = %q, want %q", *body.Comment.ParentCommentID, testSourceCommentID)
	}
	if body.Comment.VersionID != testVersionID {
		t.Errorf("version id = %q, want %q", body.Comment.VersionID, testVersionID)
	}
}

func TestCreateCommentTopLevelHasANullParent(t *testing.T) {
	store := newMemoryConversationsStore()
	store.versions[testVersionID] = true
	handler := newConversationsHandler(t, store)

	recorder := doStoryRequest(handler, http.MethodPost, "/versions/"+testVersionID+"/comments",
		validCommentBody(), testAccessToken)

	if recorder.Code != http.StatusCreated {
		t.Fatalf("status = %d, want %d (body %s)", recorder.Code, http.StatusCreated, recorder.Body.String())
	}

	var body commentEnvelope
	decodeBody(t, recorder, &body)

	if body.Comment.ParentCommentID != nil {
		t.Errorf("parent_comment_id = %q, want null for a top-level comment", *body.Comment.ParentCommentID)
	}
	// The field is always present, so a client can tell "top level" from
	// "the server did not answer the question".
	if !strings.Contains(recorder.Body.String(), `"parent_comment_id":null`) {
		t.Errorf("body = %s, want an explicit null parent_comment_id", recorder.Body.String())
	}
}

func TestCreateCommentReplyRejectsAnUnknownParent(t *testing.T) {
	store := newMemoryConversationsStore()
	store.versions[testVersionID] = true
	handler := newConversationsHandler(t, store)

	const missing = "99999999-9999-4999-8999-999999999999"
	recorder := doStoryRequest(handler, http.MethodPost, "/versions/"+testVersionID+"/comments",
		replyCommentBody(missing), testAccessToken)

	if recorder.Code != http.StatusBadRequest {
		t.Fatalf("status = %d, want %d (body %s)", recorder.Code, http.StatusBadRequest, recorder.Body.String())
	}
	if code := decodedErrorCode(t, recorder); code != codeValidation {
		t.Errorf("error code = %q, want %q", code, codeValidation)
	}
	if store.createCommentCalls != 0 {
		t.Errorf("store received %d create calls, want 0: an unknown parent is refused before anything is written",
			store.createCommentCalls)
	}
}

func TestCreateCommentReplyRejectsAParentOnAnotherVersion(t *testing.T) {
	// A reply belongs to the conversation it is written in, so a parent from a
	// different version is refused rather than silently re-pointed.
	store := newMemoryConversationsStore()
	store.versions[testVersionID] = true

	elsewhere := englishComment()
	elsewhere.ID = testTargetCommentID
	elsewhere.VersionID = testFrenchVersionID
	store.seedComment(elsewhere)
	handler := newConversationsHandler(t, store)

	recorder := doStoryRequest(handler, http.MethodPost, "/versions/"+testVersionID+"/comments",
		replyCommentBody(testTargetCommentID), testAccessToken)

	if recorder.Code != http.StatusBadRequest {
		t.Fatalf("status = %d, want %d (body %s)", recorder.Code, http.StatusBadRequest, recorder.Body.String())
	}
	if code := decodedErrorCode(t, recorder); code != codeValidation {
		t.Errorf("error code = %q, want %q", code, codeValidation)
	}
}

func TestCreateCommentReplyToAReplyAttachesToTheTopLevel(t *testing.T) {
	store := newMemoryConversationsStore()
	store.versions[testVersionID] = true
	store.seedComment(englishComment())

	reply := englishComment()
	reply.ID = testReplyCommentID
	reply.ParentCommentID = commentParentID(testSourceCommentID)
	store.seedComment(reply)
	handler := newConversationsHandler(t, store)

	recorder := doStoryRequest(handler, http.MethodPost, "/versions/"+testVersionID+"/comments",
		replyCommentBody(testReplyCommentID), testAccessToken)

	if recorder.Code != http.StatusCreated {
		t.Fatalf("status = %d, want %d (body %s)", recorder.Code, http.StatusCreated, recorder.Body.String())
	}

	var body commentEnvelope
	decodeBody(t, recorder, &body)

	if body.Comment.ParentCommentID == nil {
		t.Fatal("parent_comment_id = null, want the thread's top-level comment")
	}
	if *body.Comment.ParentCommentID != testSourceCommentID {
		t.Errorf("parent_comment_id = %q, want the top-level comment %q: threading is one level deep",
			*body.Comment.ParentCommentID, testSourceCommentID)
	}
}

func TestListCommentsServesRepliesUnderTheirParent(t *testing.T) {
	store := newMemoryConversationsStore()
	store.seedComment(englishComment())

	reply := englishComment()
	reply.ID = testReplyCommentID
	reply.Body = "It does."
	reply.ParentCommentID = commentParentID(testSourceCommentID)
	reply.CreatedAt = testNow.Add(time.Minute)
	reply.UpdatedAt = reply.CreatedAt
	store.seedComment(reply)
	handler := newConversationsHandler(t, store)

	recorder := doStoryRequest(handler, http.MethodGet, "/versions/"+testVersionID+"/comments", "", "")

	if recorder.Code != http.StatusOK {
		t.Fatalf("status = %d, want %d (body %s)", recorder.Code, http.StatusOK, recorder.Body.String())
	}

	var body commentListResponse
	decodeBody(t, recorder, &body)

	if len(body.Comments) != 2 {
		t.Fatalf("len(comments) = %d, want 2 (the parent and its reply)", len(body.Comments))
	}
	if body.Comments[0].ID != testSourceCommentID {
		t.Errorf("comments[0].id = %q, want the parent %q first", body.Comments[0].ID, testSourceCommentID)
	}
	if body.Comments[0].ParentCommentID != nil {
		t.Errorf("comments[0].parent_comment_id = %q, want null", *body.Comments[0].ParentCommentID)
	}
	if body.Comments[1].ID != testReplyCommentID {
		t.Errorf("comments[1].id = %q, want the reply %q", body.Comments[1].ID, testReplyCommentID)
	}
	if body.Comments[1].ParentCommentID == nil {
		t.Fatal("comments[1].parent_comment_id = null, want the parent it answers")
	}
	if *body.Comments[1].ParentCommentID != testSourceCommentID {
		t.Errorf("comments[1].parent_comment_id = %q, want %q", *body.Comments[1].ParentCommentID, testSourceCommentID)
	}
}

func TestListCommentsVersionNotFound(t *testing.T) {
	handler := newConversationsHandler(t, newMemoryConversationsStore())

	recorder := doStoryRequest(handler, http.MethodGet, "/versions/"+testVersionID+"/comments", "", "")

	if recorder.Code != http.StatusNotFound {
		t.Fatalf("status = %d, want %d", recorder.Code, http.StatusNotFound)
	}
	if code := decodedErrorCode(t, recorder); code != codeNotFound {
		t.Errorf("error code = %q, want %q", code, codeNotFound)
	}
}

func TestListCommentsRejectsBadLimit(t *testing.T) {
	tests := []struct {
		name  string
		query string
	}{
		{name: "zero", query: "?limit=0"},
		{name: "negative", query: "?limit=-3"},
		{name: "not a number", query: "?limit=ten"},
	}

	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			store := newMemoryConversationsStore()
			store.versions[testVersionID] = true
			handler := newConversationsHandler(t, store)

			recorder := doStoryRequest(handler, http.MethodGet, "/versions/"+testVersionID+"/comments"+test.query, "", "")

			if recorder.Code != http.StatusBadRequest {
				t.Fatalf("status = %d, want %d", recorder.Code, http.StatusBadRequest)
			}
			if code := decodedErrorCode(t, recorder); code != codeValidation {
				t.Errorf("error code = %q, want %q", code, codeValidation)
			}
		})
	}
}

func TestListCommentsRejectsMalformedCursor(t *testing.T) {
	store := newMemoryConversationsStore()
	store.versions[testVersionID] = true
	handler := newConversationsHandler(t, store)

	recorder := doStoryRequest(handler, http.MethodGet, "/versions/"+testVersionID+"/comments?cursor=not-a-cursor", "", "")

	if recorder.Code != http.StatusBadRequest {
		t.Fatalf("status = %d, want %d", recorder.Code, http.StatusBadRequest)
	}
	if code := decodedErrorCode(t, recorder); code != codeValidation {
		t.Errorf("error code = %q, want %q", code, codeValidation)
	}
}

func TestListCommentsUnexpectedFailureIsInternalError(t *testing.T) {
	handler := newConversationsHandlerWithService(t, &fakeConversationsService{listCommentsErr: errors.New("connection reset")})

	recorder := doStoryRequest(handler, http.MethodGet, "/versions/"+testVersionID+"/comments", "", "")

	if recorder.Code != http.StatusInternalServerError {
		t.Fatalf("status = %d, want %d", recorder.Code, http.StatusInternalServerError)
	}
	if code := decodedErrorCode(t, recorder); code != codeInternal {
		t.Errorf("error code = %q, want %q", code, codeInternal)
	}
}

func TestCreateBridgeRequiresAuthentication(t *testing.T) {
	store := newMemoryConversationsStore()
	store.seedComment(englishComment())
	handler := newConversationsHandler(t, store)

	recorder := doStoryRequest(handler, http.MethodPost, "/comments/"+testSourceCommentID+"/bridges", validBridgeBody(), "")

	if recorder.Code != http.StatusUnauthorized {
		t.Fatalf("status = %d, want %d", recorder.Code, http.StatusUnauthorized)
	}
	if code := decodedErrorCode(t, recorder); code != codeUnauthorized {
		t.Errorf("error code = %q, want %q", code, codeUnauthorized)
	}
	if len(store.bridges) != 0 {
		t.Errorf("store has %d bridges, want 0 — an unauthenticated request must not bridge", len(store.bridges))
	}
}

func TestCreateBridgeHappyPath(t *testing.T) {
	store := newMemoryConversationsStore()
	store.seedComment(englishComment())
	store.seedVersion(testFrenchVersionID, testStoryID, "fra")
	handler := newConversationsHandler(t, store)

	recorder := doStoryRequest(handler, http.MethodPost, "/comments/"+testSourceCommentID+"/bridges", validBridgeBody(), testAccessToken)

	if recorder.Code != http.StatusCreated {
		t.Fatalf("status = %d, want %d (body %s)", recorder.Code, http.StatusCreated, recorder.Body.String())
	}

	var body createBridgeResponse
	decodeBody(t, recorder, &body)

	if body.Bridge.ID == "" {
		t.Error("bridge id is empty, want the stored id")
	}
	if body.Bridge.AuthorID != testUserID {
		t.Errorf("bridge author id = %q, want the authenticated user %q", body.Bridge.AuthorID, testUserID)
	}
	if body.Bridge.SourceCommentID != testSourceCommentID {
		t.Errorf("bridge source id = %q, want %q", body.Bridge.SourceCommentID, testSourceCommentID)
	}
	if body.Bridge.TargetLanguage != "fra" {
		t.Errorf("target language = %q, want %q", body.Bridge.TargetLanguage, "fra")
	}
	if body.Bridge.AdaptationNote == nil {
		t.Error("adaptation note = null, want the submitted note")
	}
	if body.SourceComment.ID != testSourceCommentID {
		t.Errorf("source comment id = %q, want %q", body.SourceComment.ID, testSourceCommentID)
	}
	if body.TargetComment.ID == "" {
		t.Error("target comment id is empty, want the created comment")
	}
	if body.TargetComment.VersionID != testFrenchVersionID {
		t.Errorf("target version id = %q, want the resolved French version %q", body.TargetComment.VersionID, testFrenchVersionID)
	}
}

func TestCreateBridgeSameLanguageIsBadRequest(t *testing.T) {
	store := newMemoryConversationsStore()
	store.seedComment(englishComment()) // language "eng"
	handler := newConversationsHandler(t, store)

	body := `{"target_language":"eng","body":"Same language"}`
	recorder := doStoryRequest(handler, http.MethodPost, "/comments/"+testSourceCommentID+"/bridges", body, testAccessToken)

	if recorder.Code != http.StatusBadRequest {
		t.Fatalf("status = %d, want %d (body %s)", recorder.Code, http.StatusBadRequest, recorder.Body.String())
	}
	if code := decodedErrorCode(t, recorder); code != codeValidation {
		t.Errorf("error code = %q, want %q", code, codeValidation)
	}
	if len(store.bridges) != 0 {
		t.Errorf("store has %d bridges, want 0 — a same-language bridge must not be stored", len(store.bridges))
	}
}

func TestCreateBridgeAlreadyBridgedIsBadRequest(t *testing.T) {
	store := newMemoryConversationsStore()
	store.seedComment(englishComment())
	store.seedVersion(testFrenchVersionID, testStoryID, "fra")
	handler := newConversationsHandler(t, store)

	if recorder := doStoryRequest(handler, http.MethodPost, "/comments/"+testSourceCommentID+"/bridges", validBridgeBody(), testAccessToken); recorder.Code != http.StatusCreated {
		t.Fatalf("first bridge status = %d, want %d", recorder.Code, http.StatusCreated)
	}

	recorder := doStoryRequest(handler, http.MethodPost, "/comments/"+testSourceCommentID+"/bridges", validBridgeBody(), testAccessToken)

	if recorder.Code != http.StatusBadRequest {
		t.Fatalf("status = %d, want %d (body %s)", recorder.Code, http.StatusBadRequest, recorder.Body.String())
	}
	if code := decodedErrorCode(t, recorder); code != codeValidation {
		t.Errorf("error code = %q, want %q", code, codeValidation)
	}
}

func TestCreateBridgeMissingTargetVersionIsBadRequest(t *testing.T) {
	// The story has only its English version, so there is no French conversation
	// to bridge into.
	store := newMemoryConversationsStore()
	store.seedComment(englishComment())
	handler := newConversationsHandler(t, store)

	recorder := doStoryRequest(handler, http.MethodPost, "/comments/"+testSourceCommentID+"/bridges", validBridgeBody(), testAccessToken)

	if recorder.Code != http.StatusBadRequest {
		t.Fatalf("status = %d, want %d (body %s)", recorder.Code, http.StatusBadRequest, recorder.Body.String())
	}
	if code := decodedErrorCode(t, recorder); code != codeValidation {
		t.Errorf("error code = %q, want %q", code, codeValidation)
	}
	if len(store.bridges) != 0 {
		t.Errorf("store has %d bridges, want 0 — a bridge with no target version must not be stored", len(store.bridges))
	}
}

func TestCreateBridgeSourceNotFound(t *testing.T) {
	// A well-formed source id that names no comment is a 404.
	handler := newConversationsHandler(t, newMemoryConversationsStore())

	recorder := doStoryRequest(handler, http.MethodPost, "/comments/"+testSourceCommentID+"/bridges", validBridgeBody(), testAccessToken)

	if recorder.Code != http.StatusNotFound {
		t.Fatalf("status = %d, want %d (body %s)", recorder.Code, http.StatusNotFound, recorder.Body.String())
	}
	if code := decodedErrorCode(t, recorder); code != codeNotFound {
		t.Errorf("error code = %q, want %q", code, codeNotFound)
	}
}

func TestCreateBridgeValidationIsBadRequest(t *testing.T) {
	store := newMemoryConversationsStore()
	store.seedComment(englishComment())
	store.seedVersion(testFrenchVersionID, testStoryID, "fra")
	handler := newConversationsHandler(t, store)

	tests := []struct {
		name string
		body string
	}{
		{name: "missing target language", body: `{"body":"B"}`},
		{name: "bad target language", body: `{"target_language":"f","body":"B"}`},
		{name: "two letter target language", body: `{"target_language":"fr","body":"B"}`},
		{name: "empty body", body: `{"target_language":"fra","body":"  "}`},
	}

	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			recorder := doStoryRequest(handler, http.MethodPost, "/comments/"+testSourceCommentID+"/bridges", test.body, testAccessToken)

			if recorder.Code != http.StatusBadRequest {
				t.Fatalf("status = %d, want %d", recorder.Code, http.StatusBadRequest)
			}
			if code := decodedErrorCode(t, recorder); code != codeValidation {
				t.Errorf("error code = %q, want %q", code, codeValidation)
			}
		})
	}
}

func TestCreateBridgeUnexpectedFailureIsInternalError(t *testing.T) {
	handler := newConversationsHandlerWithService(t, &fakeConversationsService{createBridgeErr: errors.New("connection reset")})

	recorder := doStoryRequest(handler, http.MethodPost, "/comments/"+testSourceCommentID+"/bridges", validBridgeBody(), testAccessToken)

	if recorder.Code != http.StatusInternalServerError {
		t.Fatalf("status = %d, want %d", recorder.Code, http.StatusInternalServerError)
	}
	if code := decodedErrorCode(t, recorder); code != codeInternal {
		t.Errorf("error code = %q, want %q", code, codeInternal)
	}
}

func TestListBridgesHappyPath(t *testing.T) {
	store := newMemoryConversationsStore()
	store.seedComment(englishComment())
	store.seedVersion(testFrenchVersionID, testStoryID, "fra")
	handler := newConversationsHandler(t, store)

	if recorder := doStoryRequest(handler, http.MethodPost, "/comments/"+testSourceCommentID+"/bridges", validBridgeBody(), testAccessToken); recorder.Code != http.StatusCreated {
		t.Fatalf("seed bridge status = %d, want %d", recorder.Code, http.StatusCreated)
	}

	recorder := doStoryRequest(handler, http.MethodGet, "/comments/"+testSourceCommentID+"/bridges", "", "")

	if recorder.Code != http.StatusOK {
		t.Fatalf("status = %d, want %d (body %s)", recorder.Code, http.StatusOK, recorder.Body.String())
	}

	var body bridgeListResponse
	decodeBody(t, recorder, &body)

	if len(body.Bridges) != 1 {
		t.Fatalf("len(bridges) = %d, want 1", len(body.Bridges))
	}
	if body.Bridges[0].SourceCommentID != testSourceCommentID {
		t.Errorf("source id = %q, want %q", body.Bridges[0].SourceCommentID, testSourceCommentID)
	}
}

func TestListBridgesEmptyIsAnEmptyArray(t *testing.T) {
	store := newMemoryConversationsStore()
	store.seedComment(englishComment())
	handler := newConversationsHandler(t, store)

	recorder := doStoryRequest(handler, http.MethodGet, "/comments/"+testSourceCommentID+"/bridges", "", "")

	if recorder.Code != http.StatusOK {
		t.Fatalf("status = %d, want %d", recorder.Code, http.StatusOK)
	}
	if !strings.Contains(recorder.Body.String(), `"bridges":[]`) {
		t.Errorf("body = %s, want it to contain an empty array", recorder.Body.String())
	}
}

func TestListBridgesCommentNotFound(t *testing.T) {
	handler := newConversationsHandler(t, newMemoryConversationsStore())

	recorder := doStoryRequest(handler, http.MethodGet, "/comments/"+testSourceCommentID+"/bridges", "", "")

	if recorder.Code != http.StatusNotFound {
		t.Fatalf("status = %d, want %d", recorder.Code, http.StatusNotFound)
	}
	if code := decodedErrorCode(t, recorder); code != codeNotFound {
		t.Errorf("error code = %q, want %q", code, codeNotFound)
	}
}

func TestListBridgesUnexpectedFailureIsInternalError(t *testing.T) {
	handler := newConversationsHandlerWithService(t, &fakeConversationsService{listBridgesErr: errors.New("connection reset")})

	recorder := doStoryRequest(handler, http.MethodGet, "/comments/"+testSourceCommentID+"/bridges", "", "")

	if recorder.Code != http.StatusInternalServerError {
		t.Fatalf("status = %d, want %d", recorder.Code, http.StatusInternalServerError)
	}
	if code := decodedErrorCode(t, recorder); code != codeInternal {
		t.Errorf("error code = %q, want %q", code, codeInternal)
	}
}

func TestGetBridgeHappyPath(t *testing.T) {
	store := newMemoryConversationsStore()
	store.seedComment(englishComment())
	store.seedVersion(testFrenchVersionID, testStoryID, "fra")
	handler := newConversationsHandler(t, store)

	created := doStoryRequest(handler, http.MethodPost, "/comments/"+testSourceCommentID+"/bridges", validBridgeBody(), testAccessToken)
	var createdBody createBridgeResponse
	decodeBody(t, created, &createdBody)

	recorder := doStoryRequest(handler, http.MethodGet, "/bridges/"+createdBody.Bridge.ID, "", "")

	if recorder.Code != http.StatusOK {
		t.Fatalf("status = %d, want %d (body %s)", recorder.Code, http.StatusOK, recorder.Body.String())
	}

	var body bridgeEnvelope
	decodeBody(t, recorder, &body)

	if body.Bridge.ID != createdBody.Bridge.ID {
		t.Errorf("id = %q, want %q", body.Bridge.ID, createdBody.Bridge.ID)
	}
}

func TestGetBridgeNotFound(t *testing.T) {
	handler := newConversationsHandler(t, newMemoryConversationsStore())

	recorder := doStoryRequest(handler, http.MethodGet, "/bridges/"+testBridgeID, "", "")

	if recorder.Code != http.StatusNotFound {
		t.Fatalf("status = %d, want %d", recorder.Code, http.StatusNotFound)
	}
	if code := decodedErrorCode(t, recorder); code != codeNotFound {
		t.Errorf("error code = %q, want %q", code, codeNotFound)
	}
}

func TestGetBridgeMalformedIDIsNotFound(t *testing.T) {
	handler := newConversationsHandler(t, newMemoryConversationsStore())

	recorder := doStoryRequest(handler, http.MethodGet, "/bridges/not-a-uuid", "", "")

	if recorder.Code != http.StatusNotFound {
		t.Errorf("status = %d, want %d", recorder.Code, http.StatusNotFound)
	}
}

func TestGetBridgeUnexpectedFailureIsInternalError(t *testing.T) {
	handler := newConversationsHandlerWithService(t, &fakeConversationsService{getBridgeErr: errors.New("connection reset")})

	recorder := doStoryRequest(handler, http.MethodGet, "/bridges/"+testBridgeID, "", "")

	if recorder.Code != http.StatusInternalServerError {
		t.Fatalf("status = %d, want %d", recorder.Code, http.StatusInternalServerError)
	}
	if code := decodedErrorCode(t, recorder); code != codeInternal {
		t.Errorf("error code = %q, want %q", code, codeInternal)
	}
}

func TestGetCommentHappyPath(t *testing.T) {
	store := newMemoryConversationsStore()
	store.seedComment(englishComment())
	handler := newConversationsHandler(t, store)

	recorder := doStoryRequest(handler, http.MethodGet, "/comments/"+testSourceCommentID, "", "")

	if recorder.Code != http.StatusOK {
		t.Fatalf("status = %d, want %d (body %s)", recorder.Code, http.StatusOK, recorder.Body.String())
	}

	var body commentDetailEnvelope
	decodeBody(t, recorder, &body)

	if body.Comment.ID != testSourceCommentID {
		t.Errorf("id = %q, want %q", body.Comment.ID, testSourceCommentID)
	}
	if body.Comment.VersionID != testVersionID {
		t.Errorf("version id = %q, want %q", body.Comment.VersionID, testVersionID)
	}
	if body.Comment.StoryID != testStoryID {
		t.Errorf("story id = %q, want %q (the version's story)", body.Comment.StoryID, testStoryID)
	}
}

func TestGetCommentNotFound(t *testing.T) {
	handler := newConversationsHandler(t, newMemoryConversationsStore())

	recorder := doStoryRequest(handler, http.MethodGet, "/comments/"+testSourceCommentID, "", "")

	if recorder.Code != http.StatusNotFound {
		t.Fatalf("status = %d, want %d", recorder.Code, http.StatusNotFound)
	}
	if code := decodedErrorCode(t, recorder); code != codeNotFound {
		t.Errorf("error code = %q, want %q", code, codeNotFound)
	}
}

func TestGetCommentMalformedIDIsNotFound(t *testing.T) {
	handler := newConversationsHandler(t, newMemoryConversationsStore())

	recorder := doStoryRequest(handler, http.MethodGet, "/comments/not-a-uuid", "", "")

	if recorder.Code != http.StatusNotFound {
		t.Errorf("status = %d, want %d", recorder.Code, http.StatusNotFound)
	}
}

func TestGetCommentUnexpectedFailureIsInternalError(t *testing.T) {
	handler := newConversationsHandlerWithService(t, &fakeConversationsService{getCommentErr: errors.New("connection reset")})

	recorder := doStoryRequest(handler, http.MethodGet, "/comments/"+testSourceCommentID, "", "")

	if recorder.Code != http.StatusInternalServerError {
		t.Fatalf("status = %d, want %d", recorder.Code, http.StatusInternalServerError)
	}
	if code := decodedErrorCode(t, recorder); code != codeInternal {
		t.Errorf("error code = %q, want %q", code, codeInternal)
	}
}

func TestNewConversationsHandlerRejectsMissingDependencies(t *testing.T) {
	logger := slog.New(slog.NewTextHandler(io.Discard, nil))

	if _, err := NewConversationsHandler(nil, &fakeAuthorService{}, &fakeRootedService{}, logger); err == nil {
		t.Error("NewConversationsHandler(nil, authors, rooted, logger) error = nil, want an error")
	}
	if _, err := NewConversationsHandler(&fakeConversationsService{}, nil, &fakeRootedService{}, logger); err == nil {
		t.Error("NewConversationsHandler(service, nil authors, rooted, logger) error = nil, want an error")
	}
	if _, err := NewConversationsHandler(&fakeConversationsService{}, &fakeAuthorService{}, &fakeRootedService{}, nil); err == nil {
		t.Error("NewConversationsHandler(service, authors, rooted, nil) error = nil, want an error")
	}
	if _, err := NewConversationsHandler(&fakeConversationsService{}, &fakeAuthorService{}, nil, logger); err == nil {
		t.Error("NewConversationsHandler(service, authors, nil rooted, logger) error = nil, want an error")
	}
}

// ensure the fakes satisfy the handler and domain contracts at compile time.
var (
	_ ConversationsService       = (*fakeConversationsService)(nil)
	_ conversations.CommentStore = (*memoryConversationsStore)(nil)
	_ conversations.BridgeStore  = (*memoryConversationsStore)(nil)
)
