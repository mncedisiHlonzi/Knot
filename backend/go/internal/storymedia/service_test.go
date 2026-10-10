package storymedia

import (
	"bytes"
	"context"
	"errors"
	"fmt"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
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

// fakeLogger records the warnings the service emits.
type fakeLogger struct {
	warnings []string
}

func (l *fakeLogger) WarnContext(_ context.Context, msg string, args ...any) {
	l.warnings = append(l.warnings, msg)
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

	created, err := service.CreateMedia(context.Background(), validInput())
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
	if !strings.HasSuffix(created.StorageKey, ".png") {
		t.Errorf("storage key = %q, want a .png extension", created.StorageKey)
	}
	if _, ok := objects.objects[created.StorageKey]; !ok {
		t.Error("the object was not written to storage")
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

func TestCreateMediaRollsBackObjectOnStoreFailure(t *testing.T) {
	store := newFakeStore()
	store.createErr = errors.New("insert exploded")
	objects := newFakeObjects()
	service, _ := newTestService(t, store, objects)

	if _, err := service.CreateMedia(context.Background(), validInput()); err == nil {
		t.Fatal("CreateMedia() error = nil, want an error")
	}

	if len(objects.deleted) != 1 {
		t.Errorf("deleted objects = %d, want the orphaned object removed once", len(objects.deleted))
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

	created, err := service.CreateMedia(context.Background(), validInput())
	if err != nil {
		t.Fatalf("CreateMedia() error = %v", err)
	}

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

	payload := []byte("0123456789abcdefghij")
	in := validInput()
	in.SizeBytes = int64(len(payload))
	in.Body = bytes.NewReader(payload)

	created, err := service.CreateMedia(context.Background(), in)
	if err != nil {
		t.Fatalf("CreateMedia() error = %v", err)
	}

	// No Range header: the whole object.
	request := newRangeRequest("")
	body, total, mimeType, err := service.StreamMedia(context.Background(), created.ID, request)
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
	body, total, _, err = service.StreamMedia(context.Background(), created.ID, request)
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
