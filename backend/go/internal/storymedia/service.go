package storymedia

import (
	"bytes"
	"context"
	"crypto/rand"
	"errors"
	"fmt"
	"io"
	"os"
	"strconv"
	"strings"
	"time"
)

// CreateMedia validates the input, records the media row, and queues the work that
// produces its derivatives.
//
// Nothing is stored in the bucket by the time this returns: the upload is spooled
// to a temporary file and a background worker turns it into the display rendition
// and the thumbnail (or the stream rendition) before recording them. The row is
// written with StatusProcessing, so a reader that arrives too early is told to
// wait rather than shown a broken image (KNOT-037-fix7).
//
// Only the story's author may attach media in the MVP, so an uploader who is not
// the story's author is rejected with ErrForbidden. The story is resolved first,
// so a missing story is ErrStoryNotFound rather than an authorization failure.
func (s *Service) CreateMedia(ctx context.Context, in CreateMediaInput) (StoryMedia, error) {
	mediaType, err := validateCreateMedia(in)
	if err != nil {
		return StoryMedia{}, err
	}

	author, err := s.store.StoryAuthor(ctx, in.StoryID)
	if err != nil {
		if errors.Is(err, ErrStoryNotFound) {
			return StoryMedia{}, ErrStoryNotFound
		}
		return StoryMedia{}, fmt.Errorf("storymedia: resolve story: %w", err)
	}
	if author != in.UploaderID {
		return StoryMedia{}, ErrForbidden
	}

	// Early cap check: refuse before spooling bytes the store would reject, so a
	// full story never costs the user an upload. The store re-checks the count
	// under the story row lock when it inserts, which is what makes the cap
	// race-safe (KNOT-ADR-054).
	count, err := s.store.CountMedia(ctx, in.StoryID)
	if err != nil {
		return StoryMedia{}, fmt.Errorf("storymedia: count media: %w", err)
	}
	if count >= MaxMediaPerStory {
		return StoryMedia{}, ErrMediaLimit
	}

	// The upload is spooled to disk rather than held in memory: a queued job may
	// wait for a semaphore slot, and 100 MiB of video per waiting upload is memory
	// the request path cannot spare (KNOT-037-fix7).
	tempPath, err := spoolUpload(in)
	if err != nil {
		return StoryMedia{}, err
	}

	name, err := newUUIDv4()
	if err != nil {
		removeTempFile(tempPath)
		return StoryMedia{}, fmt.Errorf("storymedia: mint object key: %w", err)
	}

	base := StoryMediaKeyPrefix(in.StoryID) + name
	keys := objectKeysFor(base, mediaType)

	// A negative DisplayOrder asks the store to append the item after the story's
	// current last one; a non-negative value places it explicitly.
	order := -1
	if in.DisplayOrder != nil {
		order = *in.DisplayOrder
	}

	stored, err := s.store.CreateMedia(ctx, StoryMedia{
		StoryID:      in.StoryID,
		UploaderID:   in.UploaderID,
		StorageKey:   keys.primary,
		MediaType:    mediaType,
		MimeType:     in.MimeType,
		Source:       in.Source,
		Width:        in.Width,
		Height:       in.Height,
		DurationMS:   in.DurationMS,
		SizeBytes:    in.SizeBytes,
		ThumbnailKey: keys.thumbnail,
		Status:       StatusProcessing,
		DisplayOrder: order,
	})
	if err != nil {
		removeTempFile(tempPath)
		return StoryMedia{}, fmt.Errorf("storymedia: create media: %w", err)
	}

	s.enqueue(processJob{
		MediaID:   stored.ID,
		MediaType: mediaType,
		TempPath:  tempPath,
		BaseKey:   base,
	})

	return stored, nil
}

// mediaObjectKeys is the set of object keys one upload writes.
type mediaObjectKeys struct {
	// primary is the row's StorageKey: the display AVIF or the stream MP4.
	primary string
	// thumbnail is the feed thumbnail's key, or nil for a video (which autoplays
	// and therefore has no cover).
	thumbnail *string
}

// objectKeysFor names the objects one upload of mediaType writes.
//
// It is the single place the suffixes and extensions of a new upload are chosen,
// so the key the row stores and the key the worker writes cannot drift
// (KNOT-037-fix7).
func objectKeysFor(base string, mediaType MediaType) mediaObjectKeys {
	if mediaType == MediaTypeVideo {
		return mediaObjectKeys{primary: base + streamSuffix + mp4Extension}
	}

	thumbnail := base + thumbnailSuffix + webpExtension
	return mediaObjectKeys{
		primary:   base + displaySuffix + avifExtension,
		thumbnail: &thumbnail,
	}
}

// spoolUpload writes the request body to a temporary file and returns its path.
//
// The declared size has already been validated against the media type's limit, so
// the copy is bounded. One byte more than declared is read so a caller that
// under-reported its size is refused rather than silently truncated.
func spoolUpload(in CreateMediaInput) (string, error) {
	file, err := os.CreateTemp("", "knot-upload-*")
	if err != nil {
		return "", fmt.Errorf("storymedia: spool upload: %w", err)
	}

	path := file.Name()
	written, copyErr := io.Copy(file, io.LimitReader(in.Body, in.SizeBytes+1))
	closeErr := file.Close()

	switch {
	case copyErr != nil:
		removeTempFile(path)
		return "", fmt.Errorf("storymedia: spool upload: %w", copyErr)
	case closeErr != nil:
		removeTempFile(path)
		return "", fmt.Errorf("storymedia: spool upload: %w", closeErr)
	case written != in.SizeBytes:
		removeTempFile(path)
		return "", &ValidationError{Field: "file", Message: "does not match its declared size"}
	}

	return path, nil
}

// enqueue schedules one upload's derivatives.
//
// The goroutine takes a semaphore slot before it starts, so at most
// MaxConcurrentProcessing jobs run at once while the rest wait in order. The
// context is deliberately NOT the request's: the response has been sent and the
// work must outlive it (KNOT-037-fix7).
func (s *Service) enqueue(job processJob) {
	s.pending.Add(1)

	go func() {
		defer s.pending.Done()

		s.semaphore <- struct{}{}
		defer func() { <-s.semaphore }()

		s.runJob(context.Background(), job)
	}()
}

// runJob produces one upload's derivatives and records the outcome.
//
// The work is retried up to `retryAttempts` times, `retryDelay` apart: the common
// failure is the object store dropping a connection on an unstable uplink, which a
// second attempt usually survives (KNOT-037-fix12). Each attempt restarts the whole
// pipeline — the spooled upload is still on disk, and a partially uploaded set is
// discarded by the pipeline itself — so a retry never leaves half an item behind.
//
// A failure never loses the row: after the last attempt it is marked failed, the
// reason is logged, and the row stays visible (with no bytes) rather than pretending
// the upload never happened. The spooled file is removed either way
// (KNOT-037-fix7).
func (s *Service) runJob(ctx context.Context, job processJob) {
	defer removeTempFile(job.TempPath)

	attempts := s.retryAttempts
	if attempts < 1 {
		attempts = 1
	}

	var (
		size int64
		err  error
	)
	for attempt := 1; attempt <= attempts; attempt++ {
		size, err = s.processMedia(ctx, job)
		if err == nil {
			break
		}
		if attempt == attempts {
			break
		}

		s.logger.WarnContext(
			ctx,
			"media processing attempt failed; retrying",
			"media_id", job.MediaID,
			"media_type", string(job.MediaType),
			"attempt", attempt,
			"attempts", attempts,
			"retry_in", s.retryDelay.String(),
			"error", err.Error(),
		)
		if !sleepBeforeRetry(ctx, s.retryDelay) {
			break
		}
	}

	if err != nil {
		s.logger.ErrorContext(
			ctx,
			"media processing failed",
			"media_id", job.MediaID,
			"media_type", string(job.MediaType),
			"error", err.Error(),
		)
		s.setStatus(ctx, job.MediaID, StatusFailed, 0)
		return
	}

	s.setStatus(ctx, job.MediaID, StatusReady, size)
}

// sleepBeforeRetry waits for d, and reports false when ctx ended first. A
// cancelled context means the process is shutting down, so the job stops instead
// of sleeping through it.
func sleepBeforeRetry(ctx context.Context, d time.Duration) bool {
	if d <= 0 {
		return ctx.Err() == nil
	}

	timer := time.NewTimer(d)
	defer timer.Stop()

	select {
	case <-ctx.Done():
		return false
	case <-timer.C:
		return true
	}
}

// setStatus records a job's outcome. A failure to write it is logged rather than
// retried: the row is already processing, so the TTL of a stuck row is an
// operator's problem, not the request's.
func (s *Service) setStatus(ctx context.Context, mediaID string, status Status, sizeBytes int64) {
	if err := s.store.SetStatus(ctx, mediaID, status, sizeBytes); err != nil {
		s.logger.ErrorContext(
			ctx,
			"could not record media status",
			"media_id", mediaID,
			"status", string(status),
			"error", err.Error(),
		)
	}
}

// transcodeAndUpload is the real processor: it turns the spooled upload into its
// derivatives and stores them.
func (s *Service) transcodeAndUpload(ctx context.Context, job processJob) (int64, error) {
	switch job.MediaType {
	case MediaTypeImage:
		return s.uploadImageDerivatives(ctx, job)
	case MediaTypeVideo:
		return s.uploadStreamRendition(ctx, job)
	default:
		return 0, fmt.Errorf("storymedia: unknown media type %q", job.MediaType)
	}
}

// uploadImageDerivatives encodes and stores an image's display and thumbnail
// renditions.
//
// Both must land: the display is what the story view shows and the thumbnail is
// what the feed shows, so a row missing either is not a usable photo. Anything
// already uploaded is removed when the second one fails, or the bucket would keep
// half an item (KNOT-037-fix7).
func (s *Service) uploadImageDerivatives(ctx context.Context, job processJob) (int64, error) {
	data, err := os.ReadFile(job.TempPath)
	if err != nil {
		return 0, fmt.Errorf("read spooled upload: %w", err)
	}

	renditions, err := ImageDerivatives(data)
	if err != nil {
		return 0, err
	}
	if len(renditions) != 2 {
		return 0, fmt.Errorf("storymedia: expected a display and a thumbnail, got %d renditions", len(renditions))
	}

	uploaded := make([]string, 0, len(renditions))
	var displaySize int64
	for _, rendition := range renditions {
		key := job.BaseKey + rendition.Suffix + rendition.Extension
		if err := s.objects.Put(ctx, key, bytes.NewReader(rendition.Body), rendition.ContentType); err != nil {
			s.discardObjects(ctx, uploaded)
			return 0, fmt.Errorf("store %s: %w", rendition.Variant, err)
		}
		uploaded = append(uploaded, key)
		if rendition.Variant == VariantDisplay {
			displaySize = int64(len(rendition.Body))
		}
	}

	return displaySize, nil
}

// uploadStreamRendition transcodes an uploaded video and stores the result.
//
// No cover frame is produced: the feed autoplays the stream, so a still would be a
// second copy of the same content (KNOT-037-fix7).
func (s *Service) uploadStreamRendition(ctx context.Context, job processJob) (int64, error) {
	output := job.TempPath + mp4Extension
	defer removeTempFile(output)

	if err := TranscodeToStream(ctx, job.TempPath, output); err != nil {
		return 0, err
	}

	info, err := os.Stat(output)
	if err != nil {
		return 0, fmt.Errorf("stat transcoded stream: %w", err)
	}

	file, err := os.Open(output)
	if err != nil {
		return 0, fmt.Errorf("open transcoded stream: %w", err)
	}
	defer func() { _ = file.Close() }()

	key := job.BaseKey + streamSuffix + mp4Extension
	if err := s.objects.Put(ctx, key, file, videoContentType); err != nil {
		return 0, fmt.Errorf("store stream: %w", err)
	}

	return info.Size(), nil
}

// discardObjects removes objects an abandoned job already wrote, best-effort.
func (s *Service) discardObjects(ctx context.Context, keys []string) {
	for _, key := range keys {
		s.discardObject(ctx, key)
	}
}

// removeTempFile removes a spooled file without failing the job: the work is done
// (or given up), and a leftover temporary file is housekeeping, not an error.
func removeTempFile(path string) {
	if path == "" {
		return
	}
	_ = os.Remove(path)
}

// ListMedia returns every media item of a story, in display order.
//
// A story that does not exist is ErrStoryNotFound, mirroring the language tree:
// listing the media of a story that is not there is a 404, not an empty list.
func (s *Service) ListMedia(ctx context.Context, storyID string) ([]StoryMedia, error) {
	if !isUUID(storyID) {
		return nil, ErrStoryNotFound
	}

	if _, err := s.store.StoryAuthor(ctx, storyID); err != nil {
		if errors.Is(err, ErrStoryNotFound) {
			return nil, ErrStoryNotFound
		}
		return nil, fmt.Errorf("storymedia: resolve story: %w", err)
	}

	page, err := s.store.ListMedia(ctx, storyID)
	if err != nil {
		return nil, fmt.Errorf("storymedia: list media: %w", err)
	}
	if page == nil {
		// Emit [] rather than null so a client never special-cases an empty list.
		page = []StoryMedia{}
	}

	return page, nil
}

// FirstMedia returns each story's first media item, keyed by story id, for the
// listed stories that have any.
//
// It exists so the feed can show a thumbnail without a query per story. Unlike
// ListMedia it does not check that each story exists: it is an enrichment read,
// and a story that has no media simply has no entry.
func (s *Service) FirstMedia(ctx context.Context, storyIDs []string) (map[string]StoryMedia, error) {
	first, err := s.store.FirstMedia(ctx, storyIDs)
	if err != nil {
		return nil, fmt.Errorf("storymedia: first media: %w", err)
	}
	return first, nil
}

// GetMedia returns one media row.
//
// An id that is not canonical UUID text is reported as ErrNotFound rather than
// as a validation error: the resource genuinely does not exist, and answering
// 404 keeps the id column's index usable instead of casting it to text in SQL.
func (s *Service) GetMedia(ctx context.Context, id string) (StoryMedia, error) {
	if !isUUID(id) {
		return StoryMedia{}, ErrNotFound
	}

	media, err := s.store.GetMedia(ctx, id)
	if err != nil {
		if errors.Is(err, ErrNotFound) {
			return StoryMedia{}, ErrNotFound
		}
		return StoryMedia{}, fmt.Errorf("storymedia: get media: %w", err)
	}

	return media, nil
}

// DeleteMedia removes a media row and its object.
//
// Only the story's author or the item's uploader may delete it; anyone else is
// ErrForbidden. The row is deleted only after the caller has been authorized, so
// a rejected request never touches storage.
func (s *Service) DeleteMedia(ctx context.Context, id, byUserID string) error {
	media, err := s.GetMedia(ctx, id)
	if err != nil {
		return err
	}

	author, err := s.store.StoryAuthor(ctx, media.StoryID)
	if err != nil {
		if errors.Is(err, ErrStoryNotFound) {
			return ErrNotFound
		}
		return fmt.Errorf("storymedia: resolve story: %w", err)
	}
	if byUserID != author && byUserID != media.UploaderID {
		return ErrForbidden
	}

	// The objects are removed first and best-effort: a storage failure leaves an
	// orphaned object, which is preferable to a row that still points at a deleted
	// object. The deletion is logged, not returned, because the client cannot act
	// on it and the row is being removed regardless. Every rendition is removed
	// with the original, or the bucket would keep the bytes of a deleted item
	// (KNOT-037).
	for _, key := range append([]string{media.StorageKey}, media.derivativeKeys()...) {
		if err := s.objects.Delete(ctx, key); err != nil {
			s.logger.WarnContext(
				ctx,
				"could not delete media object",
				"key", key,
				"error", err.Error(),
			)
		}
	}

	if err := s.store.DeleteMedia(ctx, id); err != nil {
		return fmt.Errorf("storymedia: delete media: %w", err)
	}

	return nil
}

// discardObject removes an object without failing the request: the upload has
// already failed, so a deletion problem is logged and left for the operator.
func (s *Service) discardObject(ctx context.Context, key string) {
	if key == "" {
		return
	}
	if err := s.objects.Delete(ctx, key); err != nil {
		s.logger.WarnContext(
			ctx,
			"could not roll back media object",
			"key", key,
			"error", err.Error(),
		)
	}
}

// validateCreateMedia applies every input rule and returns the resolved media
// type.
func validateCreateMedia(in CreateMediaInput) (MediaType, error) {
	if !isUUID(in.StoryID) {
		return "", &ValidationError{Field: "story_id", Message: "must be a UUID"}
	}
	if !isUUID(in.UploaderID) {
		return "", &ValidationError{Field: "uploader_id", Message: "must be a UUID"}
	}

	mediaType, _, allowed := AllowedMediaType(TrimMimeType(in.MimeType))
	if !allowed {
		return "", ErrUnsupportedMediaType
	}
	if !in.Source.Valid() {
		return "", &ValidationError{
			Field:   "source",
			Message: fmt.Sprintf("must be one of %s, %s", MediaSourceCamera, MediaSourceGallery),
		}
	}

	if in.SizeBytes <= 0 {
		return "", &ValidationError{Field: "file", Message: "is empty"}
	}
	if limit := MaxBytesFor(mediaType); in.SizeBytes > limit {
		return "", ErrTooLarge
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
			return "", &ValidationError{Field: field.name, Message: "must not be negative"}
		}
	}
	if in.DisplayOrder != nil && *in.DisplayOrder < 0 {
		return "", &ValidationError{Field: "display_order", Message: "must not be negative"}
	}

	return mediaType, nil
}

// newMediaKey mints the object key for a new piece of media.
//
// The name is a fresh UUID rather than a fixed name, so uploading a new file
// never overwrites the bytes an in-flight response is still serving, and the key
// cannot be derived from the public path.
// newUUIDv4 returns a random RFC 4122 version 4 UUID.
//
// It is written out here rather than pulled from a dependency: a key needs
// uniqueness within one story's prefix, and 122 random bits are enough for that.
// It duplicates the helper in internal/httpapi for the same reason the cursor is
// duplicated (KNOT-ADR-010).
func newUUIDv4() (string, error) {
	var raw [16]byte
	if _, err := rand.Read(raw[:]); err != nil {
		return "", fmt.Errorf("storymedia: generate object id: %w", err)
	}

	raw[6] = (raw[6] & 0x0f) | 0x40 // version 4
	raw[8] = (raw[8] & 0x3f) | 0x80 // RFC 4122 variant

	return fmt.Sprintf("%x-%x-%x-%x-%x", raw[0:4], raw[4:6], raw[6:8], raw[8:10], raw[10:16]), nil
}

// ParseRange parses a single-range HTTP "Range" header of the form
// "bytes=start-end" against an object of the given size.
//
// It accepts "bytes=start-end", "bytes=start-", and "bytes=-suffix". It returns
// ok=false when the header is absent, is not a single bytes range, is
// syntactically invalid, or is unsatisfiable for size; the caller then serves the
// whole object. Multi-range requests ("bytes=0-9,20-29") are treated as
// unsatisfiable, because answering them needs multipart/byteranges, which this
// API does not produce.
func ParseRange(header string, size int64) (start, end int64, ok bool) {
	if size <= 0 || header == "" {
		return 0, 0, false
	}

	const prefix = "bytes="
	if !strings.HasPrefix(header, prefix) {
		return 0, 0, false
	}
	spec := strings.TrimSpace(strings.TrimPrefix(header, prefix))
	if spec == "" || strings.Contains(spec, ",") {
		return 0, 0, false
	}

	startText, endText, found := strings.Cut(spec, "-")
	if !found {
		return 0, 0, false
	}
	startText = strings.TrimSpace(startText)
	endText = strings.TrimSpace(endText)

	switch {
	case startText == "":
		// Suffix range: the last N bytes. "-0" is unsatisfiable.
		suffix, err := strconv.ParseInt(endText, 10, 64)
		if err != nil || suffix <= 0 {
			return 0, 0, false
		}
		if suffix > size {
			suffix = size
		}
		return size - suffix, size - 1, true
	case endText == "":
		first, err := strconv.ParseInt(startText, 10, 64)
		if err != nil || first < 0 || first >= size {
			return 0, 0, false
		}
		return first, size - 1, true
	default:
		first, firstErr := strconv.ParseInt(startText, 10, 64)
		last, lastErr := strconv.ParseInt(endText, 10, 64)
		if firstErr != nil || lastErr != nil || first < 0 || last < first || first >= size {
			return 0, 0, false
		}
		if last >= size {
			last = size - 1
		}
		return first, last, true
	}
}

// isUUID reports whether s is a canonical 8-4-4-4-12 hexadecimal UUID string.
func isUUID(s string) bool {
	if len(s) != 36 {
		return false
	}
	for i := 0; i < len(s); i++ {
		switch i {
		case 8, 13, 18, 23:
			if s[i] != '-' {
				return false
			}
		default:
			if !isHexDigit(s[i]) {
				return false
			}
		}
	}
	return true
}

// isHexDigit reports whether b is an ASCII hexadecimal digit.
func isHexDigit(b byte) bool {
	switch {
	case b >= '0' && b <= '9':
		return true
	case b >= 'a' && b <= 'f':
		return true
	case b >= 'A' && b <= 'F':
		return true
	default:
		return false
	}
}
