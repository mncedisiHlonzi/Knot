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
	"strings"
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

	// storyMediaRetryAfter is how long an item that is still processing asks a
	// client to wait before trying again, in seconds. It is a hint, not a promise:
	// a 20 MiB video transcode can outlast it, and a client should keep retrying
	// while it still sees 202 (KNOT-037-fix7).
	storyMediaRetryAfter = "3"
)

// StoryMediaService is the slice of the storymedia service that the HTTP layer
// needs. Depending on an interface (rather than the concrete service) keeps
// handler tests free of a database and an object store.
type StoryMediaService interface {
	CreateMedia(ctx context.Context, in storymedia.CreateMediaInput) (storymedia.StoryMedia, error)
	// PresignUpload signs one direct-to-bucket upload URL (KNOT-040).
	PresignUpload(ctx context.Context, in storymedia.PresignUploadInput) (storymedia.PresignedUpload, error)
	// AttachUpload records a media row for an object the client uploaded itself.
	AttachUpload(ctx context.Context, in storymedia.AttachUploadInput) (storymedia.StoryMedia, error)
	ListMedia(ctx context.Context, storyID string) ([]storymedia.StoryMedia, error)
	StreamMedia(ctx context.Context, id string, variant storymedia.Variant, format storymedia.Format, r *http.Request) (io.ReadCloser, int64, string, error)
	DeleteMedia(ctx context.Context, id, byUserID string) error
}

// StoryMediaHandler serves the story-media endpoints.
//
// Bytes are written to and read from the object store by this backend; the
// client never talks to the object store, and the bucket is never public
// (KNOT-ADR-029).
type StoryMediaHandler struct {
	service StoryMediaService
	// details drops the story's cached detail when its media changes: an upload or
	// a delete changes the media array the detail response shows. Nil means the
	// cache is switched off, and every call is a no-op (KNOT-036-fix4).
	details *DetailInvalidator
	logger  *slog.Logger
}

// NewStoryMediaHandler returns a handler backed by service.
func NewStoryMediaHandler(service StoryMediaService, logger *slog.Logger) (*StoryMediaHandler, error) {
	return NewStoryMediaHandlerWithCaches(service, logger, Caches{})
}

// NewStoryMediaHandlerWithCaches is NewStoryMediaHandler plus the Redis detail
// cache (KNOT-036-fix4).
//
// Only the story key is invalidated. A version detail inherits the story's media
// and carries its own cached copy, so a version key is left to expire rather than
// fanned out to every adaptation — an upload cannot cheaply name them all.
func NewStoryMediaHandlerWithCaches(service StoryMediaService, logger *slog.Logger, caches Caches) (*StoryMediaHandler, error) {
	if service == nil {
		return nil, fmt.Errorf("httpapi: story media handler requires a service")
	}
	if logger == nil {
		return nil, fmt.Errorf("httpapi: story media handler requires a logger")
	}
	return &StoryMediaHandler{
		service: service,
		details: NewDetailInvalidator(caches.Details, nil, nil, logger),
		logger:  logger,
	}, nil
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
	// Status says whether the derivatives exist yet: `processing`, `ready`, or
	// `failed`. A client that reads a processing item shows a placeholder rather
	// than a broken frame, and may poll (KNOT-037-fix7).
	Status string `json:"status"`
	// URL is the path on this API that serves the item's full rendition. It is
	// relative, so the client resolves it against its own API base URL. Since
	// KNOT-037-fix7 that is the DISPLAY AVIF for an image and the STREAM MP4 for a
	// video — there is no stored original to address any more. A row written before
	// that change answers with its original upload instead.
	URL string `json:"url"`
	// ThumbnailURL is the feed card's image: a 400px WebP, or null for a video,
	// which autoplays and therefore has no cover (KNOT-037-fix7).
	ThumbnailURL *string `json:"thumbnail_url"`
	// ThumbnailURLFallback is the sibling codec of a thumbnail written by
	// KNOT-037-fix6 (an AVIF thumbnail with a WebP file beside it). It is always
	// null for an item produced since KNOT-037-fix7, whose thumbnail is already
	// WebP.
	ThumbnailURLFallback *string `json:"thumbnail_url_fallback"`
	// MediumURL addresses the 1200px JPEG a row written before KNOT-037-fix7 has.
	// Since KNOT-037-fix10 it is the only retired rendition still advertised: a video
	// autoplays, so there is no cover and no `poster_url`, and no row written since
	// the async pipeline has either file.
	MediumURL *string `json:"medium_url"`
	// DisplayWebPURL is the same 1200px display as WebP, for devices that cannot
	// decode AVIF: AVIF is native only on Android 12+ and iOS 16+, and a device below
	// those draws a blank frame instead of reporting an error (KNOT-038-fix3).
	//
	// It is null for a video, whose full rendition is the stream, and for a row
	// written before KNOT-038-fix3, which has only the AVIF; a client falls back to
	// URL in either case.
	DisplayWebPURL *string `json:"display_webp_url"`
}

// storyMediaEnvelope wraps a single item, so the response shape can gain sibling
// fields later without breaking clients.
type storyMediaEnvelope struct {
	Media storyMediaResponse `json:"media"`
}

// mediaUploadURLRequest is the body of POST /media/upload-url (KNOT-040).
//
// It names the story the media will be attached to, so the story's author can be
// checked before a URL exists at all, and declares the content type, which decides
// the object key's extension — there is no body here to sniff.
type mediaUploadURLRequest struct {
	StoryID     string `json:"story_id"`
	Filename    string `json:"filename"`
	ContentType string `json:"content_type"`
}

// mediaUploadURLResponse is what the client uploads to, and what it reports back
// to POST /stories/{id}/media afterwards.
type mediaUploadURLResponse struct {
	// UploadURL is the signed URL the bytes are PUT to, straight to the object
	// store, never through this API.
	UploadURL string `json:"upload_url"`
	// ObjectKey is the key the upload lands on; it is the only thing the notify
	// call needs.
	ObjectKey string `json:"object_key"`
	// ExpiresIn is the URL's remaining lifetime in seconds.
	ExpiresIn int `json:"expires_in"`
}

// attachMediaRequest is the JSON body of POST /stories/{id}/media since
// KNOT-040: the client already uploaded the bytes and reports where they landed.
//
// `source` and the metadata are optional. They describe the file rather than the
// upload, so the client is still the only party that knows them; a missing source
// is recorded as `gallery`.
type attachMediaRequest struct {
	ObjectKey  string `json:"object_key"`
	Source     string `json:"source,omitempty"`
	Width      *int   `json:"width,omitempty"`
	Height     *int   `json:"height,omitempty"`
	DurationMS *int   `json:"duration_ms,omitempty"`
}

// listStoryMediaResponse is the GET /stories/{id}/media body. Media is always an
// array, never null.
type listStoryMediaResponse struct {
	Media []storyMediaResponse `json:"media"`
}

// Create handles POST /stories/{id}/media. The route is protected, and only the
// story's author may attach media in the MVP.
//
// Since KNOT-040 there are two ways in. A client that uploaded the bytes itself
// sends `{"object_key": "..."}` and is answered from the JSON path below. A client
// that still sends the file here is served by the multipart path, which is kept so
// a build released before the direct-upload change keeps working — it is the same
// flow as before, with the API writing the object.
func (h *StoryMediaHandler) Create(w http.ResponseWriter, r *http.Request) {
	userID, ok := UserIDFromContext(r.Context())
	if !ok {
		// Unreachable while the route is wrapped by AuthMiddleware. It is kept as
		// a fail-closed guard so a future wiring mistake cannot write media with
		// no uploader.
		writeError(w, http.StatusUnauthorized, codeUnauthorized, "authentication required")
		return
	}

	if isJSONRequest(r) {
		h.attachUploadedObject(w, r, userID)
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

	// The story's cached detail carries its media array, so a new item makes it
	// stale. Best-effort: the upload has been recorded and the TTL heals a failed
	// delete (KNOT-036-fix4).
	h.details.InvalidateForStory(r.Context(), r.PathValue("id"))

	// 202: the row exists and its job is queued; the derivatives do not exist yet.
	// The Rust media worker produces them and flips the row to ready, which is what
	// keeps a 100 MiB video upload from holding the request open (KNOT-037-fix7,
	// KNOT-038).
	writeJSON(w, http.StatusAccepted, storyMediaEnvelope{Media: newStoryMediaResponse(created, r.PathValue("id"))})
}

// UploadURL handles POST /media/upload-url. The route is protected.
//
// It answers with a signed URL the client PUTs the file to, straight to the
// object store, and the object key to report once the upload has landed
// (KNOT-040). The API never sees the bytes, which is the point: the two calls on
// this path are small JSON exchanges, so a slow uplink can no longer time out a
// request that had nothing to do with the file.
//
// Authorization is checked before a URL is signed — the caller must be the story's
// author, and the story must have room for another item — so a client is refused
// at the cheapest possible moment.
func (h *StoryMediaHandler) UploadURL(w http.ResponseWriter, r *http.Request) {
	userID, ok := UserIDFromContext(r.Context())
	if !ok {
		writeError(w, http.StatusUnauthorized, codeUnauthorized, "authentication required")
		return
	}

	var request mediaUploadURLRequest
	if err := decodeJSON(w, r, &request); err != nil {
		writeError(w, http.StatusBadRequest, codeInvalidRequest,
			"request body must be a JSON object with story_id, filename, and content_type")
		return
	}

	target, err := h.service.PresignUpload(r.Context(), storymedia.PresignUploadInput{
		StoryID:     request.StoryID,
		UploaderID:  userID,
		Filename:    request.Filename,
		ContentType: request.ContentType,
	})
	if err != nil {
		h.writeServiceError(w, r, err)
		return
	}

	writeJSON(w, http.StatusOK, mediaUploadURLResponse{
		UploadURL: target.UploadURL,
		ObjectKey: target.ObjectKey,
		ExpiresIn: target.ExpiresIn,
	})
}

// attachUploadedObject handles the JSON form of POST /stories/{id}/media: the
// client has already put the bytes in the bucket and reports the key.
//
// The object is confirmed to exist before a row is written, and the row is queued
// exactly as the multipart path queues it, so the media worker processes both
// paths identically (KNOT-040).
func (h *StoryMediaHandler) attachUploadedObject(w http.ResponseWriter, r *http.Request, userID string) {
	var request attachMediaRequest
	if err := decodeJSON(w, r, &request); err != nil {
		writeError(w, http.StatusBadRequest, codeInvalidRequest,
			"request body must be a JSON object with an object_key")
		return
	}
	if strings.TrimSpace(request.ObjectKey) == "" {
		writeError(w, http.StatusBadRequest, codeInvalidRequest, "object_key is required")
		return
	}

	created, err := h.service.AttachUpload(r.Context(), storymedia.AttachUploadInput{
		StoryID:    r.PathValue("id"),
		UploaderID: userID,
		ObjectKey:  request.ObjectKey,
		Source:     storymedia.MediaSource(request.Source),
		Width:      request.Width,
		Height:     request.Height,
		DurationMS: request.DurationMS,
	})
	if err != nil {
		h.writeServiceError(w, r, err)
		return
	}

	// The detail's media array just changed, exactly as on the multipart path.
	h.details.InvalidateForStory(r.Context(), r.PathValue("id"))

	// 202: the row exists and its job is queued; the worker produces the
	// derivatives and flips the row to ready.
	writeJSON(w, http.StatusAccepted, storyMediaEnvelope{Media: newStoryMediaResponse(created, r.PathValue("id"))})
}

// isJSONRequest reports whether the request carries a JSON body rather than the
// multipart form the pre-KNOT-040 client sends.
func isJSONRequest(r *http.Request) bool {
	return strings.EqualFold(storymedia.TrimMimeType(r.Header.Get("Content-Type")), "application/json")
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
//
// The optional `variant` parameter selects the rendition: `display` (an image's
// 1200px display), `stream` (a video's 1080p MP4), `thumb` (the 400px WebP feed
// thumbnail), or — for a row written before KNOT-037-fix7 — `medium` or `poster`.
// An absent variant means the item's full rendition, chosen from its media type.
// An unknown value is a 400 rather than a silent full-size response, because
// answering with a full-size object would send exactly the bytes the caller asked
// to avoid.
//
// Only derivatives are served (KNOT-037-fix10): the originals are not kept, so a
// variant the row cannot answer is answered 404. There is no fallback to the
// uploaded file — it either does not exist or is the multi-megabyte object this
// contract exists to stop transferring.
//
// For `variant=thumb` and `variant=display` the `format` parameter selects the
// codec: `avif` (the default) or `webp` (KNOT-037-fix6). A codec the item does not
// have falls back to the other one, so the address a client holds keeps working —
// a display produced before KNOT-038-fix3 has only the AVIF. `format` with any
// other variant is rejected rather than ignored.
//
// A row whose derivatives are still being generated answers **202 Accepted** with
// no body (and `Retry-After`), because the bytes are coming: reporting 404 would
// tell the client the media never existed. A row whose processing failed answers
// 404, because nothing is coming (KNOT-037-fix7).
func (h *StoryMediaHandler) Content(w http.ResponseWriter, r *http.Request) {
	query := r.URL.Query()

	variant, ok := storymedia.ParseVariant(query.Get("variant"))
	if !ok {
		writeError(w, http.StatusBadRequest, codeInvalidRequest,
			"variant must be thumb, medium, or poster")
		return
	}

	format, ok := storymedia.ParseFormat(query.Get("format"))
	if !ok {
		writeError(w, http.StatusBadRequest, codeInvalidRequest,
			"format must be avif or webp")
		return
	}
	// A format only means something for a rendition that exists in two codecs: the
	// thumbnail and the image display. Rejecting any other combination rather than
	// ignoring it keeps a client from believing it negotiated a codec
	// (KNOT-037-fix6, KNOT-038-fix3).
	if format != "" && variant != storymedia.VariantThumb && variant != storymedia.VariantDisplay {
		writeError(w, http.StatusBadRequest, codeInvalidRequest,
			"format may only be used with variant=thumb or display")
		return
	}

	body, total, mimeType, err := h.service.StreamMedia(r.Context(), r.PathValue("mid"), variant, format, r)
	if err != nil {
		switch {
		case errors.Is(err, storymedia.ErrProcessing):
			w.Header().Set("Retry-After", storyMediaRetryAfter)
			w.WriteHeader(http.StatusAccepted)
			return
		case errors.Is(err, storymedia.ErrProcessingFailed):
			writeError(w, http.StatusNotFound, codeNotFound, "media is not available")
			return
		}

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

	// The story's cached detail still lists the deleted item, so it goes too. The
	// path names the story, which is why no lookup is needed (KNOT-036-fix4).
	h.details.InvalidateForStory(r.Context(), r.PathValue("id"))

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
	case errors.Is(err, storymedia.ErrMediaLimit):
		writeError(w, http.StatusBadRequest, codeValidation, storymedia.MediaLimitMessage)
	case errors.As(err, &validation):
		writeError(w, http.StatusBadRequest, codeValidation, fmt.Sprintf("%s %s", validation.Field, validation.Message))
	case errors.Is(err, storymedia.ErrForbidden):
		writeError(w, http.StatusForbidden, codeForbidden, "you may not change this story's media")
	case errors.Is(err, storymedia.ErrUploadNotFound):
		// The URL was signed but nothing was uploaded to it, or the object was
		// removed. There is no row to answer 404 for, so it is a bad request.
		writeError(w, http.StatusBadRequest, codeInvalidRequest,
			"object_key does not name an uploaded file")
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
		Status:       string(media.Status),
		URL:          storyMediaFullURL(storyID, media),
		// A video has no cover: it autoplays, so a thumbnail would be a second copy
		// of the same content (KNOT-037-fix7).
		ThumbnailURL:         storyMediaImageThumbnailURL(storyID, media),
		ThumbnailURLFallback: storyMediaThumbnailFallbackURL(storyID, media),
		MediumURL:            storyMediaVariantURL(storyID, media.ID, media.MediumKey, storymedia.VariantMedium),
		DisplayWebPURL:       storyMediaDisplayWebPURL(storyID, media),
	}
}

// storyMediaFullURL is the address of an item's full rendition: the display AVIF
// for an image, the stream MP4 for a video.
//
// It is what a full-screen viewer opens. A row written before KNOT-037-fix7 has no
// such rendition and answers with its original upload instead, so the address is
// correct for both generations of row.
func storyMediaFullURL(storyID string, media storymedia.StoryMedia) string {
	variant := storymedia.VariantDisplay
	if media.MediaType == storymedia.MediaTypeVideo {
		variant = storymedia.VariantStream
	}

	return storyMediaURL(storyID, media.ID) + "?variant=" + string(variant)
}

// storyMediaImageThumbnailURL is the feed card's image address, or nil for a video.
func storyMediaImageThumbnailURL(storyID string, media storymedia.StoryMedia) *string {
	if media.MediaType != storymedia.MediaTypeImage {
		return nil
	}
	if _, ok := media.DerivativeKey(storymedia.VariantThumb); !ok {
		return nil
	}

	url := storyMediaURL(storyID, media.ID) + "?variant=" + string(storymedia.VariantThumb)
	return &url
}

// storyMediaDisplayWebPURL is the WebP address of an image's display, or nil when
// the row has no WebP display.
//
// It exists because AVIF is native only on Android 12+ and iOS 16+: a device below
// those decodes nothing and draws a blank rectangle, with no error to react to.
// WebP is the same picture at the same size in a codec every supported device can
// read, so the client is given an address it can actually use rather than being
// asked to sniff support (KNOT-038-fix3).
func storyMediaDisplayWebPURL(storyID string, media storymedia.StoryMedia) *string {
	if media.MediaType != storymedia.MediaTypeImage {
		return nil
	}
	if _, ok := media.DisplayWebPKey(); !ok {
		return nil
	}

	url := storyMediaURL(storyID, media.ID) +
		"?variant=" + string(storymedia.VariantDisplay) +
		"&format=" + string(storymedia.FormatWebP)
	return &url
}

// storyMediaThumbnailFallbackURL is the AVIF thumbnail's WebP sibling, which only
// a row written by KNOT-037-fix6 has. A thumbnail produced since KNOT-037-fix7 is
// already WebP, so there is no second file to offer.
func storyMediaThumbnailFallbackURL(storyID string, media storymedia.StoryMedia) *string {
	if media.ThumbnailKey == nil || !strings.HasSuffix(*media.ThumbnailKey, ".avif") {
		return nil
	}
	if _, ok := media.ThumbnailWebPKey(); !ok {
		return nil
	}

	url := storyMediaURL(storyID, media.ID) + "?variant=" + string(storymedia.VariantThumb) + "&format=webp"
	return &url
}

// storyMediaURL builds the relative path a client fetches the bytes from.
func storyMediaURL(storyID, mediaID string) string {
	return "/stories/" + storyID + "/media/" + mediaID + "/content"
}

// storyMediaVariantURL builds the address of one rendition, or nil when the item
// has none.
//
// The key is what proves a rendition exists; its value is never sent. A nil answer
// is what tells the client to fall back to URL (KNOT-037).
func storyMediaVariantURL(storyID, mediaID string, key *string, variant storymedia.Variant) *string {
	if key == nil || *key == "" {
		return nil
	}

	url := storyMediaURL(storyID, mediaID) + "?variant=" + string(variant)
	return &url
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
