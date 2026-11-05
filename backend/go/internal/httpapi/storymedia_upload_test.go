package httpapi

import (
	"bytes"
	"context"
	"encoding/json"
	"io"
	"log/slog"
	"net/http"
	"net/http/httptest"
	"net/url"
	"strings"
	"testing"

	"github.com/knot/backend/internal/storymedia"
)

// Endpoint tests for the direct-to-bucket upload path (KNOT-040):
//
//	POST /media/upload-url   -> a signed URL and the object key
//	PUT  <upload_url>        -> the bytes (straight to the object store)
//	POST /stories/{id}/media -> { "object_key": "..." }
//
// The stand-in object store signs a URL-shaped value and holds the bytes the
// client "uploads", so the whole three-step flow is exercised without a network.

// postJSON sends a JSON body to path with the given bearer token ("" sends none).
func postJSON(router http.Handler, path string, payload any, token string) *httptest.ResponseRecorder {
	body, err := json.Marshal(payload)
	if err != nil {
		panic(err)
	}

	request := httptest.NewRequest(http.MethodPost, path, bytes.NewReader(body))
	request.Header.Set("Content-Type", "application/json")
	if token != "" {
		request.Header.Set("Authorization", "Bearer "+token)
	}

	recorder := httptest.NewRecorder()
	router.ServeHTTP(recorder, request)
	return recorder
}

// uploadURLRequestFor is the body the mobile client sends.
func uploadURLRequestFor(contentType string) mediaUploadURLRequest {
	// The filename is only validated (the object key is a fresh UUID), but it must
	// still look like one.
	extension := "bin"
	if slash := strings.LastIndexByte(contentType, '/'); slash >= 0 {
		extension = contentType[slash+1:]
	}

	return mediaUploadURLRequest{
		StoryID:     testMediaStoryID,
		Filename:    "photo." + extension,
		ContentType: contentType,
	}
}

// requestUploadURL signs a URL and returns the decoded response, failing the test
// if the endpoint does not answer 200.
func requestUploadURL(t *testing.T, router http.Handler, payload mediaUploadURLRequest) mediaUploadURLResponse {
	t.Helper()

	recorder := postJSON(router, "/media/upload-url", payload, testAccessToken)
	if recorder.Code != http.StatusOK {
		t.Fatalf("POST /media/upload-url status = %d, want %d (body %s)",
			recorder.Code, http.StatusOK, recorder.Body.String())
	}

	var got mediaUploadURLResponse
	decodeBody(t, recorder, &got)
	return got
}

// putUploadedObject is what the client's PUT does: the bytes land in the bucket
// under the key the signed URL named.
func (e *storyMediaTestEnv) putUploadedObject(t *testing.T, key, contentType string, data []byte) {
	t.Helper()

	if err := e.objects.Put(context.Background(), key, bytes.NewReader(data), contentType); err != nil {
		t.Fatalf("simulating the client's PUT: %v", err)
	}
}

// presignAndUpload runs the first two steps and returns the third step's body.
func (e *storyMediaTestEnv) presignAndUpload(t *testing.T, router http.Handler, contentType string) attachMediaRequest {
	t.Helper()

	target := requestUploadURL(t, router, uploadURLRequestFor(contentType))
	e.putUploadedObject(t, target.ObjectKey, contentType, []byte("bytes the client uploaded"))

	return attachMediaRequest{ObjectKey: target.ObjectKey, Source: string(storymedia.MediaSourceCamera)}
}

// ---------------------------------------------------------------------------
// POST /media/upload-url
// ---------------------------------------------------------------------------

func TestMediaUploadURLReturnsASignedURLAndObjectKey(t *testing.T) {
	env := newStoryMediaTestEnv(t)
	router := env.router(t, testMediaAuthorID)

	target := requestUploadURL(t, router, uploadURLRequestFor("image/jpeg"))

	parsed, err := url.Parse(target.UploadURL)
	if err != nil {
		t.Fatalf("upload_url %q is not a URL: %v", target.UploadURL, err)
	}
	if parsed.Scheme != "https" || parsed.Host == "" {
		t.Errorf("upload_url %q is not an absolute https URL", target.UploadURL)
	}
	if signature := parsed.Query().Get("X-Amz-Signature"); signature == "" {
		t.Errorf("upload_url %q carries no signature", target.UploadURL)
	}
	if !strings.Contains(target.UploadURL, target.ObjectKey) {
		t.Errorf("upload_url %q does not address the object key %q", target.UploadURL, target.ObjectKey)
	}

	if !strings.HasPrefix(target.ObjectKey, storymedia.StoryMediaKeyPrefix(testMediaStoryID)) {
		t.Errorf("object_key = %q, want the story's prefix %q",
			target.ObjectKey, storymedia.StoryMediaKeyPrefix(testMediaStoryID))
	}
	if !strings.HasSuffix(target.ObjectKey, "_original.jpg") {
		t.Errorf("object_key = %q, want the _original suffix and the type's extension", target.ObjectKey)
	}
	if target.ExpiresIn != int(storymedia.PresignTTL.Seconds()) {
		t.Errorf("expires_in = %d, want %d", target.ExpiresIn, int(storymedia.PresignTTL.Seconds()))
	}

	// Signing a URL writes nothing: no row, no object, no job.
	if len(env.publisher.queued()) != 0 {
		t.Errorf("queued %d jobs while signing a URL, want 0", len(env.publisher.queued()))
	}
	if len(env.store.media) != 0 {
		t.Errorf("created %d media rows while signing a URL, want 0", len(env.store.media))
	}
}

func TestMediaUploadURLDerivesTheExtensionFromTheContentType(t *testing.T) {
	env := newStoryMediaTestEnv(t)
	router := env.router(t, testMediaAuthorID)

	tests := []struct {
		contentType string
		wantSuffix  string
	}{
		{contentType: "image/jpeg", wantSuffix: "_original.jpg"},
		{contentType: "image/png", wantSuffix: "_original.png"},
		{contentType: "image/webp", wantSuffix: "_original.webp"},
		{contentType: "video/mp4", wantSuffix: "_original.mp4"},
		{contentType: "video/quicktime", wantSuffix: "_original.mov"},
	}

	for _, test := range tests {
		t.Run(test.contentType, func(t *testing.T) {
			target := requestUploadURL(t, router, uploadURLRequestFor(test.contentType))
			if !strings.HasSuffix(target.ObjectKey, test.wantSuffix) {
				t.Errorf("object_key = %q, want it to end in %q", target.ObjectKey, test.wantSuffix)
			}
		})
	}
}

func TestMediaUploadURLRequiresAuthentication(t *testing.T) {
	env := newStoryMediaTestEnv(t)
	router := env.router(t, testMediaAuthorID)

	recorder := postJSON(router, "/media/upload-url", uploadURLRequestFor("image/jpeg"), "")

	if recorder.Code != http.StatusUnauthorized {
		t.Errorf("status = %d, want %d", recorder.Code, http.StatusUnauthorized)
	}
}

func TestMediaUploadURLRejectsNonAuthor(t *testing.T) {
	env := newStoryMediaTestEnv(t)
	router := env.router(t, testMediaOtherID)

	recorder := postJSON(router, "/media/upload-url", uploadURLRequestFor("image/jpeg"), testAccessToken)

	if recorder.Code != http.StatusForbidden {
		t.Errorf("status = %d, want %d (body %s)", recorder.Code, http.StatusForbidden, recorder.Body.String())
	}
}

func TestMediaUploadURLUnknownStory(t *testing.T) {
	env := newStoryMediaTestEnv(t)
	router := env.router(t, testMediaAuthorID)

	payload := uploadURLRequestFor("image/jpeg")
	payload.StoryID = "99999999-9999-4999-8999-999999999999"

	recorder := postJSON(router, "/media/upload-url", payload, testAccessToken)

	if recorder.Code != http.StatusNotFound {
		t.Errorf("status = %d, want %d (body %s)", recorder.Code, http.StatusNotFound, recorder.Body.String())
	}
}

func TestMediaUploadURLRejectsUnsupportedContentType(t *testing.T) {
	env := newStoryMediaTestEnv(t)
	router := env.router(t, testMediaAuthorID)

	recorder := postJSON(router, "/media/upload-url", uploadURLRequestFor("image/gif"), testAccessToken)

	if recorder.Code != http.StatusUnsupportedMediaType {
		t.Errorf("status = %d, want %d (body %s)",
			recorder.Code, http.StatusUnsupportedMediaType, recorder.Body.String())
	}
}

func TestMediaUploadURLRejectsAMalformedBody(t *testing.T) {
	env := newStoryMediaTestEnv(t)
	router := env.router(t, testMediaAuthorID)

	request := httptest.NewRequest(http.MethodPost, "/media/upload-url", strings.NewReader(`{"story_id":`))
	request.Header.Set("Content-Type", "application/json")
	request.Header.Set("Authorization", "Bearer "+testAccessToken)

	recorder := httptest.NewRecorder()
	router.ServeHTTP(recorder, request)

	if recorder.Code != http.StatusBadRequest {
		t.Errorf("status = %d, want %d (body %s)", recorder.Code, http.StatusBadRequest, recorder.Body.String())
	}
}

func TestMediaUploadURLRejectsAMissingFilename(t *testing.T) {
	env := newStoryMediaTestEnv(t)
	router := env.router(t, testMediaAuthorID)

	payload := uploadURLRequestFor("image/jpeg")
	payload.Filename = ""

	recorder := postJSON(router, "/media/upload-url", payload, testAccessToken)

	if recorder.Code != http.StatusBadRequest {
		t.Errorf("status = %d, want %d (body %s)", recorder.Code, http.StatusBadRequest, recorder.Body.String())
	}
}

// ---------------------------------------------------------------------------
// POST /stories/{id}/media with a JSON object key
// ---------------------------------------------------------------------------

func TestStoryMediaAttachFromUploadedObject(t *testing.T) {
	env := newStoryMediaTestEnv(t)
	router := env.router(t, testMediaAuthorID)

	body := env.presignAndUpload(t, router, "image/jpeg")
	width, height := 2, 2
	body.Width = &width
	body.Height = &height

	recorder := postJSON(router, "/stories/"+testMediaStoryID+"/media", body, testAccessToken)

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
	if envelope.Media.ThumbnailURL == nil || !strings.Contains(*envelope.Media.ThumbnailURL, "variant=thumb") {
		t.Errorf("thumbnail_url = %v, want a stable thumb address already", envelope.Media.ThumbnailURL)
	}

	// The row records the very object the client uploaded, and a job was queued for
	// the worker to turn it into derivatives.
	stored, err := env.store.GetMedia(context.Background(), envelope.Media.ID)
	if err != nil {
		t.Fatalf("GetMedia() error = %v", err)
	}
	if stored.StorageKey != body.ObjectKey {
		t.Errorf("StorageKey = %q, want %q", stored.StorageKey, body.ObjectKey)
	}

	jobs := env.publisher.queued()
	if len(jobs) != 1 {
		t.Fatalf("queued %d jobs, want 1", len(jobs))
	}
	if jobs[0].ObjectKey != body.ObjectKey || jobs[0].MediaID != envelope.Media.ID {
		t.Errorf("job = %+v, want the uploaded object and the new row", jobs[0])
	}
}

func TestStoryMediaAttachVideoQueuesAVideoJob(t *testing.T) {
	env := newStoryMediaTestEnv(t)
	router := env.router(t, testMediaAuthorID)

	body := env.presignAndUpload(t, router, "video/mp4")

	recorder := postJSON(router, "/stories/"+testMediaStoryID+"/media", body, testAccessToken)
	if recorder.Code != http.StatusAccepted {
		t.Fatalf("status = %d, want %d (body %s)", recorder.Code, http.StatusAccepted, recorder.Body.String())
	}

	var envelope storyMediaEnvelope
	decodeBody(t, recorder, &envelope)

	if envelope.Media.MediaType != string(storymedia.MediaTypeVideo) {
		t.Errorf("media_type = %q, want video", envelope.Media.MediaType)
	}
	// A video has no cover: it autoplays (KNOT-037-fix7).
	if envelope.Media.ThumbnailURL != nil {
		t.Errorf("thumbnail_url = %v, want null for a video", envelope.Media.ThumbnailURL)
	}

	jobs := env.publisher.queued()
	if len(jobs) != 1 || jobs[0].MediaType != storymedia.MediaTypeVideo {
		t.Errorf("jobs = %+v, want one video job", jobs)
	}
}

func TestStoryMediaAttachDefaultsTheSourceToGallery(t *testing.T) {
	env := newStoryMediaTestEnv(t)
	router := env.router(t, testMediaAuthorID)

	body := env.presignAndUpload(t, router, "image/png")
	body.Source = ""

	recorder := postJSON(router, "/stories/"+testMediaStoryID+"/media", body, testAccessToken)
	if recorder.Code != http.StatusAccepted {
		t.Fatalf("status = %d, want %d (body %s)", recorder.Code, http.StatusAccepted, recorder.Body.String())
	}

	var envelope storyMediaEnvelope
	decodeBody(t, recorder, &envelope)
	if envelope.Media.Source != string(storymedia.MediaSourceGallery) {
		t.Errorf("source = %q, want gallery", envelope.Media.Source)
	}
}

func TestStoryMediaAttachRejectsAnObjectThatWasNeverUploaded(t *testing.T) {
	env := newStoryMediaTestEnv(t)
	router := env.router(t, testMediaAuthorID)

	// Signed, but the client never PUT the bytes.
	target := requestUploadURL(t, router, uploadURLRequestFor("image/jpeg"))

	recorder := postJSON(
		router,
		"/stories/"+testMediaStoryID+"/media",
		attachMediaRequest{ObjectKey: target.ObjectKey},
		testAccessToken,
	)

	if recorder.Code != http.StatusBadRequest {
		t.Fatalf("status = %d, want %d (body %s)", recorder.Code, http.StatusBadRequest, recorder.Body.String())
	}
	if len(env.publisher.queued()) != 0 {
		t.Errorf("queued a job for an object that does not exist")
	}
}

func TestStoryMediaAttachRejectsAKeyForAnotherStory(t *testing.T) {
	env := newStoryMediaTestEnv(t)
	router := env.router(t, testMediaAuthorID)

	foreign := storymedia.StoryMediaKeyPrefix("88888888-8888-4888-8888-888888888888") +
		"aaaaaaaa-aaaa-4aaa-8aaa-aaaaaaaaaaaa_original.jpg"

	recorder := postJSON(
		router,
		"/stories/"+testMediaStoryID+"/media",
		attachMediaRequest{ObjectKey: foreign},
		testAccessToken,
	)

	if recorder.Code != http.StatusBadRequest {
		t.Errorf("status = %d, want %d (body %s)", recorder.Code, http.StatusBadRequest, recorder.Body.String())
	}
}

func TestStoryMediaAttachRequiresAuthentication(t *testing.T) {
	env := newStoryMediaTestEnv(t)
	router := env.router(t, testMediaAuthorID)

	body := env.presignAndUpload(t, router, "image/jpeg")

	recorder := postJSON(router, "/stories/"+testMediaStoryID+"/media", body, "")

	if recorder.Code != http.StatusUnauthorized {
		t.Errorf("status = %d, want %d", recorder.Code, http.StatusUnauthorized)
	}
}

func TestStoryMediaAttachRejectsNonAuthor(t *testing.T) {
	env := newStoryMediaTestEnv(t)
	authorRouter := env.router(t, testMediaAuthorID)
	otherRouter := env.router(t, testMediaOtherID)

	body := env.presignAndUpload(t, authorRouter, "image/jpeg")

	recorder := postJSON(otherRouter, "/stories/"+testMediaStoryID+"/media", body, testAccessToken)

	if recorder.Code != http.StatusForbidden {
		t.Errorf("status = %d, want %d (body %s)", recorder.Code, http.StatusForbidden, recorder.Body.String())
	}
}

func TestStoryMediaAttachRejectsAMissingObjectKey(t *testing.T) {
	env := newStoryMediaTestEnv(t)
	router := env.router(t, testMediaAuthorID)

	recorder := postJSON(router, "/stories/"+testMediaStoryID+"/media", attachMediaRequest{}, testAccessToken)

	if recorder.Code != http.StatusBadRequest {
		t.Errorf("status = %d, want %d (body %s)", recorder.Code, http.StatusBadRequest, recorder.Body.String())
	}
}

// ---------------------------------------------------------------------------
// The multipart path is still served
// ---------------------------------------------------------------------------

func TestStoryMediaStillAcceptsAMultipartUpload(t *testing.T) {
	env := newStoryMediaTestEnv(t)
	router := env.router(t, testMediaAuthorID)

	body, contentType := mediaUpload(t, pngImage(t), string(storymedia.MediaSourceGallery), nil)

	recorder := postMedia(router, testMediaStoryID, body, contentType, testAccessToken)

	// The pre-KNOT-040 client keeps working: its file is written by the API.
	if recorder.Code != http.StatusAccepted {
		t.Fatalf("status = %d, want %d (body %s)", recorder.Code, http.StatusAccepted, recorder.Body.String())
	}
	if len(env.publisher.queued()) != 1 {
		t.Errorf("queued %d jobs, want 1", len(env.publisher.queued()))
	}
}

// TestStoryMediaCreateDecodesTheAttachBody proves the handler decodes the JSON body
// even when the service is a double, which is what pins the wire contract
// independently of the real service.
func TestStoryMediaCreateDecodesTheAttachBody(t *testing.T) {
	logger := slog.New(slog.NewTextHandler(io.Discard, nil))
	service := &fakeStoryMediaService{attachResult: storymedia.StoryMedia{
		ID:        testMediaID,
		StoryID:   testMediaStoryID,
		MediaType: storymedia.MediaTypeImage,
		Status:    storymedia.StatusProcessing,
	}}

	handler, err := NewStoryMediaHandler(service, logger)
	if err != nil {
		t.Fatalf("NewStoryMediaHandler() error = %v, want nil", err)
	}

	width := 10
	payload := attachMediaRequest{
		ObjectKey: storymedia.StoryMediaKeyPrefix(testMediaStoryID) + "aaaaaaaa-aaaa-4aaa-8aaa-aaaaaaaaaaaa_original.jpg",
		Source:    "camera",
		Width:     &width,
	}

	body, err := json.Marshal(payload)
	if err != nil {
		t.Fatalf("marshalling the body: %v", err)
	}

	request := httptest.NewRequest(http.MethodPost, "/stories/"+testMediaStoryID+"/media", bytes.NewReader(body))
	request.Header.Set("Content-Type", "application/json")
	request.SetPathValue("id", testMediaStoryID)
	request = request.WithContext(withUserID(request.Context(), testMediaAuthorID))

	recorder := httptest.NewRecorder()
	handler.Create(recorder, request)

	if recorder.Code != http.StatusAccepted {
		t.Fatalf("status = %d, want %d (body %s)", recorder.Code, http.StatusAccepted, recorder.Body.String())
	}
	if service.gotAttach.ObjectKey != payload.ObjectKey {
		t.Errorf("decoded object_key = %q, want %q", service.gotAttach.ObjectKey, payload.ObjectKey)
	}
	if service.gotAttach.Source != storymedia.MediaSourceCamera {
		t.Errorf("decoded source = %q, want camera", service.gotAttach.Source)
	}
	if service.gotAttach.Width == nil || *service.gotAttach.Width != width {
		t.Errorf("decoded width = %v, want %d", service.gotAttach.Width, width)
	}
	if service.gotAttach.StoryID != testMediaStoryID {
		t.Errorf("decoded story_id = %q, want %q", service.gotAttach.StoryID, testMediaStoryID)
	}
}
