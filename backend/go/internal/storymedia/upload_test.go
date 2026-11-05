package storymedia

import (
	"bytes"
	"context"
	"errors"
	"strings"
	"testing"
	"time"
)

// Tests for the direct-to-bucket upload path (KNOT-040): the service mints a
// signed URL for one story-scoped key, and later records a row for an object the
// client uploaded itself.

func presignInput() PresignUploadInput {
	return PresignUploadInput{
		StoryID:     storyID,
		UploaderID:  authorID,
		Filename:    "photo.jpg",
		ContentType: "image/jpeg",
	}
}

// uploadedKey mints the key shape PresignUpload produces, so a test can put bytes
// where the client would have put them.
func uploadedKey(t *testing.T, objectKey string) string {
	t.Helper()

	if !strings.HasPrefix(objectKey, StoryMediaKeyPrefix(storyID)) {
		t.Fatalf("object key %q is not scoped to the story", objectKey)
	}
	return objectKey
}

// objectPresent reports whether the fake bucket still holds key.
func objectPresent(objects *fakeObjects, key string) bool {
	_, ok := objects.objects[key]
	return ok
}

func TestPresignUploadMintsAStoryScopedKey(t *testing.T) {
	objects := newFakeObjects()
	service, _ := newTestService(t, newFakeStore(), objects)

	got, err := service.PresignUpload(context.Background(), presignInput())
	if err != nil {
		t.Fatalf("PresignUpload() error = %v, want nil", err)
	}

	key := uploadedKey(t, got.ObjectKey)
	if !strings.HasSuffix(key, originalSuffix+".jpg") {
		t.Errorf("object key %q does not end in %q", key, originalSuffix+".jpg")
	}
	if uuidText := strings.TrimSuffix(strings.TrimPrefix(key, StoryMediaKeyPrefix(storyID)), originalSuffix+".jpg"); !isUUID(uuidText) {
		t.Errorf("object key %q does not carry a UUID name", key)
	}
	if !strings.HasPrefix(got.UploadURL, "https://objects.test/"+key) {
		t.Errorf("upload url %q does not address the object key %q", got.UploadURL, key)
	}
	if want := int(PresignTTL.Seconds()); got.ExpiresIn != want {
		t.Errorf("ExpiresIn = %d, want %d", got.ExpiresIn, want)
	}

	// What was signed is exactly what the service reported.
	if objects.presignedKey != got.ObjectKey {
		t.Errorf("presigned key = %q, want %q", objects.presignedKey, got.ObjectKey)
	}
	if objects.presignedContentType != "image/jpeg" {
		t.Errorf("presigned content type = %q, want image/jpeg", objects.presignedContentType)
	}
	if objects.presignedExpiry != PresignTTL {
		t.Errorf("presigned expiry = %s, want %s", objects.presignedExpiry, PresignTTL)
	}
}

func TestPresignUploadWritesNothing(t *testing.T) {
	store := newFakeStore()
	objects := newFakeObjects()
	service, _ := newTestService(t, store, objects)

	if _, err := service.PresignUpload(context.Background(), presignInput()); err != nil {
		t.Fatalf("PresignUpload() error = %v, want nil", err)
	}

	if len(store.media) != 0 {
		t.Errorf("presign created %d media rows, want 0", len(store.media))
	}
	if len(objects.objects) != 0 {
		t.Errorf("presign stored %d objects, want 0", len(objects.objects))
	}
}

func TestPresignUploadRejectsInvalidInput(t *testing.T) {
	service, _ := newTestService(t, newFakeStore(), newFakeObjects())

	tests := []struct {
		name   string
		mutate func(*PresignUploadInput)
		want   string
	}{
		{name: "story id is not a UUID", mutate: func(in *PresignUploadInput) { in.StoryID = "not-a-uuid" }, want: "story_id"},
		{name: "uploader id is not a UUID", mutate: func(in *PresignUploadInput) { in.UploaderID = "nope" }, want: "uploader_id"},
		{name: "filename is blank", mutate: func(in *PresignUploadInput) { in.Filename = "   " }, want: "filename"},
		{
			name:   "filename carries a path",
			mutate: func(in *PresignUploadInput) { in.Filename = "../../etc/passwd" },
			want:   "filename",
		},
		{
			name:   "filename is a control character",
			mutate: func(in *PresignUploadInput) { in.Filename = "photo\x00.jpg" },
			want:   "filename",
		},
		{
			name:   "content type is not in the allowlist",
			mutate: func(in *PresignUploadInput) { in.ContentType = "image/gif" },
			want:   "unsupported",
		},
	}

	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			in := presignInput()
			test.mutate(&in)

			_, err := service.PresignUpload(context.Background(), in)
			if err == nil {
				t.Fatalf("PresignUpload() error = nil, want an error")
			}

			switch test.want {
			case "unsupported":
				if !errors.Is(err, ErrUnsupportedMediaType) {
					t.Errorf("error = %v, want ErrUnsupportedMediaType", err)
				}
			default:
				var validation *ValidationError
				if !errors.As(err, &validation) || validation.Field != test.want {
					t.Errorf("error = %v, want a validation error on %q", err, test.want)
				}
			}
		})
	}
}

func TestPresignUploadRequiresTheStoryAuthor(t *testing.T) {
	service, _ := newTestService(t, newFakeStore(), newFakeObjects())

	in := presignInput()
	in.UploaderID = uploaderID

	if _, err := service.PresignUpload(context.Background(), in); !errors.Is(err, ErrForbidden) {
		t.Errorf("error = %v, want ErrForbidden", err)
	}
}

func TestPresignUploadUnknownStory(t *testing.T) {
	service, _ := newTestService(t, newFakeStore(), newFakeObjects())

	in := presignInput()
	in.StoryID = "99999999-9999-4999-8999-999999999999"

	if _, err := service.PresignUpload(context.Background(), in); !errors.Is(err, ErrStoryNotFound) {
		t.Errorf("error = %v, want ErrStoryNotFound", err)
	}
}

func TestPresignUploadRefusesAFullStory(t *testing.T) {
	store := newFakeStore()
	atCap := MaxMediaPerStory
	store.countOverride = &atCap

	service, _ := newTestService(t, store, newFakeObjects())

	if _, err := service.PresignUpload(context.Background(), presignInput()); !errors.Is(err, ErrMediaLimit) {
		t.Errorf("error = %v, want ErrMediaLimit", err)
	}
}

func TestPresignUploadReportsASigningFailure(t *testing.T) {
	objects := newFakeObjects()
	objects.presignErr = errors.New("signing exploded")
	service, _ := newTestService(t, newFakeStore(), objects)

	if _, err := service.PresignUpload(context.Background(), presignInput()); err == nil {
		t.Fatal("PresignUpload() error = nil, want an error")
	}
}

// sealedUpload presigns a URL, then puts the bytes where the client would have put
// them, and returns the object key.
func sealedUpload(t *testing.T, service *Service, objects *fakeObjects, contentType string) string {
	t.Helper()

	in := presignInput()
	in.ContentType = contentType
	got, err := service.PresignUpload(context.Background(), in)
	if err != nil {
		t.Fatalf("PresignUpload() error = %v, want nil", err)
	}

	if err := objects.Put(context.Background(), got.ObjectKey, bytes.NewReader([]byte("bytes the client uploaded")), contentType); err != nil {
		t.Fatalf("simulating the client's upload: %v", err)
	}

	return got.ObjectKey
}

func TestAttachUploadRecordsAProcessingRowAndQueuesTheJob(t *testing.T) {
	store := newFakeStore()
	objects := newFakeObjects()
	service, publisher, _ := newTestServiceWithQueue(t, store, objects)

	key := sealedUpload(t, service, objects, "image/jpeg")

	width, height := 1200, 800
	created, err := service.AttachUpload(context.Background(), AttachUploadInput{
		StoryID:    storyID,
		UploaderID: authorID,
		ObjectKey:  key,
		Width:      &width,
		Height:     &height,
	})
	if err != nil {
		t.Fatalf("AttachUpload() error = %v, want nil", err)
	}

	if created.StorageKey != key {
		t.Errorf("StorageKey = %q, want %q", created.StorageKey, key)
	}
	if created.Status != StatusProcessing {
		t.Errorf("Status = %q, want %q", created.Status, StatusProcessing)
	}
	if created.MediaType != MediaTypeImage {
		t.Errorf("MediaType = %q, want image", created.MediaType)
	}
	if created.MimeType != "image/jpeg" {
		t.Errorf("MimeType = %q, want image/jpeg (read back from the key)", created.MimeType)
	}
	// A client that does not report a source is recorded as gallery.
	if created.Source != MediaSourceGallery {
		t.Errorf("Source = %q, want gallery", created.Source)
	}
	if created.Width == nil || *created.Width != width || created.Height == nil || *created.Height != height {
		t.Errorf("dimensions = %v x %v, want %d x %d", created.Width, created.Height, width, height)
	}
	if created.SizeBytes != 0 {
		t.Errorf("SizeBytes = %d, want 0 until the worker reports the derivative", created.SizeBytes)
	}

	// The thumbnail key is pre-minted for an image, from the same stem the worker
	// derives every rendition from.
	wantThumb := strings.TrimSuffix(key, originalSuffix+".jpg") + thumbnailSuffix + webpExtension
	if created.ThumbnailKey == nil || *created.ThumbnailKey != wantThumb {
		t.Errorf("ThumbnailKey = %v, want %q", created.ThumbnailKey, wantThumb)
	}

	if len(publisher.jobs) != 1 {
		t.Fatalf("queued %d jobs, want 1", len(publisher.jobs))
	}
	job := publisher.jobs[0]
	if job.ObjectKey != key || job.MediaID != created.ID || job.MediaType != MediaTypeImage {
		t.Errorf("job = %+v, want the uploaded object and the new row", job)
	}
}

func TestAttachUploadPrefersTheReportedSource(t *testing.T) {
	store := newFakeStore()
	objects := newFakeObjects()
	service, _ := newTestService(t, store, objects)

	key := sealedUpload(t, service, objects, "image/png")

	created, err := service.AttachUpload(context.Background(), AttachUploadInput{
		StoryID:    storyID,
		UploaderID: authorID,
		ObjectKey:  key,
		Source:     MediaSourceCamera,
	})
	if err != nil {
		t.Fatalf("AttachUpload() error = %v, want nil", err)
	}
	if created.Source != MediaSourceCamera {
		t.Errorf("Source = %q, want camera", created.Source)
	}
	if created.ThumbnailKey == nil || !strings.HasSuffix(*created.ThumbnailKey, thumbnailSuffix+webpExtension) {
		t.Errorf("ThumbnailKey = %v, want a webp thumbnail beside the upload", created.ThumbnailKey)
	}
}

func TestAttachUploadRejectsAnObjectThatWasNeverUploaded(t *testing.T) {
	objects := newFakeObjects()
	service, _ := newTestService(t, newFakeStore(), objects)

	got, err := service.PresignUpload(context.Background(), presignInput())
	if err != nil {
		t.Fatalf("PresignUpload() error = %v, want nil", err)
	}

	// No PUT: this is the "signed but never used" case.
	_, err = service.AttachUpload(context.Background(), AttachUploadInput{
		StoryID:    storyID,
		UploaderID: authorID,
		ObjectKey:  got.ObjectKey,
	})
	if !errors.Is(err, ErrUploadNotFound) {
		t.Errorf("error = %v, want ErrUploadNotFound", err)
	}
}

func TestAttachUploadRejectsAKeyThatIsNotAnOriginal(t *testing.T) {
	objects := newFakeObjects()
	service, _ := newTestService(t, newFakeStore(), objects)

	key := sealedUpload(t, service, objects, "image/jpeg")

	tests := []struct {
		name string
		key  string
	}{
		{
			name: "another story",
			key:  StoryMediaKeyPrefix("88888888-8888-4888-8888-888888888888") + "aaaaaaaa-aaaa-4aaa-8aaa-aaaaaaaaaaaa" + originalSuffix + ".jpg",
		},
		{name: "a derivative", key: strings.TrimSuffix(key, originalSuffix+".jpg") + displaySuffix + avifExtension},
		{name: "no original suffix", key: strings.TrimSuffix(key, originalSuffix+".jpg") + ".jpg"},
		{name: "no extension", key: strings.TrimSuffix(key, originalSuffix+".jpg") + originalSuffix},
		{name: "a path", key: StoryMediaKeyPrefix(storyID) + "nested/dir" + originalSuffix + ".jpg"},
		{name: "an unknown extension", key: strings.TrimSuffix(key, ".jpg") + ".gif"},
		{name: "an empty key", key: ""},
	}

	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			_, err := service.AttachUpload(context.Background(), AttachUploadInput{
				StoryID:    storyID,
				UploaderID: authorID,
				ObjectKey:  test.key,
			})
			if err == nil {
				t.Fatalf("AttachUpload(%q) error = nil, want an error", test.key)
			}
			if errors.Is(err, ErrUploadNotFound) {
				t.Errorf("error = %v, want a rejection before the object is looked up", err)
			}
		})
	}
}

func TestAttachUploadRequiresTheStoryAuthor(t *testing.T) {
	objects := newFakeObjects()
	service, _ := newTestService(t, newFakeStore(), objects)
	key := sealedUpload(t, service, objects, "image/jpeg")

	_, err := service.AttachUpload(context.Background(), AttachUploadInput{
		StoryID:    storyID,
		UploaderID: uploaderID,
		ObjectKey:  key,
	})
	if !errors.Is(err, ErrForbidden) {
		t.Errorf("error = %v, want ErrForbidden", err)
	}
}

func TestAttachUploadRefusesAFullStory(t *testing.T) {
	objects := newFakeObjects()
	store := newFakeStore()
	service, _ := newTestService(t, store, objects)
	key := sealedUpload(t, service, objects, "image/jpeg")

	atCap := MaxMediaPerStory
	store.countOverride = &atCap

	_, err := service.AttachUpload(context.Background(), AttachUploadInput{
		StoryID:    storyID,
		UploaderID: authorID,
		ObjectKey:  key,
	})
	if !errors.Is(err, ErrMediaLimit) {
		t.Errorf("error = %v, want ErrMediaLimit", err)
	}
}

func TestAttachUploadDiscardsTheObjectWhenTheRowFails(t *testing.T) {
	objects := newFakeObjects()
	store := newFakeStore()
	service, _ := newTestService(t, store, objects)
	key := sealedUpload(t, service, objects, "image/jpeg")

	store.createErr = errors.New("insert refused")

	_, err := service.AttachUpload(context.Background(), AttachUploadInput{
		StoryID:    storyID,
		UploaderID: authorID,
		ObjectKey:  key,
	})
	if err == nil {
		t.Fatal("AttachUpload() error = nil, want an error")
	}
	if objectPresent(objects, key) {
		t.Errorf("the upload %q survived a refused row", key)
	}
}

func TestAttachUploadRollsBackWhenTheJobCannotBeQueued(t *testing.T) {
	objects := newFakeObjects()
	store := newFakeStore()
	service, publisher, _ := newTestServiceWithQueue(t, store, objects)
	key := sealedUpload(t, service, objects, "image/jpeg")

	publisher.err = errors.New("queue unavailable")

	_, err := service.AttachUpload(context.Background(), AttachUploadInput{
		StoryID:    storyID,
		UploaderID: authorID,
		ObjectKey:  key,
	})
	if err == nil {
		t.Fatal("AttachUpload() error = nil, want an error")
	}
	if objectPresent(objects, key) {
		t.Errorf("the upload %q survived a refused job", key)
	}
	if len(store.media) != 0 {
		t.Errorf("left %d media rows behind, want 0", len(store.media))
	}
}

func TestParseUploadedObjectKeyReadsTheTypeFromTheKey(t *testing.T) {
	const name = "aaaaaaaa-aaaa-4aaa-8aaa-aaaaaaaaaaaa"
	prefix := StoryMediaKeyPrefix(storyID)

	tests := []struct {
		extension string
		wantMime  string
		wantType  MediaType
	}{
		{extension: ".jpg", wantMime: "image/jpeg", wantType: MediaTypeImage},
		{extension: ".png", wantMime: "image/png", wantType: MediaTypeImage},
		{extension: ".webp", wantMime: "image/webp", wantType: MediaTypeImage},
		{extension: ".mp4", wantMime: "video/mp4", wantType: MediaTypeVideo},
		{extension: ".mov", wantMime: "video/quicktime", wantType: MediaTypeVideo},
	}

	for _, test := range tests {
		t.Run(test.wantMime, func(t *testing.T) {
			key := prefix + name + originalSuffix + test.extension

			got, err := parseUploadedObjectKey(storyID, key)
			if err != nil {
				t.Fatalf("parseUploadedObjectKey() error = %v, want nil", err)
			}
			if got.MimeType != test.wantMime || got.MediaType != test.wantType {
				t.Errorf("got %q/%q, want %q/%q", got.MimeType, got.MediaType, test.wantMime, test.wantType)
			}
			if got.Key != key {
				t.Errorf("Key = %q, want %q", got.Key, key)
			}
			if want := prefix + name; got.BaseKey != want {
				t.Errorf("BaseKey = %q, want %q", got.BaseKey, want)
			}
		})
	}
}

func TestParseUploadedObjectKeyRejectsUppercaseExtensions(t *testing.T) {
	key := StoryMediaKeyPrefix(storyID) + "aaaaaaaa-aaaa-4aaa-8aaa-aaaaaaaaaaaa" + originalSuffix + ".JPG"

	if _, err := parseUploadedObjectKey(storyID, key); !errors.Is(err, ErrUnsupportedMediaType) {
		t.Errorf("error = %v, want ErrUnsupportedMediaType", err)
	}
}

func TestPresignTTLIsFifteenMinutes(t *testing.T) {
	// The contract the mobile client and the docs both quote.
	if PresignTTL != 15*time.Minute {
		t.Errorf("PresignTTL = %s, want 15m", PresignTTL)
	}
}
