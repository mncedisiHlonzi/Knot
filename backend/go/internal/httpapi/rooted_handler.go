package httpapi

import (
	"context"
	"errors"
	"fmt"
	"log/slog"
	"net/http"
	"time"

	"github.com/knot/backend/internal/rooted"
)

// RootedService is the slice of the rooted service that the HTTP layer needs.
// Depending on an interface (rather than the concrete service) keeps handler tests
// free of a database.
type RootedService interface {
	SetSignal(ctx context.Context, userID string, in rooted.SetSignalInput) (rooted.Signal, error)
	GetMySignals(ctx context.Context, userID string) ([]rooted.Signal, error)
	GetPublicSignals(ctx context.Context, userID string) ([]rooted.Signal, error)
	BatchGetPrimaryPublicSignals(ctx context.Context, userIDs []string) (map[string]*rooted.Signal, error)
}

// RootedHandler serves the Rooted endpoints.
type RootedHandler struct {
	service RootedService
	logger  *slog.Logger
}

// NewRootedHandler returns a handler backed by service.
func NewRootedHandler(service RootedService, logger *slog.Logger) (*RootedHandler, error) {
	if service == nil {
		return nil, fmt.Errorf("httpapi: rooted handler requires a service")
	}
	if logger == nil {
		return nil, fmt.Errorf("httpapi: rooted handler requires a logger")
	}
	return &RootedHandler{service: service, logger: logger}, nil
}

// setSignalRequest is the POST /users/me/rooted body.
//
// IsPublic is a pointer so that an absent field defaults to public, which is the
// visibility default for a Rooted signal: a signal is public unless its owner
// explicitly hides it. Sending "is_public": false hides it.
type setSignalRequest struct {
	Place          string                `json:"place"`
	Latitude       *float64              `json:"latitude"`
	Longitude      *float64              `json:"longitude"`
	PlaceCountry   string                `json:"place_country"`
	DurationBucket rooted.DurationBucket `json:"duration_bucket"`
	IsPublic       *bool                 `json:"is_public"`
}

// signalResponse is the public projection of a Rooted signal. It is an explicit
// type, not the domain Signal, so the wire format is a deliberate choice.
type signalResponse struct {
	ID             string                `json:"id"`
	UserID         string                `json:"user_id"`
	Place          string                `json:"place"`
	Latitude       *float64              `json:"latitude"`
	Longitude      *float64              `json:"longitude"`
	PlaceCountry   *string               `json:"place_country"`
	DurationBucket rooted.DurationBucket `json:"duration_bucket"`
	IsPublic       bool                  `json:"is_public"`
	IsPrimary      bool                  `json:"is_primary"`
	CreatedAt      time.Time             `json:"created_at"`
	UpdatedAt      time.Time             `json:"updated_at"`
}

// signalEnvelope wraps a single signal, so the response shape can gain sibling
// fields later without breaking clients.
type signalEnvelope struct {
	Signal signalResponse `json:"signal"`
}

// signalListResponse is the body of both GET /users/me/rooted and
// GET /users/{id}/rooted. Signals is always an array, never null.
type signalListResponse struct {
	Signals []signalResponse `json:"signals"`
}

// SetSignal handles POST /users/me/rooted. The route is protected, so the owner
// comes from the context rather than from the body.
func (h *RootedHandler) SetSignal(w http.ResponseWriter, r *http.Request) {
	userID, ok := UserIDFromContext(r.Context())
	if !ok {
		// Unreachable while the route is wrapped by AuthMiddleware. It is kept as
		// a fail-closed guard so a future wiring mistake cannot write a signal
		// with no owner.
		writeError(w, http.StatusUnauthorized, codeUnauthorized, "authentication required")
		return
	}

	var body setSignalRequest
	if err := decodeJSON(w, r, &body); err != nil {
		writeDecodeError(w, r, h.logger, err)
		return
	}

	isPublic := true
	if body.IsPublic != nil {
		isPublic = *body.IsPublic
	}

	signal, err := h.service.SetSignal(r.Context(), userID, rooted.SetSignalInput{
		Place:          body.Place,
		Latitude:       body.Latitude,
		Longitude:      body.Longitude,
		PlaceCountry:   body.PlaceCountry,
		DurationBucket: body.DurationBucket,
		IsPublic:       isPublic,
	})
	if err != nil {
		h.writeServiceError(w, r, err, "user not found")
		return
	}

	writeJSON(w, http.StatusOK, signalEnvelope{Signal: newSignalResponse(signal)})
}

// GetMySignals handles GET /users/me/rooted. It returns the authenticated user's
// own signals, including any that are hidden from public read.
func (h *RootedHandler) GetMySignals(w http.ResponseWriter, r *http.Request) {
	userID, ok := UserIDFromContext(r.Context())
	if !ok {
		writeError(w, http.StatusUnauthorized, codeUnauthorized, "authentication required")
		return
	}

	signals, err := h.service.GetMySignals(r.Context(), userID)
	if err != nil {
		h.writeServiceError(w, r, err, "user not found")
		return
	}

	writeJSON(w, http.StatusOK, signalListResponse{Signals: newSignalResponses(signals)})
}

// GetUserSignals handles GET /users/{id}/rooted. The route is public, and only a
// user's public signals are returned. An unknown user is a 404, so an empty list
// means "this user has declared nothing public" rather than "no such user".
func (h *RootedHandler) GetUserSignals(w http.ResponseWriter, r *http.Request) {
	signals, err := h.service.GetPublicSignals(r.Context(), r.PathValue("id"))
	if err != nil {
		h.writeServiceError(w, r, err, "user not found")
		return
	}

	writeJSON(w, http.StatusOK, signalListResponse{Signals: newSignalResponses(signals)})
}

// writeServiceError maps domain errors onto HTTP status codes. Only errors we
// recognise as safe are described to the client; everything else becomes a generic
// 500 and is logged with the request id.
func (h *RootedHandler) writeServiceError(w http.ResponseWriter, r *http.Request, err error, notFoundMessage string) {
	var validation *rooted.ValidationError

	switch {
	case errors.As(err, &validation):
		writeError(w, http.StatusBadRequest, codeValidation, fmt.Sprintf("%s %s", validation.Field, validation.Message))
	case errors.Is(err, rooted.ErrUserNotFound):
		writeError(w, http.StatusNotFound, codeNotFound, notFoundMessage)
	default:
		h.logger.ErrorContext(
			r.Context(),
			"rooted request failed",
			slog.String("request_id", RequestIDFromContext(r.Context())),
			slog.String("path", r.URL.Path),
			slog.String("error", err.Error()),
		)
		writeError(w, http.StatusInternalServerError, codeInternal, "internal server error")
	}
}

// newSignalResponse projects a domain signal onto the wire format.
func newSignalResponse(signal rooted.Signal) signalResponse {
	return signalResponse{
		Latitude:       signal.Latitude,
		Longitude:      signal.Longitude,
		PlaceCountry:   signal.PlaceCountry,
		ID:             signal.ID,
		UserID:         signal.UserID,
		Place:          signal.Place,
		DurationBucket: signal.DurationBucket,
		IsPublic:       signal.IsPublic,
		IsPrimary:      signal.IsPrimary,
		CreatedAt:      signal.CreatedAt,
		UpdatedAt:      signal.UpdatedAt,
	}
}

// newSignalResponses projects a slice of domain signals, always emitting an array
// rather than null.
func newSignalResponses(signals []rooted.Signal) []signalResponse {
	out := make([]signalResponse, 0, len(signals))
	for _, signal := range signals {
		out = append(out, newSignalResponse(signal))
	}
	return out
}
