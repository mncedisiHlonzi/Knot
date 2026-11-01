package storymedia

import (
	"bytes"
	"context"
	"errors"
	"fmt"
	"io"
	"net/http"
	"net/http/httptest"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"sync"
	"testing"
	"time"
)

const (
	authorID   = "11111111-1111-4111-8111-111111111111"
	otherID    = "22222222-2222-4222-8222-222222222222"
	uploaderID = "33333333-3333-4333-8333-333333333333"
	storyID    = "55555555-5555-4555-8555-555555555555"
)

// ---------------------------------------------------------------------------
// Fakes
// ---------------------------------------------------------------------------

// fakeStore is an in-memory StoryMediaStore.
type fakeStore struct {
	stories map[string]string // story id -> author id
	media   map[string]StoryMedia
	order   []string
	seq     int

	createErr error
	listErr   error
	firstErr  error
	getErr    error
	deleteErr error
	authorErr error
	countErr  error

	// countOverride, when set, is what CountMedia reports regardless of the map, so
	// a test can place a story at or over the cap without inserting rows.
	countOverride *int

	// statusCalls records every SetStatus call, in order (KNOT-037-fix7).
	statusCalls []Status

	gotCreate StoryMedia
}

func newFakeStore() *fakeStore {
	return &fakeStore{
		stories: map[string]string{storyID: authorID},
		media:   map[string]StoryMedia{},
	}
}

func (s *fakeStore) CreateMedia(_ context.Context, media StoryMedia) (StoryMedia, error) {
	if s.createErr != nil {
		return StoryMedia{}, s.createErr
	}
	s.gotCreate = media

	s.seq++
	media.ID = fmt.Sprintf("00000000-0000-4000-8000-%012d", s.seq)
	media.CreatedAt = time.Date(2026, 10, 9, 12, 0, s.seq, 0, time.UTC)
	if media.DisplayOrder < 0 {
		media.DisplayOrder = len(s.order)
	}
	s.media[media.ID] = media
	s.order = append(s.order, media.ID)

	return media, nil
}

func (s *fakeStore) ListMedia(_ context.Context, story string) ([]StoryMedia, error) {
	if s.listErr != nil {
		return nil, s.listErr
	}
	out := make([]StoryMedia, 0)
	for _, id := range s.order {
		if s.media[id].StoryID == story {
			out = append(out, s.media[id])
		}
	}
	return out, nil
}

func (s *fakeStore) FirstMedia(_ context.Context, storyIDs []string) (map[string]StoryMedia, error) {
	if s.firstErr != nil {
		return nil, s.firstErr
	}
	wanted := map[string]bool{}
	for _, id := range storyIDs {
		wanted[id] = true
	}
	out := map[string]StoryMedia{}
	for _, id := range s.order {
		media := s.media[id]
		if wanted[media.StoryID] {
			if _, seen := out[media.StoryID]; !seen {
				out[media.StoryID] = media
			}
		}
	}
	return out, nil
}

func (s *fakeStore) GetMedia(_ context.Context, id string) (StoryMedia, error) {
	if s.getErr != nil {
		return StoryMedia{}, s.getErr
	}
	media, ok := s.media[id]
	if !ok {
		return StoryMedia{}, ErrNotFound
	}
	return media, nil
}

func (s *fakeStore) DeleteMedia(_ context.Context, id string) error {
	if s.deleteErr != nil {
		return s.deleteErr
	}
	if _, ok := s.media[id]; !ok {
		return ErrNotFound
	}
	delete(s.media, id)
	return nil
}

func (s *fakeStore) StoryAuthor(_ context.Context, id string) (string, error) {
	if s.authorErr != nil {
		return "", s.authorErr
	}
	author, ok := s.stories[id]
	if !ok {
		return "", ErrStoryNotFound
	}
	return author, nil
}

func (s *fakeStore) CountMedia(_ context.Context, storyID string) (int, error) {
	if s.countErr != nil {
		return 0, s.countErr
	}
	if s.countOverride != nil {
		return *s.countOverride, nil
	}

	count := 0
	for _, id := range s.order {
		if s.media[id].StoryID == storyID {
			count++
		}
	}
	return count, nil
}

// SetStatus records a pipeline transition and applies it to the stored row, so a
// reader sees the same state the worker reported (KNOT-037-fix7).
func (s *fakeStore) SetStatus(_ context.Context, id string, status Status, sizeBytes int64) error {
	media, ok := s.media[id]
	if !ok {
		return ErrNotFound
	}

	s.statusCalls = append(s.statusCalls, status)
	media.Status = status
	if sizeBytes > 0 {
		media.SizeBytes = sizeBytes
	}
	s.media[id] = media

	return nil
}

// fakeObjects is an in-memory Storage.
type fakeObjects struct {
	objects   map[string][]byte
	deleted   []string
	putErr    error
	getErr    error
	rangeErr  error
	deleteErr error
}

func newFakeObjects() *fakeObjects {
	return &fakeObjects{objects: map[string][]byte{}}
}

func (o *fakeObjects) Put(_ context.Context, key string, body io.Reader, _ string) error {
	if o.putErr != nil {
		return o.putErr
	}
	data, err := io.ReadAll(body)
	if err != nil {
		return err
	}
	o.objects[key] = data
	return nil
}

func (o *fakeObjects) Get(_ context.Context, key string) (io.ReadCloser, string, error) {
	if o.getErr != nil {
		return nil, "", o.getErr
	}
	data, ok := o.objects[key]
	if !ok {
		return nil, "", ErrNotFound
	}
	return io.NopCloser(bytes.NewReader(data)), "application/octet-stream", nil
}

func (o *fakeObjects) GetRange(_ context.Context, key string, start, end int64) (io.ReadCloser, error) {
	if o.rangeErr != nil {
		return nil, o.rangeErr
	}
	data, ok := o.objects[key]
	if !ok {
		return nil, ErrNotFound
	}
	if end >= int64(len(data)) {
		end = int64(len(data)) - 1
	}
	return io.NopCloser(bytes.NewReader(data[start : end+1])), nil
}

func (o *fakeObjects) Delete(_ context.Context, key string) error {
	if o.deleteErr != nil {
		return o.deleteErr
	}
	o.deleted = append(o.deleted, key)
	delete(o.objects, key)
	return nil
}

// fakeLogger records the warnings and errors the service emits.
type fakeLogger struct {
	warnings []string
	errors   []string
}

func (l *fakeLogger) WarnContext(_ context.Context, msg string, args ...any) {
	l.warnings = append(l.warnings, msg)
}

func (l *fakeLogger) ErrorContext(_ context.Context, msg string, args ...any) {
	l.errors = append(l.errors, msg)
}

func newTestService(t *testing.T, store StoryMediaStore, objects Storage) (*Service, *fakeLogger) {
	t.Helper()

	logger := &fakeLogger{}
	service, err := NewService(store, objects, logger)
	if err != nil {
		t.Fatalf("NewService() error = %v, want nil", err)
	}

	return service, logger
}

func validInput() CreateMediaInput {
	payload := []byte("pretend png bytes")
	return CreateMediaInput{
		StoryID:    storyID,
		UploaderID: authorID,
		MimeType:   "image/png",
		Source:     MediaSourceCamera,
		Body:       bytes.NewReader(payload),
		SizeBytes:  int64(len(payload)),
	}
}

// ---------------------------------------------------------------------------
// Construction
// ---------------------------------------------------------------------------

func TestNewServiceRejectsMissingDependencies(t *testing.T) {
	if _, err := NewService(nil, newFakeObjects(), &fakeLogger{}); err == nil {
		t.Error("NewService(nil store) error = nil, want an error")
	}
	if _, err := NewService(newFakeStore(), nil, &fakeLogger{}); err == nil {
		t.Error("NewService(nil objects) error = nil, want an error")
	}
	if _, err := NewService(newFakeStore(), newFakeObjects(), nil); err == nil {
		t.Error("NewService(nil logger) error = nil, want an error")
	}
}

// ---------------------------------------------------------------------------
// CreateMedia
// ---------------------------------------------------------------------------

func TestCreateMediaSuccess(t *testing.T) {
	store := newFakeStore()
	objects := newFakeObjects()
	service, _ := newTestService(t, store, objects)

	created, err := service.CreateMedia(context.Background(), imageInput(t, 300, 200))
	if err != nil {
		t.Fatalf("CreateMedia() error = %v, want nil", err)
	}

	if created.Source != MediaSourceCamera {
		t.Errorf("source = %q, want camera", created.Source)
	}
	if created.MediaType != MediaTypeImage {
		t.Errorf("media type = %q, want image", created.MediaType)
	}
	if created.UploaderID != authorID {
		t.Errorf("uploader = %q, want %q", created.UploaderID, authorID)
	}
	if !strings.HasPrefix(created.StorageKey, StoryMediaKeyPrefix(storyID)) {
		t.Errorf("storage key = %q, want the story's prefix", created.StorageKey)
	}
	// The row's primary object is the derivative the pipeline writes, not the
	// upload (KNOT-037-fix7).
	if !strings.HasSuffix(created.StorageKey, "_display.avif") {
		t.Errorf("storage key = %q, want a display avif", created.StorageKey)
	}

	service.WaitForProcessing()
	if _, ok := objects.objects[created.StorageKey]; !ok {
		t.Error("the display rendition was not written to storage")
	}
}

func TestCreateMediaAppendsInOrder(t *testing.T) {
	store := newFakeStore()
	service, _ := newTestService(t, store, newFakeObjects())

	first, err := service.CreateMedia(context.Background(), validInput())
	if err != nil {
		t.Fatalf("first CreateMedia() error = %v", err)
	}
	second, err := service.CreateMedia(context.Background(), validInput())
	if err != nil {
		t.Fatalf("second CreateMedia() error = %v", err)
	}

	if first.DisplayOrder != 0 || second.DisplayOrder != 1 {
		t.Errorf("display orders = %d, %d, want 0 then 1", first.DisplayOrder, second.DisplayOrder)
	}
}

// TestCreateMediaEnforcesTheStoryMediaCap pins the early cap check: a story below
// the cap accepts an upload, and one at or over the cap is refused with
// ErrMediaLimit before any bytes are stored (KNOT-ADR-054).
func TestCreateMediaEnforcesTheStoryMediaCap(t *testing.T) {
	tests := []struct {
		name    string
		count   int
		wantErr bool
	}{
		{name: "below the cap", count: MaxMediaPerStory - 1, wantErr: false},
		{name: "at the cap", count: MaxMediaPerStory, wantErr: true},
		{name: "above the cap", count: MaxMediaPerStory + 5, wantErr: true},
	}

	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			store := newFakeStore()
			count := test.count
			store.countOverride = &count
			objects := newFakeObjects()
			service, _ := newTestService(t, store, objects)

			_, err := service.CreateMedia(context.Background(), validInput())

			if test.wantErr {
				if !errors.Is(err, ErrMediaLimit) {
					t.Fatalf("CreateMedia() error = %v, want ErrMediaLimit", err)
				}
				// The upload is refused before the object is written, so a full story
				// never leaves an orphan behind.
				if len(objects.objects) != 0 {
					t.Errorf("objects stored = %d, want 0 when the cap is reached", len(objects.objects))
				}
				return
			}

			if err != nil {
				t.Fatalf("CreateMedia() error = %v, want nil", err)
			}
		})
	}
}

// TestMediaLimitMessageMatchesTheCap keeps the text the handler returns in step
// with the number the service and store enforce.
func TestMediaLimitMessageMatchesTheCap(t *testing.T) {
	want := fmt.Sprintf("a story may have at most %d media items", MaxMediaPerStory)
	if MediaLimitMessage != want {
		t.Errorf("MediaLimitMessage = %q, want %q", MediaLimitMessage, want)
	}
}

func TestCreateMediaRejectsForbiddenUploader(t *testing.T) {
	service, _ := newTestService(t, newFakeStore(), newFakeObjects())

	in := validInput()
	in.UploaderID = otherID

	if _, err := service.CreateMedia(context.Background(), in); !errors.Is(err, ErrForbidden) {
		t.Fatalf("CreateMedia() error = %v, want ErrForbidden", err)
	}
}

func TestCreateMediaRejectsUnknownStory(t *testing.T) {
	service, _ := newTestService(t, newFakeStore(), newFakeObjects())

	in := validInput()
	in.StoryID = "99999999-9999-4999-8999-999999999999"

	if _, err := service.CreateMedia(context.Background(), in); !errors.Is(err, ErrStoryNotFound) {
		t.Fatalf("CreateMedia() error = %v, want ErrStoryNotFound", err)
	}
}

func TestCreateMediaRejectsUnsupportedType(t *testing.T) {
	service, _ := newTestService(t, newFakeStore(), newFakeObjects())

	in := validInput()
	in.MimeType = "image/gif"

	if _, err := service.CreateMedia(context.Background(), in); !errors.Is(err, ErrUnsupportedMediaType) {
		t.Fatalf("CreateMedia() error = %v, want ErrUnsupportedMediaType", err)
	}
}

func TestCreateMediaRejectsOversizedPayloads(t *testing.T) {
	service, _ := newTestService(t, newFakeStore(), newFakeObjects())

	image := validInput()
	image.SizeBytes = MaxImageBytes + 1

	video := validInput()
	video.MimeType = "video/mp4"
	video.SizeBytes = MaxVideoBytes + 1

	for name, in := range map[string]CreateMediaInput{"image": image, "video": video} {
		if _, err := service.CreateMedia(context.Background(), in); !errors.Is(err, ErrTooLarge) {
			t.Errorf("%s: CreateMedia() error = %v, want ErrTooLarge", name, err)
		}
	}
}

func TestCreateMediaValidationFailures(t *testing.T) {
	negative := -1
	zero := 0

	tests := map[string]func(in *CreateMediaInput){
		"bad story id":     func(in *CreateMediaInput) { in.StoryID = "not-a-uuid" },
		"bad uploader id":  func(in *CreateMediaInput) { in.UploaderID = "nope" },
		"bad source":       func(in *CreateMediaInput) { in.Source = "screenshot" },
		"empty payload":    func(in *CreateMediaInput) { in.SizeBytes = 0 },
		"negative width":   func(in *CreateMediaInput) { in.Width = &negative },
		"negative height":  func(in *CreateMediaInput) { in.Height = &negative },
		"negative display": func(in *CreateMediaInput) { in.DisplayOrder = &negative },
	}

	for name, mutate := range tests {
		in := validInput()
		mutate(&in)

		service, _ := newTestService(t, newFakeStore(), newFakeObjects())
		_, err := service.CreateMedia(context.Background(), in)
		if err == nil {
			t.Errorf("%s: CreateMedia() error = nil, want an error", name)
			continue
		}
		if !errors.Is(err, ErrValidation) {
			t.Errorf("%s: error = %v, want ErrValidation", name, err)
		}
	}

	// A zero display order is valid (the first item).
	in := validInput()
	in.DisplayOrder = &zero
	service, _ := newTestService(t, newFakeStore(), newFakeObjects())
	if _, err := service.CreateMedia(context.Background(), in); err != nil {
		t.Errorf("display order 0: CreateMedia() error = %v, want nil", err)
	}
}

// TestCreateMediaLeavesTheBucketAloneWhenTheRowCannotBeWritten pins the order of
// the new pipeline: the row is written first and nothing is uploaded until the
// worker runs, so a failed insert cannot orphan an object (KNOT-037-fix7).
func TestCreateMediaLeavesTheBucketAloneWhenTheRowCannotBeWritten(t *testing.T) {
	store := newFakeStore()
	store.createErr = errors.New("insert exploded")
	objects := newFakeObjects()
	service, _ := newTestService(t, store, objects)

	if _, err := service.CreateMedia(context.Background(), imageInput(t, 300, 200)); err == nil {
		t.Fatal("CreateMedia() error = nil, want an error")
	}

	service.WaitForProcessing()
	if len(objects.objects) != 0 || len(objects.deleted) != 0 {
		t.Errorf("objects = %d, deletions = %d, want the bucket untouched (nothing was ever put)",
			len(objects.objects), len(objects.deleted))
	}
}

// ---------------------------------------------------------------------------
// ListMedia / GetMedia
// ---------------------------------------------------------------------------

func TestListMediaEmptyIsNotNil(t *testing.T) {
	service, _ := newTestService(t, newFakeStore(), newFakeObjects())

	page, err := service.ListMedia(context.Background(), storyID)
	if err != nil {
		t.Fatalf("ListMedia() error = %v, want nil", err)
	}
	if page == nil {
		t.Fatal("ListMedia() = nil, want an empty slice")
	}
	if len(page) != 0 {
		t.Errorf("ListMedia() = %d items, want 0", len(page))
	}
}

func TestListMediaUnknownStory(t *testing.T) {
	service, _ := newTestService(t, newFakeStore(), newFakeObjects())

	if _, err := service.ListMedia(context.Background(), "99999999-9999-4999-8999-999999999999"); !errors.Is(err, ErrStoryNotFound) {
		t.Fatalf("ListMedia() error = %v, want ErrStoryNotFound", err)
	}
	if _, err := service.ListMedia(context.Background(), "not-a-uuid"); !errors.Is(err, ErrStoryNotFound) {
		t.Fatalf("ListMedia(bad id) error = %v, want ErrStoryNotFound", err)
	}
}

func TestGetMediaNotFound(t *testing.T) {
	service, _ := newTestService(t, newFakeStore(), newFakeObjects())

	if _, err := service.GetMedia(context.Background(), "99999999-9999-4999-8999-999999999999"); !errors.Is(err, ErrNotFound) {
		t.Fatalf("GetMedia() error = %v, want ErrNotFound", err)
	}
	if _, err := service.GetMedia(context.Background(), "not-a-uuid"); !errors.Is(err, ErrNotFound) {
		t.Fatalf("GetMedia(bad id) error = %v, want ErrNotFound", err)
	}
}

// ---------------------------------------------------------------------------
// DeleteMedia
// ---------------------------------------------------------------------------

func TestDeleteMediaByAuthorAndUploader(t *testing.T) {
	store := newFakeStore()
	objects := newFakeObjects()
	service, _ := newTestService(t, store, objects)

	created, err := service.CreateMedia(context.Background(), validInput())
	if err != nil {
		t.Fatalf("CreateMedia() error = %v", err)
	}

	if err := service.DeleteMedia(context.Background(), created.ID, authorID); err != nil {
		t.Fatalf("DeleteMedia(author) error = %v, want nil", err)
	}
	if _, ok := objects.objects[created.StorageKey]; ok {
		t.Error("the object was not removed from storage")
	}
}

func TestDeleteMediaRejectsStranger(t *testing.T) {
	store := newFakeStore()
	objects := newFakeObjects()
	service, _ := newTestService(t, store, objects)

	created, err := service.CreateMedia(context.Background(), imageInput(t, 300, 200))
	if err != nil {
		t.Fatalf("CreateMedia() error = %v", err)
	}
	service.WaitForProcessing()

	if err := service.DeleteMedia(context.Background(), created.ID, otherID); !errors.Is(err, ErrForbidden) {
		t.Fatalf("DeleteMedia(stranger) error = %v, want ErrForbidden", err)
	}
	if _, ok := objects.objects[created.StorageKey]; !ok {
		t.Error("a rejected delete must not touch storage")
	}
}

func TestDeleteMediaMissing(t *testing.T) {
	service, _ := newTestService(t, newFakeStore(), newFakeObjects())

	if err := service.DeleteMedia(context.Background(), "99999999-9999-4999-8999-999999999999", authorID); !errors.Is(err, ErrNotFound) {
		t.Fatalf("DeleteMedia() error = %v, want ErrNotFound", err)
	}
}

func TestDeleteMediaStorageFailureIsBestEffort(t *testing.T) {
	store := newFakeStore()
	objects := newFakeObjects()
	objects.deleteErr = errors.New("object store down")
	logger := &fakeLogger{}
	service, err := NewService(store, objects, logger)
	if err != nil {
		t.Fatalf("NewService() error = %v, want nil", err)
	}

	created, err := service.CreateMedia(context.Background(), validInput())
	if err != nil {
		t.Fatalf("CreateMedia() error = %v", err)
	}

	if err := service.DeleteMedia(context.Background(), created.ID, authorID); err != nil {
		t.Fatalf("DeleteMedia() error = %v, want nil even when storage fails", err)
	}
	if len(logger.warnings) == 0 {
		t.Error("a failed object deletion should be logged")
	}
	if _, err := store.GetMedia(context.Background(), created.ID); !errors.Is(err, ErrNotFound) {
		t.Error("the row should still be deleted when storage deletion fails")
	}
}

// ---------------------------------------------------------------------------
// StreamMedia / ParseRange
// ---------------------------------------------------------------------------

func TestStreamMediaFullAndRange(t *testing.T) {
	store := newFakeStore()
	objects := newFakeObjects()
	service, _ := newTestService(t, store, objects)

	// A row whose primary object is its upload: that is the shape of a row written
	// before KNOT-037-fix7, and the shape the Range path is about.
	payload := []byte("0123456789abcdefghij")
	key := StoryMediaKeyPrefix(storyID) + "original.png"
	objects.objects[key] = payload

	created, err := store.CreateMedia(context.Background(), StoryMedia{
		StoryID:      storyID,
		UploaderID:   authorID,
		StorageKey:   key,
		MediaType:    MediaTypeImage,
		MimeType:     "image/png",
		Source:       MediaSourceCamera,
		SizeBytes:    int64(len(payload)),
		Status:       StatusReady,
		DisplayOrder: -1,
	})
	if err != nil {
		t.Fatalf("CreateMedia() error = %v", err)
	}

	// No Range header: the whole object. No variant asks for the original.
	request := newRangeRequest("")
	body, total, mimeType, err := service.StreamMedia(context.Background(), created.ID, "", "", request)
	if err != nil {
		t.Fatalf("StreamMedia(full) error = %v", err)
	}
	full, _ := io.ReadAll(body)
	if !bytes.Equal(full, payload) {
		t.Errorf("full body = %q, want %q", full, payload)
	}
	if total != int64(len(payload)) {
		t.Errorf("total = %d, want %d", total, len(payload))
	}
	if mimeType != "image/png" {
		t.Errorf("mime = %q, want image/png", mimeType)
	}

	// A Range header: only the requested bytes.
	request = newRangeRequest("bytes=3-6")
	body, total, _, err = service.StreamMedia(context.Background(), created.ID, "", "", request)
	if err != nil {
		t.Fatalf("StreamMedia(range) error = %v", err)
	}
	slice, _ := io.ReadAll(body)
	if want := payload[3:7]; !bytes.Equal(slice, want) {
		t.Errorf("ranged body = %q, want %q", slice, want)
	}
	if total != int64(len(payload)) {
		t.Errorf("total = %d, want the full size %d", total, len(payload))
	}
}

func TestParseRange(t *testing.T) {
	const size = 100

	tests := []struct {
		name   string
		header string
		start  int64
		end    int64
		ok     bool
	}{
		{"absent", "", 0, 0, false},
		{"explicit", "bytes=0-9", 0, 9, true},
		{"middle", "bytes=10-19", 10, 19, true},
		{"clamped end", "bytes=90-999", 90, 99, true},
		{"open ended", "bytes=50-", 50, 99, true},
		{"suffix", "bytes=-10", 90, 99, true},
		{"suffix larger than size", "bytes=-500", 0, 99, true},
		{"unsatisfiable start", "bytes=100-200", 0, 0, false},
		{"inverted", "bytes=20-10", 0, 0, false},
		{"multi-range", "bytes=0-9,20-29", 0, 0, false},
		{"wrong unit", "items=0-9", 0, 0, false},
		{"zero suffix", "bytes=-0", 0, 0, false},
		{"non numeric", "bytes=a-b", 0, 0, false},
	}

	for _, test := range tests {
		start, end, ok := ParseRange(test.header, size)
		if ok != test.ok {
			t.Errorf("%s: ParseRange(%q) ok = %v, want %v", test.name, test.header, ok, test.ok)
			continue
		}
		if !ok {
			continue
		}
		if start != test.start || end != test.end {
			t.Errorf("%s: ParseRange(%q) = (%d, %d), want (%d, %d)", test.name, test.header, start, end, test.start, test.end)
		}
	}
}

func TestParseRangeZeroSize(t *testing.T) {
	if _, _, ok := ParseRange("bytes=0-9", 0); ok {
		t.Error("ParseRange(size=0) ok = true, want false")
	}
}

// newRangeRequest builds a GET request carrying an optional Range header.
func newRangeRequest(header string) *http.Request {
	request := httptest.NewRequest(http.MethodGet, "/stories/x/media/y/content", nil)
	if header != "" {
		request.Header.Set("Range", header)
	}
	return request
}

// ---------------------------------------------------------------------------
// Derivatives (KNOT-037)
// ---------------------------------------------------------------------------

// imageInput is a create input carrying a real PNG, so the derivative pipeline
// runs instead of failing to decode.
func imageInput(t *testing.T, width, height int) CreateMediaInput {
	t.Helper()

	payload := testPNG(t, width, height)
	in := validInput()
	in.MimeType = "image/png"
	in.Body = bytes.NewReader(payload)
	in.SizeBytes = int64(len(payload))

	return in
}

// videoInput is a create input carrying bytes the video pipeline is asked to
// transcode. The bytes are not a real video unless a test replaces them: most
// tests stub the processor, so only the type and metadata matter.
func videoInput(t *testing.T, durationMS *int) CreateMediaInput {
	t.Helper()

	payload := []byte("pretend mp4 bytes")
	return CreateMediaInput{
		StoryID:    storyID,
		UploaderID: authorID,
		MimeType:   "video/mp4",
		Source:     MediaSourceGallery,
		Body:       bytes.NewReader(payload),
		SizeBytes:  int64(len(payload)),
		DurationMS: durationMS,
	}
}

// blockingProcessor returns a processor that signals when it starts and waits for
// release, so a test can observe the row while its derivatives are still missing.
func blockingProcessor() (processor, <-chan struct{}, chan<- struct{}) {
	started := make(chan struct{}, 1)
	release := make(chan struct{})

	return func(context.Context, processJob) (int64, error) {
		select {
		case started <- struct{}{}:
		default:
		}
		<-release
		return 0, nil
	}, started, release
}

func TestCreateMediaReturnsBeforeTheDerivativesExist(t *testing.T) {
	store := newFakeStore()
	objects := newFakeObjects()
	service, logger := newTestService(t, store, objects)

	process, started, release := blockingProcessor()
	service.processMedia = process

	created, err := service.CreateMedia(context.Background(), imageInput(t, 300, 200))
	if err != nil {
		t.Fatalf("CreateMedia() error = %v, want nil", err)
	}

	// The response describes what is coming: the keys are minted, but nothing is in
	// the bucket and the row says it is still working (KNOT-037-fix7).
	if created.Status != StatusProcessing {
		t.Errorf("status = %q, want %q", created.Status, StatusProcessing)
	}
	if !strings.HasSuffix(created.StorageKey, "_display.avif") {
		t.Errorf("storage key = %q, want a display avif", created.StorageKey)
	}
	if created.ThumbnailKey == nil || !strings.HasSuffix(*created.ThumbnailKey, "_thumb.webp") {
		t.Errorf("thumbnail key = %v, want a webp thumbnail", created.ThumbnailKey)
	}
	if created.MediumKey != nil || created.PosterKey != nil {
		t.Errorf("medium/poster keys = (%v, %v), want nil: the pipeline writes neither",
			created.MediumKey, created.PosterKey)
	}
	if len(objects.objects) != 0 {
		t.Errorf("objects = %d, want none until the worker runs", len(objects.objects))
	}

	// A content request in this window must be told to wait, not shown a broken
	// image.
	if _, _, _, err := service.StreamMedia(context.Background(), created.ID, VariantThumb, "", newRangeRequest("")); !errors.Is(err, ErrProcessing) {
		t.Errorf("StreamMedia(processing) error = %v, want ErrProcessing", err)
	}

	<-started
	close(release)
	service.WaitForProcessing()

	stored, err := store.GetMedia(context.Background(), created.ID)
	if err != nil {
		t.Fatalf("GetMedia() error = %v", err)
	}
	if stored.Status != StatusReady {
		t.Errorf("status after the job = %q, want %q", stored.Status, StatusReady)
	}
	if len(logger.errors) != 0 {
		t.Errorf("errors = %v, want none", logger.errors)
	}
}

func TestCreateMediaProcessesAnImageIntoDisplayAndThumbnail(t *testing.T) {
	store := newFakeStore()
	objects := newFakeObjects()
	service, logger := newTestService(t, store, objects)

	created, err := service.CreateMedia(context.Background(), imageInput(t, 1000, 500))
	if err != nil {
		t.Fatalf("CreateMedia() error = %v, want nil", err)
	}
	service.WaitForProcessing()

	stored, err := store.GetMedia(context.Background(), created.ID)
	if err != nil {
		t.Fatalf("GetMedia() error = %v", err)
	}
	if stored.Status != StatusReady {
		t.Fatalf("status = %q, want %q", stored.Status, StatusReady)
	}

	// Two files and nothing else: the upload itself is never stored.
	if len(objects.objects) != 2 {
		t.Fatalf("objects = %d, want the display and the thumbnail only", len(objects.objects))
	}

	display, ok := objects.objects[stored.StorageKey]
	if !ok {
		t.Fatalf("the display object %q was not written", stored.StorageKey)
	}
	// 1000 wide is under the 2048 cap, so the display keeps its own size.
	if got := decodeAVIF(t, display).Bounds().Dx(); got != 1000 {
		t.Errorf("display width = %d, want the source's 1000", got)
	}

	thumbnailKey, ok := stored.ThumbnailWebPKey()
	if !ok {
		t.Fatal("ThumbnailWebPKey() = false, want the stored webp thumbnail")
	}
	thumbnail, ok := objects.objects[thumbnailKey]
	if !ok {
		t.Fatalf("the thumbnail object %q was not written", thumbnailKey)
	}
	if got := decodeWebP(t, thumbnail).Bounds().Dx(); got != ThumbnailMaxWidth {
		t.Errorf("thumbnail width = %d, want %d", got, ThumbnailMaxWidth)
	}

	if len(logger.errors) != 0 {
		t.Errorf("errors = %v, want none", logger.errors)
	}
}

func TestCreateMediaMarksAFailedJob(t *testing.T) {
	store := newFakeStore()
	objects := newFakeObjects()
	service, logger := newTestService(t, store, objects)

	service.processMedia = func(context.Context, processJob) (int64, error) {
		return 0, errors.New("ffmpeg said no")
	}

	created, err := service.CreateMedia(context.Background(), videoInput(t, nil))
	if err != nil {
		t.Fatalf("CreateMedia() error = %v, want nil", err)
	}
	service.WaitForProcessing()

	stored, err := store.GetMedia(context.Background(), created.ID)
	if err != nil {
		t.Fatalf("GetMedia() error = %v", err)
	}
	if stored.Status != StatusFailed {
		t.Errorf("status = %q, want %q", stored.Status, StatusFailed)
	}
	if len(logger.errors) == 0 {
		t.Error("a failed job must be logged")
	}
	if len(objects.objects) != 0 {
		t.Errorf("objects = %d, want none", len(objects.objects))
	}

	// A failed row has nothing coming, so it is reported as missing rather than as
	// "still processing" — otherwise a client would poll forever.
	if _, _, _, err := service.StreamMedia(context.Background(), created.ID, VariantStream, "", newRangeRequest("")); !errors.Is(err, ErrProcessingFailed) {
		t.Errorf("StreamMedia(failed) error = %v, want ErrProcessingFailed", err)
	}
}

func TestCreateMediaFailsOnAnUndecodableImage(t *testing.T) {
	store := newFakeStore()
	objects := newFakeObjects()
	service, logger := newTestService(t, store, objects)

	// validInput() carries bytes that are not an image, so the real pipeline cannot
	// produce a display or a thumbnail.
	created, err := service.CreateMedia(context.Background(), validInput())
	if err != nil {
		t.Fatalf("CreateMedia() error = %v, want nil: the row is accepted and fails later", err)
	}
	service.WaitForProcessing()

	stored, err := store.GetMedia(context.Background(), created.ID)
	if err != nil {
		t.Fatalf("GetMedia() error = %v", err)
	}
	if stored.Status != StatusFailed {
		t.Errorf("status = %q, want %q", stored.Status, StatusFailed)
	}
	if len(objects.objects) != 0 {
		t.Errorf("objects = %d, want none", len(objects.objects))
	}
	if len(logger.errors) == 0 {
		t.Error("the decode failure was not logged")
	}
}

// TestCreateMediaTranscodesAVideo proves the video half of the pipeline end to
// end: one object, named as the row says, and it is an MP4.
func TestCreateMediaTranscodesAVideo(t *testing.T) {
	binary, err := exec.LookPath(ffmpegBinary)
	if err != nil {
		t.Skip("FFmpeg is not installed")
	}

	dir := t.TempDir()
	clip := filepath.Join(dir, "clip.mp4")
	build := exec.Command(binary, "-y",
		"-f", "lavfi", "-i", "testsrc=size=320x240:rate=10:duration=1",
		"-pix_fmt", "yuv420p", clip,
	)
	if output, err := build.CombinedOutput(); err != nil {
		t.Fatalf("building a test clip: %v: %s", err, output)
	}
	payload, err := os.ReadFile(clip)
	if err != nil {
		t.Fatalf("reading the test clip: %v", err)
	}

	store := newFakeStore()
	objects := newFakeObjects()
	service, logger := newTestService(t, store, objects)

	duration := 1000
	in := videoInput(t, &duration)
	in.Body = bytes.NewReader(payload)
	in.SizeBytes = int64(len(payload))

	created, err := service.CreateMedia(context.Background(), in)
	if err != nil {
		t.Fatalf("CreateMedia() error = %v, want nil", err)
	}
	service.WaitForProcessing()

	stored, err := store.GetMedia(context.Background(), created.ID)
	if err != nil {
		t.Fatalf("GetMedia() error = %v", err)
	}
	if stored.Status != StatusReady {
		t.Fatalf("status = %q, want %q (errors: %v)", stored.Status, StatusReady, logger.errors)
	}

	// A video has no cover and no medium: one object, the stream (KNOT-037-fix7).
	if stored.ThumbnailKey != nil || stored.MediumKey != nil || stored.PosterKey != nil {
		t.Errorf("thumbnail/medium/poster keys = (%v, %v, %v), want all nil for a video",
			stored.ThumbnailKey, stored.MediumKey, stored.PosterKey)
	}
	if !strings.HasSuffix(stored.StorageKey, "_stream.mp4") {
		t.Errorf("storage key = %q, want a stream mp4", stored.StorageKey)
	}
	if len(objects.objects) != 1 {
		t.Fatalf("objects = %d, want the stream only", len(objects.objects))
	}

	stream, ok := objects.objects[stored.StorageKey]
	if !ok {
		t.Fatalf("the stream object %q was not written", stored.StorageKey)
	}
	if len(stream) < 12 || string(stream[4:8]) != "ftyp" {
		t.Errorf("the stream does not start with an MP4 ftyp box")
	}
}

// TestProcessingIsCappedByTheSemaphore proves the worker runs at most
// MaxConcurrentProcessing jobs at once, and that the rest queue up rather than
// being dropped (KNOT-037-fix7).
func TestProcessingIsCappedByTheSemaphore(t *testing.T) {
	store := newFakeStore()
	objects := newFakeObjects()
	service, _ := newTestService(t, store, objects)

	if got := cap(service.semaphore); got != MaxConcurrentProcessing {
		t.Fatalf("semaphore capacity = %d, want %d", got, MaxConcurrentProcessing)
	}

	var mu sync.Mutex
	running, peak := 0, 0
	release := make(chan struct{})
	service.processMedia = func(context.Context, processJob) (int64, error) {
		mu.Lock()
		running++
		if running > peak {
			peak = running
		}
		mu.Unlock()

		<-release

		mu.Lock()
		running--
		mu.Unlock()
		return 0, nil
	}

	for i := 0; i < MaxConcurrentProcessing+2; i++ {
		if _, err := service.CreateMedia(context.Background(), imageInput(t, 200, 100)); err != nil {
			t.Fatalf("CreateMedia(%d) error = %v, want nil", i, err)
		}
	}

	// Wait for the cap to be reached, then give any extra goroutine a moment to
	// start: without the semaphore the peak would climb past the cap here.
	deadline := time.Now().Add(5 * time.Second)
	for {
		mu.Lock()
		reached := peak
		mu.Unlock()
		if reached >= MaxConcurrentProcessing || time.Now().After(deadline) {
			break
		}
		time.Sleep(5 * time.Millisecond)
	}
	time.Sleep(50 * time.Millisecond)

	mu.Lock()
	got := peak
	mu.Unlock()
	if got != MaxConcurrentProcessing {
		t.Errorf("peak concurrency = %d, want exactly %d", got, MaxConcurrentProcessing)
	}

	close(release)
	service.WaitForProcessing()
}

func TestStreamMediaServesTheThumbnail(t *testing.T) {
	store := newFakeStore()
	objects := newFakeObjects()
	service, _ := newTestService(t, store, objects)

	created, err := service.CreateMedia(context.Background(), imageInput(t, 1000, 500))
	if err != nil {
		t.Fatalf("CreateMedia() error = %v, want nil", err)
	}
	service.WaitForProcessing()

	body, total, mimeType, err := service.StreamMedia(context.Background(), created.ID, VariantThumb, "", newRangeRequest(""))
	if err != nil {
		t.Fatalf("StreamMedia(thumb) error = %v, want nil", err)
	}

	thumbnailKey, ok := created.ThumbnailWebPKey()
	if !ok {
		t.Fatal("ThumbnailWebPKey() = false, want the stored webp thumbnail")
	}

	served, _ := io.ReadAll(body)
	want := objects.objects[thumbnailKey]
	if !bytes.Equal(served, want) {
		t.Error("StreamMedia(thumb) did not serve the stored thumbnail")
	}
	if total != int64(len(want)) {
		t.Errorf("total = %d, want the rendition's %d bytes", total, len(want))
	}
	if mimeType != webpContentType {
		t.Errorf("mime = %q, want %q", mimeType, webpContentType)
	}
}

// TestStreamMediaServesTheThumbnailForAnyRequestedFormat pins the KNOT-037-fix7
// contract: the feed thumbnail is a WebP, so `format` no longer selects a second
// file. It is still accepted (an older client sends it) and answers with the bytes
// that exist, under the type they really are.
func TestStreamMediaServesTheThumbnailForAnyRequestedFormat(t *testing.T) {
	store := newFakeStore()
	objects := newFakeObjects()
	service, _ := newTestService(t, store, objects)

	created, err := service.CreateMedia(context.Background(), imageInput(t, 300, 200))
	if err != nil {
		t.Fatalf("CreateMedia() error = %v, want nil", err)
	}
	service.WaitForProcessing()

	thumbnailKey, _ := created.ThumbnailWebPKey()
	for _, format := range []Format{"", FormatAVIF, FormatWebP} {
		body, _, mimeType, err := service.StreamMedia(context.Background(), created.ID, VariantThumb, format, newRangeRequest(""))
		if err != nil {
			t.Fatalf("StreamMedia(thumb, %q) error = %v, want nil", format, err)
		}

		served, _ := io.ReadAll(body)
		if !bytes.Equal(served, objects.objects[thumbnailKey]) {
			t.Errorf("format %q: did not serve the stored webp thumbnail", format)
		}
		if mimeType != webpContentType {
			t.Errorf("format %q: mime = %q, want %q", format, mimeType, webpContentType)
		}
	}
}

func TestStreamMediaServesTheDisplay(t *testing.T) {
	store := newFakeStore()
	objects := newFakeObjects()
	service, _ := newTestService(t, store, objects)

	created, err := service.CreateMedia(context.Background(), imageInput(t, 1000, 500))
	if err != nil {
		t.Fatalf("CreateMedia() error = %v, want nil", err)
	}
	service.WaitForProcessing()

	body, total, mimeType, err := service.StreamMedia(context.Background(), created.ID, VariantDisplay, "", newRangeRequest(""))
	if err != nil {
		t.Fatalf("StreamMedia(display) error = %v, want nil", err)
	}

	served, _ := io.ReadAll(body)
	if want := objects.objects[created.StorageKey]; !bytes.Equal(served, want) {
		t.Error("StreamMedia(display) did not serve the display rendition")
	}
	if total != int64(len(objects.objects[created.StorageKey])) {
		t.Errorf("total = %d, want the display's %d bytes", total, len(objects.objects[created.StorageKey]))
	}
	if mimeType != avifContentType {
		t.Errorf("mime = %q, want %q", mimeType, avifContentType)
	}
}

func TestStreamMediaFallsBackToTheDisplayWhenTheThumbnailIsMissing(t *testing.T) {
	store := newFakeStore()
	objects := newFakeObjects()
	service, _ := newTestService(t, store, objects)

	created, err := service.CreateMedia(context.Background(), imageInput(t, 300, 200))
	if err != nil {
		t.Fatalf("CreateMedia() error = %v, want nil", err)
	}
	service.WaitForProcessing()

	// The thumbnail object is gone, so the display is the next best picture rather
	// than a 404.
	thumbnailKey, _ := created.ThumbnailWebPKey()
	delete(objects.objects, thumbnailKey)

	body, _, mimeType, err := service.StreamMedia(context.Background(), created.ID, VariantThumb, "", newRangeRequest(""))
	if err != nil {
		t.Fatalf("StreamMedia(thumb, missing object) error = %v, want the display", err)
	}

	served, _ := io.ReadAll(body)
	if !bytes.Equal(served, objects.objects[created.StorageKey]) {
		t.Error("the fallback did not serve the display rendition")
	}
	if mimeType != avifContentType {
		t.Errorf("mime = %q, want the codec actually served, %q", mimeType, avifContentType)
	}
}

func TestStreamMediaServesALegacyJPEGThumbnail(t *testing.T) {
	store := newFakeStore()
	objects := newFakeObjects()
	service, _ := newTestService(t, store, objects)

	created, err := service.CreateMedia(context.Background(), imageInput(t, 300, 200))
	if err != nil {
		t.Fatalf("CreateMedia() error = %v, want nil", err)
	}
	service.WaitForProcessing()

	// A row written before KNOT-037-fix7 keeps a JPEG thumbnail key and its original
	// upload as the primary object. Asking for a thumbnail must still answer with
	// that small file rather than with the full-size original.
	legacyThumb := strings.TrimSuffix(*created.ThumbnailKey, webpExtension) + jpegExtension
	objects.objects[legacyThumb] = []byte("legacy jpeg thumbnail bytes")

	stored := store.media[created.ID]
	stored.ThumbnailKey = &legacyThumb
	store.media[created.ID] = stored

	for _, format := range []Format{"", FormatAVIF, FormatWebP} {
		body, total, mimeType, err := service.StreamMedia(context.Background(), created.ID, VariantThumb, format, newRangeRequest(""))
		if err != nil {
			t.Fatalf("StreamMedia(thumb, %q) error = %v, want the legacy jpeg thumbnail", format, err)
		}

		served, _ := io.ReadAll(body)
		if !bytes.Equal(served, objects.objects[legacyThumb]) {
			t.Errorf("format %q: the fallback did not serve the legacy thumbnail", format)
		}
		if total != int64(len(objects.objects[legacyThumb])) {
			t.Errorf("format %q: total = %d, want the legacy thumbnail's %d", format, total, len(objects.objects[legacyThumb]))
		}
		if mimeType != jpegContentType {
			t.Errorf("format %q: mime = %q, want the legacy thumbnail's %q", format, mimeType, jpegContentType)
		}
	}
}

func TestStreamMediaFallsBackForAVariantTheRowDoesNotHave(t *testing.T) {
	store := newFakeStore()
	objects := newFakeObjects()
	service, _ := newTestService(t, store, objects)

	created, err := service.CreateMedia(context.Background(), imageInput(t, 300, 200))
	if err != nil {
		t.Fatalf("CreateMedia() error = %v, want nil", err)
	}
	service.WaitForProcessing()

	// A new row has no poster, so asking for one must still return a picture: its
	// primary object, served as the type its key names rather than the upload's.
	body, _, mimeType, err := service.StreamMedia(context.Background(), created.ID, VariantPoster, "", newRangeRequest(""))
	if err != nil {
		t.Fatalf("StreamMedia(poster on an image) error = %v, want the primary object", err)
	}

	served, _ := io.ReadAll(body)
	if !bytes.Equal(served, objects.objects[created.StorageKey]) {
		t.Error("the fallback did not serve the primary object")
	}
	if mimeType != avifContentType {
		t.Errorf("mime = %q, want the served object's type %q", mimeType, avifContentType)
	}
}

func TestDeleteMediaRemovesTheDerivatives(t *testing.T) {
	store := newFakeStore()
	objects := newFakeObjects()
	service, _ := newTestService(t, store, objects)

	created, err := service.CreateMedia(context.Background(), imageInput(t, 1000, 500))
	if err != nil {
		t.Fatalf("CreateMedia() error = %v, want nil", err)
	}
	service.WaitForProcessing()

	if err := service.DeleteMedia(context.Background(), created.ID, authorID); err != nil {
		t.Fatalf("DeleteMedia() error = %v, want nil", err)
	}

	// Everything the upload created goes with the row, or the bucket keeps the
	// bytes of a deleted item.
	for _, key := range append([]string{created.StorageKey}, created.derivativeKeys()...) {
		if _, ok := objects.objects[key]; ok {
			t.Errorf("object %q still present after deleting the media", key)
		}
	}
	if len(objects.objects) != 0 {
		t.Errorf("objects = %d, want none", len(objects.objects))
	}
}
