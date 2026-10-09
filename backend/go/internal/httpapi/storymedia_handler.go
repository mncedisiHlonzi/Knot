package httpapi

import (
	"bytes"
	"context"
	"errors"
	"fmt"
	"io"
	"log/slog"
	"net/http"
	"strconv"
	"time"

	"github.com/knot/backend/internal/storymedia"
)

// codeForbidden is returned when the authenticated user may not perform the
// action on the named resource (they are neither the story's author nor the
// item's uploader).
const codeForbidden = "forbidden"

const (
	// storyMediaFormField is the multipart field carrying the media bytes.
	storyMediaFormField = "file"
	// storyMediaSourceField is the multipart field carrying "camera" or "gallery".
	storyMediaSourceField = "source"

	// maxStoryMediaRequestOverhead is the slack allowed on top of the file itself
	// for multipart boundaries and headers, so the body cap can be enforced while
	// the request is being read.
	maxStoryMediaRequestOverhead = 1 << 20 // 1 MiB

	// storyMediaCacheControl lets a browser or an intermediary reuse a media
	// object for an hour. Story media is part of a public story.
	storyMediaCacheControl = "public, max-age=3600"
)

// StoryMediaService is the slice of the storymedia service that the HTTP layer
// needs. Depending on an interface (rather than the concrete service) keeps
// handler tests free of a database and an object store.
type StoryMediaService interface {
	CreateMedia(ctx context.Context, in storymedia.CreateMediaInput) (storymedia.StoryMedia, error)
	ListMedia(ctx context.Context, storyID string) ([]storymedia.StoryMedia, error)
	StreamMedia(ctx context.Context, id string, r *http.Request) (io.ReadCloser, int64, string, error)
	DeleteMedia(ctx context.Context, id, byUserID string) error
}

// StoryMediaHandler serves the story-media endpoints.
//
// Bytes are written to and read from the object store by this backend; the
// client never talks to the object store, and the bucket is never public
// (KNOT-ADR-029).
type StoryMediaHandler struct {
	service StoryMediaService
	logger  *slog.Logger
}

// NewStoryMediaHandler returns a handler backed by service.
func NewStoryMediaHandler(service StoryMediaService, logger *slog.Logger) (*StoryMediaHandler, error) {
	if service == nil {
		return nil, fmt.Errorf("httpapi: story media handler requires a service")
	}
	if logger == nil {
		return nil, fmt.Errorf("httpapi: story media handler requires a logger")
	}
	return &StoryMediaHandler{service: service, logger: logger}, nil
}

// storyMediaResponse is the public projection of a piece of story media. It is
// an explicit type, not the domain StoryMedia, so storage_key can never be
// serialised by accident.
type storyMediaResponse struct {
	ID           string    `json:"id"`
	MediaType    string    `json:"media_type"`
	Source       string    `json:"source"`
	MimeType     string    `json:"mime_type"`
	Width        *int      `json:"width"`
	Height       *int      `json:"height"`
	DurationMS   *int      `json:"duration_ms"`
	SizeBytes    int64     `json:"size_bytes"`
	DisplayOrder int       `json:"display_order"`
	CreatedAt    time.Time `json:"created_at"`
	// URL is the path on this API that serves the bytes. It is relative, so the
	// client resolves it against its own API base URL.
	URL string `json:"url"`
}

// storyMediaEnvelope wraps a single item, so the response shape can gain sibling
// fields later without breaking clients.
type storyMediaEnvelope struct {
	Media storyMediaResponse `json:"media"`
}

// listStoryMediaResponse is the GET /stories/{id}/media body. Media is always an
// array, never null.
type listStoryMediaResponse struct {
	Media []storyMediaResponse `json:"media"`
}

// Create handles POST /stories/{id}/media. The route is protected, and only the
// story's author may attach media in the MVP.
func (h *StoryMediaHandler) Create(w http.ResponseWriter, r *http.Request) {
	userID, ok := UserIDFromContext(r.Context())
	if !ok {
		// Unreachable while the route is wrapped by AuthMiddleware. It is kept as
		// a fail-closed guard so a future wiring mistake cannot write media with
		// no uploader.
		writeError(w, http.StatusUnauthorized, codeUnauthorized, "authentication required")
		return
	}

	limit := int64(storymedia.MaxVideoBytes) + maxStoryMediaRequestOverhead
	if r.ContentLength > limit {
		h.writeTooLarge(w)
		return
	}
	r.Body = http.MaxBytesReader(w, r.Body, limit)

	file, _, err := r.FormFile(storyMediaFormField)
	if err != nil {
		h.writeUploadReadError(w, r, err)
		return
	}
	defer file.Close()

	// Read one byte past the cap: if it arrives, the upload is too large. Only the
	// cap is ever buffered, which bounds memory at the video limit.
	data, err := io.ReadAll(io.LimitReader(file, storymedia.MaxVideoBytes+1))
	if err != nil {
		var tooLarge *http.MaxBytesError
		if errors.As(err, &tooLarge) {
			h.writeTooLarge(w)
			return
		}
		h.writeFailure(w, r, "read uploaded media", err)
		return
	}
	if len(data) > storymedia.MaxVideoBytes {
		h.writeTooLarge(w)
		return
	}
	if len(data) == 0 {
		writeError(w, http.StatusBadRequest, codeInvalidRequest, "the uploaded media is empty")
		return
	}

	// The type is decided by inspecting the bytes, never by the client's declared
	// Content-Type, so a caller cannot have HTML served back from a media URL by
	// mislabelling it.
	mimeType := storymedia.TrimMimeType(http.DetectContentType(data))
	if _, _, accepted := storymedia.AllowedMediaType(mimeType); !accepted {
		writeError(w, http.StatusUnsupportedMediaType, codeUnsupportedMediaType,
			"media must be a JPEG, PNG, or WebP image, or an MP4 or MOV video")
		return
	}

	source := storymedia.MediaSource(r.FormValue(storyMediaSourceField))

	created, err := h.service.CreateMedia(r.Context(), storymedia.CreateMediaInput{
		StoryID:    r.PathValue("id"),
		UploaderID: userID,
		MimeType:   mimeType,
		Source:     source,
		Body:       bytes.NewReader(data),
		SizeBytes:  int64(len(data)),
		Width:      optionalFormInt(r, "width"),
		Height:     optionalFormInt(r, "height"),
		DurationMS: optionalFormInt(r, "duration_ms"),
	})
	if err != nil {
		h.writeServiceError(w, r, err)
		return
	}

	writeJSON(w, http.StatusCreated, storyMediaEnvelope{Media: newStoryMediaResponse(created, r.PathValue("id"))})
}

// List handles GET /stories/{id}/media. The route is public.
func (h *StoryMediaHandler) List(w http.ResponseWriter, r *http.Request) {
	media, err := h.service.ListMedia(r.Context(), r.PathValue("id"))
	if err != nil {
		h.writeServiceError(w, r, err)
		return
	}

	items := make([]storyMediaResponse, 0, len(media))
	for _, item := range media {
		items = append(items, newStoryMediaResponse(item, r.PathValue("id")))
	}

	writeJSON(w, http.StatusOK, listStoryMediaResponse{Media: items})
}

// Content handles GET /stories/{id}/media/{mid}/content. The route is public.
//
// It streams the object from storage, honouring a single-range HTTP Range header
// so a video can be played without transferring the whole file. A valid range is
// answered 206 with Content-Range; anything else (no header, or an unusable one)
// is answered 200 with the whole object.
func (h *StoryMediaHandler) Content(w http.ResponseWriter, r *http.Request) {
	body, total, mimeType, err := h.service.StreamMedia(r.Context(), r.PathValue("mid"), r)
	if err != nil {
		h.writeServiceError(w, r, err)
		return
	}
	defer body.Close()

	w.Header().Set("Content-Type", mimeType)
	w.Header().Set("Cache-Control", storyMediaCacheControl)
	w.Header().Set("Accept-Ranges", "bytes")
	// The stored type is sniffed on upload, but a browser must still not be given
	// the option of reinterpreting the bytes.
	w.Header().Set("X-Content-Type-Options", "nosniff")

	if start, end, ok := storymedia.ParseRange(r.Header.Get("Range"), total); ok {
		w.Header().Set("Content-Range", fmt.Sprintf("bytes %d-%d/%d", start, end, total))
		w.Header().Set("Content-Length", strconv.FormatInt(end-start+1, 10))
		w.WriteHeader(http.StatusPartialContent)
	} else {
		w.Header().Set("Content-Length", strconv.FormatInt(total, 10))
		w.WriteHeader(http.StatusOK)
	}

	if _, err := io.Copy(w, body); err != nil {
		// The status line is already sent, so this cannot become an error
		// response. It is logged instead, and the connection is left to fail.
		h.logger.ErrorContext(
			r.Context(),
			"story media response failed",
			slog.String("request_id", RequestIDFromContext(r.Context())),
			slog.String("path", r.URL.Path),
			slog.String("error", err.Error()),
		)
	}
}

// Delete handles DELETE /stories/{id}/media/{mid}. The route is protected, and
// only the story's author or the item's uploader may delete it.
func (h *StoryMediaHandler) Delete(w http.ResponseWriter, r *http.Request) {
	userID, ok := UserIDFromContext(r.Context())
	if !ok {
		writeError(w, http.StatusUnauthorized, codeUnauthorized, "authentication required")
		return
	}

	if err := h.service.DeleteMedia(r.Context(), r.PathValue("mid"), userID); err != nil {
		h.writeServiceError(w, r, err)
		return
	}

	w.WriteHeader(http.StatusNoContent)
}

// writeUploadReadError classifies a failure to open the uploaded file.
func (h *StoryMediaHandler) writeUploadReadError(w http.ResponseWriter, r *http.Request, err error) {
	var tooLarge *http.MaxBytesError
	switch {
	case errors.As(err, &tooLarge):
		h.writeTooLarge(w)
	case errors.Is(err, http.ErrMissingFile):
		writeError(w, http.StatusBadRequest, codeInvalidRequest,
			fmt.Sprintf("request must be multipart/form-data with a %q file field", storyMediaFormField))
	default:
		h.logger.WarnContext(
			r.Context(),
			"rejected media upload",
			slog.String("request_id", RequestIDFromContext(r.Context())),
			slog.String("path", r.URL.Path),
			slog.String("error", err.Error()),
		)
		writeError(w, http.StatusBadRequest, codeInvalidRequest,
			fmt.Sprintf("request must be multipart/form-data with a %q file field", storyMediaFormField))
	}
}

// writeTooLarge reports an upload over the request cap.
func (h *StoryMediaHandler) writeTooLarge(w http.ResponseWriter) {
	writeError(w, http.StatusRequestEntityTooLarge, codeRequestTooLarge,
		fmt.Sprintf("media must be at most %d bytes", storymedia.MaxVideoBytes))
}

// writeServiceError maps a domain error onto the wire format, and turns anything
// unrecognised into a logged 500.
func (h *StoryMediaHandler) writeServiceError(w http.ResponseWriter, r *http.Request, err error) {
	var validation *storymedia.ValidationError

	switch {
	case errors.As(err, &validation):
		writeError(w, http.StatusBadRequest, codeValidation, fmt.Sprintf("%s %s", validation.Field, validation.Message))
	case errors.Is(err, storymedia.ErrForbidden):
		writeError(w, http.StatusForbidden, codeForbidden, "you may not change this story's media")
	case errors.Is(err, storymedia.ErrNotFound), errors.Is(err, storymedia.ErrStoryNotFound):
		writeError(w, http.StatusNotFound, codeNotFound, "media not found")
	case errors.Is(err, storymedia.ErrTooLarge):
		h.writeTooLarge(w)
	case errors.Is(err, storymedia.ErrUnsupportedMediaType):
		writeError(w, http.StatusUnsupportedMediaType, codeUnsupportedMediaType,
			"media must be a JPEG, PNG, or WebP image, or an MP4 or MOV video")
	default:
		h.writeFailure(w, r, "story media request failed", err)
	}
}

// writeFailure logs err and answers with a generic 500.
func (h *StoryMediaHandler) writeFailure(w http.ResponseWriter, r *http.Request, what string, err error) {
	h.logger.ErrorContext(
		r.Context(),
		what,
		slog.String("request_id", RequestIDFromContext(r.Context())),
		slog.String("path", r.URL.Path),
		slog.String("error", err.Error()),
	)
	writeError(w, http.StatusInternalServerError, codeInternal, "internal server error")
}

// newStoryMediaResponse projects a domain item onto the wire format. storyID is
// the story the item belongs to, which the item's URL is built from.
func newStoryMediaResponse(media storymedia.StoryMedia, storyID string) storyMediaResponse {
	return storyMediaResponse{
		ID:           media.ID,
		MediaType:    string(media.MediaType),
		Source:       string(media.Source),
		MimeType:     media.MimeType,
		Width:        media.Width,
		Height:       media.Height,
		DurationMS:   media.DurationMS,
		SizeBytes:    media.SizeBytes,
		DisplayOrder: media.DisplayOrder,
		CreatedAt:    media.CreatedAt,
		URL:          storyMediaURL(storyID, media.ID),
	}
}

// storyMediaURL builds the relative path a client fetches the bytes from.
func storyMediaURL(storyID, mediaID string) string {
	return "/stories/" + storyID + "/media/" + mediaID + "/content"
}

// optionalFormInt reads an optional non-negative integer form field, returning
// nil when the field is absent or blank.
func optionalFormInt(r *http.Request, field string) *int {
	raw := r.FormValue(field)
	if raw == "" {
		return nil
	}
	value, err := strconv.Atoi(raw)
	if err != nil {
		return nil
	}
	return &value
}
