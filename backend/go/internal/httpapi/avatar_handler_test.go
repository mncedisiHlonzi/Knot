package httpapi

import (
	"bytes"
	"context"
	"errors"
	"fmt"
	"image"
	"image/jpeg"
	"image/png"
	"io"
	"log/slog"
	"mime/multipart"
	"net/http"
	"net/http/httptest"
	"regexp"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/knot/backend/internal/identity"
	"github.com/knot/backend/internal/storage"
)

// testAvatarUserID is the subject the fake token parser authenticates, and the
// owner of the user in newAvatarTestEnv.
const testAvatarUserID = "33333333-3333-4333-8333-333333333333"

// ---------------------------------------------------------------------------
// Fakes
// ---------------------------------------------------------------------------

// memObject is one stored object: its bytes and the type it was stored under.
type memObject struct {
	data        []byte
	contentType string
}

// memStorage is an in-memory storage.Storage.
//
// It records the keys it was asked to delete, because "the replaced avatar is
// removed" and "a failed upload does not leak an object" are only observable
// through what the handler tried to delete.
type memStorage struct {
	mu        sync.Mutex
	objects   map[string]memObject
	deleted   []string
	putErr    error
	getErr    error
	deleteErr error
}

func newMemStorage() *memStorage {
	return &memStorage{objects: map[string]memObject{}}
}

func (s *memStorage) Put(_ context.Context, key string, body io.Reader, contentType string) error {
	if s.putErr != nil {
		return s.putErr
	}

	data, err := io.ReadAll(body)
	if err != nil {
		return err
	}

	s.mu.Lock()
	defer s.mu.Unlock()
	s.objects[key] = memObject{data: data, contentType: contentType}

	return nil
}

func (s *memStorage) Get(_ context.Context, key string) (io.ReadCloser, string, error) {
	if s.getErr != nil {
		return nil, "", s.getErr
	}

	s.mu.Lock()
	defer s.mu.Unlock()

	object, ok := s.objects[key]
	if !ok {
		return nil, "", storage.ErrObjectNotFound
	}

	return io.NopCloser(bytes.NewReader(object.data)), object.contentType, nil
}

func (s *memStorage) Delete(_ context.Context, key string) error {
	if s.deleteErr != nil {
		return s.deleteErr
	}

	s.mu.Lock()
	defer s.mu.Unlock()
	s.deleted = append(s.deleted, key)
	delete(s.objects, key)

	return nil
}

func (s *memStorage) GetRange(_ context.Context, key string, start, end int64) (io.ReadCloser, error) {
	s.mu.Lock()
	defer s.mu.Unlock()

	object, ok := s.objects[key]
	if !ok {
		return nil, storage.ErrObjectNotFound
	}
	if end >= int64(len(object.data)) {
		end = int64(len(object.data)) - 1
	}
	if start < 0 || end < start {
		return nil, fmt.Errorf("memStorage: invalid range %d-%d", start, end)
	}

	return io.NopCloser(bytes.NewReader(object.data[start : end+1])), nil
}

func (s *memStorage) Exists(_ context.Context, key string) (bool, error) {
	s.mu.Lock()
	defer s.mu.Unlock()

	_, ok := s.objects[key]

	return ok, nil
}

// object returns the stored object, and whether it exists.
func (s *memStorage) object(key string) (memObject, bool) {
	s.mu.Lock()
	defer s.mu.Unlock()

	object, ok := s.objects[key]

	return object, ok
}

// storedKeys returns the keys currently held. Exactly one is expected after a
// successful upload.
func (s *memStorage) storedKeys() []string {
	s.mu.Lock()
	defer s.mu.Unlock()

	keys := make([]string, 0, len(s.objects))
	for key := range s.objects {
		keys = append(keys, key)
	}

	return keys
}

// deletedKeys returns the keys the handler asked to delete, in order.
func (s *memStorage) deletedKeys() []string {
	s.mu.Lock()
	defer s.mu.Unlock()

	return append([]string(nil), s.deleted...)
}

// fakeAvatarService is the AvatarService the handler is tested against.
type fakeAvatarService struct {
	mu      sync.Mutex
	users   map[string]*identity.User
	findErr error
	setErr  error
}

func newFakeAvatarService(users ...*identity.User) *fakeAvatarService {
	byID := make(map[string]*identity.User, len(users))
	for _, user := range users {
		byID[user.ID] = user
	}

	return &fakeAvatarService{users: byID}
}

func (s *fakeAvatarService) UserByID(_ context.Context, id string) (*identity.User, error) {
	if s.findErr != nil {
		return nil, s.findErr
	}

	s.mu.Lock()
	defer s.mu.Unlock()

	user, ok := s.users[id]
	if !ok {
		return nil, identity.ErrUserNotFound
	}

	return user, nil
}

func (s *fakeAvatarService) SetAvatarURL(_ context.Context, userID, key string) (*identity.User, error) {
	if s.setErr != nil {
		return nil, s.setErr
	}

	s.mu.Lock()
	defer s.mu.Unlock()

	user, ok := s.users[userID]
	if !ok {
		return nil, identity.ErrUserNotFound
	}

	user.AvatarURL = key

	return user, nil
}

// ---------------------------------------------------------------------------
// Fixtures
// ---------------------------------------------------------------------------

// avatarTestEnv is a handler wired to in-memory fakes, plus the fake service and
// storage so a test can inspect and perturb them.
type avatarTestEnv struct {
	handler *AvatarHandler
	service *fakeAvatarService
	storage *memStorage
	user    *identity.User
}

func newAvatarTestEnv(t *testing.T) *avatarTestEnv {
	t.Helper()

	user := &identity.User{
		ID:                 testAvatarUserID,
		Email:              "ada@example.com",
		DisplayName:        "Ada Lovelace",
		PreferredLanguages: []string{"eng"},
		CreatedAt:          time.Date(2026, 1, 2, 3, 4, 5, 0, time.UTC),
	}

	service := newFakeAvatarService(user)
	store := newMemStorage()

	handler, err := NewAvatarHandler(service, store, discardLogger())
	if err != nil {
		t.Fatalf("NewAvatarHandler() error = %v, want nil", err)
	}

	return &avatarTestEnv{handler: handler, service: service, storage: store, user: user}
}

// newTestAvatarHandler returns an avatar handler over empty in-memory fakes. The
// other handler test files use it to assemble a router, where the avatar routes
// are incidental to what they are testing.
func newTestAvatarHandler(t *testing.T, logger *slog.Logger) *AvatarHandler {
	t.Helper()

	handler, err := NewAvatarHandler(newFakeAvatarService(), newMemStorage(), logger)
	if err != nil {
		t.Fatalf("NewAvatarHandler() error = %v, want nil", err)
	}

	return handler
}

// pngImage returns a real, encoded PNG.
func pngImage(t *testing.T) []byte {
	t.Helper()

	var buffer bytes.Buffer
	if err := png.Encode(&buffer, image.NewRGBA(image.Rect(0, 0, 2, 2))); err != nil {
		t.Fatalf("encoding PNG: %v", err)
	}

	return buffer.Bytes()
}

// jpegImage returns a real, encoded JPEG.
func jpegImage(t *testing.T) []byte {
	t.Helper()

	var buffer bytes.Buffer
	if err := jpeg.Encode(&buffer, image.NewRGBA(image.Rect(0, 0, 2, 2)), nil); err != nil {
		t.Fatalf("encoding JPEG: %v", err)
	}

	return buffer.Bytes()
}

// webpImage returns the shortest byte sequence that sniffs as image/webp. Go has
// no WebP encoder, and sniffing only inspects the RIFF/WEBP container header.
func webpImage() []byte {
	return []byte("RIFF\x00\x00\x00\x00WEBPVP")
}

// multipartRequest builds a POST /users/me/avatar request carrying data in the
// named form field.
func multipartRequest(t *testing.T, field string, data []byte) *http.Request {
	t.Helper()

	var body bytes.Buffer
	writer := multipart.NewWriter(&body)

	part, err := writer.CreateFormFile(field, "upload.bin")
	if err != nil {
		t.Fatalf("CreateFormFile() error = %v, want nil", err)
	}
	if _, err := part.Write(data); err != nil {
		t.Fatalf("writing part: %v", err)
	}
	if err := writer.Close(); err != nil {
		t.Fatalf("closing multipart writer: %v", err)
	}

	request := httptest.NewRequest(http.MethodPost, "/users/me/avatar", &body)
	request.Header.Set("Content-Type", writer.FormDataContentType())

	return request
}

// uploadAvatar calls the upload handler directly, with the authenticated user on
// the context exactly as the auth middleware would have left it.
func (e *avatarTestEnv) uploadAvatar(t *testing.T, request *http.Request) *httptest.ResponseRecorder {
	t.Helper()

	recorder := httptest.NewRecorder()
	e.handler.Upload(recorder, request.WithContext(withUserID(request.Context(), testAvatarUserID)))

	return recorder
}

// newAvatarTestRouter assembles the router the way cmd/knot does, so the avatar
// routes are tested through the real route table and middleware chain.
func newAvatarTestRouter(t *testing.T, env *avatarTestEnv) http.Handler {
	t.Helper()

	logger := discardLogger()

	authHandler, err := NewAuthHandler(&fakeAuthService{}, logger)
	if err != nil {
		t.Fatalf("NewAuthHandler() error = %v, want nil", err)
	}

	storiesHandler, err := NewStoriesHandler(&fakeStoriesService{}, &fakeAuthorService{}, &fakeRootedService{}, &fakeStoryMediaLookup{}, &fakeReactionsLookup{}, logger)
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

	discoveryHandler, err := NewDiscoveryHandler(fakeDiscoveryService{}, &fakeRootedService{}, logger)
	if err != nil {
		t.Fatalf("NewDiscoveryHandler() error = %v, want nil", err)
	}

	authMiddleware, err := NewAuthMiddleware(&fakeTokenParser{subject: testAvatarUserID}, logger)
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
		env.handler,
		newTestStoryMediaHandler(t, logger),
		newTestNotificationsHandler(t, logger),
		newTestProfileHandler(t, logger),
		newTestReactionsRouterHandler(t, logger),
		newTestInquiriesHandler(t, logger),
		newTestModerationHandler(t, logger),
		authMiddleware,
		"0.1.0",
		logger,
	)
	if err != nil {
		t.Fatalf("NewRouter() error = %v, want nil", err)
	}

	return router.Handler()
}

// ---------------------------------------------------------------------------
// Construction
// ---------------------------------------------------------------------------

func TestNewAvatarHandlerRejectsMissingDependencies(t *testing.T) {
	tests := []struct {
		name    string
		service AvatarService
		storage storage.Storage
		logger  *slog.Logger
	}{
		{name: "no service", service: nil, storage: newMemStorage(), logger: discardLogger()},
		{name: "no storage", service: newFakeAvatarService(), storage: nil, logger: discardLogger()},
		{name: "no logger", service: newFakeAvatarService(), storage: newMemStorage(), logger: nil},
	}

	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			if _, err := NewAvatarHandler(test.service, test.storage, test.logger); err == nil {
				t.Error("NewAvatarHandler() error = nil, want an error")
			}
		})
	}
}

// ---------------------------------------------------------------------------
// POST /users/me/avatar
// ---------------------------------------------------------------------------

func TestAvatarUploadRequiresAuthentication(t *testing.T) {
	env := newAvatarTestEnv(t)
	router := newAvatarTestRouter(t, env)

	recorder := httptest.NewRecorder()
	router.ServeHTTP(recorder, multipartRequest(t, avatarFormField, pngImage(t)))

	if recorder.Code != http.StatusUnauthorized {
		t.Fatalf("status = %d, want %d", recorder.Code, http.StatusUnauthorized)
	}
	if code := decodedErrorCode(t, recorder); code != codeUnauthorized {
		t.Errorf("error code = %q, want %q", code, codeUnauthorized)
	}
	if len(env.storage.storedKeys()) != 0 {
		t.Errorf("stored keys = %v, want none: an unauthenticated upload must not write an object", env.storage.storedKeys())
	}
}

func TestAvatarUploadWithoutAnyIdentityOnTheContextIsUnauthorized(t *testing.T) {
	env := newAvatarTestEnv(t)

	recorder := httptest.NewRecorder()
	env.handler.Upload(recorder, multipartRequest(t, avatarFormField, pngImage(t)))

	if recorder.Code != http.StatusUnauthorized {
		t.Fatalf("status = %d, want %d", recorder.Code, http.StatusUnauthorized)
	}
	if code := decodedErrorCode(t, recorder); code != codeUnauthorized {
		t.Errorf("error code = %q, want %q", code, codeUnauthorized)
	}
}

func TestAvatarUploadStoresImageAndReturnsURL(t *testing.T) {
	env := newAvatarTestEnv(t)
	router := newAvatarTestRouter(t, env)

	image := pngImage(t)
	request := multipartRequest(t, avatarFormField, image)
	authorizeAs(request)

	recorder := httptest.NewRecorder()
	router.ServeHTTP(recorder, request)

	if recorder.Code != http.StatusOK {
		t.Fatalf("status = %d, want %d (body %s)", recorder.Code, http.StatusOK, recorder.Body.String())
	}

	var body avatarResponse
	decodeBody(t, recorder, &body)

	// The object key is namespaced by the owner, so a user can never write into
	// another user's prefix, and it carries a generated UUID rather than a fixed
	// name, so a replacement never overwrites bytes still being served.
	key := env.user.AvatarURL
	prefix := identity.AvatarKeyPrefix(testAvatarUserID)
	if !strings.HasPrefix(key, prefix) {
		t.Fatalf("stored key = %q, want it to start with %q", key, prefix)
	}
	if !regexp.MustCompile(`^[0-9a-f]{8}-[0-9a-f]{4}-4[0-9a-f]{3}-[89ab][0-9a-f]{3}-[0-9a-f]{12}\.png$`).MatchString(strings.TrimPrefix(key, prefix)) {
		t.Errorf("object name = %q, want a version 4 UUID followed by .png", strings.TrimPrefix(key, prefix))
	}

	stored := env.storage.storedKeys()
	if len(stored) != 1 || stored[0] != key {
		t.Fatalf("stored keys = %v, want exactly [%s]", stored, key)
	}

	object, ok := env.storage.object(key)
	if !ok {
		t.Fatalf("object %q is not in the store", key)
	}
	if object.contentType != "image/png" {
		t.Errorf("stored content type = %q, want %q", object.contentType, "image/png")
	}
	if !bytes.Equal(object.data, image) {
		t.Error("stored bytes differ from the uploaded bytes")
	}

	// The response advertises a path on this API, never a link to object storage,
	// and the v parameter changes with the object so caches cannot serve a stale
	// avatar after a replacement.
	wantURL := "/users/" + testAvatarUserID + "/avatar?v=" + strings.TrimPrefix(key, prefix)
	if body.User.AvatarURL != wantURL {
		t.Errorf("avatar_url = %q, want %q", body.User.AvatarURL, wantURL)
	}
	if body.User.ID != testAvatarUserID {
		t.Errorf("user.id = %q, want %q", body.User.ID, testAvatarUserID)
	}
}

// TestAvatarUploadReturnsTheSharedUserProjection pins that an upload answers with
// the same user shape that register and login return, so a client has one profile
// type to parse.
func TestAvatarUploadReturnsTheSharedUserProjection(t *testing.T) {
	env := newAvatarTestEnv(t)

	recorder := env.uploadAvatar(t, multipartRequest(t, avatarFormField, pngImage(t)))
	if recorder.Code != http.StatusOK {
		t.Fatalf("status = %d, want %d", recorder.Code, http.StatusOK)
	}

	var body avatarResponse
	decodeBody(t, recorder, &body)

	if body.User.Email != env.user.Email {
		t.Errorf("user.email = %q, want %q", body.User.Email, env.user.Email)
	}
	if body.User.DisplayName != env.user.DisplayName {
		t.Errorf("user.display_name = %q, want %q", body.User.DisplayName, env.user.DisplayName)
	}
	if body.User.AvatarURL == "" {
		t.Error("user.avatar_url = \"\", want the URL of the avatar just stored")
	}
	if strings.Contains(recorder.Body.String(), identity.AvatarKeyPrefix(testAvatarUserID)) {
		t.Errorf("response body %q exposes the stored object key", recorder.Body.String())
	}
}

func TestAvatarUploadAcceptsSupportedImageTypes(t *testing.T) {
	tests := []struct {
		name       string
		image      func(*testing.T) []byte
		wantType   string
		wantSuffix string
	}{
		{name: "jpeg", image: jpegImage, wantType: "image/jpeg", wantSuffix: ".jpg"},
		{name: "png", image: pngImage, wantType: "image/png", wantSuffix: ".png"},
		{name: "webp", image: func(*testing.T) []byte { return webpImage() }, wantType: "image/webp", wantSuffix: ".webp"},
	}

	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			env := newAvatarTestEnv(t)

			recorder := env.uploadAvatar(t, multipartRequest(t, avatarFormField, test.image(t)))
			if recorder.Code != http.StatusOK {
				t.Fatalf("status = %d, want %d (body %s)", recorder.Code, http.StatusOK, recorder.Body.String())
			}

			if !strings.HasSuffix(env.user.AvatarURL, test.wantSuffix) {
				t.Errorf("stored key = %q, want suffix %q", env.user.AvatarURL, test.wantSuffix)
			}

			object, ok := env.storage.object(env.user.AvatarURL)
			if !ok {
				t.Fatalf("object %q is not in the store", env.user.AvatarURL)
			}
			if object.contentType != test.wantType {
				t.Errorf("stored content type = %q, want %q", object.contentType, test.wantType)
			}
		})
	}
}

// TestAvatarUploadIgnoresTheDeclaredContentType pins that the format is decided
// by inspecting the bytes. A caller must not be able to have its own type
// reflected back from the avatar URL.
func TestAvatarUploadIgnoresTheDeclaredContentType(t *testing.T) {
	env := newAvatarTestEnv(t)

	recorder := env.uploadAvatar(t, multipartRequest(t, avatarFormField, []byte("<script>alert(1)</script>")))

	if recorder.Code != http.StatusUnsupportedMediaType {
		t.Fatalf("status = %d, want %d", recorder.Code, http.StatusUnsupportedMediaType)
	}
	if code := decodedErrorCode(t, recorder); code != codeUnsupportedMediaType {
		t.Errorf("error code = %q, want %q", code, codeUnsupportedMediaType)
	}
	if len(env.storage.storedKeys()) != 0 {
		t.Errorf("stored keys = %v, want none", env.storage.storedKeys())
	}
}

func TestAvatarUploadRejectsUnsupportedImageTypes(t *testing.T) {
	tests := []struct {
		name  string
		bytes []byte
	}{
		{name: "text", bytes: []byte("this is not an image")},
		{name: "gif is an image but not an accepted one", bytes: []byte("GIF89a\x01\x00\x01\x00\x00\x00\x00;")},
		{name: "pdf", bytes: []byte("%PDF-1.7\n")},
	}

	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			env := newAvatarTestEnv(t)

			recorder := env.uploadAvatar(t, multipartRequest(t, avatarFormField, test.bytes))
			if recorder.Code != http.StatusUnsupportedMediaType {
				t.Fatalf("status = %d, want %d (body %s)", recorder.Code, http.StatusUnsupportedMediaType, recorder.Body.String())
			}
			if code := decodedErrorCode(t, recorder); code != codeUnsupportedMediaType {
				t.Errorf("error code = %q, want %q", code, codeUnsupportedMediaType)
			}
			if len(env.storage.storedKeys()) != 0 {
				t.Errorf("stored keys = %v, want none", env.storage.storedKeys())
			}
		})
	}
}

func TestAvatarUploadRejectsOversizedImage(t *testing.T) {
	tests := []struct {
		name  string
		bytes []byte
	}{
		{
			// One byte past the cap, with a body that still fits under the request
			// cap: this exercises the read limit rather than the declared length.
			name:  "one byte over the limit",
			bytes: bytes.Repeat([]byte{0x41}, maxAvatarBytes+1),
		},
		{
			name:  "far over the limit",
			bytes: bytes.Repeat([]byte{0x41}, 2*maxAvatarBytes),
		},
	}

	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			env := newAvatarTestEnv(t)

			recorder := env.uploadAvatar(t, multipartRequest(t, avatarFormField, test.bytes))
			if recorder.Code != http.StatusRequestEntityTooLarge {
				t.Fatalf("status = %d, want %d (body %s)", recorder.Code, http.StatusRequestEntityTooLarge, recorder.Body.String())
			}
			if code := decodedErrorCode(t, recorder); code != codeRequestTooLarge {
				t.Errorf("error code = %q, want %q", code, codeRequestTooLarge)
			}
			if len(env.storage.storedKeys()) != 0 {
				t.Errorf("stored keys = %v, want none", env.storage.storedKeys())
			}
		})
	}
}

func TestAvatarUploadAcceptsAnImageAtExactlyTheLimit(t *testing.T) {
	env := newAvatarTestEnv(t)

	// A PNG signature followed by padding, sized to exactly the cap.
	image := make([]byte, 0, maxAvatarBytes)
	image = append(image, pngImage(t)...)
	image = append(image, bytes.Repeat([]byte{0x00}, maxAvatarBytes-len(image))...)

	recorder := env.uploadAvatar(t, multipartRequest(t, avatarFormField, image))
	if recorder.Code != http.StatusOK {
		t.Fatalf("status = %d, want %d (body %s)", recorder.Code, http.StatusOK, recorder.Body.String())
	}
}

func TestAvatarUploadRejectsRequestsWithoutTheFileField(t *testing.T) {
	env := newAvatarTestEnv(t)

	recorder := env.uploadAvatar(t, multipartRequest(t, "avatar", pngImage(t)))
	if recorder.Code != http.StatusBadRequest {
		t.Fatalf("status = %d, want %d", recorder.Code, http.StatusBadRequest)
	}
	if code := decodedErrorCode(t, recorder); code != codeInvalidRequest {
		t.Errorf("error code = %q, want %q", code, codeInvalidRequest)
	}
}

func TestAvatarUploadRejectsNonMultipartBodies(t *testing.T) {
	env := newAvatarTestEnv(t)

	request := httptest.NewRequest(http.MethodPost, "/users/me/avatar", strings.NewReader(`{"avatar":"..."}`))
	request.Header.Set("Content-Type", "application/json")

	recorder := env.uploadAvatar(t, request)
	if recorder.Code != http.StatusBadRequest {
		t.Fatalf("status = %d, want %d (body %s)", recorder.Code, http.StatusBadRequest, recorder.Body.String())
	}
	if code := decodedErrorCode(t, recorder); code != codeInvalidRequest {
		t.Errorf("error code = %q, want %q", code, codeInvalidRequest)
	}
}

func TestAvatarUploadRejectsEmptyImages(t *testing.T) {
	env := newAvatarTestEnv(t)

	recorder := env.uploadAvatar(t, multipartRequest(t, avatarFormField, nil))
	if recorder.Code != http.StatusBadRequest {
		t.Fatalf("status = %d, want %d", recorder.Code, http.StatusBadRequest)
	}
	if code := decodedErrorCode(t, recorder); code != codeInvalidRequest {
		t.Errorf("error code = %q, want %q", code, codeInvalidRequest)
	}
}

func TestAvatarUploadRejectsUnknownUsers(t *testing.T) {
	env := newAvatarTestEnv(t)
	env.service.users = map[string]*identity.User{}

	recorder := env.uploadAvatar(t, multipartRequest(t, avatarFormField, pngImage(t)))
	if recorder.Code != http.StatusNotFound {
		t.Fatalf("status = %d, want %d", recorder.Code, http.StatusNotFound)
	}
	if code := decodedErrorCode(t, recorder); code != codeNotFound {
		t.Errorf("error code = %q, want %q", code, codeNotFound)
	}
	if len(env.storage.storedKeys()) != 0 {
		t.Errorf("stored keys = %v, want none", env.storage.storedKeys())
	}
}

func TestAvatarUploadReplacesThePreviousAvatarAndDeletesIt(t *testing.T) {
	env := newAvatarTestEnv(t)

	first := env.uploadAvatar(t, multipartRequest(t, avatarFormField, pngImage(t)))
	if first.Code != http.StatusOK {
		t.Fatalf("first upload status = %d, want %d", first.Code, http.StatusOK)
	}
	firstKey := env.user.AvatarURL

	second := env.uploadAvatar(t, multipartRequest(t, avatarFormField, jpegImage(t)))
	if second.Code != http.StatusOK {
		t.Fatalf("second upload status = %d, want %d", second.Code, http.StatusOK)
	}
	secondKey := env.user.AvatarURL

	if secondKey == firstKey {
		t.Fatalf("both uploads used the key %q, want a new key per upload", firstKey)
	}

	stored := env.storage.storedKeys()
	if len(stored) != 1 || stored[0] != secondKey {
		t.Errorf("stored keys = %v, want exactly [%s]: the replaced object should be gone", stored, secondKey)
	}

	deleted := env.storage.deletedKeys()
	if len(deleted) != 1 || deleted[0] != firstKey {
		t.Errorf("deleted keys = %v, want exactly [%s]", deleted, firstKey)
	}
}

func TestAvatarUploadReportsAFailedObjectStoreWrite(t *testing.T) {
	env := newAvatarTestEnv(t)
	env.storage.putErr = errors.New("object store is unreachable")

	recorder := env.uploadAvatar(t, multipartRequest(t, avatarFormField, pngImage(t)))
	if recorder.Code != http.StatusInternalServerError {
		t.Fatalf("status = %d, want %d", recorder.Code, http.StatusInternalServerError)
	}
	if code := decodedErrorCode(t, recorder); code != codeInternal {
		t.Errorf("error code = %q, want %q", code, codeInternal)
	}
	if env.user.AvatarURL != "" {
		t.Errorf("avatar_url = %q, want it unchanged after a failed write", env.user.AvatarURL)
	}
	if body := recorder.Body.String(); strings.Contains(body, "unreachable") {
		t.Errorf("response body %q leaks the underlying error", body)
	}
}

func TestAvatarUploadDiscardsTheObjectWhenTheRowCannotBeUpdated(t *testing.T) {
	env := newAvatarTestEnv(t)
	env.service.setErr = errors.New("database is unreachable")

	recorder := env.uploadAvatar(t, multipartRequest(t, avatarFormField, pngImage(t)))
	if recorder.Code != http.StatusInternalServerError {
		t.Fatalf("status = %d, want %d", recorder.Code, http.StatusInternalServerError)
	}
	if len(env.storage.storedKeys()) != 0 {
		t.Errorf("stored keys = %v, want none: a failed upload must not leak an object", env.storage.storedKeys())
	}
}

// TestAvatarUploadKeepsTheOldAvatarWhenStorageFails pins the ordering: the
// previous key is read before the new object is written, so a failure leaves the
// user's existing avatar working rather than unreferenced.
func TestAvatarUploadKeepsTheOldAvatarWhenStorageFails(t *testing.T) {
	env := newAvatarTestEnv(t)

	first := env.uploadAvatar(t, multipartRequest(t, avatarFormField, pngImage(t)))
	if first.Code != http.StatusOK {
		t.Fatalf("first upload status = %d, want %d", first.Code, http.StatusOK)
	}
	firstKey := env.user.AvatarURL

	env.storage.putErr = errors.New("object store is unreachable")
	second := env.uploadAvatar(t, multipartRequest(t, avatarFormField, jpegImage(t)))
	if second.Code != http.StatusInternalServerError {
		t.Fatalf("second upload status = %d, want %d", second.Code, http.StatusInternalServerError)
	}

	if env.user.AvatarURL != firstKey {
		t.Errorf("avatar_url = %q, want it to remain %q", env.user.AvatarURL, firstKey)
	}
	if _, ok := env.storage.object(firstKey); !ok {
		t.Error("the existing avatar object was removed")
	}
}

// ---------------------------------------------------------------------------
// GET /users/{id}/avatar
// ---------------------------------------------------------------------------

func TestAvatarServeReturnsTheStoredImage(t *testing.T) {
	env := newAvatarTestEnv(t)
	image := pngImage(t)

	upload := env.uploadAvatar(t, multipartRequest(t, avatarFormField, image))
	if upload.Code != http.StatusOK {
		t.Fatalf("upload status = %d, want %d", upload.Code, http.StatusOK)
	}

	router := newAvatarTestRouter(t, env)
	request := httptest.NewRequest(http.MethodGet, "/users/"+testAvatarUserID+"/avatar", nil)
	recorder := httptest.NewRecorder()
	router.ServeHTTP(recorder, request)

	if recorder.Code != http.StatusOK {
		t.Fatalf("status = %d, want %d (body %s)", recorder.Code, http.StatusOK, recorder.Body.String())
	}
	if got := recorder.Header().Get("Content-Type"); got != "image/png" {
		t.Errorf("Content-Type = %q, want %q", got, "image/png")
	}
	if got := recorder.Header().Get("Cache-Control"); got != avatarCacheControl {
		t.Errorf("Cache-Control = %q, want %q", got, avatarCacheControl)
	}
	if got := recorder.Header().Get("X-Content-Type-Options"); got != "nosniff" {
		t.Errorf("X-Content-Type-Options = %q, want %q", got, "nosniff")
	}
	if !bytes.Equal(recorder.Body.Bytes(), image) {
		t.Error("served bytes differ from the uploaded bytes")
	}
}

// TestAvatarServeIsPublic pins that reading an avatar needs no credentials: an
// avatar is part of a public profile.
func TestAvatarServeIsPublic(t *testing.T) {
	env := newAvatarTestEnv(t)
	env.user.AvatarURL = identity.AvatarKeyPrefix(testAvatarUserID) + "abcdef.png"
	if err := env.storage.Put(context.Background(), env.user.AvatarURL, bytes.NewReader(pngImage(t)), "image/png"); err != nil {
		t.Fatalf("Put() error = %v, want nil", err)
	}

	router := newAvatarTestRouter(t, env)
	request := httptest.NewRequest(http.MethodGet, "/users/"+testAvatarUserID+"/avatar", nil)
	recorder := httptest.NewRecorder()
	router.ServeHTTP(recorder, request)

	if recorder.Code != http.StatusOK {
		t.Fatalf("status = %d, want %d", recorder.Code, http.StatusOK)
	}
}

func TestAvatarServeReturnsNotFoundWhenThereIsNoAvatar(t *testing.T) {
	tests := []struct {
		name   string
		setup  func(*avatarTestEnv)
		userID string
	}{
		{
			name:   "the user exists but has never uploaded one",
			setup:  func(*avatarTestEnv) {},
			userID: testAvatarUserID,
		},
		{
			name:   "the user does not exist",
			setup:  func(*avatarTestEnv) {},
			userID: "44444444-4444-4444-8444-444444444444",
		},
		{
			name: "the row references an object that is gone",
			setup: func(env *avatarTestEnv) {
				env.user.AvatarURL = identity.AvatarKeyPrefix(testAvatarUserID) + "missing.png"
			},
			userID: testAvatarUserID,
		},
		{
			name:   "the id is not a uuid",
			setup:  func(*avatarTestEnv) {},
			userID: "not-a-uuid",
		},
	}

	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			env := newAvatarTestEnv(t)
			test.setup(env)

			recorder := httptest.NewRecorder()
			env.handler.Get(recorder, pathRequest(t, test.userID, ""))

			if recorder.Code != http.StatusNotFound {
				t.Fatalf("status = %d, want %d (body %s)", recorder.Code, http.StatusNotFound, recorder.Body.String())
			}
			if code := decodedErrorCode(t, recorder); code != codeNotFound {
				t.Errorf("error code = %q, want %q", code, codeNotFound)
			}
		})
	}
}

func TestAvatarServeReturnsInternalErrorWhenTheStoreFails(t *testing.T) {
	env := newAvatarTestEnv(t)
	env.user.AvatarURL = identity.AvatarKeyPrefix(testAvatarUserID) + "abcdef.png"
	env.storage.getErr = errors.New("object store is unreachable")

	recorder := httptest.NewRecorder()
	env.handler.Get(recorder, pathRequest(t, testAvatarUserID, ""))

	if recorder.Code != http.StatusInternalServerError {
		t.Fatalf("status = %d, want %d", recorder.Code, http.StatusInternalServerError)
	}
	if code := decodedErrorCode(t, recorder); code != codeInternal {
		t.Errorf("error code = %q, want %q", code, codeInternal)
	}
}

// TestAvatarServeIgnoresTheCacheBustingParameter pins that the v parameter is
// decoration for caches only: the object served is the one the row references.
func TestAvatarServeIgnoresTheCacheBustingParameter(t *testing.T) {
	env := newAvatarTestEnv(t)
	env.user.AvatarURL = identity.AvatarKeyPrefix(testAvatarUserID) + "abcdef.png"
	if err := env.storage.Put(context.Background(), env.user.AvatarURL, bytes.NewReader(pngImage(t)), "image/png"); err != nil {
		t.Fatalf("Put() error = %v, want nil", err)
	}

	recorder := httptest.NewRecorder()
	env.handler.Get(recorder, pathRequest(t, testAvatarUserID, "v=00000000-0000-4000-8000-000000000000.png"))

	if recorder.Code != http.StatusOK {
		t.Fatalf("status = %d, want %d", recorder.Code, http.StatusOK)
	}
}

// ---------------------------------------------------------------------------
// Helpers under test
// ---------------------------------------------------------------------------

func TestNewAvatarKeyIsNamespacedAndUnique(t *testing.T) {
	first, err := newAvatarKey(testAvatarUserID, ".png")
	if err != nil {
		t.Fatalf("newAvatarKey() error = %v, want nil", err)
	}
	second, err := newAvatarKey(testAvatarUserID, ".png")
	if err != nil {
		t.Fatalf("newAvatarKey() error = %v, want nil", err)
	}

	if first == second {
		t.Errorf("both keys = %q, want a fresh name per upload", first)
	}
	if !strings.HasPrefix(first, identity.AvatarKeyPrefix(testAvatarUserID)) {
		t.Errorf("key = %q, want it to start with %q", first, identity.AvatarKeyPrefix(testAvatarUserID))
	}
	if !strings.HasSuffix(first, ".png") {
		t.Errorf("key = %q, want the .png suffix", first)
	}
}

func TestAvatarPathFor(t *testing.T) {
	tests := []struct {
		name string
		user *identity.User
		want string
	}{
		{name: "nil user", user: nil, want: ""},
		{name: "no avatar", user: &identity.User{ID: testAvatarUserID}, want: ""},
		{
			name: "stored key is translated into a path on this API",
			user: &identity.User{
				ID:        testAvatarUserID,
				AvatarURL: identity.AvatarKeyPrefix(testAvatarUserID) + "abcdef-1234.png",
			},
			want: "/users/" + testAvatarUserID + "/avatar?v=abcdef-1234.png",
		},
	}

	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			if got := avatarPathFor(test.user); got != test.want {
				t.Errorf("avatarPathFor() = %q, want %q", got, test.want)
			}
		})
	}
}

// authorizeAs adds the bearer token the fake parser accepts, so a protected route
// sees the authenticated user on the context.
func authorizeAs(request *http.Request) {
	request.Header.Set("Authorization", "Bearer "+testAccessToken)
}

// pathRequest builds a GET request whose path variable is populated, so a handler
// reading r.PathValue("id") sees the same value the router would give it.
func pathRequest(t *testing.T, id string, rawQuery string) *http.Request {
	t.Helper()

	request := httptest.NewRequest(http.MethodGet, "/users/"+id+"/avatar", nil)
	request.URL.RawQuery = rawQuery
	request.SetPathValue("id", id)

	return request
}
