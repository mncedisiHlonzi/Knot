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
	// testMediaID names a media item for the tests that call a handler method
	// directly, where no upload has assigned a real id.
	testMediaID = "66666666-6666-4666-8666-666666666666"
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
	// gotVariant is the variant the content handler asked for.
	gotVariant storymedia.Variant
	// gotFormat is the format the content handler asked for.
	gotFormat storymedia.Format

	// The direct-upload path (KNOT-040).
	presignResult storymedia.PresignedUpload
	presignErr    error
	attachResult  storymedia.StoryMedia
	attachErr     error
	gotPresign    storymedia.PresignUploadInput
	gotAttach     storymedia.AttachUploadInput
}

func (f *fakeStoryMediaService) CreateMedia(_ context.Context, _ storymedia.CreateMediaInput) (storymedia.StoryMedia, error) {
	return f.createResult, f.createErr
}

func (f *fakeStoryMediaService) ListMedia(_ context.Context, _ string) ([]storymedia.StoryMedia, error) {
	return f.listResult, f.listErr
}

func (f *fakeStoryMediaService) StreamMedia(_ context.Context, _ string, variant storymedia.Variant, format storymedia.Format, _ *http.Request) (io.ReadCloser, int64, string, error) {
	f.gotVariant = variant
	f.gotFormat = format
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

// PresignUpload answers with the canned result, and records what the handler
// asked for so a test can assert the wire request was decoded (KNOT-040).
func (f *fakeStoryMediaService) PresignUpload(_ context.Context, in storymedia.PresignUploadInput) (storymedia.PresignedUpload, error) {
	f.gotPresign = in
	return f.presignResult, f.presignErr
}

// AttachUpload answers with the canned result, and records the decoded input.
func (f *fakeStoryMediaService) AttachUpload(_ context.Context, in storymedia.AttachUploadInput) (storymedia.StoryMedia, error) {
	f.gotAttach = in
	return f.attachResult, f.attachErr
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

// SetStatus applies a pipeline transition, so a later read sees what the worker
// reported (KNOT-037-fix7).
func (m *memoryMediaStore) SetStatus(_ context.Context, id string, status storymedia.Status, sizeBytes int64) error {
	m.mu.Lock()
	defer m.mu.Unlock()

	media, ok := m.media[id]
	if !ok {
		return storymedia.ErrNotFound
	}
	media.Status = status
	if sizeBytes > 0 {
		media.SizeBytes = sizeBytes
	}
	m.media[id] = media

	return nil
}

// markReady applies what the media worker records: the derivatives it wrote, the
// size of the object a reader is served, and the ready status (KNOT-038).
func (m *memoryMediaStore) markReady(id, storageKey string, thumbnailKey *string, sizeBytes int64) {
	m.mu.Lock()
	defer m.mu.Unlock()

	media, ok := m.media[id]
	if !ok {
		return
	}

	media.Status = storymedia.StatusReady
	media.StorageKey = storageKey
	media.ThumbnailKey = thumbnailKey
	media.SizeBytes = sizeBytes
	m.media[id] = media
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

func (m *memoryMediaStore) CountMedia(_ context.Context, storyID string) (int, error) {
	m.mu.Lock()
	defer m.mu.Unlock()

	count := 0
	for _, id := range m.order {
		if m.media[id].StoryID == storyID {
			count++
		}
	}
	return count, nil
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

// Exists backs the notify path's HEAD: a key only resolves when something was
// actually uploaded to it (KNOT-040).
func (m *memoryObjects) Exists(_ context.Context, key string) (bool, error) {
	return m.has(key), nil
}

// PresignPut answers with a URL shaped like a real presigned one, so the handler
// test can assert the response is a usable URL rather than a placeholder
// (KNOT-040). The signature is fixed because nothing verifies it here.
func (m *memoryObjects) PresignPut(_ context.Context, key, contentType string, expires time.Duration) (string, error) {
	if key == "" || contentType == "" || expires <= 0 {
		return "", fmt.Errorf("memoryObjects: presign needs a key, a content type and an expiry")
	}

	return fmt.Sprintf(
		"https://objects.test/knot-media/%s?X-Amz-Algorithm=AWS4-HMAC-SHA256&X-Amz-Expires=%d&X-Amz-Signature=test-signature",
		key,
		int(expires.Seconds()),
	), nil
}

// memoryPublisher records the jobs the service queues, and is what a test asserts
// after an upload: the API's job is to queue, not to encode (KNOT-038).
type memoryPublisher struct {
	mu   sync.Mutex
	jobs []storymedia.Job
}

func (p *memoryPublisher) Publish(_ context.Context, job storymedia.Job) error {
	p.mu.Lock()
	defer p.mu.Unlock()
	p.jobs = append(p.jobs, job)
	return nil
}

func (p *memoryPublisher) queued() []storymedia.Job {
	p.mu.Lock()
	defer p.mu.Unlock()
	return append([]storymedia.Job(nil), p.jobs...)
}

// storyMediaTestEnv wires the real service and handler over in-memory fakes.
type storyMediaTestEnv struct {
	handler   *StoryMediaHandler
	service   *storymedia.Service
	store     *memoryMediaStore
	objects   *memoryObjects
	publisher *memoryPublisher
	logger    *slog.Logger
}

func newStoryMediaTestEnv(t *testing.T) *storyMediaTestEnv {
	t.Helper()
	return newStoryMediaTestEnvWith(t, nil, slog.New(slog.NewTextHandler(io.Discard, nil)))
}

// newStoryMediaTestEnvWith is newStoryMediaTestEnv with a detail cache and a
// logger the caller supplies, so a test can assert both the invalidation an upload
// or delete performs and the warning a failed one logs (KNOT-036-fix4).
func newStoryMediaTestEnvWith(t *testing.T, details DetailCache, logger *slog.Logger) *storyMediaTestEnv {
	t.Helper()

	store := newMemoryMediaStore()
	objects := newMemoryObjects()
	publisher := &memoryPublisher{}

	service, err := storymedia.NewService(store, objects, publisher, logger)
	if err != nil {
		t.Fatalf("storymedia.NewService() error = %v, want nil", err)
	}

	handler, err := NewStoryMediaHandlerWithCaches(service, logger, Caches{Details: details})
	if err != nil {
		t.Fatalf("NewStoryMediaHandlerWithCaches() error = %v, want nil", err)
	}

	return &storyMediaTestEnv{handler: handler, service: service, store: store, objects: objects, publisher: publisher, logger: logger}
}

// router assembles the full router with this env's media handler as both the
// media routes and the stories handler's media lookup. subject is the user id the
// fake token parser authenticates.
func (e *storyMediaTestEnv) router(t *testing.T, subject string) http.Handler {
	t.Helper()

	authHandler, err := NewAuthHandler(&fakeAuthService{}, newTestLimiter(t), e.logger)
	if err != nil {
		t.Fatalf("NewAuthHandler() error = %v, want nil", err)
	}

	storiesHandler, err := NewStoriesHandler(&fakeStoriesService{}, &fakeAuthorService{}, &fakeRootedService{}, e.service, &fakeReactionsLookup{}, &fakeStoryStatsLookup{}, &fakeVersionChainLookup{}, &fakeInteractionsLookup{}, e.logger)
	if err != nil {
		t.Fatalf("NewStoriesHandler() error = %v, want nil", err)
	}

	versionsHandler, err := NewVersionsHandler(&fakeVersionsService{}, &fakeAuthorService{}, &fakeRootedService{}, &fakeReactionsLookup{}, e.logger)
	if err != nil {
		t.Fatalf("NewVersionsHandler() error = %v, want nil", err)
	}

	conversationsHandler, err := NewConversationsHandler(&fakeConversationsService{}, &fakeAuthorService{}, &fakeRootedService{}, &fakeReactionsLookup{}, e.logger)
	if err != nil {
		t.Fatalf("NewConversationsHandler() error = %v, want nil", err)
	}

	rootedHandler, err := NewRootedHandler(&fakeRootedService{}, e.logger)
	if err != nil {
		t.Fatalf("NewRootedHandler() error = %v, want nil", err)
	}

	discoveryHandler := newTestDiscoveryHandler(t, e.logger, fakeDiscoveryService{})

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
		newTestReactionsRouterHandler(t, e.logger),
		newTestInquiriesHandler(t, e.logger),
		newTestModerationHandler(t, e.logger),
		newTestSearchHandler(t, e.logger),
		newTestAccountHandler(t, e.logger),
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

// uploadMedia posts one item and has the stand-in worker finish it, so a test that
// reads the bytes afterwards sees a ready row. The response itself is 202: the work
// is queued, not done (KNOT-037-fix7, KNOT-038).
func (e *storyMediaTestEnv) uploadMedia(t *testing.T, router http.Handler, data []byte, source string) storyMediaResponse {
	t.Helper()

	created := e.postMedia(t, router, data, source)
	e.completeMedia(t, created.ID)

	return created
}

// completeMedia stands in for the Rust media worker: it writes the derivatives the
// Go service used to encode itself, deletes the upload, and flips the row to ready.
//
// The worker's own behaviour is tested in its crate; here it only has to produce
// the state every read path is written against, which is what lets these tests keep
// asserting what a client actually receives.
func (e *storyMediaTestEnv) completeMedia(t *testing.T, mediaID string) {
	t.Helper()

	media, err := e.store.GetMedia(context.Background(), mediaID)
	if err != nil {
		t.Fatalf("GetMedia(%q) error = %v", mediaID, err)
	}

	base, _, found := strings.Cut(media.StorageKey, "_original")
	if !found {
		t.Fatalf("storage key %q does not name an upload", media.StorageKey)
	}

	display := base + "_display.avif"
	// Long enough for the Range assertions to exercise a real slice.
	displayBody := append([]byte("display bytes for "+mediaID+":"), bytes.Repeat([]byte("x"), 512)...)
	if err := e.objects.Put(context.Background(), display, bytes.NewReader(displayBody), "image/avif"); err != nil {
		t.Fatalf("storing the display rendition: %v", err)
	}

	var thumbnail *string
	if media.MediaType == storymedia.MediaTypeImage {
		// The same display picture in a codec every supported device can decode, which
		// is what display_webp_url points at (KNOT-038-fix3).
		webpDisplay := base + "_display.webp"
		webpBody := append([]byte("webp display bytes for "+mediaID+":"), bytes.Repeat([]byte("w"), 512)...)
		if err := e.objects.Put(context.Background(), webpDisplay, bytes.NewReader(webpBody), "image/webp"); err != nil {
			t.Fatalf("storing the webp display: %v", err)
		}

		key := base + "_thumb.webp"
		if err := e.objects.Put(context.Background(), key, strings.NewReader("thumb bytes for "+mediaID), "image/webp"); err != nil {
			t.Fatalf("storing the thumbnail: %v", err)
		}
		thumbnail = &key
	}

	e.store.markReady(mediaID, display, thumbnail, int64(len(displayBody)))

	// The upload is removed once its derivatives exist, exactly as the worker does.
	if err := e.objects.Delete(context.Background(), media.StorageKey); err != nil {
		t.Fatalf("removing the upload: %v", err)
	}
}

// postMedia posts one item and returns the 202 response without waiting for the
// background work, which is what a processing-state test needs.
func (e *storyMediaTestEnv) postMedia(t *testing.T, router http.Handler, data []byte, source string) storyMediaResponse {
	t.Helper()

	body, contentType := mediaUpload(t, data, source, nil)
	recorder := postMedia(router, testMediaStoryID, body, contentType, testAccessToken)
	if recorder.Code != http.StatusAccepted {
		t.Fatalf("upload status = %d, want %d (body %s)", recorder.Code, http.StatusAccepted, recorder.Body.String())
	}

	var envelope storyMediaEnvelope
	decodeBody(t, recorder, &envelope)
	return envelope.Media
}

// storedDerivative returns the bytes of the object whose key ends with suffix, so
// a test can compare what the API served against what the worker wrote.
func (e *storyMediaTestEnv) storedDerivative(t *testing.T, suffix string) []byte {
	t.Helper()

	for key, data := range e.objects.objects {
		if strings.HasSuffix(key, suffix) {
			return data
		}
	}

	t.Fatalf("no object ending in %q was stored", suffix)
	return nil
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
	// 202: the row is accepted and the derivatives are queued (KNOT-037-fix7).
	if recorder.Code != http.StatusAccepted {
		t.Fatalf("status = %d, want %d (body %s)", recorder.Code, http.StatusAccepted, recorder.Body.String())
	}

	var envelope storyMediaEnvelope
	decodeBody(t, recorder, &envelope)

	if envelope.Media.Status != string(storymedia.StatusProcessing) {
		t.Errorf("status = %q, want %q", envelope.Media.Status, storymedia.StatusProcessing)
	}
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
	// Both dimensions are stored, not just the first: a list frames the media at
	// the pair's ratio, so a height that went missing would leave every card
	// cropping to the 16:9 fallback (KNOT-020g-fix5, KNOT-020g-fix6).
	if envelope.Media.Height == nil || *envelope.Media.Height != 2 {
		t.Errorf("height = %v, want 2", envelope.Media.Height)
	}

	// The address names the display rendition, not the upload: there is no stored
	// original any more (KNOT-037-fix7).
	wantURL := "/stories/" + testMediaStoryID + "/media/" + envelope.Media.ID + "/content?variant=display"
	if envelope.Media.URL != wantURL {
		t.Errorf("url = %q, want %q", envelope.Media.URL, wantURL)
	}
	if !strings.HasPrefix(envelope.Media.URL, "/stories/") {
		t.Errorf("url = %q, want a relative path", envelope.Media.URL)
	}
}

// TestStoryMediaCreateWithoutDimensionsStoresNulls proves the dimensions are
// optional: a client that cannot measure the file omits the fields, and the row
// keeps NULL rather than a zero. A list then falls back to its own default frame
// ratio, so a zero would be read as "a shape that is 0 pixels wide" and a null is
// read as "unknown" (KNOT-020g-fix6).
func TestStoryMediaCreateWithoutDimensionsStoresNulls(t *testing.T) {
	env := newStoryMediaTestEnv(t)
	router := env.router(t, testMediaAuthorID)

	body, contentType := mediaUpload(t, pngImage(t), string(storymedia.MediaSourceGallery), nil)

	recorder := postMedia(router, testMediaStoryID, body, contentType, testAccessToken)
	if recorder.Code != http.StatusAccepted {
		t.Fatalf("status = %d, want %d (body %s)", recorder.Code, http.StatusAccepted, recorder.Body.String())
	}

	var envelope storyMediaEnvelope
	decodeBody(t, recorder, &envelope)

	if envelope.Media.Width != nil {
		t.Errorf("width = %v, want nil when the client omits it", envelope.Media.Width)
	}
	if envelope.Media.Height != nil {
		t.Errorf("height = %v, want nil when the client omits it", envelope.Media.Height)
	}
	// The rest of the upload still works, so the omission is genuinely optional
	// rather than a silent failure.
	if envelope.Media.MediaType != string(storymedia.MediaTypeImage) {
		t.Errorf("media_type = %q, want image", envelope.Media.MediaType)
	}
}

// TestStoryMediaCreateEnforcesTheCap fills a story to the cap and proves the next
// upload is refused with a 400 validation_error carrying the exact message
// (KNOT-ADR-054).
func TestStoryMediaCreateEnforcesTheCap(t *testing.T) {
	env := newStoryMediaTestEnv(t)
	router := env.router(t, testMediaAuthorID)

	for i := 0; i < storymedia.MaxMediaPerStory; i++ {
		env.uploadMedia(t, router, pngImage(t), string(storymedia.MediaSourceGallery))
	}

	body, contentType := mediaUpload(t, pngImage(t), string(storymedia.MediaSourceGallery), nil)
	recorder := postMedia(router, testMediaStoryID, body, contentType, testAccessToken)

	if recorder.Code != http.StatusBadRequest {
		t.Fatalf("status = %d, want %d (body %s)", recorder.Code, http.StatusBadRequest, recorder.Body.String())
	}
	if code := decodedErrorCode(t, recorder); code != codeValidation {
		t.Errorf("error code = %q, want %q", code, codeValidation)
	}

	var errorBody struct {
		Error struct {
			Message string `json:"message"`
		} `json:"error"`
	}
	decodeBody(t, recorder, &errorBody)
	if errorBody.Error.Message != storymedia.MediaLimitMessage {
		t.Errorf("error message = %q, want %q", errorBody.Error.Message, storymedia.MediaLimitMessage)
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

	// A ready image carries a second display address in WebP, so a device that
	// cannot decode AVIF has something to draw (KNOT-038-fix3).
	for i, item := range body.Media {
		if item.DisplayWebPURL == nil {
			t.Fatalf("media %d: display_webp_url = null, want the webp display address", i)
		}
		want := "/stories/" + testMediaStoryID + "/media/" + item.ID + "/content?variant=display&format=webp"
		if *item.DisplayWebPURL != want {
			t.Errorf("media %d: display_webp_url = %q, want %q", i, *item.DisplayWebPURL, want)
		}
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

	created := env.uploadMedia(t, router, pngImage(t), "camera")
	display := env.storedDerivative(t, "_display.avif")

	recorder := doRequest(router, http.MethodGet, created.URL, "")
	if recorder.Code != http.StatusOK {
		t.Fatalf("status = %d, want %d (body %s)", recorder.Code, http.StatusOK, recorder.Body.String())
	}
	// The address names the display AVIF, and it is served AS an AVIF — not as the
	// PNG that was uploaded (KNOT-037-fix7).
	if got := recorder.Header().Get("Content-Type"); got != "image/avif" {
		t.Errorf("content type = %q, want image/avif", got)
	}
	if got := recorder.Header().Get("Accept-Ranges"); got != "bytes" {
		t.Errorf("accept-ranges = %q, want bytes", got)
	}
	if got := recorder.Header().Get("Cache-Control"); got != storyMediaCacheControl {
		t.Errorf("cache-control = %q, want %q", got, storyMediaCacheControl)
	}
	if !bytes.Equal(recorder.Body.Bytes(), display) {
		t.Errorf("body = %d bytes, want the %d stored bytes", recorder.Body.Len(), len(display))
	}
}

// TestStoryMediaContentServesTheWebPDisplay is KNOT-038-fix3 end to end: the
// address the API advertises for a device that cannot decode AVIF answers with the
// WebP display under the WebP type, while the primary address keeps answering with
// the AVIF. It is what makes a story photo visible on a pre-Android-12 handset.
func TestStoryMediaContentServesTheWebPDisplay(t *testing.T) {
	env := newStoryMediaTestEnv(t)
	router := env.router(t, testMediaAuthorID)

	created := env.uploadMedia(t, router, pngImage(t), "camera")

	listRecorder := doRequest(router, http.MethodGet, "/stories/"+testMediaStoryID+"/media", "")
	if listRecorder.Code != http.StatusOK {
		t.Fatalf("list status = %d, want %d", listRecorder.Code, http.StatusOK)
	}

	var body listStoryMediaResponse
	decodeBody(t, listRecorder, &body)
	if len(body.Media) != 1 {
		t.Fatalf("media count = %d, want 1", len(body.Media))
	}
	if body.Media[0].DisplayWebPURL == nil {
		t.Fatal("display_webp_url = null, want the webp display address of a ready image")
	}

	recorder := doRequest(router, http.MethodGet, *body.Media[0].DisplayWebPURL, "")
	if recorder.Code != http.StatusOK {
		t.Fatalf("status = %d, want %d (body %s)", recorder.Code, http.StatusOK, recorder.Body.String())
	}
	if got := recorder.Header().Get("Content-Type"); got != "image/webp" {
		t.Errorf("content type = %q, want image/webp", got)
	}
	if webpDisplay := env.storedDerivative(t, "_display.webp"); !bytes.Equal(recorder.Body.Bytes(), webpDisplay) {
		t.Errorf("body = %d bytes, want the %d stored webp bytes", recorder.Body.Len(), len(webpDisplay))
	}

	// The AVIF display is still what the primary address serves.
	avifRecorder := doRequest(router, http.MethodGet, created.URL, "")
	if got := avifRecorder.Header().Get("Content-Type"); got != "image/avif" {
		t.Errorf("primary content type = %q, want image/avif", got)
	}
}

func TestStoryMediaContentRangeRequest(t *testing.T) {
	env := newStoryMediaTestEnv(t)
	router := env.router(t, testMediaAuthorID)

	created := env.uploadMedia(t, router, pngImage(t), "gallery")
	data := env.storedDerivative(t, "_display.avif")

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

	created := env.uploadMedia(t, router, pngImage(t), "gallery")
	data := env.storedDerivative(t, "_display.avif")

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

	created := env.uploadMedia(t, router, pngImage(t), "camera")
	data := env.storedDerivative(t, "_display.avif")

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
	// An image upload produces four objects over its life: the upload (removed by
	// the worker once the derivatives existed), the display AVIF, the WebP thumbnail
	// and the ~20px placeholder. Deleting the item must remove the three the row
	// still names, not just the one it serves (KNOT-037-fix7, KNOT-038, KNOT-043).
	if len(env.objects.deleted) != 4 {
		t.Errorf("deleted objects = %d, want 4 (the upload, the display, the thumbnail, and the placeholder)", len(env.objects.deleted))
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

// ---------------------------------------------------------------------------
// Derivatives (KNOT-037)
// ---------------------------------------------------------------------------

func TestStoryMediaCreateExposesDerivativeURLs(t *testing.T) {
	env := newStoryMediaTestEnv(t)
	router := env.router(t, testMediaAuthorID)

	created := env.uploadMedia(t, router, pngImage(t), string(storymedia.MediaSourceCamera))

	// A ready image is addressed by its display AVIF, with the feed thumbnail as a
	// separate WebP address (KNOT-037-fix7).
	base := "/stories/" + testMediaStoryID + "/media/" + created.ID + "/content"
	if want := base + "?variant=display"; created.URL != want {
		t.Errorf("url = %q, want %q", created.URL, want)
	}
	if created.ThumbnailURL == nil {
		t.Fatal("thumbnail_url = null, want the thumbnail address")
	}
	if want := base + "?variant=thumb"; *created.ThumbnailURL != want {
		t.Errorf("thumbnail_url = %q, want %q", *created.ThumbnailURL, want)
	}

	// Since the thumbnail IS the WebP file, there is no second codec to fall back
	// to, and neither retired rendition exists on a new row.
	if created.ThumbnailURLFallback != nil {
		t.Errorf("thumbnail_url_fallback = %q, want null", *created.ThumbnailURLFallback)
	}
	if created.MediumURL != nil {
		t.Errorf("medium_url = %q, want null: the pipeline writes no medium", *created.MediumURL)
	}

	// The 202 describes a row the worker has not finished, so it offers no second
	// display codec yet: whether a WebP display exists is a property of the finished
	// row, and the address appears with it (KNOT-038-fix3).
	if created.DisplayWebPURL != nil {
		t.Errorf("display_webp_url = %q, want null while the row is processing", *created.DisplayWebPURL)
	}
}

// TestStoryMediaVideoURLsHaveNoThumbnail pins Item 5's rule for videos: the full
// address is the stream, and there is no cover (KNOT-037-fix7).
func TestStoryMediaVideoURLsHaveNoThumbnail(t *testing.T) {
	video := storymedia.StoryMedia{
		ID:         testMediaID,
		StoryID:    testMediaStoryID,
		MediaType:  storymedia.MediaTypeVideo,
		MimeType:   "video/mp4",
		StorageKey: "story-media/" + testMediaStoryID + "/x_stream.mp4",
		Status:     storymedia.StatusReady,
	}

	response := newStoryMediaResponse(video, testMediaStoryID)

	base := "/stories/" + testMediaStoryID + "/media/" + testMediaID + "/content"
	if want := base + "?variant=stream"; response.URL != want {
		t.Errorf("url = %q, want %q", response.URL, want)
	}
	if response.ThumbnailURL != nil {
		t.Errorf("thumbnail_url = %q, want null for a video", *response.ThumbnailURL)
	}
	if response.ThumbnailURLFallback != nil {
		t.Errorf("thumbnail_url_fallback = %q, want null for a video", *response.ThumbnailURLFallback)
	}
	// A video is a single stream, so there is no second display codec to offer.
	if response.DisplayWebPURL != nil {
		t.Errorf("display_webp_url = %q, want null for a video", *response.DisplayWebPURL)
	}
}

func TestStoryMediaContentAsksForTheVariant(t *testing.T) {
	service := &fakeStoryMediaService{streamSize: 3, streamMime: "image/jpeg"}
	handler, err := NewStoryMediaHandler(service, slog.New(slog.NewTextHandler(io.Discard, nil)))
	if err != nil {
		t.Fatalf("NewStoryMediaHandler() error = %v, want nil", err)
	}

	request := httptest.NewRequest(
		http.MethodGet,
		"/stories/"+testMediaStoryID+"/media/"+testMediaID+"/content?variant=thumb",
		nil,
	)
	request.SetPathValue("mid", testMediaID)
	recorder := httptest.NewRecorder()
	handler.Content(recorder, request)

	if recorder.Code != http.StatusOK {
		t.Fatalf("status = %d, want %d", recorder.Code, http.StatusOK)
	}
	if service.gotVariant != storymedia.VariantThumb {
		t.Errorf("variant = %q, want %q", service.gotVariant, storymedia.VariantThumb)
	}
}

func TestStoryMediaContentWithoutAVariantAsksForTheOriginal(t *testing.T) {
	service := &fakeStoryMediaService{streamSize: 3, streamMime: "image/png"}
	handler, err := NewStoryMediaHandler(service, slog.New(slog.NewTextHandler(io.Discard, nil)))
	if err != nil {
		t.Fatalf("NewStoryMediaHandler() error = %v, want nil", err)
	}

	request := httptest.NewRequest(
		http.MethodGet,
		"/stories/"+testMediaStoryID+"/media/"+testMediaID+"/content",
		nil,
	)
	request.SetPathValue("mid", testMediaID)
	recorder := httptest.NewRecorder()
	handler.Content(recorder, request)

	if recorder.Code != http.StatusOK {
		t.Fatalf("status = %d, want %d", recorder.Code, http.StatusOK)
	}
	if service.gotVariant != storymedia.Variant("") {
		t.Errorf("variant = %q, want the empty (original) variant", service.gotVariant)
	}
}

// TestStoryMediaContentServesTheNegotiatedCodec pins Item 4: the format the
// client asks for reaches the service, and the response carries the content type
// of the file that was actually served (KNOT-037-fix6).
func TestStoryMediaContentServesTheNegotiatedCodec(t *testing.T) {
	cases := []struct {
		name       string
		query      string
		streamMime string
		wantFormat storymedia.Format
		wantType   string
	}{
		{
			name:       "avif",
			query:      "?variant=thumb&format=avif",
			streamMime: "image/avif",
			wantFormat: storymedia.FormatAVIF,
			wantType:   "image/avif",
		},
		{
			name:       "webp fallback",
			query:      "?variant=thumb&format=webp",
			streamMime: "image/webp",
			wantFormat: storymedia.FormatWebP,
			wantType:   "image/webp",
		},
		{
			name:       "absent format asks for the primary codec",
			query:      "?variant=thumb",
			streamMime: "image/avif",
			wantFormat: storymedia.Format(""),
			wantType:   "image/avif",
		},
		{
			// The server answered in the sibling codec because the requested file was
			// missing; the header must follow the bytes, not the request.
			name:       "fallback content type",
			query:      "?variant=thumb&format=webp",
			streamMime: "image/avif",
			wantFormat: storymedia.FormatWebP,
			wantType:   "image/avif",
		},
	}

	for _, testCase := range cases {
		service := &fakeStoryMediaService{streamSize: 3, streamMime: testCase.streamMime}
		handler, err := NewStoryMediaHandler(service, slog.New(slog.NewTextHandler(io.Discard, nil)))
		if err != nil {
			t.Fatalf("NewStoryMediaHandler() error = %v, want nil", err)
		}

		request := httptest.NewRequest(
			http.MethodGet,
			"/stories/"+testMediaStoryID+"/media/"+testMediaID+"/content"+testCase.query,
			nil,
		)
		request.SetPathValue("mid", testMediaID)
		recorder := httptest.NewRecorder()
		handler.Content(recorder, request)

		if recorder.Code != http.StatusOK {
			t.Fatalf("%s: status = %d, want %d", testCase.name, recorder.Code, http.StatusOK)
		}
		if service.gotFormat != testCase.wantFormat {
			t.Errorf("%s: format = %q, want %q", testCase.name, service.gotFormat, testCase.wantFormat)
		}
		if got := recorder.Header().Get("Content-Type"); got != testCase.wantType {
			t.Errorf("%s: Content-Type = %q, want %q", testCase.name, got, testCase.wantType)
		}
	}
}

func TestStoryMediaContentRejectsAnUnknownFormat(t *testing.T) {
	service := &fakeStoryMediaService{}
	handler, err := NewStoryMediaHandler(service, slog.New(slog.NewTextHandler(io.Discard, nil)))
	if err != nil {
		t.Fatalf("NewStoryMediaHandler() error = %v, want nil", err)
	}

	request := httptest.NewRequest(
		http.MethodGet,
		"/stories/"+testMediaStoryID+"/media/"+testMediaID+"/content?variant=thumb&format=gif",
		nil,
	)
	request.SetPathValue("mid", testMediaID)
	recorder := httptest.NewRecorder()
	handler.Content(recorder, request)

	if recorder.Code != http.StatusBadRequest {
		t.Fatalf("status = %d, want %d", recorder.Code, http.StatusBadRequest)
	}
	if code := decodedErrorCode(t, recorder); code != codeInvalidRequest {
		t.Errorf("error code = %q, want %q", code, codeInvalidRequest)
	}
	if service.gotVariant != storymedia.Variant("") {
		t.Errorf("the service was called for an invalid format, want no call")
	}
}

func TestStoryMediaContentRejectsAFormatWithoutTheThumbVariant(t *testing.T) {
	service := &fakeStoryMediaService{}
	handler, err := NewStoryMediaHandler(service, slog.New(slog.NewTextHandler(io.Discard, nil)))
	if err != nil {
		t.Fatalf("NewStoryMediaHandler() error = %v, want nil", err)
	}

	request := httptest.NewRequest(
		http.MethodGet,
		"/stories/"+testMediaStoryID+"/media/"+testMediaID+"/content?variant=medium&format=avif",
		nil,
	)
	request.SetPathValue("mid", testMediaID)
	recorder := httptest.NewRecorder()
	handler.Content(recorder, request)

	if recorder.Code != http.StatusBadRequest {
		t.Fatalf("status = %d, want %d", recorder.Code, http.StatusBadRequest)
	}
	if code := decodedErrorCode(t, recorder); code != codeInvalidRequest {
		t.Errorf("error code = %q, want %q", code, codeInvalidRequest)
	}
	if service.gotVariant != storymedia.Variant("") {
		t.Errorf("the service was called for a rejected combination, want no call")
	}
}

// TestStoryMediaContentAnswers202WhileProcessing pins Item 5's processing gate: a
// client is told to wait rather than shown a 404 for media that is on its way
// (KNOT-037-fix7).
func TestStoryMediaContentAnswers202WhileProcessing(t *testing.T) {
	service := &fakeStoryMediaService{streamErr: storymedia.ErrProcessing}
	handler, err := NewStoryMediaHandler(service, slog.New(slog.NewTextHandler(io.Discard, nil)))
	if err != nil {
		t.Fatalf("NewStoryMediaHandler() error = %v, want nil", err)
	}

	request := httptest.NewRequest(
		http.MethodGet,
		"/stories/"+testMediaStoryID+"/media/"+testMediaID+"/content?variant=display",
		nil,
	)
	request.SetPathValue("mid", testMediaID)
	recorder := httptest.NewRecorder()
	handler.Content(recorder, request)

	if recorder.Code != http.StatusAccepted {
		t.Fatalf("status = %d, want %d", recorder.Code, http.StatusAccepted)
	}
	if got := recorder.Header().Get("Retry-After"); got != storyMediaRetryAfter {
		t.Errorf("Retry-After = %q, want %q", got, storyMediaRetryAfter)
	}
	if recorder.Body.Len() != 0 {
		t.Errorf("body = %q, want none", recorder.Body.String())
	}
}

// TestStoryMediaContentAnswers404ForAMissingVariant pins KNOT-037-fix10: a
// variant the row cannot answer is a 404, never a fallback to the original.
func TestStoryMediaContentAnswers404ForAMissingVariant(t *testing.T) {
	service := &fakeStoryMediaService{streamErr: storymedia.ErrNotFound}
	handler, err := NewStoryMediaHandler(service, slog.New(slog.NewTextHandler(io.Discard, nil)))
	if err != nil {
		t.Fatalf("NewStoryMediaHandler() error = %v, want nil", err)
	}

	request := httptest.NewRequest(
		http.MethodGet,
		"/stories/"+testMediaStoryID+"/media/"+testMediaID+"/content?variant=display",
		nil,
	)
	request.SetPathValue("mid", testMediaID)
	recorder := httptest.NewRecorder()
	handler.Content(recorder, request)

	if recorder.Code != http.StatusNotFound {
		t.Fatalf("status = %d, want %d", recorder.Code, http.StatusNotFound)
	}
	if code := decodedErrorCode(t, recorder); code != codeNotFound {
		t.Errorf("error code = %q, want %q", code, codeNotFound)
	}
}

// TestStoryMediaContentAnswers404WhenProcessingFailed: nothing is coming for a
// failed row, so the client must stop waiting.
func TestStoryMediaContentAnswers404WhenProcessingFailed(t *testing.T) {
	service := &fakeStoryMediaService{streamErr: storymedia.ErrProcessingFailed}
	handler, err := NewStoryMediaHandler(service, slog.New(slog.NewTextHandler(io.Discard, nil)))
	if err != nil {
		t.Fatalf("NewStoryMediaHandler() error = %v, want nil", err)
	}

	request := httptest.NewRequest(
		http.MethodGet,
		"/stories/"+testMediaStoryID+"/media/"+testMediaID+"/content?variant=display",
		nil,
	)
	request.SetPathValue("mid", testMediaID)
	recorder := httptest.NewRecorder()
	handler.Content(recorder, request)

	if recorder.Code != http.StatusNotFound {
		t.Fatalf("status = %d, want %d", recorder.Code, http.StatusNotFound)
	}
}

func TestStoryMediaContentRejectsAnUnknownVariant(t *testing.T) {
	service := &fakeStoryMediaService{}
	handler, err := NewStoryMediaHandler(service, slog.New(slog.NewTextHandler(io.Discard, nil)))
	if err != nil {
		t.Fatalf("NewStoryMediaHandler() error = %v, want nil", err)
	}

	request := httptest.NewRequest(
		http.MethodGet,
		"/stories/"+testMediaStoryID+"/media/"+testMediaID+"/content?variant=huge",
		nil,
	)
	request.SetPathValue("mid", testMediaID)
	recorder := httptest.NewRecorder()
	handler.Content(recorder, request)

	if recorder.Code != http.StatusBadRequest {
		t.Fatalf("status = %d, want %d", recorder.Code, http.StatusBadRequest)
	}
	if code := decodedErrorCode(t, recorder); code != codeInvalidRequest {
		t.Errorf("error code = %q, want %q", code, codeInvalidRequest)
	}
	// A rejected request must not reach storage: answering with the original would
	// send exactly the bytes the caller asked to avoid.
	if service.gotVariant != storymedia.Variant("") {
		t.Errorf("the service was called with variant %q, want none", service.gotVariant)
	}
}

// TestFirstMediaFieldsPrefersTheThumbnail is the feed's contract: a card reads
// `first_media_url`, and that address is the thumbnail when the item has one.
func TestFirstMediaFieldsPrefersTheThumbnail(t *testing.T) {
	original := "/stories/" + testMediaStoryID + "/media/" + testMediaID + "/content"
	thumbnail := original + "?variant=thumb&format=avif"

	withThumbnail := firstMediaFields([]storyMediaResponse{
		{MediaType: "image", URL: original, ThumbnailURL: &thumbnail},
	})
	if withThumbnail.url == nil || *withThumbnail.url != thumbnail {
		t.Errorf("first media url = %v, want the thumbnail %q", withThumbnail.url, thumbnail)
	}

	// An item with no rendition keeps the original, so nothing disappears when the
	// pipeline did not run for it.
	withoutThumbnail := firstMediaFields([]storyMediaResponse{{MediaType: "image", URL: original}})
	if withoutThumbnail.url == nil || *withoutThumbnail.url != original {
		t.Errorf("first media url = %v, want the original %q", withoutThumbnail.url, original)
	}
}
