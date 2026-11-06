package storymedia

import (
	"bytes"
	"context"
	"errors"
	"fmt"
	"image"
	"image/png"
	"io"
	"net/http"
	"net/http/httptest"
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
	objects    map[string][]byte
	deleted    []string
	putErr     error
	getErr     error
	rangeErr   error
	deleteErr  error
	existsErr  error
	presignErr error

	// What the last PresignPut was asked for (KNOT-040).
	presignedKey         string
	presignedContentType string
	presignedExpiry      time.Duration
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

// Exists reports whether the key was written, which is what the notify path asks
// before it records a row (KNOT-040).
func (o *fakeObjects) Exists(_ context.Context, key string) (bool, error) {
	if o.existsErr != nil {
		return false, o.existsErr
	}
	_, ok := o.objects[key]
	return ok, nil
}

// PresignPut records what was asked for and answers with a URL-shaped value, so a
// test can assert the key, the content type and the expiry the service chose
// (KNOT-040).
func (o *fakeObjects) PresignPut(_ context.Context, key, contentType string, expires time.Duration) (string, error) {
	if o.presignErr != nil {
		return "", o.presignErr
	}
	o.presignedKey = key
	o.presignedContentType = contentType
	o.presignedExpiry = expires
	return fmt.Sprintf("https://objects.test/%s?content-type=%s&expires=%d", key, contentType, int(expires.Seconds())), nil
}

// fakeLogger records the lines the service emits. The mutex is required: the media
// pipeline logs from background goroutines, and two jobs finishing at once append
// concurrently (KNOT-037-fix17).
type fakeLogger struct {
	mu       sync.Mutex
	infos    []string
	warnings []string
	errors   []string
	entries  []logEntry
}

// logEntry is one captured line with its structured attributes.
type logEntry struct {
	level string
	msg   string
	attrs map[string]string
}

func (l *fakeLogger) InfoContext(_ context.Context, msg string, args ...any) {
	l.record("info", msg, args)
	l.mu.Lock()
	defer l.mu.Unlock()
	l.infos = append(l.infos, msg)
}

func (l *fakeLogger) WarnContext(_ context.Context, msg string, args ...any) {
	l.record("warn", msg, args)
	l.mu.Lock()
	defer l.mu.Unlock()
	l.warnings = append(l.warnings, msg)
}

func (l *fakeLogger) ErrorContext(_ context.Context, msg string, args ...any) {
	l.record("error", msg, args)
	l.mu.Lock()
	defer l.mu.Unlock()
	l.errors = append(l.errors, msg)
}

// record captures one line and its attributes, so a test can assert exactly what
// an operator would read in the log stream.
func (l *fakeLogger) record(level, msg string, args []any) {
	attrs := make(map[string]string, len(args)/2)
	for i := 0; i+1 < len(args); i += 2 {
		key, ok := args[i].(string)
		if !ok {
			continue
		}
		attrs[key] = fmt.Sprint(args[i+1])
	}

	l.mu.Lock()
	defer l.mu.Unlock()
	l.entries = append(l.entries, logEntry{level: level, msg: msg, attrs: attrs})
}

// find returns the first recorded line with msg.
func (l *fakeLogger) find(msg string) (logEntry, bool) {
	l.mu.Lock()
	defer l.mu.Unlock()

	for _, entry := range l.entries {
		if entry.msg == msg {
			return entry, true
		}
	}

	return logEntry{}, false
}

// messages lists every recorded message, for a failure that needs context.
func (l *fakeLogger) messages() []string {
	l.mu.Lock()
	defer l.mu.Unlock()

	out := make([]string, 0, len(l.entries))
	for _, entry := range l.entries {
		out = append(out, entry.msg)
	}

	return out
}

func newTestService(t *testing.T, store StoryMediaStore, objects Storage) (*Service, *fakeLogger) {
	t.Helper()

	service, _, logger := newTestServiceWithQueue(t, store, objects)
	return service, logger
}

// newTestServiceWithQueue is newTestService for a test that asserts what was
// queued. The publisher is the seam the Rust media worker sits behind, so a test
// that checks the job is the closest a unit test gets to the worker
// (KNOT-038).
func newTestServiceWithQueue(t *testing.T, store StoryMediaStore, objects Storage) (*Service, *fakePublisher, *fakeLogger) {
	t.Helper()

	logger := &fakeLogger{}
	publisher := &fakePublisher{}
	service, err := NewService(store, objects, publisher, logger)
	if err != nil {
		t.Fatalf("NewService() error = %v, want nil", err)
	}

	return service, publisher, logger
}

// fakePublisher is the JobPublisher a test drives.
//
// It records every job, and can be made to fail, which is the only way an upload
// can be refused after the bytes are already in the bucket.
type fakePublisher struct {
	jobs []Job
	err  error
}

func (p *fakePublisher) Publish(_ context.Context, job Job) error {
	if p.err != nil {
		return p.err
	}
	p.jobs = append(p.jobs, job)
	return nil
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
	if _, err := NewService(nil, newFakeObjects(), &fakePublisher{}, &fakeLogger{}); err == nil {
		t.Error("NewService(nil store) error = nil, want an error")
	}
	if _, err := NewService(newFakeStore(), nil, &fakePublisher{}, &fakeLogger{}); err == nil {
		t.Error("NewService(nil objects) error = nil, want an error")
	}
	if _, err := NewService(newFakeStore(), newFakeObjects(), nil, &fakeLogger{}); err == nil {
		t.Error("NewService(nil publisher) error = nil, want an error")
	}
	if _, err := NewService(newFakeStore(), newFakeObjects(), &fakePublisher{}, nil); err == nil {
		t.Error("NewService(nil logger) error = nil, want an error")
	}
}

// ---------------------------------------------------------------------------
// CreateMedia
// ---------------------------------------------------------------------------

// TestCreateMediaStoresTheUploadAndQueuesTheJob is the whole contract of the new
// request path: one object, one processing row, one job (KNOT-038).
func TestCreateMediaStoresTheUploadAndQueuesTheJob(t *testing.T) {
	store := newFakeStore()
	objects := newFakeObjects()
	service, publisher, _ := newTestServiceWithQueue(t, store, objects)

	created, err := service.CreateMedia(context.Background(), pngInput())
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
	if created.Status != StatusProcessing {
		t.Errorf("status = %q, want %q: the worker finishes the row", created.Status, StatusProcessing)
	}

	// The row names the UPLOAD until the worker replaces it with the display
	// rendition, and there is no thumbnail key yet because no thumbnail exists.
	if !strings.HasPrefix(created.StorageKey, StoryMediaKeyPrefix(storyID)) {
		t.Errorf("storage key = %q, want the story's prefix", created.StorageKey)
	}
	if !strings.HasSuffix(created.StorageKey, "_original.png") {
		t.Errorf("storage key = %q, want the uploaded png", created.StorageKey)
	}
	if created.ThumbnailKey == nil {
		t.Error("thumbnail key = nil, want the key the worker will write")
	} else if !strings.HasSuffix(*created.ThumbnailKey, "_thumb.webp") {
		t.Errorf("thumbnail key = %q, want a webp thumbnail", *created.ThumbnailKey)
	}

	// The bytes are really in the bucket, under the key the row and the job name.
	stored, ok := objects.objects[created.StorageKey]
	if !ok {
		t.Fatalf("the upload %q was not stored", created.StorageKey)
	}
	if !bytes.Equal(stored, pngFixture()) {
		t.Error("the stored object is not the uploaded bytes")
	}

	// One job, naming the row, the story, the object and the pipeline.
	if len(publisher.jobs) != 1 {
		t.Fatalf("queued jobs = %d, want 1", len(publisher.jobs))
	}
	job := publisher.jobs[0]
	if job.MediaID != created.ID || job.StoryID != storyID {
		t.Errorf("job = %+v, want media %q of story %q", job, created.ID, storyID)
	}
	if job.ObjectKey != created.StorageKey {
		t.Errorf("job object key = %q, want %q", job.ObjectKey, created.StorageKey)
	}
	if job.MediaType != MediaTypeImage {
		t.Errorf("job media type = %q, want image", job.MediaType)
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

// TestCreateMediaDiscardsTheUploadWhenTheRowCannotBeWritten pins the rollback
// order: the bytes are stored first (so the job can name them), which means a
// failed insert must remove them again. A row naming an object nobody wrote and
// an object no row names are both leaks (KNOT-038).
func TestCreateMediaDiscardsTheUploadWhenTheRowCannotBeWritten(t *testing.T) {
	store := newFakeStore()
	store.createErr = errors.New("insert exploded")
	objects := newFakeObjects()
	service, publisher, _ := newTestServiceWithQueue(t, store, objects)

	if _, err := service.CreateMedia(context.Background(), pngInput()); err == nil {
		t.Fatal("CreateMedia() error = nil, want an error")
	}

	if len(objects.objects) != 0 || len(objects.deleted) != 1 {
		t.Errorf("objects = %d, deletions = %d, want the upload removed again",
			len(objects.objects), len(objects.deleted))
	}
	if len(publisher.jobs) != 0 {
		t.Errorf("queued jobs = %d, want 0: nothing was recorded", len(publisher.jobs))
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
	stored, err := store.GetMedia(context.Background(), created.ID)
	if err != nil {
		t.Fatalf("GetMedia() error = %v", err)
	}
	if _, ok := objects.objects[stored.StorageKey]; !ok {
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
	service, err := NewService(store, objects, &fakePublisher{}, logger)
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

	// A row written by the derivative pipeline: the primary object IS the display
	// AVIF, which is what the empty (full-rendition) variant addresses.
	payload := []byte("0123456789abcdefghij")
	key := StoryMediaKeyPrefix(storyID) + "photo_display.avif"
	objects.objects[key] = payload

	created, err := store.CreateMedia(context.Background(), StoryMedia{
		StoryID:      storyID,
		UploaderID:   authorID,
		StorageKey:   key,
		MediaType:    MediaTypeImage,
		MimeType:     "image/jpeg",
		Source:       MediaSourceCamera,
		SizeBytes:    int64(len(payload)),
		Status:       StatusReady,
		DisplayOrder: -1,
	})
	if err != nil {
		t.Fatalf("CreateMedia() error = %v", err)
	}

	// No Range header: the whole object. An absent variant asks for the item's full
	// rendition, which is the display AVIF this row names.
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
	// The type follows the object's key, not the upload's MIME type.
	if mimeType != "image/avif" {
		t.Errorf("mime = %q, want image/avif", mimeType)
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
// The queued upload (KNOT-038)
// ---------------------------------------------------------------------------

// pngFixture is a real 2x2 PNG.
//
// It is encoded here, in a test, because the SERVICE no longer decodes anything:
// the only thing that matters to it is that the bytes are what the handler
// sniffed as an image. The fixture stays real so the job's extension and the
// stored bytes agree.
func pngFixture() []byte {
	var buffer bytes.Buffer
	if err := png.Encode(&buffer, image.NewRGBA(image.Rect(0, 0, 2, 2))); err != nil {
		panic("storymedia: encoding the test PNG: " + err.Error())
	}
	return buffer.Bytes()
}

// pngInput is a create input carrying the PNG fixture.
func pngInput() CreateMediaInput {
	payload := pngFixture()
	return CreateMediaInput{
		StoryID:    storyID,
		UploaderID: authorID,
		MimeType:   "image/png",
		Source:     MediaSourceCamera,
		Body:       bytes.NewReader(payload),
		SizeBytes:  int64(len(payload)),
	}
}

// videoInput is a create input for a video. The bytes are not a real video: the
// service never transcodes, so only the type and the metadata matter here.
func videoInput() CreateMediaInput {
	payload := []byte("pretend mp4 bytes")
	return CreateMediaInput{
		StoryID:    storyID,
		UploaderID: authorID,
		MimeType:   "video/mp4",
		Source:     MediaSourceGallery,
		Body:       bytes.NewReader(payload),
		SizeBytes:  int64(len(payload)),
	}
}

// imageInput is kept for the read-path tests, which used it to get a real image
// through the old encoder. The service no longer decodes anything, so the
// dimensions have no meaning to it (KNOT-038).
func imageInput(t *testing.T, _, _ int) CreateMediaInput {
	t.Helper()
	return pngInput()
}

// WaitForProcessing is a TEST-ONLY stand-in for the background worker KNOT-038
// removed from this process.
//
// In production the Rust media worker writes the derivatives and flips the row;
// here the read-path tests still need a ready row to serve, so this shim applies
// the same effect to the in-memory fakes: it writes a display AVIF (plus the WebP
// display of KNOT-038-fix3 and a WebP thumbnail for an image), records the
// thumbnail key on the row, marks it ready, and removes the upload. It
// deliberately does no encoding — the encoders are gone from this package —
// because the bytes only have to be distinguishable.
func (s *Service) WaitForProcessing() {
	store, ok := s.store.(*fakeStore)
	if !ok {
		return
	}
	objects, ok := s.objects.(*fakeObjects)
	if !ok {
		return
	}

	for id, media := range store.media {
		if media.Status != StatusProcessing {
			continue
		}

		base, _, found := strings.Cut(media.StorageKey, originalSuffix)
		if !found {
			continue
		}

		displayKey := base + displaySuffix + avifExtension
		// Long enough for the Range assertions to exercise a real slice.
		displayBody := append([]byte("display bytes for "+id+":"), bytes.Repeat([]byte("x"), 512)...)
		objects.objects[displayKey] = displayBody

		var thumbnailKey *string
		if media.MediaType == MediaTypeImage {
			// The WebP display of KNOT-038-fix3: same size, a codec every supported
			// device can decode, distinct bytes so a test can tell the two apart.
			webpDisplayKey := base + displaySuffix + webpExtension
			objects.objects[webpDisplayKey] = append([]byte("webp display bytes for "+id+":"), bytes.Repeat([]byte("w"), 512)...)

			key := base + thumbnailSuffix + webpExtension
			objects.objects[key] = []byte("thumbnail bytes for " + id)
			thumbnailKey = &key
		}

		delete(objects.objects, media.StorageKey)

		media.Status = StatusReady
		media.StorageKey = displayKey
		media.ThumbnailKey = thumbnailKey
		media.SizeBytes = int64(len(displayBody))
		store.media[id] = media
	}
}

// TestCreateMediaKeysTheUploadByItsSniffedType pins the object-key extension to
// the allowlist entry rather than to a filename: the worker decodes the file from
// its key, and the key is the only record of what was uploaded.
func TestCreateMediaKeysTheUploadByItsSniffedType(t *testing.T) {
	cases := map[string]string{
		"image/png":       ".png",
		"image/jpeg":      ".jpg",
		"image/webp":      ".webp",
		"video/mp4":       ".mp4",
		"video/quicktime": ".mov",
	}

	for mimeType, extension := range cases {
		t.Run(mimeType, func(t *testing.T) {
			store := newFakeStore()
			objects := newFakeObjects()
			service, publisher, _ := newTestServiceWithQueue(t, store, objects)

			in := videoInput()
			in.MimeType = mimeType
			created, err := service.CreateMedia(context.Background(), in)
			if err != nil {
				t.Fatalf("CreateMedia() error = %v, want nil", err)
			}

			if !strings.HasSuffix(created.StorageKey, "_original"+extension) {
				t.Errorf("storage key = %q, want an %s upload", created.StorageKey, extension)
			}
			if publisher.jobs[0].ObjectKey != created.StorageKey {
				t.Errorf("queued object key = %q, want %q", publisher.jobs[0].ObjectKey, created.StorageKey)
			}
		})
	}
}

// TestCreateMediaQueuesTheUploadAndTellsReadersToWait is the async contract: by
// the time the call returns the bytes are stored and the job is queued, and a
// reader that arrives early is told to wait rather than shown a broken image.
func TestCreateMediaQueuesTheUploadAndTellsReadersToWait(t *testing.T) {
	store := newFakeStore()
	objects := newFakeObjects()
	service, publisher, _ := newTestServiceWithQueue(t, store, objects)

	created, err := service.CreateMedia(context.Background(), pngInput())
	if err != nil {
		t.Fatalf("CreateMedia() error = %v, want nil", err)
	}

	if len(publisher.jobs) != 1 {
		t.Fatalf("queued jobs = %d, want 1", len(publisher.jobs))
	}
	if _, ok := objects.objects[created.StorageKey]; !ok {
		t.Fatalf("the upload %q is not in the bucket", created.StorageKey)
	}

	// No derivative exists yet, so every variant answers "still working".
	for _, variant := range []Variant{VariantThumb, VariantDisplay} {
		if _, _, _, err := service.StreamMedia(context.Background(), created.ID, variant, "", newRangeRequest("")); !errors.Is(err, ErrProcessing) {
			t.Errorf("StreamMedia(%q) error = %v, want ErrProcessing", variant, err)
		}
	}
}

// TestCreateMediaRollsBackWhenTheQueueIsDown covers the one failure the new
// request path can leave behind: the bytes are in the bucket and the row exists,
// but nothing will ever process them. Both are removed, so a client that retries
// starts from a clean state instead of leaving an orphan behind forever.
func TestCreateMediaRollsBackWhenTheQueueIsDown(t *testing.T) {
	store := newFakeStore()
	objects := newFakeObjects()
	service, publisher, _ := newTestServiceWithQueue(t, store, objects)
	publisher.err = errors.New("redis is down")

	if _, err := service.CreateMedia(context.Background(), pngInput()); err == nil {
		t.Fatal("CreateMedia() error = nil, want the queue failure")
	}

	if len(objects.objects) != 0 {
		t.Errorf("objects = %d, want the upload removed again", len(objects.objects))
	}
	if len(store.media) != 0 {
		t.Errorf("media rows = %d, want the row removed again", len(store.media))
	}
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

	stored, err := store.GetMedia(context.Background(), created.ID)
	if err != nil {
		t.Fatalf("GetMedia() error = %v", err)
	}

	body, total, mimeType, err := service.StreamMedia(context.Background(), created.ID, VariantDisplay, "", newRangeRequest(""))
	if err != nil {
		t.Fatalf("StreamMedia(display) error = %v, want nil", err)
	}

	served, _ := io.ReadAll(body)
	want := objects.objects[stored.StorageKey]
	if !bytes.Equal(served, want) {
		t.Error("StreamMedia(display) did not serve the display rendition")
	}
	if total != int64(len(want)) {
		t.Errorf("total = %d, want the display's %d bytes", total, len(want))
	}
	if mimeType != avifContentType {
		t.Errorf("mime = %q, want %q", mimeType, avifContentType)
	}
}

// TestStreamMediaServesTheWebPDisplay pins the KNOT-038-fix3 contract: an image's
// display exists in two codecs, and `format=webp` selects the WebP one. AVIF is
// native only on Android 12+ and iOS 16+, so a device below those decodes the
// primary display into a blank rectangle with no error to react to.
func TestStreamMediaServesTheWebPDisplay(t *testing.T) {
	store := newFakeStore()
	objects := newFakeObjects()
	service, _ := newTestService(t, store, objects)

	created, err := service.CreateMedia(context.Background(), imageInput(t, 1000, 500))
	if err != nil {
		t.Fatalf("CreateMedia() error = %v, want nil", err)
	}
	service.WaitForProcessing()

	stored, err := store.GetMedia(context.Background(), created.ID)
	if err != nil {
		t.Fatalf("GetMedia() error = %v", err)
	}

	webpKey, ok := stored.DisplayWebPKey()
	if !ok {
		t.Fatal("DisplayWebPKey() = false, want the stored webp display")
	}

	body, total, mimeType, err := service.StreamMedia(context.Background(), created.ID, VariantDisplay, FormatWebP, newRangeRequest(""))
	if err != nil {
		t.Fatalf("StreamMedia(display, webp) error = %v, want nil", err)
	}

	served, _ := io.ReadAll(body)
	want := objects.objects[webpKey]
	if !bytes.Equal(served, want) {
		t.Error("StreamMedia(display, webp) did not serve the webp display")
	}
	if total != int64(len(want)) {
		t.Errorf("total = %d, want the webp display's %d bytes", total, len(want))
	}
	if mimeType != webpContentType {
		t.Errorf("mime = %q, want %q", mimeType, webpContentType)
	}
}

// TestStreamMediaFallsBackToTheAVIFDisplayForAnOlderRow covers a row written
// before KNOT-038-fix3, which has no WebP display. Asking for one must answer with
// the bytes that exist rather than a 404: the client has no other address.
func TestStreamMediaFallsBackToTheAVIFDisplayForAnOlderRow(t *testing.T) {
	store := newFakeStore()
	objects := newFakeObjects()
	service, _ := newTestService(t, store, objects)

	created, err := service.CreateMedia(context.Background(), imageInput(t, 1000, 500))
	if err != nil {
		t.Fatalf("CreateMedia() error = %v, want nil", err)
	}
	service.WaitForProcessing()

	stored, err := store.GetMedia(context.Background(), created.ID)
	if err != nil {
		t.Fatalf("GetMedia() error = %v", err)
	}

	webpKey, ok := stored.DisplayWebPKey()
	if !ok {
		t.Fatal("DisplayWebPKey() = false, want the stored webp display")
	}
	delete(objects.objects, webpKey)

	body, total, mimeType, err := service.StreamMedia(context.Background(), created.ID, VariantDisplay, FormatWebP, newRangeRequest(""))
	if err != nil {
		t.Fatalf("StreamMedia(display, webp, missing webp) error = %v, want nil", err)
	}

	served, _ := io.ReadAll(body)
	want := objects.objects[stored.StorageKey]
	if !bytes.Equal(served, want) {
		t.Error("the fallback did not serve the AVIF display")
	}
	if total != int64(len(want)) {
		t.Errorf("total = %d, want the AVIF display's %d bytes", total, len(want))
	}
	if mimeType != avifContentType {
		t.Errorf("mime = %q, want %q so the client sees what it really got", mimeType, avifContentType)
	}
}

func TestStreamMediaAnswers404WhenTheThumbnailIsMissing(t *testing.T) {
	store := newFakeStore()
	objects := newFakeObjects()
	service, _ := newTestService(t, store, objects)

	created, err := service.CreateMedia(context.Background(), imageInput(t, 300, 200))
	if err != nil {
		t.Fatalf("CreateMedia() error = %v, want nil", err)
	}
	service.WaitForProcessing()

	// The thumbnail object is gone. There is no original to fall back to any more,
	// and answering with the multi-megabyte display would defeat the rendition
	// request, so the item is reported missing (KNOT-037-fix10).
	thumbnailKey, _ := created.ThumbnailWebPKey()
	delete(objects.objects, thumbnailKey)

	if _, _, _, err := service.StreamMedia(context.Background(), created.ID, VariantThumb, "", newRangeRequest("")); !errors.Is(err, ErrNotFound) {
		t.Errorf("StreamMedia(thumb, missing object) error = %v, want ErrNotFound", err)
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

func TestStreamMediaAnswers404ForAVariantTheRowDoesNotHave(t *testing.T) {
	store := newFakeStore()
	objects := newFakeObjects()
	service, _ := newTestService(t, store, objects)

	created, err := service.CreateMedia(context.Background(), imageInput(t, 300, 200))
	if err != nil {
		t.Fatalf("CreateMedia() error = %v, want nil", err)
	}
	service.WaitForProcessing()

	// This row has no poster, and the primary object is the display AVIF. Answering a
	// poster request with it would send a full-size picture for a still frame, so the
	// request is a 404 (KNOT-037-fix10).
	if _, _, _, err := service.StreamMedia(context.Background(), created.ID, VariantPoster, "", newRangeRequest("")); !errors.Is(err, ErrNotFound) {
		t.Errorf("StreamMedia(poster on an image) error = %v, want ErrNotFound", err)
	}
}

// TestStreamMediaServesNoOriginalForALegacyRow is KNOT-037-fix10's core rule: a row
// written before the derivative pipeline names its uploaded file as the primary
// object, and that file must never be served.
func TestStreamMediaServesNoOriginalForALegacyRow(t *testing.T) {
	store := newFakeStore()
	objects := newFakeObjects()
	service, _ := newTestService(t, store, objects)

	created, err := service.CreateMedia(context.Background(), imageInput(t, 300, 200))
	if err != nil {
		t.Fatalf("CreateMedia() error = %v, want nil", err)
	}
	service.WaitForProcessing()

	// Rewrite the row as a pre-KNOT-037-fix7 one, with the upload present in the
	// bucket so a fallback would SUCCEED and go unnoticed if the rule regressed.
	legacyKey := StoryMediaKeyPrefix(created.StoryID) + "legacy_upload.jpg"
	objects.objects[legacyKey] = []byte("the multi-megabyte original")
	stored := store.media[created.ID]
	stored.StorageKey = legacyKey
	store.media[created.ID] = stored

	for _, variant := range []Variant{"", VariantDisplay, VariantStream} {
		if _, _, _, err := service.StreamMedia(context.Background(), created.ID, variant, "", newRangeRequest("")); !errors.Is(err, ErrNotFound) {
			t.Errorf("StreamMedia(%q on a legacy row) error = %v, want ErrNotFound", variant, err)
		}
	}
}

// TestStreamMediaServesTheStreamAsMP4 pins the video half of the full-rendition
// contract: the stream object is served as `video/mp4`, never as the upload's
// declared type (KNOT-037-fix10).
func TestStreamMediaServesTheStreamAsMP4(t *testing.T) {
	store := newFakeStore()
	objects := newFakeObjects()
	service, _ := newTestService(t, store, objects)

	created, err := service.CreateMedia(context.Background(), imageInput(t, 300, 200))
	if err != nil {
		t.Fatalf("CreateMedia() error = %v, want nil", err)
	}
	service.WaitForProcessing()

	// Rewrite the row as a video produced by the derivative pipeline, so no FFmpeg
	// is needed: the object the row names is the stream itself.
	streamKey := StoryMediaKeyPrefix(created.StoryID) + "clip_stream.mp4"
	objects.objects[streamKey] = []byte("the h264 stream")
	stored := store.media[created.ID]
	stored.MediaType = MediaTypeVideo
	stored.StorageKey = streamKey
	store.media[created.ID] = stored

	body, total, mimeType, err := service.StreamMedia(context.Background(), created.ID, VariantStream, "", newRangeRequest(""))
	if err != nil {
		t.Fatalf("StreamMedia(stream) error = %v, want nil", err)
	}

	served, _ := io.ReadAll(body)
	if !bytes.Equal(served, objects.objects[streamKey]) {
		t.Error("StreamMedia(stream) did not serve the stream object")
	}
	if total != stored.SizeBytes {
		t.Errorf("total = %d, want the row's %d", total, stored.SizeBytes)
	}
	if mimeType != videoContentType {
		t.Errorf("mime = %q, want %q", mimeType, videoContentType)
	}
	if mimeType != "video/mp4" {
		t.Errorf("mime = %q, want the literal video/mp4", mimeType)
	}
}

// TestStreamMediaAnswers404ForTheOtherFullVariant keeps the two full renditions
// apart: an image has no stream, and a video has no display.
func TestStreamMediaAnswers404ForTheOtherFullVariant(t *testing.T) {
	store := newFakeStore()
	objects := newFakeObjects()
	service, _ := newTestService(t, store, objects)

	created, err := service.CreateMedia(context.Background(), imageInput(t, 300, 200))
	if err != nil {
		t.Fatalf("CreateMedia() error = %v, want nil", err)
	}
	service.WaitForProcessing()

	if _, _, _, err := service.StreamMedia(context.Background(), created.ID, VariantDisplay, "", newRangeRequest("")); err != nil {
		t.Fatalf("StreamMedia(display) error = %v, want nil", err)
	}
	if _, _, _, err := service.StreamMedia(context.Background(), created.ID, VariantStream, "", newRangeRequest("")); !errors.Is(err, ErrNotFound) {
		t.Errorf("StreamMedia(stream on an image) error = %v, want ErrNotFound", err)
	}
}

func TestDeleteMediaRemovesTheUpload(t *testing.T) {
	store := newFakeStore()
	objects := newFakeObjects()
	service, _ := newTestService(t, store, objects)

	created, err := service.CreateMedia(context.Background(), pngInput())
	if err != nil {
		t.Fatalf("CreateMedia() error = %v, want nil", err)
	}

	if err := service.DeleteMedia(context.Background(), created.ID, authorID); err != nil {
		t.Fatalf("DeleteMedia() error = %v, want nil", err)
	}

	// The upload goes with the row. Anything the worker may have written is named
	// by the row's derivative columns, and deleting is best-effort for objects that
	// were never created (a job that has not run yet has none).
	if _, ok := objects.objects[created.StorageKey]; ok {
		t.Errorf("object %q still present after deleting the media", created.StorageKey)
	}
	if len(objects.objects) != 0 {
		t.Errorf("objects = %d, want none", len(objects.objects))
	}
}

// ---------------------------------------------------------------------------
// Spliced-out pipeline diagnostics (KNOT-038)
// ---------------------------------------------------------------------------
//
// KNOT-037-fix17's step-level trace lived here because the pipeline ran in this
// process. It now runs in the Rust worker, which logs the same steps from its own
// modules; there is nothing left in the Go service to observe.

// ---------------------------------------------------------------------------
// The low-quality placeholder (KNOT-043)
// ---------------------------------------------------------------------------

// TestLQIPKeyIsDerivedFromTheStill pins the naming rule: the placeholder sits
// beside whichever still the row has, from the same `<uuid>` base, so it needs no
// column of its own.
func TestLQIPKeyIsDerivedFromTheStill(t *testing.T) {
	webpThumb := "story-media/s/abc_thumb.webp"
	avifThumb := "story-media/s/abc_thumb.avif"
	legacyThumb := "story-media/s/abc_thumb.jpg"
	poster := "story-media/s/abc_poster.webp"
	legacyPoster := "story-media/s/abc_poster.jpg"
	want := "story-media/s/abc_lqip.webp"

	for _, testCase := range []struct {
		name  string
		media StoryMedia
		want  string
		ok    bool
	}{
		{name: "a webp thumbnail", media: StoryMedia{ThumbnailKey: &webpThumb}, want: want, ok: true},
		{name: "an avif thumbnail", media: StoryMedia{ThumbnailKey: &avifThumb}, want: want, ok: true},
		{name: "a video poster", media: StoryMedia{PosterKey: &poster}, want: want, ok: true},
		// A row written before the placeholder existed has no file to name: the
		// answer is false, not a key that would 404 on every read.
		{name: "a legacy jpeg thumbnail", media: StoryMedia{ThumbnailKey: &legacyThumb}},
		{name: "a legacy jpeg poster", media: StoryMedia{PosterKey: &legacyPoster}},
		{name: "no stills at all", media: StoryMedia{}},
	} {
		t.Run(testCase.name, func(t *testing.T) {
			got, ok := testCase.media.LQIPKey()
			if ok != testCase.ok {
				t.Fatalf("LQIPKey() ok = %v, want %v (key %q)", ok, testCase.ok, got)
			}
			if ok && got != testCase.want {
				t.Errorf("LQIPKey() = %q, want %q", got, testCase.want)
			}
		})
	}
}

// TestStreamMediaServesTheLQIP proves the placeholder is served whole, as WebP,
// from the derivative beside the still.
func TestStreamMediaServesTheLQIP(t *testing.T) {
	store := newFakeStore()
	objects := newFakeObjects()
	service, _ := newTestService(t, store, objects)

	created, err := service.CreateMedia(context.Background(), imageInput(t, 900, 600))
	if err != nil {
		t.Fatalf("CreateMedia() error = %v, want nil", err)
	}
	service.WaitForProcessing()

	lqipKey, ok := created.LQIPKey()
	if !ok {
		t.Fatalf("LQIPKey() = false for %+v, want the derived placeholder", created)
	}
	placeholder := []byte("RIFF....WEBP")
	objects.objects[lqipKey] = placeholder

	body, total, mimeType, err := service.StreamMedia(context.Background(), created.ID, VariantLQIP, "", newRangeRequest(""))
	if err != nil {
		t.Fatalf("StreamMedia(lqip) error = %v, want nil", err)
	}

	served, _ := io.ReadAll(body)
	if !bytes.Equal(served, placeholder) {
		t.Error("StreamMedia(lqip) did not serve the stored placeholder")
	}
	if total != int64(len(placeholder)) {
		t.Errorf("total = %d, want %d", total, len(placeholder))
	}
	if mimeType != webpContentType {
		t.Errorf("mime = %q, want %q", mimeType, webpContentType)
	}
}

// TestStreamMediaFallsBackToTheStillWithoutAPlaceholder covers the rows that
// predate KNOT-043: asking for a placeholder must still answer with the smallest
// picture the row has rather than 404, because a card draws whatever it gets.
func TestStreamMediaFallsBackToTheStillWithoutAPlaceholder(t *testing.T) {
	store := newFakeStore()
	objects := newFakeObjects()
	service, _ := newTestService(t, store, objects)

	created, err := service.CreateMedia(context.Background(), imageInput(t, 300, 200))
	if err != nil {
		t.Fatalf("CreateMedia() error = %v, want nil", err)
	}
	service.WaitForProcessing()

	// No placeholder was written for this row.
	lqipKey, ok := created.LQIPKey()
	if !ok {
		t.Fatal("LQIPKey() = false, want a derived key")
	}
	delete(objects.objects, lqipKey)

	thumbnailKey, _ := created.ThumbnailWebPKey()

	body, _, mimeType, err := service.StreamMedia(context.Background(), created.ID, VariantLQIP, "", newRangeRequest(""))
	if err != nil {
		t.Fatalf("StreamMedia(lqip) error = %v, want nil", err)
	}

	served, _ := io.ReadAll(body)
	if !bytes.Equal(served, objects.objects[thumbnailKey]) {
		t.Error("StreamMedia(lqip) did not fall back to the thumbnail")
	}
	if mimeType != webpContentType {
		t.Errorf("mime = %q, want %q", mimeType, webpContentType)
	}
}
