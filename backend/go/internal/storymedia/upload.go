package storymedia

import (
	"context"
	"errors"
	"fmt"
	"strings"
	"time"
	"unicode"
)

// Direct-to-bucket uploads (KNOT-040).
//
// The request path used to receive the bytes and write them to the object store
// itself. That put the whole file through the API: a slow uplink could outlast the
// server's write deadline, and every byte of every upload occupied a request. Now
// the API only mints a signed URL, and the client sends the bytes straight to the
// bucket:
//
//	POST /media/upload-url   -> { upload_url, object_key, expires_in }
//	PUT  <upload_url>        -> the bytes (straight to the object store)
//	POST /stories/{id}/media -> { object_key }
//
// The two API calls are small JSON exchanges, so neither can time out because of
// the file's size. The job the second call queues is the same one the old path
// queued, which is why the media worker needed no change at all (KNOT-038).

// PresignTTL is how long a signed upload URL stays usable.
//
// Fifteen minutes is long enough for a 100 MiB video on a slow link and short
// enough that a leaked URL is worthless by the time anyone finds it. The API
// reports it to the client as `expires_in`, so a client can warn rather than
// discover it by failing.
const PresignTTL = 15 * time.Minute

// maxUploadFilenameLength bounds the client-declared filename. The name is only
// validated, never used in the object key (the key is a fresh UUID), so the limit
// exists purely to refuse absurd input early.
const maxUploadFilenameLength = 255

// maxUploadNameLength bounds the part of an object key after the story prefix:
// a UUID, the original suffix and an extension are under 60 bytes.
const maxUploadNameLength = 80

// PresignUploadInput is what a client asks for: where it intends to upload, and
// what it is uploading.
//
// Filename is the client's own name for the file. It is validated and then
// discarded — the object key is a fresh UUID, so two uploads of "photo.jpg" can
// never collide and the key cannot be guessed from the name.
type PresignUploadInput struct {
	// StoryID is the story the media will be attached to. Only that story's
	// author may upload media for it.
	StoryID string
	// UploaderID is the authenticated user. In the running server it comes from
	// the request, never from the body.
	UploaderID string
	// Filename is the client's name for the file.
	Filename string
	// ContentType is the media type the client declares. It must be in the
	// allowlist, and it becomes the object key's extension.
	ContentType string
}

// PresignedUpload is the answer: where to send the bytes, and what to report back
// afterwards.
type PresignedUpload struct {
	// UploadURL is the signed URL the client PUTs the bytes to.
	UploadURL string
	// ObjectKey is what the client reports to POST /stories/{id}/media.
	ObjectKey string
	// ExpiresIn is how many seconds the URL stays valid.
	ExpiresIn int
}

// PresignUpload validates an intended upload and signs a URL for it.
//
// The rules are the same ones the old multipart path applied — the story must
// exist, the caller must be its author, and the story must have room — plus one
// that only exists here: the declared content type must be in the allowlist,
// because it decides the object key's extension and there is no body to sniff.
//
// Failing here is the cheapest possible outcome: the client learns it may not
// upload before it spends a byte.
func (s *Service) PresignUpload(ctx context.Context, in PresignUploadInput) (PresignedUpload, error) {
	if !isUUID(in.StoryID) {
		return PresignedUpload{}, &ValidationError{Field: "story_id", Message: "must be a UUID"}
	}
	if !isUUID(in.UploaderID) {
		return PresignedUpload{}, &ValidationError{Field: "uploader_id", Message: "must be a UUID"}
	}
	if err := validateUploadFilename(in.Filename); err != nil {
		return PresignedUpload{}, err
	}

	_, extension, allowed := AllowedMediaType(TrimMimeType(in.ContentType))
	if !allowed {
		return PresignedUpload{}, ErrUnsupportedMediaType
	}

	if err := s.authorizeUpload(ctx, in.StoryID, in.UploaderID); err != nil {
		return PresignedUpload{}, err
	}

	name, err := newUUIDv4()
	if err != nil {
		return PresignedUpload{}, fmt.Errorf("storymedia: mint object key: %w", err)
	}

	// The key is the same one the multipart path minted, so the Rust worker
	// accepts it unchanged: it insists on the story's prefix and on _original, and
	// derives every derivative name from the part before it (KNOT-038).
	objectKey := StoryMediaKeyPrefix(in.StoryID) + name + originalSuffix + extension
	contentType := TrimMimeType(in.ContentType)

	url, err := s.objects.PresignPut(ctx, objectKey, contentType, PresignTTL)
	if err != nil {
		return PresignedUpload{}, fmt.Errorf("storymedia: sign upload url: %w", err)
	}

	s.logger.InfoContext(
		ctx,
		"media upload url signed",
		"story_id", in.StoryID,
		"object_key", objectKey,
		"content_type", contentType,
	)

	return PresignedUpload{
		UploadURL: url,
		ObjectKey: objectKey,
		ExpiresIn: int(PresignTTL.Seconds()),
	}, nil
}

// AttachUploadInput is a client reporting that its bytes are in the bucket.
type AttachUploadInput struct {
	// StoryID is the story the media belongs to.
	StoryID string
	// UploaderID is the authenticated user who received the signed URL.
	UploaderID string
	// ObjectKey is the key the presigned response carried.
	ObjectKey string
	// Source is camera or gallery. Empty means gallery, which is what a client
	// that does not report it means: the file was not captured in this session.
	Source MediaSource
	// Width, Height, and DurationMS are optional client-reported metadata.
	Width      *int
	Height     *int
	DurationMS *int
}

// AttachUpload records a media row for an object the client uploaded directly,
// then queues the job that produces its derivatives.
//
// The object is confirmed with a HEAD before anything is written: a URL that was
// signed but never used, or used and since deleted, must not leave a row pointing
// at nothing. There is deliberately no byte sniffing — the type was fixed when the
// URL was signed and the key's extension records it.
//
// Like CreateMedia, a failed write rolls the object back. An abandoned upload is
// already the orphan case this path can leave behind (a signed URL that is never
// reported), and a row that will never be processed is the worse of the two.
func (s *Service) AttachUpload(ctx context.Context, in AttachUploadInput) (StoryMedia, error) {
	if !isUUID(in.StoryID) {
		return StoryMedia{}, &ValidationError{Field: "story_id", Message: "must be a UUID"}
	}
	if !isUUID(in.UploaderID) {
		return StoryMedia{}, &ValidationError{Field: "uploader_id", Message: "must be a UUID"}
	}

	upload, err := parseUploadedObjectKey(in.StoryID, in.ObjectKey)
	if err != nil {
		return StoryMedia{}, err
	}

	source := in.Source
	if source == "" {
		source = MediaSourceGallery
	}
	if !source.Valid() {
		return StoryMedia{}, &ValidationError{
			Field:   "source",
			Message: fmt.Sprintf("must be one of %s, %s", MediaSourceCamera, MediaSourceGallery),
		}
	}

	for _, field := range []struct {
		name  string
		value *int
	}{
		{"width", in.Width},
		{"height", in.Height},
		{"duration_ms", in.DurationMS},
	} {
		if field.value != nil && *field.value < 0 {
			return StoryMedia{}, &ValidationError{Field: field.name, Message: "must not be negative"}
		}
	}

	if err := s.authorizeUpload(ctx, in.StoryID, in.UploaderID); err != nil {
		return StoryMedia{}, err
	}

	exists, err := s.objects.Exists(ctx, upload.Key)
	if err != nil {
		return StoryMedia{}, fmt.Errorf("storymedia: check uploaded object: %w", err)
	}
	if !exists {
		return StoryMedia{}, ErrUploadNotFound
	}

	// An image's thumbnail key is recorded before the file exists, exactly as on
	// the multipart path, so the 202 can carry a stable thumbnail_url (KNOT-038).
	var thumbnailKey *string
	if upload.MediaType == MediaTypeImage {
		key := upload.BaseKey + thumbnailSuffix + webpExtension
		thumbnailKey = &key
	}

	stored, err := s.store.CreateMedia(ctx, StoryMedia{
		StoryID:      in.StoryID,
		UploaderID:   in.UploaderID,
		StorageKey:   upload.Key,
		ThumbnailKey: thumbnailKey,
		MediaType:    upload.MediaType,
		MimeType:     upload.MimeType,
		Source:       source,
		Width:        in.Width,
		Height:       in.Height,
		DurationMS:   in.DurationMS,
		// The size belongs to the derivative the worker will produce; the upload's
		// own size is not known here and would describe a file nobody is served.
		SizeBytes:    0,
		Status:       StatusProcessing,
		DisplayOrder: -1,
	})
	if err != nil {
		s.discardObject(ctx, upload.Key)
		return StoryMedia{}, fmt.Errorf("storymedia: create media: %w", err)
	}

	if err := s.jobs.Publish(ctx, Job{
		MediaID:   stored.ID,
		StoryID:   in.StoryID,
		ObjectKey: upload.Key,
		MediaType: upload.MediaType,
	}); err != nil {
		s.discardObject(ctx, upload.Key)
		if deleteErr := s.store.DeleteMedia(ctx, stored.ID); deleteErr != nil {
			s.logger.ErrorContext(
				ctx,
				"could not roll back an unqueued media row",
				"media_id", stored.ID,
				"error", deleteErr.Error(),
			)
		}
		return StoryMedia{}, fmt.Errorf("storymedia: enqueue media job: %w", err)
	}

	s.logger.InfoContext(
		ctx,
		"media upload attached",
		"media_id", stored.ID,
		"media_type", string(upload.MediaType),
		"object_key", upload.Key,
	)

	return stored, nil
}

// authorizeUpload applies the rules that decide whether a user may add media to a
// story at all: the story exists, they are its author, and it has room.
//
// The cap check here is the early one; the store re-checks it under the story row
// lock, which is what makes the cap hold against concurrent uploads
// (KNOT-ADR-054).
func (s *Service) authorizeUpload(ctx context.Context, storyID, uploaderID string) error {
	author, err := s.store.StoryAuthor(ctx, storyID)
	if err != nil {
		if errors.Is(err, ErrStoryNotFound) {
			return ErrStoryNotFound
		}
		return fmt.Errorf("storymedia: resolve story: %w", err)
	}
	if author != uploaderID {
		return ErrForbidden
	}

	count, err := s.store.CountMedia(ctx, storyID)
	if err != nil {
		return fmt.Errorf("storymedia: count media: %w", err)
	}
	if count >= MaxMediaPerStory {
		return ErrMediaLimit
	}

	return nil
}

// uploadedObject is a parsed object key: the bytes a client uploaded directly.
type uploadedObject struct {
	// Key is the whole object key, as the client reported it.
	Key string
	// BaseKey is the key without the original suffix and extension. Every
	// derivative name is built from it, which is the scheme the worker uses.
	BaseKey string
	// MediaType and MimeType are derived from the key's extension.
	MediaType MediaType
	MimeType  string
}

// parseUploadedObjectKey validates that key names an ORIGINAL upload for storyID
// and reads the media type back from it.
//
// It is the security boundary of the direct-upload path: the key arrives from the
// client, so it must not be able to name another story's object, an arbitrary key
// in the bucket, or a derivative file. The shape it accepts is exactly the one
// PresignUpload mints — `story-media/{story}/{uuid}_original.{ext}` — and anything
// else is refused rather than reinterpreted.
func parseUploadedObjectKey(storyID, key string) (uploadedObject, error) {
	prefix := StoryMediaKeyPrefix(storyID)
	if !strings.HasPrefix(key, prefix) {
		return uploadedObject{}, &ValidationError{
			Field:   "object_key",
			Message: "must name an upload for this story",
		}
	}

	remainder := strings.TrimPrefix(key, prefix)
	if remainder == "" || len(remainder) > maxUploadNameLength {
		return uploadedObject{}, &ValidationError{Field: "object_key", Message: "must name an uploaded file"}
	}
	// One path segment only, and nothing that could be read as a query or a
	// fragment: the key is a name in the bucket, not a URL.
	if strings.ContainsAny(remainder, "/\\?#") {
		return uploadedObject{}, &ValidationError{Field: "object_key", Message: "must name an uploaded file"}
	}

	base, extension, found := strings.Cut(remainder, ".")
	if !found || extension == "" {
		return uploadedObject{}, &ValidationError{Field: "object_key", Message: "must name an uploaded file"}
	}
	if !strings.HasSuffix(base, originalSuffix) {
		return uploadedObject{}, &ValidationError{Field: "object_key", Message: "must name an uploaded file"}
	}
	name := strings.TrimSuffix(base, originalSuffix)
	if !isUUID(name) {
		return uploadedObject{}, &ValidationError{Field: "object_key", Message: "must name an uploaded file"}
	}

	mimeType, mediaType, allowed := contentTypeForExtension("." + extension)
	if !allowed {
		return uploadedObject{}, ErrUnsupportedMediaType
	}

	return uploadedObject{
		Key:       key,
		BaseKey:   prefix + name,
		MediaType: mediaType,
		MimeType:  mimeType,
	}, nil
}

// validateUploadFilename checks the client's declared filename.
//
// The name never reaches the object key, so this is about refusing input that is
// obviously not a filename — empty, path-shaped, control characters, or absurdly
// long — rather than about sanitising a value that is used.
func validateUploadFilename(filename string) error {
	name := strings.TrimSpace(filename)
	if name == "" {
		return &ValidationError{Field: "filename", Message: "is required"}
	}
	if len(name) > maxUploadFilenameLength {
		return &ValidationError{
			Field:   "filename",
			Message: fmt.Sprintf("must be at most %d bytes", maxUploadFilenameLength),
		}
	}
	if strings.ContainsAny(name, `/\`) {
		return &ValidationError{Field: "filename", Message: "must not contain a path"}
	}
	for _, r := range name {
		if unicode.IsControl(r) {
			return &ValidationError{Field: "filename", Message: "must not contain control characters"}
		}
	}

	return nil
}
