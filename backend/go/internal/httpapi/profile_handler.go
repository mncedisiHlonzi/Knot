package httpapi

import (
	"context"
	"errors"
	"fmt"
	"log/slog"
	"net/http"
	"strconv"
	"time"

	"github.com/knot/backend/internal/profile"
)

// ProfileService is the slice of the profile service that the HTTP layer needs.
// Depending on an interface (rather than the concrete service) keeps handler tests
// free of a database.
type ProfileService interface {
	GetProfile(ctx context.Context, userID string, rawCursor string, limit int) (profile.Profile, string, error)
}

// ProfileHandler serves the public profile-wall endpoint.
type ProfileHandler struct {
	service ProfileService
	rooted  RootedLookup
	logger  *slog.Logger
}

// NewProfileHandler returns a handler backed by service. The rooted lookup
// attaches the wall owner's inline Rooted summary, exactly as every other content
// response does.
func NewProfileHandler(service ProfileService, rooted RootedLookup, logger *slog.Logger) (*ProfileHandler, error) {
	if service == nil {
		return nil, fmt.Errorf("httpapi: profile handler requires a service")
	}
	if rooted == nil {
		return nil, fmt.Errorf("httpapi: profile handler requires a rooted lookup")
	}
	if logger == nil {
		return nil, fmt.Errorf("httpapi: profile handler requires a logger")
	}
	return &ProfileHandler{service: service, rooted: rooted, logger: logger}, nil
}

// profileUserResponse is the public identity header of a wall.
//
// AvatarURL is a pointer so an author with no avatar is JSON null, and Rooted is
// the same inline summary every content response carries (KNOT-ADR-017). The
// owner's email, phone, and preferred languages are deliberately absent: this is a
// public wall, not the account.
type profileUserResponse struct {
	ID          string         `json:"id"`
	DisplayName string         `json:"display_name"`
	AvatarURL   *string        `json:"avatar_url"`
	Rooted      *rootedSummary `json:"rooted"`
	JoinedAt    time.Time      `json:"joined_at"`
}

// profileActivityResponse is one entry on a wall.
//
// Every activity is authored by the wall's owner, so there is no per-activity
// author here: the name and avatar are on the User header above. The Payload is
// the kind-specific context, and its shape follows the activity's kind
// (KNOT-ADR-042).
type profileActivityResponse struct {
	Kind      string          `json:"kind"`
	ID        string          `json:"id"`
	CreatedAt time.Time       `json:"created_at"`
	Payload   profile.Payload `json:"payload"`
}

// profileResponse is the GET /users/{id}/profile body. Activities is always an
// array, never null, and NextCursor is the empty string on the last page.
type profileResponse struct {
	User       profileUserResponse       `json:"user"`
	Activities []profileActivityResponse `json:"activities"`
	NextCursor string                    `json:"next_cursor"`
}

// Get handles GET /users/{id}/profile. The route is public.
func (h *ProfileHandler) Get(w http.ResponseWriter, r *http.Request) {
	query := r.URL.Query()

	limit, err := parseProfileLimit(query.Get("limit"))
	if err != nil {
		h.writeServiceError(w, r, err, "user not found")
		return
	}

	result, next, err := h.service.GetProfile(r.Context(), r.PathValue("id"), query.Get("cursor"), limit)
	if err != nil {
		h.writeServiceError(w, r, err, "user not found")
		return
	}

	if result.User == nil {
		// The service never returns a nil owner with a nil error; this is a
		// fail-closed guard so a wiring mistake cannot panic the request path.
		h.logger.ErrorContext(
			r.Context(),
			"profile response carried no user",
			slog.String("request_id", RequestIDFromContext(r.Context())),
			slog.String("path", r.URL.Path),
		)
		writeError(w, http.StatusInternalServerError, codeInternal, "internal server error")
		return
	}

	items := make([]profileActivityResponse, 0, len(result.Activities))
	for _, activity := range result.Activities {
		items = append(items, profileActivityResponse{
			Kind:      string(activity.Kind),
			ID:        activity.ID,
			CreatedAt: activity.CreatedAt,
			Payload:   activity.Payload,
		})
	}

	// The owner is named and pictured in one batched lookup, the same helper the
	// content handlers use.
	ownerRooted := authorRootedSummaries(r.Context(), h.rooted, h.logger, []string{result.User.ID})[result.User.ID]

	writeJSON(w, http.StatusOK, profileResponse{
		User: profileUserResponse{
			ID:          result.User.ID,
			DisplayName: result.User.DisplayName,
			AvatarURL:   optionalString(avatarPathFor(result.User)),
			Rooted:      ownerRooted,
			JoinedAt:    result.User.CreatedAt,
		},
		Activities: items,
		NextCursor: next,
	})
}

// parseProfileLimit reads the optional "limit" query parameter.
//
// An absent limit means the profile domain's default. A limit above the maximum is
// clamped rather than rejected, so a client asking for "as many as you can" gets a
// usable page. A limit that is not a positive integer is a validation failure: it
// is a malformed request, not an over-large one.
func parseProfileLimit(raw string) (int, error) {
	if raw == "" {
		return profile.DefaultListLimit, nil
	}

	value, err := strconv.Atoi(raw)
	if err != nil {
		return 0, &profile.ValidationError{Field: "limit", Message: "must be an integer"}
	}
	if value < 1 {
		return 0, &profile.ValidationError{Field: "limit", Message: "must be at least 1"}
	}
	if value > profile.MaxListLimit {
		return profile.MaxListLimit, nil
	}

	return value, nil
}

// writeServiceError maps domain errors onto HTTP status codes. Only errors we
// recognise as safe are described to the client; everything else becomes a generic
// 500 and is logged with the request id.
func (h *ProfileHandler) writeServiceError(w http.ResponseWriter, r *http.Request, err error, notFoundMessage string) {
	var validation *profile.ValidationError

	switch {
	case errors.As(err, &validation):
		writeError(w, http.StatusBadRequest, codeValidation, fmt.Sprintf("%s %s", validation.Field, validation.Message))
	case errors.Is(err, profile.ErrNotFound):
		writeError(w, http.StatusNotFound, codeNotFound, notFoundMessage)
	default:
		h.logger.ErrorContext(
			r.Context(),
			"profile request failed",
			slog.String("request_id", RequestIDFromContext(r.Context())),
			slog.String("path", r.URL.Path),
			slog.String("error", err.Error()),
		)
		writeError(w, http.StatusInternalServerError, codeInternal, "internal server error")
	}
}
