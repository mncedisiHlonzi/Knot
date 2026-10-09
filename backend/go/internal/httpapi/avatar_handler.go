package httpapi

import (
	"bytes"
	"context"
	"crypto/rand"
	"errors"
	"fmt"
	"io"
	"log/slog"
	"net/http"
	"path"
	"strings"

	"github.com/knot/backend/internal/identity"
	"github.com/knot/backend/internal/storage"
)

// codeUnsupportedMediaType is returned when an uploaded avatar is not an image
// type Knot accepts.
const codeUnsupportedMediaType = "unsupported_media_type"

const (
	// maxAvatarBytes bounds an avatar upload. It is deliberately larger than
	// maxRequestBodyBytes, which caps JSON bodies: an avatar is binary and a
	// modern phone camera produces multi-megabyte JPEGs.
	maxAvatarBytes = 5 << 20 // 5 MiB

	// maxAvatarRequestOverhead is the slack allowed on top of the file itself for
	// multipart boundaries and headers. It exists so the body cap is enforced
	// while the request is being read, before ParseMultipartForm can spool a
	// large upload to temporary files.
	maxAvatarRequestOverhead = 64 << 10 // 64 KiB

	// avatarFormField is the multipart field carrying the image. Naming a field
	// rather than guessing means an unexpected form shape is a clear 400.
	avatarFormField = "file"

	// avatarCacheControl lets a browser or an intermediary reuse an avatar for an
	// hour. It is safe to cache publicly: an avatar is part of a public profile,
	// and replacing one changes the URL's v parameter.
	avatarCacheControl = "public, max-age=3600"
)

// avatarMediaTypes is the allowlist of accepted avatar formats, keyed by the
// sniffed media type and mapped to the file extension used in the object key.
//
// The type is decided by inspecting the uploaded bytes, never by the
// Content-Type the client declares, so a caller cannot have HTML served back
// from the avatar URL by mislabelling it.
var avatarMediaTypes = map[string]string{
	"image/jpeg": ".jpg",
	"image/png":  ".png",
	"image/webp": ".webp",
}

// AvatarService is the slice of the identity service that the HTTP layer needs.
// Depending on an interface (rather than the concrete service) keeps handler
// tests free of a database.
type AvatarService interface {
	UserByID(ctx context.Context, id string) (*identity.User, error)
	SetAvatarURL(ctx context.Context, userID, key string) (*identity.User, error)
}

// AvatarHandler serves the avatar endpoints.
//
// Bytes are written to and read from the object store by this backend; the
// client never talks to the object store, and the bucket is never public
// (KNOT-ADR-029).
type AvatarHandler struct {
	service AvatarService
	storage storage.Storage
	logger  *slog.Logger
}

// NewAvatarHandler returns a handler backed by service and the given object store.
func NewAvatarHandler(service AvatarService, store storage.Storage, logger *slog.Logger) (*AvatarHandler, error) {
	if service == nil {
		return nil, fmt.Errorf("httpapi: avatar handler requires a service")
	}
	if store == nil {
		return nil, fmt.Errorf("httpapi: avatar handler requires a storage backend")
	}
	if logger == nil {
		return nil, fmt.Errorf("httpapi: avatar handler requires a logger")
	}
	return &AvatarHandler{service: service, storage: store, logger: logger}, nil
}

// avatarResponse is the body of POST /users/me/avatar. It returns the whole user
// projection, so a client gets the new avatar_url without a second request.
type avatarResponse struct {
	User userResponse `json:"user"`
}

// Upload handles POST /users/me/avatar.
//
// The route is protected, so the owner comes from the context and never from the
// request, which is what stops one account writing into another account's
// namespace.
func (h *AvatarHandler) Upload(w http.ResponseWriter, r *http.Request) {
	userID, ok := UserIDFromContext(r.Context())
	if !ok {
		// Unreachable while the route is wrapped by AuthMiddleware. It is kept as
		// a fail-closed guard so a future wiring mistake cannot write an object
		// with no owner.
		writeError(w, http.StatusUnauthorized, codeUnauthorized, "authentication required")
		return
	}

	// Reject a body that declares itself too large before reading any of it. The
	// MaxBytesReader below still applies, because a chunked request may declare
	// nothing at all.
	if r.ContentLength > maxAvatarBytes+maxAvatarRequestOverhead {
		h.writeAvatarTooLarge(w, maxAvatarBytes)
		return
	}

	// Cap the body before parsing, so an oversized upload is refused while it is
	// being read rather than after it has been buffered or spooled to disk.
	r.Body = http.MaxBytesReader(w, r.Body, maxAvatarBytes+maxAvatarRequestOverhead)

	file, _, err := r.FormFile(avatarFormField)
	if err != nil {
		h.writeUploadReadError(w, r, err)
		return
	}
	defer file.Close()

	// Read one byte past the limit: if it arrives, the file is too large. This
	// bounds memory as well as size, because only the limit is ever buffered.
	data, err := io.ReadAll(io.LimitReader(file, maxAvatarBytes+1))
	if err != nil {
		var tooLarge *http.MaxBytesError
		if errors.As(err, &tooLarge) {
			h.writeAvatarTooLarge(w, maxAvatarBytes)
			return
		}
		h.writeUploadFailure(w, r, "read uploaded avatar", err)
		return
	}
	if len(data) > maxAvatarBytes {
		h.writeAvatarTooLarge(w, maxAvatarBytes)
		return
	}
	if len(data) == 0 {
		writeError(w, http.StatusBadRequest, codeInvalidRequest, "the uploaded avatar is empty")
		return
	}

	mediaType := strings.TrimSpace(strings.SplitN(http.DetectContentType(data), ";", 2)[0])
	extension, accepted := avatarMediaTypes[mediaType]
	if !accepted {
		writeError(w, http.StatusUnsupportedMediaType, codeUnsupportedMediaType,
			"avatar must be a JPEG, PNG, or WebP image")
		return
	}

	// The previous key is read before the new object is written, so a failure
	// later cannot leave the user's old avatar unreachable. It is held as a plain
	// string, because the user record is mutated by the update below.
	current, err := h.service.UserByID(r.Context(), userID)
	if err != nil {
		h.writeServiceError(w, r, err, "user not found")
		return
	}
	previousKey := current.AvatarURL
	key, err := newAvatarKey(userID, extension)
	if err != nil {
		h.writeUploadFailure(w, r, "generate avatar object key", err)
		return
	}

	if err := h.storage.Put(r.Context(), key, bytes.NewReader(data), mediaType); err != nil {
		h.writeUploadFailure(w, r, "store avatar", err)
		return
	}

	updated, err := h.service.SetAvatarURL(r.Context(), userID, key)
	if err != nil {
		// The object is now orphaned rather than referenced. Remove it so a failed
		// upload does not leak storage; the old avatar is still intact.
		h.discardObject(r.Context(), key, "roll back avatar upload")
		h.writeServiceError(w, r, err, "user not found")
		return
	}

	if previousKey != "" && previousKey != key {
		h.discardObject(r.Context(), previousKey, "delete replaced avatar")
	}

	writeJSON(w, http.StatusOK, avatarResponse{User: newUserResponse(updated)})
}

// Get handles GET /users/{id}/avatar. The route is public: an avatar is part of a
// public profile.
//
// A user id that does not exist and a user with no avatar are deliberately
// indistinguishable, so this endpoint cannot be used to test whether an account
// exists.
func (h *AvatarHandler) Get(w http.ResponseWriter, r *http.Request) {
	user, err := h.service.UserByID(r.Context(), r.PathValue("id"))
	if err != nil {
		h.writeServiceError(w, r, err, "avatar not found")
		return
	}
	if user.AvatarURL == "" {
		writeError(w, http.StatusNotFound, codeNotFound, "avatar not found")
		return
	}

	body, contentType, err := h.storage.Get(r.Context(), user.AvatarURL)
	if err != nil {
		if errors.Is(err, storage.ErrObjectNotFound) {
			// The row survived but the object did not. Reported as a plain 404,
			// because from the client's point of view there is simply no avatar.
			writeError(w, http.StatusNotFound, codeNotFound, "avatar not found")
			return
		}
		h.writeUploadFailure(w, r, "read avatar", err)
		return
	}
	defer body.Close()

	w.Header().Set("Content-Type", contentType)
	w.Header().Set("Cache-Control", avatarCacheControl)
	// The stored type is sniffed on upload, but a browser must still not be given
	// the option of reinterpreting the bytes.
	w.Header().Set("X-Content-Type-Options", "nosniff")
	w.WriteHeader(http.StatusOK)

	if _, err := io.Copy(w, body); err != nil {
		// The status line is already sent, so this cannot become an error
		// response. It is logged instead, and the connection is left to fail.
		h.logger.ErrorContext(
			r.Context(),
			"avatar response failed",
			slog.String("request_id", RequestIDFromContext(r.Context())),
			slog.String("path", r.URL.Path),
			slog.String("error", err.Error()),
		)
	}
}

// writeUploadReadError classifies a failure to open the uploaded file.
func (h *AvatarHandler) writeUploadReadError(w http.ResponseWriter, r *http.Request, err error) {
	var tooLarge *http.MaxBytesError
	switch {
	case errors.As(err, &tooLarge):
		h.writeAvatarTooLarge(w, maxAvatarBytes)
	case errors.Is(err, http.ErrMissingFile):
		writeError(w, http.StatusBadRequest, codeInvalidRequest,
			fmt.Sprintf("request must be multipart/form-data with a %q file field", avatarFormField))
	default:
		// Covers a body that is not multipart at all, and malformed multipart.
		h.logger.WarnContext(
			r.Context(),
			"rejected avatar upload",
			slog.String("request_id", RequestIDFromContext(r.Context())),
			slog.String("path", r.URL.Path),
			slog.String("error", err.Error()),
		)
		writeError(w, http.StatusBadRequest, codeInvalidRequest,
			fmt.Sprintf("request must be multipart/form-data with a %q file field", avatarFormField))
	}
}

// writeAvatarTooLarge reports an upload over the size cap.
func (h *AvatarHandler) writeAvatarTooLarge(w http.ResponseWriter, limit int64) {
	writeError(w, http.StatusRequestEntityTooLarge, codeRequestTooLarge,
		fmt.Sprintf("avatar must be at most %d bytes", limit))
}

// writeServiceError maps a domain error onto the wire format, and turns anything
// unrecognised into a logged 500. fallbackMessage describes the not-found case.
func (h *AvatarHandler) writeServiceError(w http.ResponseWriter, r *http.Request, err error, fallbackMessage string) {
	var validation *identity.ValidationError

	switch {
	case errors.As(err, &validation):
		writeError(w, http.StatusBadRequest, codeValidation, fmt.Sprintf("%s %s", validation.Field, validation.Message))
	case errors.Is(err, identity.ErrUserNotFound):
		writeError(w, http.StatusNotFound, codeNotFound, fallbackMessage)
	default:
		h.writeUploadFailure(w, r, "avatar request failed", err)
	}
}

// writeUploadFailure logs err and answers with a generic 500. Internal errors are
// never described to the client, because driver and object store messages quote
// the operation that failed.
func (h *AvatarHandler) writeUploadFailure(w http.ResponseWriter, r *http.Request, what string, err error) {
	h.logger.ErrorContext(
		r.Context(),
		what,
		slog.String("request_id", RequestIDFromContext(r.Context())),
		slog.String("path", r.URL.Path),
		slog.String("error", err.Error()),
	)
	writeError(w, http.StatusInternalServerError, codeInternal, "internal server error")
}

// discardObject removes an object without failing the request: the upload has
// already been answered, so a deletion problem is logged and left for the
// operator rather than turned into an error the client cannot act on.
func (h *AvatarHandler) discardObject(ctx context.Context, key, what string) {
	if key == "" {
		return
	}
	if err := h.storage.Delete(ctx, key); err != nil {
		h.logger.WarnContext(
			ctx,
			what,
			slog.String("key", key),
			slog.String("error", err.Error()),
		)
	}
}

// newAvatarKey mints the object key for a new avatar.
//
// The name is a fresh UUID rather than a fixed name, so uploading a new avatar
// never overwrites the bytes an in-flight response or a cache is still serving.
// It also means the key cannot be derived from the public path, which is why the
// key — and not a URL — is what users.avatar_url stores.
func newAvatarKey(userID, extension string) (string, error) {
	id, err := newUUIDv4()
	if err != nil {
		return "", err
	}
	return identity.AvatarKeyPrefix(userID) + id + extension, nil
}

// newUUIDv4 returns a random RFC 4122 version 4 UUID.
//
// It is written out here rather than pulled from a dependency: a key needs
// uniqueness within one user's prefix, and 122 random bits are enough for that.
func newUUIDv4() (string, error) {
	var raw [16]byte
	if _, err := rand.Read(raw[:]); err != nil {
		return "", fmt.Errorf("httpapi: generate avatar object id: %w", err)
	}

	raw[6] = (raw[6] & 0x0f) | 0x40 // version 4
	raw[8] = (raw[8] & 0x3f) | 0x80 // RFC 4122 variant

	return fmt.Sprintf("%x-%x-%x-%x-%x", raw[0:4], raw[4:6], raw[6:8], raw[8:10], raw[10:16]), nil
}

// avatarPathFor builds the avatar URL a client should fetch, or "" when the user
// has no avatar.
//
// The query parameter carries the object's file name, so replacing an avatar
// yields a different URL and caches keyed on the old one are simply not reused.
func avatarPathFor(user *identity.User) string {
	if user == nil || user.AvatarURL == "" {
		return ""
	}
	return "/users/" + user.ID + "/avatar?v=" + path.Base(user.AvatarURL)
}
