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

// Upload size limits, mirroring the product decision recorded in KNOT-ADR-032.
const (
	// MaxImageBytes bounds an image upload: 10 MiB.
	MaxImageBytes = 10 << 20
	// MaxVideoBytes bounds a video upload: 100 MiB. It is also the cap on the
	// whole multipart request body.
	MaxVideoBytes = 100 << 20
)

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
	// DisplayOrder is the item's position within its story. Lower comes first.
	DisplayOrder int
	// CreatedAt is the insertion time.
	CreatedAt time.Time
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
	// CreateMedia inserts a media row and returns the stored row.
	CreateMedia(ctx context.Context, media StoryMedia) (StoryMedia, error)
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
	// StoryAuthor returns the author id of a story, or ErrStoryNotFound.
	StoryAuthor(ctx context.Context, storyID string) (string, error)
}

// Service holds the story-media business rules.
//
// It depends on the StoryMediaStore abstraction and the storage.Storage
// contract, and it knows nothing about HTTP, JSON, or SQL.
type Service struct {
	store   StoryMediaStore
	objects Storage
	logger  Logger
}

// Storage is the slice of the object-storage contract this package needs:
// writing an object, reading a byte range, and deleting one. Depending on this
// narrow interface (rather than the concrete S3Storage) keeps the service tests
// free of an object store.
type Storage interface {
	Put(ctx context.Context, key string, body io.Reader, contentType string) error
	Get(ctx context.Context, key string) (io.ReadCloser, string, error)
	GetRange(ctx context.Context, key string, start, end int64) (io.ReadCloser, error)
	Delete(ctx context.Context, key string) error
}

// Logger is the slice of slog.Logger this package needs: a warning when a
// best-effort object deletion fails. Depending on an interface keeps the service
// tests free of a logger.
type Logger interface {
	WarnContext(ctx context.Context, msg string, args ...any)
}

// NewService wires a store, an object store, and a logger together.
func NewService(store StoryMediaStore, objects Storage, logger Logger) (*Service, error) {
	if store == nil {
		return nil, fmt.Errorf("storymedia: service requires a media store")
	}
	if objects == nil {
		return nil, fmt.Errorf("storymedia: service requires an object store")
	}
	if logger == nil {
		return nil, fmt.Errorf("storymedia: service requires a logger")
	}
	return &Service{store: store, objects: objects, logger: logger}, nil
}

// TrimMimeType normalises a MIME type by dropping any parameters, so
// "image/png; charset=binary" matches "image/png". http.DetectContentType always
// returns a bare type, but a value from a header may not.
func TrimMimeType(raw string) string {
	return strings.TrimSpace(strings.SplitN(raw, ";", 2)[0])
}

// StreamMedia resolves a media id to a reader positioned for the request's Range
// header.
//
// It returns the reader, the object's TOTAL size, and the stored MIME type. The
// reader yields the requested byte range when the Range header is valid for the
// object's size, and the whole object otherwise; the caller uses ParseRange with
// the same header and the returned total to decide whether to answer 206 or 200.
// Putting the decision in one exported helper is what keeps the service and the
// handler from disagreeing about which bytes were sent.
func (s *Service) StreamMedia(ctx context.Context, id string, r *http.Request) (io.ReadCloser, int64, string, error) {
	media, err := s.GetMedia(ctx, id)
	if err != nil {
		return nil, 0, "", err
	}

	if start, end, ok := ParseRange(r.Header.Get("Range"), media.SizeBytes); ok {
		body, err := s.objects.GetRange(ctx, media.StorageKey, start, end)
		if err != nil {
			return nil, 0, "", storageReadError(err)
		}
		return body, media.SizeBytes, media.MimeType, nil
	}

	body, _, err := s.objects.Get(ctx, media.StorageKey)
	if err != nil {
		return nil, 0, "", storageReadError(err)
	}

	return body, media.SizeBytes, media.MimeType, nil
}

// storageReadError turns a missing object into ErrNotFound and leaves anything
// else wrapped, so the handler can answer 404 for the first and 500 for the rest.
func storageReadError(err error) error {
	if errors.Is(err, storage.ErrObjectNotFound) {
		return ErrNotFound
	}
	return fmt.Errorf("storymedia: read object: %w", err)
}
