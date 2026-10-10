package storymedia

import (
	"context"
	"crypto/rand"
	"errors"
	"fmt"
	"strconv"
	"strings"
)

// CreateMedia validates the input, uploads the bytes to the object store, and
// records the media row.
//
// Only the story's author may attach media in the MVP, so an uploader who is not
// the story's author is rejected with ErrForbidden. The story is resolved first,
// so a missing story is ErrStoryNotFound rather than an authorization failure.
func (s *Service) CreateMedia(ctx context.Context, in CreateMediaInput) (StoryMedia, error) {
	mediaType, extension, err := validateCreateMedia(in)
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

	// Early cap check: refuse before uploading bytes the store would reject, so a
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

	key, err := newMediaKey(in.StoryID, extension)
	if err != nil {
		return StoryMedia{}, err
	}

	if err := s.objects.Put(ctx, key, in.Body, in.MimeType); err != nil {
		return StoryMedia{}, fmt.Errorf("storymedia: store object: %w", err)
	}

	// A negative DisplayOrder asks the store to append the item after the story's
	// current last one; a non-negative value places it explicitly.
	order := -1
	if in.DisplayOrder != nil {
		order = *in.DisplayOrder
	}

	stored, err := s.store.CreateMedia(ctx, StoryMedia{
		StoryID:      in.StoryID,
		UploaderID:   in.UploaderID,
		StorageKey:   key,
		MediaType:    mediaType,
		MimeType:     in.MimeType,
		Source:       in.Source,
		Width:        in.Width,
		Height:       in.Height,
		DurationMS:   in.DurationMS,
		SizeBytes:    in.SizeBytes,
		DisplayOrder: order,
	})
	if err != nil {
		// The row failed, so the object is now orphaned rather than referenced.
		// Remove it best-effort so a failed upload does not leak storage.
		s.discardObject(ctx, key)
		return StoryMedia{}, fmt.Errorf("storymedia: create media: %w", err)
	}

	return stored, nil
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

	// The object is removed first and best-effort: a storage failure leaves an
	// orphaned object, which is preferable to a row that still points at a
	// deleted object. The deletion is logged, not returned, because the client
	// cannot act on it and the row is being removed regardless.
	if err := s.objects.Delete(ctx, media.StorageKey); err != nil {
		s.logger.WarnContext(
			ctx,
			"could not delete media object",
			"key", media.StorageKey,
			"error", err.Error(),
		)
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
// type and the object-key extension.
func validateCreateMedia(in CreateMediaInput) (MediaType, string, error) {
	if !isUUID(in.StoryID) {
		return "", "", &ValidationError{Field: "story_id", Message: "must be a UUID"}
	}
	if !isUUID(in.UploaderID) {
		return "", "", &ValidationError{Field: "uploader_id", Message: "must be a UUID"}
	}

	mediaType, extension, allowed := AllowedMediaType(TrimMimeType(in.MimeType))
	if !allowed {
		return "", "", ErrUnsupportedMediaType
	}
	if !in.Source.Valid() {
		return "", "", &ValidationError{
			Field:   "source",
			Message: fmt.Sprintf("must be one of %s, %s", MediaSourceCamera, MediaSourceGallery),
		}
	}

	if in.SizeBytes <= 0 {
		return "", "", &ValidationError{Field: "file", Message: "is empty"}
	}
	if limit := MaxBytesFor(mediaType); in.SizeBytes > limit {
		return "", "", ErrTooLarge
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
			return "", "", &ValidationError{Field: field.name, Message: "must not be negative"}
		}
	}
	if in.DisplayOrder != nil && *in.DisplayOrder < 0 {
		return "", "", &ValidationError{Field: "display_order", Message: "must not be negative"}
	}

	return mediaType, extension, nil
}

// newMediaKey mints the object key for a new piece of media.
//
// The name is a fresh UUID rather than a fixed name, so uploading a new file
// never overwrites the bytes an in-flight response is still serving, and the key
// cannot be derived from the public path.
func newMediaKey(storyID, extension string) (string, error) {
	suffix, err := newUUIDv4()
	if err != nil {
		return "", err
	}
	return StoryMediaKeyPrefix(storyID) + suffix + extension, nil
}

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
