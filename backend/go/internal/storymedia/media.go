// Package storymedia implements the Knot story-media domain: the images and
// videos attached to a story, how they are stored, and how they are read back.
//
// The package is deliberately layered, mirroring internal/stories:
//
//	media.go          domain types, limits, errors, and the store contract
//	cursor.go         opaque pagination cursors (duplicated per KNOT-ADR-010)
//	service.go        business rules (CreateMedia, ListMedia, DeleteMedia, ...)
//	postgres_store.go the PostgreSQL implementation of the store contract
//
// Bytes live in the object store, not in PostgreSQL: this package depends on the
// narrow storage.Storage contract for the write and read of every object, so the
// same code runs against MinIO locally and a managed S3-compatible service in
// production (KNOT-ADR-028, KNOT-ADR-029). A row records the object's key and
// the metadata a client needs to render it; the key itself never leaves the
// server.
//
// Nothing in this package knows about HTTP, JSON, or SQL types. Ids are plain
// strings holding canonical UUID text, the convention the rest of the backend
// uses (KNOT-ADR-010).
package storymedia

import (
	"bytes"
	"context"
	"errors"
	"fmt"
	"io"
	"net/http"
	"strings"
	"time"

	"github.com/knot/backend/internal/storage"
)

// Sentinel errors returned by the service and store layers. Handlers map these
// onto HTTP status codes; they are never returned to clients verbatim.
var (
	// ErrNotFound is returned when no media matches the requested id, or when
	// its object is missing from the object store.
	ErrNotFound = errors.New("storymedia: media not found")
	// ErrStoryNotFound is returned when the story a piece of media would attach
	// to does not exist.
	ErrStoryNotFound = errors.New("storymedia: story not found")
	// ErrForbidden is returned when the authenticated user may not attach media
	// to a story, or may not delete a piece of media.
	ErrForbidden = errors.New("storymedia: not permitted")
	// ErrTooLarge is returned when an upload exceeds the limit for its media type.
	ErrTooLarge = errors.New("storymedia: media too large")
	// ErrUnsupportedMediaType is returned when an upload's sniffed type is not in
	// the allowlist for images or videos.
	ErrUnsupportedMediaType = errors.New("storymedia: unsupported media type")
	// ErrValidation is returned for rejected input. Every validation failure in
	// this package also carries a *ValidationError naming the offending field, so
	// errors.Is(err, ErrValidation) and errors.As(err, &validation) are both true.
	ErrValidation = errors.New("storymedia: invalid input")
	// ErrMediaLimit is returned when a story already holds MaxMediaPerStory items
	// and another would exceed the cap. The handler maps it to a 400
	// validation_error carrying MediaLimitMessage (KNOT-ADR-054).
	ErrMediaLimit = errors.New("storymedia: story media limit reached")
	// ErrProcessing is returned when a row's derivatives are still being generated
	// by the background worker, so there are no bytes to serve yet. The handler
	// answers 202 Accepted for it (KNOT-037-fix7).
	ErrProcessing = errors.New("storymedia: media is still processing")
	// ErrProcessingFailed is returned when the background worker could not produce
	// the derivatives. The row keeps the reason in the log, not in the response.
	ErrProcessingFailed = errors.New("storymedia: media processing failed")
	// ErrUploadNotFound is returned when a client reports an object key that does
	// not name a file in the bucket (KNOT-040). It is a client error, not a
	// missing media row: the upload either expired, never happened, or was
	// mistyped, and no row exists to answer 404 for.
	ErrUploadNotFound = errors.New("storymedia: uploaded object not found")
)

// ValidationError describes a rejected field on a request. Handlers expose the
// Field and Message so clients can highlight the offending input.
type ValidationError struct {
	// Field is the request field that failed validation.
	Field string
	// Message explains the failure in a client-safe way. It never contains
	// secrets or internal detail.
	Message string
}

// Error implements the error interface.
func (e *ValidationError) Error() string {
	return fmt.Sprintf("storymedia: invalid %s: %s", e.Field, e.Message)
}

// Unwrap makes every ValidationError satisfy errors.Is(err, ErrValidation).
func (e *ValidationError) Unwrap() error { return ErrValidation }

// MediaType is the coarse kind of a piece of media. It is a closed set because
// the client renders an image and a video differently, and the CHECK constraint
// in 0008_story_media pins the same two values in the database.
type MediaType string

// The supported media types.
const (
	// MediaTypeImage is a still image.
	MediaTypeImage MediaType = "image"
	// MediaTypeVideo is a moving picture with or without sound.
	MediaTypeVideo MediaType = "video"
)

// Valid reports whether t is one of the supported media types.
func (t MediaType) Valid() bool {
	switch t {
	case MediaTypeImage, MediaTypeVideo:
		return true
	default:
		return false
	}
}

// MediaSource records how a piece of media was obtained. It is the feature this
// task exists for: a photo taken with the device camera is marked differently
// from one chosen out of the gallery, so the UI can show a capture badge.
type MediaSource string

// The supported media sources.
const (
	// MediaSourceCamera marks media captured with the device camera.
	MediaSourceCamera MediaSource = "camera"
	// MediaSourceGallery marks media chosen from the device's photo library.
	MediaSourceGallery MediaSource = "gallery"
)

// Valid reports whether s is one of the supported media sources.
func (s MediaSource) Valid() bool {
	switch s {
	case MediaSourceCamera, MediaSourceGallery:
		return true
	default:
		return false
	}
}

// Status says where an item's derivatives are in the pipeline (KNOT-037-fix7).
//
// The upload request only records the row; a background worker produces the
// objects and moves the row from processing to ready (or failed). A reader can
// therefore never mistake "not generated yet" for "there is nothing here", and
// the CHECK constraint in migration 0026 pins the same three values.
type Status string

// The pipeline states.
const (
	// StatusProcessing means the row exists but its derivatives do not yet.
	StatusProcessing Status = "processing"
	// StatusReady means every derivative exists and the row can be served.
	StatusReady Status = "ready"
	// StatusFailed means the worker gave up; the reason is in the log, never in a
	// response.
	StatusFailed Status = "failed"
)

// Valid reports whether s is one of the pipeline states.
func (s Status) Valid() bool {
	switch s {
	case StatusProcessing, StatusReady, StatusFailed:
		return true
	default:
		return false
	}
}

// Upload size limits, mirroring the product decision recorded in KNOT-ADR-032.
const (
	// MaxImageBytes bounds an image upload: 10 MiB.
	MaxImageBytes = 10 << 20
	// MaxVideoBytes bounds a video upload: 100 MiB. It is also the cap on the
	// whole multipart request body.
	MaxVideoBytes = 100 << 20
)

// MaxMediaPerStory is the hard cap on how many media items one story may hold
// (KNOT-ADR-054).
//
// It is enforced in code rather than by a database constraint, because a CHECK
// cannot count rows: the insert transaction locks the story row and counts its
// media before it writes.
const MaxMediaPerStory = 10

// MediaLimitMessage is the exact client-facing text returned when the cap is
// reached. It is spelled once so the handler, the tests, and the docs cannot
// drift; a test asserts it agrees with MaxMediaPerStory.
const MediaLimitMessage = "a story may have at most 10 media items"

// mediaTypeNamespace is the bucket prefix reserved for story media. Every object
// in it is namespaced by the story id, so a key can always be traced back to
// exactly one story.
const mediaTypeNamespace = "story-media/"

// StoryMediaKeyPrefix returns the bucket prefix that belongs to storyID. Callers
// that mint media object keys use it so the namespace is spelled in one place.
func StoryMediaKeyPrefix(storyID string) string {
	return mediaTypeNamespace + storyID + "/"
}

// mediaAllowlist maps a sniffed MIME type onto its media type and the file
// extension used in the object key. The map is the single source of truth for
// what the backend accepts; a type outside it is a 415.
var mediaAllowlist = map[string]struct {
	mediaType MediaType
	extension string
}{
	"image/jpeg":      {MediaTypeImage, ".jpg"},
	"image/png":       {MediaTypeImage, ".png"},
	"image/webp":      {MediaTypeImage, ".webp"},
	"video/mp4":       {MediaTypeVideo, ".mp4"},
	"video/quicktime": {MediaTypeVideo, ".mov"},
}

// AllowedMediaType returns the media type and object-key extension for a sniffed
// MIME type, and whether the type is accepted at all.
func AllowedMediaType(mimeType string) (MediaType, string, bool) {
	entry, ok := mediaAllowlist[mimeType]
	if !ok {
		return "", "", false
	}
	return entry.mediaType, entry.extension, true
}

// contentTypeForExtension is AllowedMediaType's inverse: the MIME type and media
// type an object key's extension implies.
//
// A client that uploaded straight to the bucket told the service its content type
// when the URL was signed, so the key it comes back with carries the extension
// that type maps to. Reading the type back from the key is what keeps the two
// facts from disagreeing — an uploaded `.gif` can never be attached as an image
// just because the request said `image/jpeg` (KNOT-040).
func contentTypeForExtension(extension string) (string, MediaType, bool) {
	for mimeType, entry := range mediaAllowlist {
		if entry.extension == extension {
			return mimeType, entry.mediaType, true
		}
	}
	return "", "", false
}

// MaxBytesFor returns the upload limit for a media type. An unknown type has no
// limit and callers should reject it before asking.
func MaxBytesFor(t MediaType) int64 {
	switch t {
	case MediaTypeImage:
		return MaxImageBytes
	case MediaTypeVideo:
		return MaxVideoBytes
	default:
		return 0
	}
}

// StoryMedia is one image or video attached to a story.
type StoryMedia struct {
	// ID is the canonical UUID text assigned by PostgreSQL.
	ID string
	// StoryID is the story the media belongs to.
	StoryID string
	// UploaderID is the user who uploaded it. In the MVP this is always the
	// story's author.
	UploaderID string
	// StorageKey is the object key in the media bucket. It never leaves the
	// server; the wire format carries a relative URL instead.
	//
	// While the row is `processing` it names the UPLOADED file
	// (`..._original.<ext>`), which is what the worker downloads. When the worker
	// finishes it replaces it with the full rendition: the display AVIF for an
	// image, the 1080p H.264 stream for a video (KNOT-038). A row written before
	// KNOT-037-fix7 still names its original upload here, and a `display`/`stream`
	// request for such a row is refused rather than served, because that file would
	// be the multi-megabyte upload the renditions exist to avoid.
	StorageKey string
	// MediaType is image or video.
	MediaType MediaType
	// MimeType is the stored content type, decided by sniffing the bytes.
	MimeType string
	// Source is camera or gallery.
	Source MediaSource
	// Width and Height are the pixel dimensions when the client reported them,
	// or nil. They are advisory hints for layout, not validated against the bytes.
	Width  *int
	Height *int
	// DurationMS is a video's duration in milliseconds when known, or nil.
	DurationMS *int
	// SizeBytes is the stored object's size.
	SizeBytes int64
	// ThumbnailKey, MediumKey, and PosterKey are the object keys of renditions, or
	// nil when this item has none. Each is a key, never a URL, exactly as
	// StorageKey is (KNOT-ADR-029).
	//
	// Since KNOT-038 an image's thumbnail key is written WHEN THE ROW IS INSERTED,
	// naming the WebP the worker will produce, so the 202 response can carry a
	// stable `thumbnail_url`; a read that arrives before the file exists answers 202
	// because the row is still `processing`. It is nil for a video, which autoplays
	// in the feed, so no cover is generated at all. A row written before
	// KNOT-037-fix7 keeps whatever it stored here (a JPEG or AVIF thumbnail, or a
	// video poster's key is in PosterKey), and the extension tells the reader which
	// codec it is.
	//
	// MediumKey and PosterKey are RETIRED by KNOT-037-fix7 — new rows leave both nil
	// — but they stay part of the row so media written before the change keeps its
	// 1200px JPEG and its poster frame.
	ThumbnailKey *string
	MediumKey    *string
	PosterKey    *string
	// Status says whether the derivatives exist yet: processing (the worker is
	// still running), ready, or failed. Only a ready row can be served
	// (KNOT-037-fix7).
	Status Status
	// DisplayOrder is the item's position within its story. Lower comes first.
	DisplayOrder int
	// CreatedAt is the insertion time.
	CreatedAt time.Time
}

// fullRenditionFor answers the variant that carries a media type's full rendition:
// the display AVIF for an image, the stream MP4 for a video.
func fullRenditionFor(mediaType MediaType) Variant {
	if mediaType == MediaTypeVideo {
		return VariantStream
	}
	return VariantDisplay
}

// servesFullRendition reports whether an object key names a full rendition of the
// given variant, and not an original upload.
//
// It is a suffix test because the key is the only evidence a row has: a row
// written since KNOT-037-fix7 names `..._display.avif` or `..._stream.mp4` there,
// while a legacy row names the file its author uploaded. The distinction is what
// makes "serve derivatives only" enforceable without a second column
// (KNOT-037-fix10).
func servesFullRendition(key string, variant Variant) bool {
	switch variant {
	case VariantDisplay:
		return strings.HasSuffix(key, displaySuffix+avifExtension)
	case VariantStream:
		return strings.HasSuffix(key, streamSuffix+mp4Extension) ||
			strings.HasSuffix(key, streamSuffix+movExtension)
	default:
		return false
	}
}

// DerivativeKey answers the object key for one variant, and whether this row has
// that rendition.
//
// The original is not a variant: a row with no rendition answers false, and the
// caller serves the object StorageKey names. Display and stream both answer with
// StorageKey, because since KNOT-037-fix7 that IS the full rendition rather than
// an upload (KNOT-037-fix7).
func (m StoryMedia) DerivativeKey(variant Variant) (string, bool) {
	var key *string
	switch variant {
	case VariantDisplay, VariantStream:
		if strings.TrimSpace(m.StorageKey) == "" {
			return "", false
		}
		return m.StorageKey, true
	case VariantThumb:
		key = m.ThumbnailKey
	case VariantMedium:
		key = m.MediumKey
	case VariantPoster:
		key = m.PosterKey
	default:
		return "", false
	}

	if key == nil || strings.TrimSpace(*key) == "" {
		return "", false
	}

	return *key, true
}

// derivativeKeys lists the rendition object keys this row holds, in a stable
// order. It exists so deleting an item can remove everything the upload created.
//
// The primary object is included when it is a derivative (every row written since
// KNOT-037-fix7), and the derived WebP thumbnail is included for a row whose
// thumbnail is an AVIF with a WebP sibling. Duplicates are dropped, because the
// same file must not be deleted twice.
func (m StoryMedia) derivativeKeys() []string {
	keys := make([]string, 0, 4)
	seen := make(map[string]bool, 4)

	add := func(key string) {
		if strings.TrimSpace(key) == "" || seen[key] {
			return
		}
		seen[key] = true
		keys = append(keys, key)
	}

	for _, key := range []*string{m.ThumbnailKey, m.MediumKey, m.PosterKey} {
		if key != nil {
			add(*key)
		}
	}
	if webpKey, ok := m.ThumbnailWebPKey(); ok {
		add(webpKey)
	}

	return keys
}

// thumbnailRenditions maps each thumbnail format this row can answer to the
// object key that holds it.
//
// A row stores ONE thumbnail key, so the format it is in decides what else can be
// derived. An AVIF key implies its WebP sibling: both files are written beside each
// other with the same base name, so no second column and no migration is needed
// (KNOT-037-fix6). A WebP key stands alone (the upload encoded no AVIF), and a
// legacy JPEG key is all a row written before the dual-format ladder has.
func (m StoryMedia) thumbnailRenditions() map[Format]rendition {
	available := make(map[Format]rendition, 3)
	if m.ThumbnailKey == nil || strings.TrimSpace(*m.ThumbnailKey) == "" {
		return available
	}

	stored := *m.ThumbnailKey
	switch {
	case strings.HasSuffix(stored, avifExtension):
		available[FormatAVIF] = rendition{key: stored, contentType: avifContentType}
		available[FormatWebP] = rendition{
			key:         strings.TrimSuffix(stored, avifExtension) + webpExtension,
			contentType: webpContentType,
		}
	case strings.HasSuffix(stored, webpExtension):
		available[FormatWebP] = rendition{key: stored, contentType: webpContentType}
	case strings.HasSuffix(stored, jpegExtension):
		available[FormatJPEG] = rendition{key: stored, contentType: jpegContentType}
	}

	return available
}

// ThumbnailWebPKey derives the WebP thumbnail's object key from the stored
// thumbnail key, and reports whether this row has a WebP rendition at all.
//
// The key is derived rather than stored so the two files cannot disagree about
// which upload they belong to. A row whose thumbnail is a legacy JPEG answers
// false: it has no WebP file, and the JPEG thumbnail is what a thumbnail request
// falls back to.
func (m StoryMedia) ThumbnailWebPKey() (string, bool) {
	webpRendition, ok := m.thumbnailRenditions()[FormatWebP]
	return webpRendition.key, ok
}

// DisplayWebPKey derives the WebP display's object key from the primary object,
// and reports whether this row's display exists in WebP.
//
// An image's display is stored TWICE beside its upload — `<uuid>_display.avif` and
// `<uuid>_display.webp` — and the WebP key is derived rather than stored, exactly
// like the thumbnail's sibling, so the two files cannot disagree about which upload
// they belong to. A row written before KNOT-038-fix3 has only the AVIF, so it
// answers false and a WebP request falls back to the AVIF display (KNOT-038-fix3).
func (m StoryMedia) DisplayWebPKey() (string, bool) {
	if !strings.HasSuffix(m.StorageKey, displaySuffix+avifExtension) {
		return "", false
	}

	return strings.TrimSuffix(m.StorageKey, avifExtension) + webpExtension, true
}

// rendition is one stored derivative a content request may be answered from.
type rendition struct {
	// key is the object key to read.
	key string
	// contentType is the type to serve it as, which follows the format actually
	// read rather than the format that was asked for.
	contentType string
}

// contentTypeForKey answers the content type an object key names, falling back to
// the given type when the extension is one this package does not mint.
//
// It exists because a row's stored MimeType describes the UPLOAD, and since
// KNOT-037-fix7 the object the row names is a derivative of that upload: serving
// AVIF bytes as image/png is exactly the kind of lie this prevents.
func contentTypeForKey(key, fallback string) string {
	switch {
	case strings.HasSuffix(key, avifExtension):
		return avifContentType
	case strings.HasSuffix(key, webpExtension):
		return webpContentType
	case strings.HasSuffix(key, jpegExtension):
		return jpegContentType
	case strings.HasSuffix(key, mp4Extension):
		return videoContentType
	case strings.HasSuffix(key, movExtension):
		return quickTimeContentType
	default:
		return fallback
	}
}

// renditionCandidates lists the SMALL objects a variant request may be answered
// from, in the order they should be tried.
//
// Only the resident images are candidates. A display rendition or a stream is the
// row's primary object and is served by the caller's range-aware path instead: a
// video has to answer a `Range` request to seek, and reading 60 MiB into memory to
// hand back 200 bytes would be worse than not having the variant at all
// (KNOT-037-fix7).
//
// For a thumbnail the stored file comes first, then the legacy codecs a row
// written before KNOT-037-fix6 may still hold, so old media keeps a small image to
// show.
func (m StoryMedia) renditionCandidates(variant Variant, format Format) []rendition {
	candidates := make([]rendition, 0, 3)
	add := func(candidate rendition) {
		candidates = append(candidates, candidate)
	}

	switch variant {
	case VariantThumb:
		available := m.thumbnailRenditions()

		order := []Format{FormatWebP, FormatAVIF, FormatJPEG}
		if format == FormatAVIF {
			order = []Format{FormatAVIF, FormatWebP, FormatJPEG}
		}
		for _, candidateFormat := range order {
			if candidate, ok := available[candidateFormat]; ok {
				add(candidate)
			}
		}
	case VariantMedium:
		if key, ok := m.DerivativeKey(VariantMedium); ok {
			add(rendition{key: key, contentType: contentTypeForKey(key, jpegContentType)})
		}
	case VariantPoster:
		if key, ok := m.DerivativeKey(VariantPoster); ok {
			add(rendition{key: key, contentType: contentTypeForKey(key, jpegContentType)})
		}
	}

	return candidates
}

// CreateMediaInput is the input to CreateMedia. It is a domain type, not an HTTP
// type, so the handler layer stays free of validation rules.
type CreateMediaInput struct {
	// StoryID is required and must be canonical UUID text.
	StoryID string
	// UploaderID is required and must be canonical UUID text. In the running
	// server it comes from the authenticated request, never from the body.
	UploaderID string
	// MimeType is the sniffed content type. It must be in the allowlist.
	MimeType string
	// Source is camera or gallery.
	Source MediaSource
	// Body is the media bytes to store.
	Body io.Reader
	// SizeBytes is the number of bytes in Body.
	SizeBytes int64
	// Width, Height, and DurationMS are optional client-reported metadata.
	Width      *int
	Height     *int
	DurationMS *int
	// DisplayOrder, when set, places the item explicitly. When nil the store
	// appends it after the story's current last item, which is what the upload
	// path wants.
	DisplayOrder *int
}

// StoryMediaStore is the persistence contract for story media. The service
// depends on this interface rather than on pgx, so the business rules can be
// tested without a database.
type StoryMediaStore interface {
	// CreateMedia inserts a media row and returns the stored row. It refuses a row
	// that would take the story past MaxMediaPerStory, re-checked under a lock on
	// the story row so the cap holds against concurrent uploads (KNOT-ADR-054).
	CreateMedia(ctx context.Context, media StoryMedia) (StoryMedia, error)
	// CountMedia returns how many media items a story currently has. The service
	// calls it as an early check, so an upload that would be refused never leaves
	// the device; the store re-checks the same count under the story row lock
	// inside CreateMedia, which is the authoritative check (KNOT-ADR-054).
	CountMedia(ctx context.Context, storyID string) (int, error)
	// ListMedia returns every media item of a story in display order.
	ListMedia(ctx context.Context, storyID string) ([]StoryMedia, error)
	// FirstMedia returns each story's first item, keyed by story id, for the
	// listed stories that have any. It exists so the feed can show a preview
	// without a query per story.
	FirstMedia(ctx context.Context, storyIDs []string) (map[string]StoryMedia, error)
	// GetMedia returns one media row, or ErrNotFound.
	GetMedia(ctx context.Context, id string) (StoryMedia, error)
	// DeleteMedia removes one media row, or reports ErrNotFound.
	DeleteMedia(ctx context.Context, id string) error
	// SetStatus records where an item's derivatives are in the pipeline, and the
	// size of the object it now serves. A size of zero leaves the stored value
	// alone, which is what a failed job wants (KNOT-037-fix7).
	SetStatus(ctx context.Context, id string, status Status, sizeBytes int64) error
	// StoryAuthor returns the author id of a story, or ErrStoryNotFound.
	StoryAuthor(ctx context.Context, storyID string) (string, error)
}

// Service holds the story-media business rules.
//
// It depends on the StoryMediaStore abstraction, the storage.Storage contract, and
// the JobPublisher seam, and it knows nothing about HTTP, JSON, SQL, or Redis.
//
// Since KNOT-038 it does no encoding at all: the request path stores one upload and
// one row, and the CPU-bound work happens in the Rust media worker. That is why
// there is no semaphore, no retry loop, and no background goroutine left here.
type Service struct {
	store   StoryMediaStore
	objects Storage
	// jobs queues a Job for the worker. If it is unreachable the upload is rolled
	// back, because a row nothing will ever process is a promise the API cannot
	// keep.
	jobs   JobPublisher
	logger Logger
}

// Storage is the slice of the object-storage contract this package needs:
// writing an object, reading a byte range, deleting one, and signing one upload
// URL. Depending on this narrow interface (rather than the concrete S3Storage)
// keeps the service tests free of an object store.
type Storage interface {
	Put(ctx context.Context, key string, body io.Reader, contentType string) error
	Get(ctx context.Context, key string) (io.ReadCloser, string, error)
	GetRange(ctx context.Context, key string, start, end int64) (io.ReadCloser, error)
	Delete(ctx context.Context, key string) error
	// Exists reports whether an object is present. It is a HEAD, so the answer
	// costs no bytes; the notify path uses it to refuse a key nothing was uploaded
	// to (KNOT-040).
	Exists(ctx context.Context, key string) (bool, error)
	// PresignPut signs one upload URL, which is how a client sends media bytes
	// straight to the bucket instead of through this API (KNOT-040).
	PresignPut(ctx context.Context, key, contentType string, expires time.Duration) (string, error)
}

// Logger is the slice of slog.Logger this package needs: a warning when a
// best-effort object deletion fails or a background job cannot be recorded.
// Depending on an interface keeps the service tests free of a logger.
type Logger interface {
	// InfoContext records one step of a background job. The pipeline runs after the
	// response has been sent, so these lines are the only trace of it in production
	// logs — a job that never finishes and a job that never started otherwise look
	// identical (KNOT-037-fix17).
	InfoContext(ctx context.Context, msg string, args ...any)
	WarnContext(ctx context.Context, msg string, args ...any)
	// ErrorContext reports a job that failed outright: the row is marked failed
	// and the reason must reach the operator (KNOT-037-fix7).
	ErrorContext(ctx context.Context, msg string, args ...any)
}

// NewService wires a store, an object store, a job queue, and a logger together.
//
// The queue is required: without it an upload would be recorded and never
// processed, which is the failure mode this architecture exists to avoid
// (KNOT-038).
func NewService(store StoryMediaStore, objects Storage, jobs JobPublisher, logger Logger) (*Service, error) {
	if store == nil {
		return nil, fmt.Errorf("storymedia: service requires a media store")
	}
	if objects == nil {
		return nil, fmt.Errorf("storymedia: service requires an object store")
	}
	if jobs == nil {
		return nil, fmt.Errorf("storymedia: service requires a job publisher")
	}
	if logger == nil {
		return nil, fmt.Errorf("storymedia: service requires a logger")
	}

	return &Service{
		store:   store,
		objects: objects,
		jobs:    jobs,
		logger:  logger,
	}, nil
}

// TrimMimeType normalises a MIME type by dropping any parameters, so
// "image/png; charset=binary" matches "image/png". http.DetectContentType always
// returns a bare type, but a value from a header may not.
func TrimMimeType(raw string) string {
	return strings.TrimSpace(strings.SplitN(raw, ";", 2)[0])
}

// Variant names one of the generated renditions a client may ask for instead of
// the original upload (KNOT-037, KNOT-ADR-091).
type Variant string

// The derivative variants. The empty string is the full rendition (or, for a row
// written before KNOT-037-fix7, the original upload), which is why Variant's zero
// value means "serve what the row's primary key names".
const (
	// VariantThumb is the feed thumbnail: a 400px WebP (KNOT-037-fix7).
	VariantThumb Variant = "thumb"
	// VariantDisplay is an image's full rendition: a 2048px AVIF. A row written
	// before KNOT-037-fix7 answers with the original upload instead.
	VariantDisplay Variant = "display"
	// VariantStream is a video's full rendition: a 1080p H.264 MP4 with its index
	// at the front, so it starts playing before it has downloaded.
	VariantStream Variant = "stream"
	// VariantMedium is the story-detail rendition of a row written before
	// KNOT-037-fix7, at most MediumMaxWidth wide. New rows have none.
	VariantMedium Variant = "medium"
	// VariantPoster is a video's poster frame on a row written before
	// KNOT-037-fix7. New videos have none: they autoplay (KNOT-037-fix7).
	VariantPoster Variant = "poster"
)

// Format names the image encoding a thumbnail is stored and served in
// (KNOT-037-fix6).
type Format string

// The formats a thumbnail exists in, plus the internal JPEG value that names the
// legacy thumbnails written before the dual-format ladder and the medium
// rendition. Only FormatAVIF and FormatWebP are accepted from a client.
const (
	// FormatAVIF is the primary thumbnail: smallest bytes at the same quality.
	FormatAVIF Format = "avif"
	// FormatWebP is the thumbnail a device without an AVIF decoder falls back to.
	FormatWebP Format = "webp"
	// FormatJPEG is a JPEG rendition. It is never requested by a client: it is what
	// the medium rendition is, and what a thumbnail key written before
	// KNOT-037-fix6 names.
	FormatJPEG Format = "jpeg"
)

// ParseFormat validates the `format` query parameter of the content endpoint.
//
// An absent or blank value resolves to the zero Format, which asks for the primary
// encoding (AVIF) while still allowing the sibling codec as a fallback. Anything
// other than avif or webp must be rejected rather than ignored: silently answering
// in a codec the caller cannot decode is the failure this parameter exists to
// prevent.
func ParseFormat(raw string) (Format, bool) {
	trimmed := Format(strings.ToLower(strings.TrimSpace(raw)))
	switch trimmed {
	case "", FormatAVIF, FormatWebP:
		return trimmed, true
	default:
		return "", false
	}
}

// ParseVariant validates the `variant` query parameter of the content endpoint.
//
// An absent or blank value resolves to the item's full rendition, which is chosen
// from its media type. An unknown value must be rejected rather than ignored:
// silently answering with a full-size object would send a multi-megabyte file to a
// client that asked for a 400px thumbnail, which is the opposite of what the
// parameter is for.
func ParseVariant(raw string) (Variant, bool) {
	trimmed := Variant(strings.TrimSpace(raw))
	switch trimmed {
	case "", VariantThumb, VariantDisplay, VariantStream, VariantMedium, VariantPoster:
		return trimmed, true
	default:
		return "", false
	}
}

// StreamMedia resolves a media id to a reader positioned for the request's Range
// header.
//
// It returns the reader, the object's TOTAL size, and the content type. The reader
// yields the requested byte range when the Range header is valid for the object's
// size, and the whole object otherwise; the caller uses ParseRange with the same
// header and the returned total to decide whether to answer 206 or 200. Putting
// the decision in one exported helper is what keeps the service and the handler
// from disagreeing about which bytes were sent.
//
// Only derivatives are served (KNOT-037-fix10). A row written since
// KNOT-037-fix7 stores its display AVIF or stream MP4 as the primary object, and
// that file is what a full-rendition request answers from; the upload itself is
// never stored. A row written earlier still names its original upload there, and
// the originals are not in the bucket any more, so a variant such a row cannot
// answer is a 404 rather than a fallback to a file that either no longer exists or
// is exactly the multi-megabyte upload this contract exists to avoid.
//
// Thumbnails, and the medium and poster files a legacy row may still hold, are
// small, so they are read whole and sent without range support — which also means
// a thumbnail request can never be turned into a full-size transfer.
//
// A thumbnail request also names a format. The requested format is tried first and
// the other one second, so a device that cannot decode one codec still gets a
// small image rather than nothing (KNOT-037-fix6). The content type returned is the
// type of the file that was actually read, not the one that was asked for.
func (s *Service) StreamMedia(ctx context.Context, id string, variant Variant, format Format, r *http.Request) (io.ReadCloser, int64, string, error) {
	media, err := s.GetMedia(ctx, id)
	if err != nil {
		return nil, 0, "", err
	}

	// A row whose derivatives are still being generated has no bytes to serve yet.
	// Answering 404 would tell the client the media does not exist, so the caller is
	// told to wait instead; a failed row has nothing coming, and is reported as
	// missing because there is nothing else it can ever be (KNOT-037-fix7).
	switch media.Status {
	case StatusProcessing:
		return nil, 0, "", ErrProcessing
	case StatusFailed:
		return nil, 0, "", ErrProcessingFailed
	}

	// The small renditions: the feed thumbnail in either codec, plus the medium and
	// poster files a row written before KNOT-037-fix7 may still hold. A row with
	// none of them has nothing to answer this request, and there is no original to
	// fall back to.
	if variant != "" && variant != VariantDisplay && variant != VariantStream {
		for _, candidate := range media.renditionCandidates(variant, format) {
			body, size, readErr := s.readRendition(ctx, candidate.key)
			switch {
			case readErr == nil:
				return body, size, candidate.contentType, nil
			case !errors.Is(readErr, ErrNotFound):
				return nil, 0, "", readErr
			}
			// The object is gone, so try the next candidate.
		}

		return nil, 0, "", ErrNotFound
	}

	// A full rendition, named explicitly or implied by an absent variant: the row's
	// primary object, but only when it really is that rendition. This check is what
	// stops a legacy row from answering with its upload.
	full := variant
	if full == "" {
		full = fullRenditionFor(media.MediaType)
	}
	if !servesFullRendition(media.StorageKey, full) {
		return nil, 0, "", ErrNotFound
	}

	// An image's display exists in two codecs. A request that names WebP is answered
	// from the WebP file when it is there, and falls back to the AVIF when it is not:
	// a row produced before KNOT-038-fix3 has only the AVIF, and the address a client
	// holds should keep working. Both files are small enough to read whole, and the
	// row's size_bytes describes the AVIF, so the WebP's own size is measured here
	// rather than taken from the row (KNOT-038-fix3).
	if full == VariantDisplay && format == FormatWebP {
		if webpKey, ok := media.DisplayWebPKey(); ok {
			body, size, readErr := s.readRendition(ctx, webpKey)
			switch {
			case readErr == nil:
				return body, size, webpContentType, nil
			case !errors.Is(readErr, ErrNotFound):
				return nil, 0, "", readErr
			}
		}
	}

	// Served with the type its key names: the display AVIF or the stream MP4, never
	// the upload's MIME type.
	contentType := contentTypeForKey(media.StorageKey, media.MimeType)

	if start, end, ok := ParseRange(r.Header.Get("Range"), media.SizeBytes); ok {
		body, err := s.objects.GetRange(ctx, media.StorageKey, start, end)
		if err != nil {
			return nil, 0, "", storageReadError(err)
		}
		return body, media.SizeBytes, contentType, nil
	}

	body, _, err := s.objects.Get(ctx, media.StorageKey)
	if err != nil {
		return nil, 0, "", storageReadError(err)
	}

	return body, media.SizeBytes, contentType, nil
}

// readRendition reads one rendition whole.
//
// The storage contract has no stat method, and a rendition is small (a thumbnail is
// tens of kilobytes), so buffering it is what allows an exact Content-Length
// without widening the interface. Renditions are images, so they are never played
// with a Range request.
func (s *Service) readRendition(ctx context.Context, key string) (io.ReadCloser, int64, error) {
	body, _, err := s.objects.Get(ctx, key)
	if err != nil {
		return nil, 0, storageReadError(err)
	}
	defer func() { _ = body.Close() }()

	data, err := io.ReadAll(body)
	if err != nil {
		return nil, 0, fmt.Errorf("storymedia: read rendition: %w", err)
	}

	return io.NopCloser(bytes.NewReader(data)), int64(len(data)), nil
}

// storageReadError turns a missing object into ErrNotFound and leaves anything
// else wrapped, so the handler can answer 404 for the first and 500 for the rest.
func storageReadError(err error) error {
	if errors.Is(err, storage.ErrObjectNotFound) {
		return ErrNotFound
	}
	return fmt.Errorf("storymedia: read object: %w", err)
}
