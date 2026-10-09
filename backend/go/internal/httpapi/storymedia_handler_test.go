package httpapi

import (
	"bytes"
	"context"
	"fmt"
	"io"
	"log/slog"
	"mime/multipart"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/knot/backend/internal/storymedia"
)

// testMediaAuthorID is the author of the seeded story (and the subject the
// default test token parser authenticates). testMediaOtherID is a second user.
const (
	testMediaAuthorID = "22222222-2222-4222-8222-222222222222"
	testMediaOtherID  = "44444444-4444-4444-8444-444444444444"
	testMediaStoryID  = "55555555-5555-4555-8555-555555555555"
)

// ---------------------------------------------------------------------------
// Fakes used by the other handler test files
// ---------------------------------------------------------------------------

// fakeStoryMediaLookup is an inert StoryMediaLookup, so a router assembled for
// another handler's tests has a story-media enrichment dependency that adds
// nothing.
type fakeStoryMediaLookup struct {
	byStory map[string][]storymedia.StoryMedia
	first   map[string]storymedia.StoryMedia
	err     error
}

func (f *fakeStoryMediaLookup) ListMedia(_ context.Context, storyID string) ([]storymedia.StoryMedia, error) {
	if f.err != nil {
		return nil, f.err
	}
	return f.byStory[storyID], nil
}

func (f *fakeStoryMediaLookup) FirstMedia(_ context.Context, storyIDs []string) (map[string]storymedia.StoryMedia, error) {
	if f.err != nil {
		return nil, f.err
	}
	out := make(map[string]storymedia.StoryMedia)
	for _, id := range storyIDs {
		if media, ok := f.first[id]; ok {
			out[id] = media
		}
	}
	return out, nil
}

// fakeStoryMediaService returns canned values, so the failure paths a real
// service cannot produce (an infrastructure error) can still be tested.
type fakeStoryMediaService struct {
	createResult storymedia.StoryMedia
	createErr    error
	listResult   []storymedia.StoryMedia
	listErr      error
	streamBody   io.ReadCloser
	streamSize   int64
	streamMime   string
	streamErr    error
	deleteErr    error

	gotDeleteID   string
	gotDeleteUser string
}

func (f *fakeStoryMediaService) CreateMedia(_ context.Context, _ storymedia.CreateMediaInput) (storymedia.StoryMedia, error) {
	return f.createResult, f.createErr
}

func (f *fakeStoryMediaService) ListMedia(_ context.Context, _ string) ([]storymedia.StoryMedia, error) {
	return f.listResult, f.listErr
}

func (f *fakeStoryMediaService) StreamMedia(_ context.Context, _ string, _ *http.Request) (io.ReadCloser, int64, string, error) {
	if f.streamErr != nil {
		return nil, 0, "", f.streamErr
	}
	body := f.streamBody
	if body == nil {
		body = io.NopCloser(bytes.NewReader(nil))
	}
	return body, f.streamSize, f.streamMime, nil
}

func (f *fakeStoryMediaService) DeleteMedia(_ context.Context, id, byUserID string) error {
	f.gotDeleteID = id
	f.gotDeleteUser = byUserID
	return f.deleteErr
}

// newTestStoryMediaHandler returns a handler over an inert fake service. The
// other handler test files use it to assemble a router, where the story-media
// routes are incidental to what they are testing.
func newTestStoryMediaHandler(t *testing.T, logger *slog.Logger) *StoryMediaHandler {
	t.Helper()

	handler, err := NewStoryMediaHandler(&fakeStoryMediaService{}, logger)
	if err != nil {
		t.Fatalf("NewStoryMediaHandler() error = %v, want nil", err)
	}

	return handler
}

// ---------------------------------------------------------------------------
// In-memory store + object store, exercised through the real service
// ---------------------------------------------------------------------------

// memoryMediaStore is an in-memory storymedia.StoryMediaStore.
type memoryMediaStore struct {
	mu      sync.Mutex
	media   map[string]storymedia.StoryMedia
	order   []string
	stories map[string]string // story id -> author id
	seq     int
	err     error
}

func newMemoryMediaStore() *memoryMediaStore {
	return &memoryMediaStore{
		media:   map[string]storymedia.StoryMedia{},
		stories: map[string]string{testMediaStoryID: testMediaAuthorID},
	}
}

func (m *memoryMediaStore) CreateMedia(_ context.Context, media storymedia.StoryMedia) (storymedia.StoryMedia, error) {
	if m.err != nil {
		return storymedia.StoryMedia{}, m.err
	}

	m.mu.Lock()
	defer m.mu.Unlock()

	m.seq++
	media.ID = fmt.Sprintf("00000000-0000-4000-8000-%012d", m.seq)
	media.CreatedAt = time.Date(2026, 10, 9, 12, 0, m.seq, 0, time.UTC)
	if media.DisplayOrder < 0 {
		media.DisplayOrder = len(m.order)
	}

	m.media[media.ID] = media
	m.order = append(m.order, media.ID)

	return media, nil
}

func (m *memoryMediaStore) ListMedia(_ context.Context, storyID string) ([]storymedia.StoryMedia, error) {
	m.mu.Lock()
	defer m.mu.Unlock()

	out := make([]storymedia.StoryMedia, 0)
	for _, id := range m.order {
		if m.media[id].StoryID == storyID {
			out = append(out, m.media[id])
		}
	}
	return out, nil
}

func (m *memoryMediaStore) FirstMedia(_ context.Context, storyIDs []string) (map[string]storymedia.StoryMedia, error) {
	m.mu.Lock()
	defer m.mu.Unlock()

	wanted := make(map[string]bool, len(storyIDs))
	for _, id := range storyIDs {
		wanted[id] = true
	}

	out := make(map[string]storymedia.StoryMedia)
	for _, id := range m.order {
		media := m.media[id]
		if wanted[media.StoryID] {
			if _, seen := out[media.StoryID]; !seen {
				out[media.StoryID] = media
			}
		}
	}
	return out, nil
}

func (m *memoryMediaStore) GetMedia(_ context.Context, id string) (storymedia.StoryMedia, error) {
	m.mu.Lock()
	defer m.mu.Unlock()

	media, ok := m.media[id]
	if !ok {
		return storymedia.StoryMedia{}, storymedia.ErrNotFound
	}
	return media, nil
}

func (m *memoryMediaStore) DeleteMedia(_ context.Context, id string) error {
	m.mu.Lock()
	defer m.mu.Unlock()

	if _, ok := m.media[id]; !ok {
		return storymedia.ErrNotFound
	}
	delete(m.media, id)

	for i, candidate := range m.order {
		if candidate == id {
			m.order = append(m.order[:i], m.order[i+1:]...)
			break
		}
	}
	return nil
}

func (m *memoryMediaStore) StoryAuthor(_ context.Context, storyID string) (string, error) {
	m.mu.Lock()
	defer m.mu.Unlock()

	author, ok := m.stories[storyID]
	if !ok {
		return "", storymedia.ErrStoryNotFound
	}
	return author, nil
}

// memoryObjects is an in-memory storymedia.Storage.
type memoryObjects struct {
	mu      sync.Mutex
	objects map[string][]byte
	deleted []string
}

func newMemoryObjects() *memoryObjects {
	return &memoryObjects{objects: map[string][]byte{}}
}

func (m *memoryObjects) Put(_ context.Context, key string, body io.Reader, _ string) error {
	data, err := io.ReadAll(body)
	if err != nil {
		return err
	}

	m.mu.Lock()
	defer m.mu.Unlock()
	m.objects[key] = data
	return nil
}

func (m *memoryObjects) Get(_ context.Context, key string) (io.ReadCloser, string, error) {
	m.mu.Lock()
	defer m.mu.Unlock()

	data, ok := m.objects[key]
	if !ok {
		return nil, "", storymedia.ErrNotFound
	}
	return io.NopCloser(bytes.NewReader(data)), "application/octet-stream", nil
}

func (m *memoryObjects) GetRange(_ context.Context, key string, start, end int64) (io.ReadCloser, error) {
	m.mu.Lock()
	defer m.mu.Unlock()

	data, ok := m.objects[key]
	if !ok {
		return nil, storymedia.ErrNotFound
	}
	if end >= int64(len(data)) {
		end = int64(len(data)) - 1
	}
	return io.NopCloser(bytes.NewReader(data[start : end+1])), nil
}

func (m *memoryObjects) Delete(_ context.Context, key string) error {
	m.mu.Lock()
	defer m.mu.Unlock()

	m.deleted = append(m.deleted, key)
	delete(m.objects, key)
	return nil
}

func (m *memoryObjects) has(key string) bool {
	m.mu.Lock()
	defer m.mu.Unlock()
	_, ok := m.objects[key]
	return ok
}

// storyMediaTestEnv wires the real service and handler over in-memory fakes.
type storyMediaTestEnv struct {
	handler *StoryMediaHandler
	service *storymedia.Service
	store   *memoryMediaStore
	objects *memoryObjects
	logger  *slog.Logger
}

func newStoryMediaTestEnv(t *testing.T) *storyMediaTestEnv {
	t.Helper()

	logger := slog.New(slog.NewTextHandler(io.Discard, nil))
	store := newMemoryMediaStore()
	objects := newMemoryObjects()

	service, err := storymedia.NewService(store, objects, logger)
	if err != nil {
		t.Fatalf("storymedia.NewService() error = %v, want nil", err)
	}

	handler, err := NewStoryMediaHandler(service, logger)
	if err != nil {
		t.Fatalf("NewStoryMediaHandler() error = %v, want nil", err)
	}

	return &storyMediaTestEnv{handler: handler, service: service, store: store, objects: objects, logger: logger}
}

// router assembles the full router with this env's media handler as both the
// media routes and the stories handler's media lookup. subject is the user id the
// fake token parser authenticates.
func (e *storyMediaTestEnv) router(t *testing.T, subject string) http.Handler {
	t.Helper()

	authHandler, err := NewAuthHandler(&fakeAuthService{}, e.logger)
	if err != nil {
		t.Fatalf("NewAuthHandler() error = %v, want nil", err)
	}

	storiesHandler, err := NewStoriesHandler(&fakeStoriesService{}, &fakeAuthorService{}, &fakeRootedService{}, e.service, e.logger)
	if err != nil {
		t.Fatalf("NewStoriesHandler() error = %v, want nil", err)
	}

	versionsHandler, err := NewVersionsHandler(&fakeVersionsService{}, &fakeAuthorService{}, &fakeRootedService{}, e.logger)
	if err != nil {
		t.Fatalf("NewVersionsHandler() error = %v, want nil", err)
	}

	conversationsHandler, err := NewConversationsHandler(&fakeConversationsService{}, &fakeAuthorService{}, &fakeRootedService{}, e.logger)
	if err != nil {
		t.Fatalf("NewConversationsHandler() error = %v, want nil", err)
	}

	rootedHandler, err := NewRootedHandler(&fakeRootedService{}, e.logger)
	if err != nil {
		t.Fatalf("NewRootedHandler() error = %v, want nil", err)
	}

	discoveryHandler, err := NewDiscoveryHandler(fakeDiscoveryService{}, &fakeRootedService{}, e.logger)
	if err != nil {
		t.Fatalf("NewDiscoveryHandler() error = %v, want nil", err)
	}

	authMiddleware, err := NewAuthMiddleware(&fakeTokenParser{subject: subject}, e.logger)
	if err != nil {
		t.Fatalf("NewAuthMiddleware() error = %v, want nil", err)
	}

	router, err := NewRouter(
		authHandler,
		storiesHandler,
		versionsHandler,
		conversationsHandler,
		rootedHandler,
		discoveryHandler,
		newTestAvatarHandler(t, e.logger),
		e.handler,
		newTestNotificationsHandler(t, e.logger),
		newTestProfileHandler(t, e.logger),
		authMiddleware,
		"0.1.0",
		e.logger,
	)
	if err != nil {
		t.Fatalf("NewRouter() error = %v, want nil", err)
	}

	return router.Handler()
}

// mediaUpload builds a multipart POST /stories/{id}/media body and returns it
// together with the matching Content-Type.
func mediaUpload(t *testing.T, data []byte, source string, extras map[string]string) (*bytes.Buffer, string) {
	t.Helper()

	var body bytes.Buffer
	writer := multipart.NewWriter(&body)

	part, err := writer.CreateFormFile(storyMediaFormField, "upload.bin")
	if err != nil {
		t.Fatalf("CreateFormFile() error = %v, want nil", err)
	}
	if _, err := part.Write(data); err != nil {
		t.Fatalf("writing file part: %v", err)
	}
	if source != "" {
		if err := writer.WriteField(storyMediaSourceField, source); err != nil {
			t.Fatalf("writing source field: %v", err)
		}
	}
	for key, value := range extras {
		if err := writer.WriteField(key, value); err != nil {
			t.Fatalf("writing %s field: %v", key, err)
		}
	}
	if err := writer.Close(); err != nil {
		t.Fatalf("closing multipart writer: %v", err)
	}

	return &body, writer.FormDataContentType()
}

// postMedia sends a multipart upload through the router with the given bearer
// token ("" sends none).
func postMedia(router http.Handler, storyID string, body *bytes.Buffer, contentType, token string) *httptest.ResponseRecorder {
	request := httptest.NewRequest(http.MethodPost, "/stories/"+storyID+"/media", body)
	request.Header.Set("Content-Type", contentType)
	if token != "" {
		request.Header.Set("Authorization", "Bearer "+token)
	}

	recorder := httptest.NewRecorder()
	router.ServeHTTP(recorder, request)
	return recorder
}

// uploadMedia performs a full successful upload and returns the created item.
// It fails the test if the upload did not answer 201.
func (e *storyMediaTestEnv) uploadMedia(t *testing.T, router http.Handler, data []byte, source string) storyMediaResponse {
	t.Helper()

	body, contentType := mediaUpload(t, data, source, nil)
	recorder := postMedia(router, testMediaStoryID, body, contentType, testAccessToken)
	if recorder.Code != http.StatusCreated {
		t.Fatalf("upload status = %d, want %d (body %s)", recorder.Code, http.StatusCreated, recorder.Body.String())
	}

	var envelope storyMediaEnvelope
	decodeBody(t, recorder, &envelope)
	return envelope.Media
}

// ---------------------------------------------------------------------------
// Construction
// ---------------------------------------------------------------------------

func TestNewStoryMediaHandlerRejectsMissingDependencies(t *testing.T) {
	logger := slog.New(slog.NewTextHandler(io.Discard, nil))

	if _, err := NewStoryMediaHandler(nil, logger); err == nil {
		t.Error("NewStoryMediaHandler(nil, logger) error = nil, want an error")
	}
	if _, err := NewStoryMediaHandler(&fakeStoryMediaService{}, nil); err == nil {
		t.Error("NewStoryMediaHandler(service, nil) error = nil, want an error")
	}
}

// ---------------------------------------------------------------------------
// POST /stories/{id}/media
// ---------------------------------------------------------------------------

func TestStoryMediaCreateHappyPath(t *testing.T) {
	env := newStoryMediaTestEnv(t)
	router := env.router(t, testMediaAuthorID)

	body, contentType := mediaUpload(t, pngImage(t), string(storymedia.MediaSourceCamera), map[string]string{"width": "2", "height": "2"})

	recorder := postMedia(router, testMediaStoryID, body, contentType, testAccessToken)
	if recorder.Code != http.StatusCreated {
		t.Fatalf("status = %d, want %d (body %s)", recorder.Code, http.StatusCreated, recorder.Body.String())
	}

	var envelope storyMediaEnvelope
	decodeBody(t, recorder, &envelope)

	if envelope.Media.MediaType != string(storymedia.MediaTypeImage) {
		t.Errorf("media_type = %q, want image", envelope.Media.MediaType)
	}
	if envelope.Media.Source != string(storymedia.MediaSourceCamera) {
		t.Errorf("source = %q, want camera", envelope.Media.Source)
	}
	if envelope.Media.MimeType != "image/png" {
		t.Errorf("mime_type = %q, want image/png", envelope.Media.MimeType)
	}
	if envelope.Media.Width == nil || *envelope.Media.Width != 2 {
		t.Errorf("width = %v, want 2", envelope.Media.Width)
	}
	wantURL := "/stories/" + testMediaStoryID + "/media/" + envelope.Media.ID + "/content"
	if envelope.Media.URL != wantURL {
		t.Errorf("url = %q, want %q", envelope.Media.URL, wantURL)
	}
	if !strings.HasPrefix(envelope.Media.URL, "/stories/") {
		t.Errorf("url = %q, want a relative path", envelope.Media.URL)
	}

}

func TestStoryMediaCreateRequiresAuthentication(t *testing.T) {
	env := newStoryMediaTestEnv(t)
	router := env.router(t, testMediaAuthorID)

	body, contentType := mediaUpload(t, pngImage(t), "camera", nil)

	recorder := postMedia(router, testMediaStoryID, body, contentType, "")
	if recorder.Code != http.StatusUnauthorized {
		t.Fatalf("status = %d, want %d", recorder.Code, http.StatusUnauthorized)
	}
}

func TestStoryMediaCreateRejectsNonAuthor(t *testing.T) {
	env := newStoryMediaTestEnv(t)
	router := env.router(t, testMediaOtherID)

	body, contentType := mediaUpload(t, pngImage(t), "camera", nil)

	recorder := postMedia(router, testMediaStoryID, body, contentType, testAccessToken)
	if recorder.Code != http.StatusForbidden {
		t.Fatalf("status = %d, want %d (body %s)", recorder.Code, http.StatusForbidden, recorder.Body.String())
	}
	if code := decodedErrorCode(t, recorder); code != codeForbidden {
		t.Errorf("error code = %q, want %q", code, codeForbidden)
	}
}

func TestStoryMediaCreateUnknownStory(t *testing.T) {
	env := newStoryMediaTestEnv(t)
	router := env.router(t, testMediaAuthorID)

	const unknownStory = "99999999-9999-4999-8999-999999999999"
	body, contentType := mediaUpload(t, pngImage(t), "camera", nil)

	recorder := postMedia(router, unknownStory, body, contentType, testAccessToken)
	if recorder.Code != http.StatusNotFound {
		t.Fatalf("status = %d, want %d (body %s)", recorder.Code, http.StatusNotFound, recorder.Body.String())
	}
}

func TestStoryMediaCreateRejectsUnsupportedType(t *testing.T) {
	env := newStoryMediaTestEnv(t)
	router := env.router(t, testMediaAuthorID)

	body, contentType := mediaUpload(t, []byte("this is plainly not an image or a video"), "camera", nil)

	recorder := postMedia(router, testMediaStoryID, body, contentType, testAccessToken)
	if recorder.Code != http.StatusUnsupportedMediaType {
		t.Fatalf("status = %d, want %d (body %s)", recorder.Code, http.StatusUnsupportedMediaType, recorder.Body.String())
	}
	if code := decodedErrorCode(t, recorder); code != codeUnsupportedMediaType {
		t.Errorf("error code = %q, want %q", code, codeUnsupportedMediaType)
	}
}

func TestStoryMediaCreateRejectsOversizedImage(t *testing.T) {
	env := newStoryMediaTestEnv(t)
	router := env.router(t, testMediaAuthorID)

	// A real PNG header followed by padding, so it sniffs as image/png and then
	// exceeds the image limit.
	oversized := append(pngImage(t), make([]byte, storymedia.MaxImageBytes+1)...)

	body, contentType := mediaUpload(t, oversized, "gallery", nil)

	recorder := postMedia(router, testMediaStoryID, body, contentType, testAccessToken)
	if recorder.Code != http.StatusRequestEntityTooLarge {
		t.Fatalf("status = %d, want %d (body %s)", recorder.Code, http.StatusRequestEntityTooLarge, recorder.Body.String())
	}
}

func TestStoryMediaCreateRejectsMissingFileField(t *testing.T) {
	env := newStoryMediaTestEnv(t)
	router := env.router(t, testMediaAuthorID)

	var body bytes.Buffer
	writer := multipart.NewWriter(&body)
	if err := writer.WriteField(storyMediaSourceField, "camera"); err != nil {
		t.Fatalf("writing source field: %v", err)
	}
	if err := writer.Close(); err != nil {
		t.Fatalf("closing multipart writer: %v", err)
	}

	recorder := postMedia(router, testMediaStoryID, &body, writer.FormDataContentType(), testAccessToken)
	if recorder.Code != http.StatusBadRequest {
		t.Fatalf("status = %d, want %d (body %s)", recorder.Code, http.StatusBadRequest, recorder.Body.String())
	}
}

func TestStoryMediaCreateRejectsBadSource(t *testing.T) {
	env := newStoryMediaTestEnv(t)
	router := env.router(t, testMediaAuthorID)

	body, contentType := mediaUpload(t, pngImage(t), "satellite", nil)

	recorder := postMedia(router, testMediaStoryID, body, contentType, testAccessToken)
	if recorder.Code != http.StatusBadRequest {
		t.Fatalf("status = %d, want %d (body %s)", recorder.Code, http.StatusBadRequest, recorder.Body.String())
	}
	if code := decodedErrorCode(t, recorder); code != codeValidation {
		t.Errorf("error code = %q, want %q", code, codeValidation)
	}
}

// ---------------------------------------------------------------------------
// GET /stories/{id}/media
// ---------------------------------------------------------------------------

func TestStoryMediaList(t *testing.T) {
	env := newStoryMediaTestEnv(t)
	router := env.router(t, testMediaAuthorID)

	env.uploadMedia(t, router, pngImage(t), "camera")
	env.uploadMedia(t, router, pngImage(t), "gallery")

	recorder := doRequest(router, http.MethodGet, "/stories/"+testMediaStoryID+"/media", "")
	if recorder.Code != http.StatusOK {
		t.Fatalf("status = %d, want %d", recorder.Code, http.StatusOK)
	}

	var body listStoryMediaResponse
	decodeBody(t, recorder, &body)

	if len(body.Media) != 2 {
		t.Fatalf("media count = %d, want 2", len(body.Media))
	}
	if body.Media[0].Source != "camera" || body.Media[1].Source != "gallery" {
		t.Errorf("sources = %q, %q, want camera then gallery", body.Media[0].Source, body.Media[1].Source)
	}
	if body.Media[0].DisplayOrder != 0 || body.Media[1].DisplayOrder != 1 {
		t.Errorf("display orders = %d, %d, want 0 then 1", body.Media[0].DisplayOrder, body.Media[1].DisplayOrder)
	}
}

func TestStoryMediaListUnknownStory(t *testing.T) {
	env := newStoryMediaTestEnv(t)
	router := env.router(t, testMediaAuthorID)

	recorder := doRequest(router, http.MethodGet, "/stories/99999999-9999-4999-8999-999999999999/media", "")
	if recorder.Code != http.StatusNotFound {
		t.Fatalf("status = %d, want %d", recorder.Code, http.StatusNotFound)
	}
}

// ---------------------------------------------------------------------------
// GET /stories/{id}/media/{mid}/content
// ---------------------------------------------------------------------------

func TestStoryMediaContentServesBytes(t *testing.T) {
	env := newStoryMediaTestEnv(t)
	router := env.router(t, testMediaAuthorID)

	data := pngImage(t)
	created := env.uploadMedia(t, router, data, "camera")

	recorder := doRequest(router, http.MethodGet, created.URL, "")
	if recorder.Code != http.StatusOK {
		t.Fatalf("status = %d, want %d (body %s)", recorder.Code, http.StatusOK, recorder.Body.String())
	}
	if got := recorder.Header().Get("Content-Type"); got != "image/png" {
		t.Errorf("content type = %q, want image/png", got)
	}
	if got := recorder.Header().Get("Accept-Ranges"); got != "bytes" {
		t.Errorf("accept-ranges = %q, want bytes", got)
	}
	if got := recorder.Header().Get("Cache-Control"); got != storyMediaCacheControl {
		t.Errorf("cache-control = %q, want %q", got, storyMediaCacheControl)
	}
	if !bytes.Equal(recorder.Body.Bytes(), data) {
		t.Errorf("body = %d bytes, want the %d stored bytes", recorder.Body.Len(), len(data))
	}
}

func TestStoryMediaContentRangeRequest(t *testing.T) {
	env := newStoryMediaTestEnv(t)
	router := env.router(t, testMediaAuthorID)

	// A PNG padded so the object is comfortably longer than the range.
	data := append(pngImage(t), make([]byte, 400)...)
	created := env.uploadMedia(t, router, data, "gallery")

	request := httptest.NewRequest(http.MethodGet, created.URL, nil)
	request.Header.Set("Range", "bytes=0-99")
	recorder := httptest.NewRecorder()
	router.ServeHTTP(recorder, request)

	if recorder.Code != http.StatusPartialContent {
		t.Fatalf("status = %d, want %d", recorder.Code, http.StatusPartialContent)
	}
	wantContentRange := fmt.Sprintf("bytes 0-99/%d", len(data))
	if got := recorder.Header().Get("Content-Range"); got != wantContentRange {
		t.Errorf("content-range = %q, want %q", got, wantContentRange)
	}
	if recorder.Body.Len() != 100 {
		t.Errorf("body length = %d, want 100", recorder.Body.Len())
	}
	if !bytes.Equal(recorder.Body.Bytes(), data[0:100]) {
		t.Error("body bytes do not match the requested range")
	}
}

func TestStoryMediaContentRangeSuffixAndOpenEnded(t *testing.T) {
	env := newStoryMediaTestEnv(t)
	router := env.router(t, testMediaAuthorID)

	data := append(pngImage(t), make([]byte, 400)...)
	created := env.uploadMedia(t, router, data, "gallery")

	// Open-ended: bytes=10- means 10 through the end.
	request := httptest.NewRequest(http.MethodGet, created.URL, nil)
	request.Header.Set("Range", "bytes=10-")
	recorder := httptest.NewRecorder()
	router.ServeHTTP(recorder, request)

	if recorder.Code != http.StatusPartialContent {
		t.Fatalf("open-ended status = %d, want %d", recorder.Code, http.StatusPartialContent)
	}
	if want := len(data) - 10; recorder.Body.Len() != want {
		t.Errorf("open-ended body length = %d, want %d", recorder.Body.Len(), want)
	}

	// A suffix range is the last N bytes.
	request = httptest.NewRequest(http.MethodGet, created.URL, nil)
	request.Header.Set("Range", "bytes=-50")
	recorder = httptest.NewRecorder()
	router.ServeHTTP(recorder, request)

	if recorder.Code != http.StatusPartialContent {
		t.Fatalf("suffix status = %d, want %d", recorder.Code, http.StatusPartialContent)
	}
	if recorder.Body.Len() != 50 {
		t.Errorf("suffix body length = %d, want 50", recorder.Body.Len())
	}
}

func TestStoryMediaContentIgnoresUnsatisfiableRange(t *testing.T) {
	env := newStoryMediaTestEnv(t)
	router := env.router(t, testMediaAuthorID)

	data := pngImage(t)
	created := env.uploadMedia(t, router, data, "camera")

	request := httptest.NewRequest(http.MethodGet, created.URL, nil)
	request.Header.Set("Range", "bytes=99999-100000")
	recorder := httptest.NewRecorder()
	router.ServeHTTP(recorder, request)

	// An unusable range is answered 200 with the whole object, never a partial.
	if recorder.Code != http.StatusOK {
		t.Fatalf("status = %d, want %d", recorder.Code, http.StatusOK)
	}
	if recorder.Body.Len() != len(data) {
		t.Errorf("body length = %d, want the full object (%d)", recorder.Body.Len(), len(data))
	}
}

func TestStoryMediaContentMissing(t *testing.T) {
	env := newStoryMediaTestEnv(t)
	router := env.router(t, testMediaAuthorID)

	recorder := doRequest(router, http.MethodGet, "/stories/"+testMediaStoryID+"/media/99999999-9999-4999-8999-999999999999/content", "")
	if recorder.Code != http.StatusNotFound {
		t.Fatalf("status = %d, want %d", recorder.Code, http.StatusNotFound)
	}
}

// ---------------------------------------------------------------------------
// DELETE /stories/{id}/media/{mid}
// ---------------------------------------------------------------------------

func TestStoryMediaDelete(t *testing.T) {
	env := newStoryMediaTestEnv(t)
	router := env.router(t, testMediaAuthorID)

	created := env.uploadMedia(t, router, pngImage(t), "camera")

	request := httptest.NewRequest(http.MethodDelete, "/stories/"+testMediaStoryID+"/media/"+created.ID, nil)
	request.Header.Set("Authorization", "Bearer "+testAccessToken)
	recorder := httptest.NewRecorder()
	router.ServeHTTP(recorder, request)

	if recorder.Code != http.StatusNoContent {
		t.Fatalf("status = %d, want %d (body %s)", recorder.Code, http.StatusNoContent, recorder.Body.String())
	}

	// The object is gone from the object store and the row no longer resolves.
	recorder = doRequest(router, http.MethodGet, created.URL, "")
	if recorder.Code != http.StatusNotFound {
		t.Errorf("content after delete status = %d, want %d", recorder.Code, http.StatusNotFound)
	}
	if len(env.objects.deleted) != 1 {
		t.Errorf("deleted objects = %d, want 1", len(env.objects.deleted))
	}
}

func TestStoryMediaDeleteRequiresAuthentication(t *testing.T) {
	env := newStoryMediaTestEnv(t)
	router := env.router(t, testMediaAuthorID)

	created := env.uploadMedia(t, router, pngImage(t), "camera")

	request := httptest.NewRequest(http.MethodDelete, "/stories/"+testMediaStoryID+"/media/"+created.ID, nil)
	recorder := httptest.NewRecorder()
	router.ServeHTTP(recorder, request)

	if recorder.Code != http.StatusUnauthorized {
		t.Fatalf("status = %d, want %d", recorder.Code, http.StatusUnauthorized)
	}
}

func TestStoryMediaDeleteRejectsNonAuthor(t *testing.T) {
	env := newStoryMediaTestEnv(t)
	authorRouter := env.router(t, testMediaAuthorID)
	created := env.uploadMedia(t, authorRouter, pngImage(t), "camera")

	otherRouter := env.router(t, testMediaOtherID)
	request := httptest.NewRequest(http.MethodDelete, "/stories/"+testMediaStoryID+"/media/"+created.ID, nil)
	request.Header.Set("Authorization", "Bearer "+testAccessToken)
	recorder := httptest.NewRecorder()
	otherRouter.ServeHTTP(recorder, request)

	if recorder.Code != http.StatusForbidden {
		t.Fatalf("status = %d, want %d (body %s)", recorder.Code, http.StatusForbidden, recorder.Body.String())
	}
}

func TestStoryMediaDeleteMissing(t *testing.T) {
	env := newStoryMediaTestEnv(t)
	router := env.router(t, testMediaAuthorID)

	request := httptest.NewRequest(http.MethodDelete, "/stories/"+testMediaStoryID+"/media/99999999-9999-4999-8999-999999999999", nil)
	request.Header.Set("Authorization", "Bearer "+testAccessToken)
	recorder := httptest.NewRecorder()
	router.ServeHTTP(recorder, request)

	if recorder.Code != http.StatusNotFound {
		t.Fatalf("status = %d, want %d", recorder.Code, http.StatusNotFound)
	}
}

// ---------------------------------------------------------------------------
// Internal failure
// ---------------------------------------------------------------------------

func TestStoryMediaListInternalErrorIsGeneric(t *testing.T) {
	logger := slog.New(slog.NewTextHandler(io.Discard, nil))
	handler, err := NewStoryMediaHandler(&fakeStoryMediaService{listErr: fmt.Errorf("database on fire")}, logger)
	if err != nil {
		t.Fatalf("NewStoryMediaHandler() error = %v, want nil", err)
	}

	request := httptest.NewRequest(http.MethodGet, "/stories/"+testMediaStoryID+"/media", nil)
	request.SetPathValue("id", testMediaStoryID)
	recorder := httptest.NewRecorder()
	handler.List(recorder, request)

	if recorder.Code != http.StatusInternalServerError {
		t.Fatalf("status = %d, want %d", recorder.Code, http.StatusInternalServerError)
	}
	if strings.Contains(recorder.Body.String(), "database on fire") {
		t.Errorf("response leaked internal detail: %s", recorder.Body.String())
	}
}
