package httpapi

import (
	"context"
	"errors"
	"fmt"
	"log/slog"
	"net/http"
	"strconv"
	"time"

	"github.com/knot/backend/internal/conversations"
)

// ConversationsService is the slice of the conversations service that the HTTP
// layer needs. Depending on an interface (rather than the concrete service)
// keeps handler tests free of a database.
type ConversationsService interface {
	CreateComment(ctx context.Context, in conversations.CreateCommentInput) (conversations.Comment, error)
	ListComments(ctx context.Context, versionID string, rawCursor string, limit int) ([]conversations.Comment, string, error)
	CreateBridge(ctx context.Context, in conversations.CreateBridgeInput) (conversations.Bridge, conversations.Comment, conversations.Comment, error)
	GetBridge(ctx context.Context, id string) (conversations.Bridge, error)
	ListBridgesForComment(ctx context.Context, commentID string) ([]conversations.Bridge, error)
}

// ConversationsHandler serves the comment and bridge endpoints.
type ConversationsHandler struct {
	service ConversationsService
	logger  *slog.Logger
}

// NewConversationsHandler returns a handler backed by service.
func NewConversationsHandler(service ConversationsService, logger *slog.Logger) (*ConversationsHandler, error) {
	if service == nil {
		return nil, fmt.Errorf("httpapi: conversations handler requires a service")
	}
	if logger == nil {
		return nil, fmt.Errorf("httpapi: conversations handler requires a logger")
	}
	return &ConversationsHandler{service: service, logger: logger}, nil
}

// createCommentRequest is the POST /versions/{id}/comments body.
//
// There is deliberately no author_id field. The commenter is taken from the
// authenticated request, so a client cannot comment as somebody else.
type createCommentRequest struct {
	Body     string `json:"body"`
	Language string `json:"language"`
}

// createBridgeRequest is the POST /comments/{id}/bridges body.
//
// The bridger is taken from the access token, and the source comment comes from
// the path, so neither is in the body.
type createBridgeRequest struct {
	TargetLanguage string `json:"target_language"`
	Body           string `json:"body"`
	AdaptationNote string `json:"adaptation_note"`
}

// commentResponse is the public projection of a comment.
type commentResponse struct {
	ID        string    `json:"id"`
	VersionID string    `json:"version_id"`
	AuthorID  string    `json:"author_id"`
	Language  string    `json:"language"`
	Body      string    `json:"body"`
	CreatedAt time.Time `json:"created_at"`
	UpdatedAt time.Time `json:"updated_at"`
}

// commentEnvelope wraps a single comment so the response shape can gain sibling
// fields later without breaking clients.
type commentEnvelope struct {
	Comment commentResponse `json:"comment"`
}

// commentListResponse is the GET /versions/{id}/comments body. NextCursor is the
// empty string on the last page; the field is always present.
type commentListResponse struct {
	Comments   []commentResponse `json:"comments"`
	NextCursor string            `json:"next_cursor"`
}

// bridgeResponse is the public projection of a bridge.
type bridgeResponse struct {
	ID              string    `json:"id"`
	SourceCommentID string    `json:"source_comment_id"`
	TargetCommentID string    `json:"target_comment_id"`
	AuthorID        string    `json:"author_id"`
	TargetLanguage  string    `json:"target_language"`
	AdaptationNote  *string   `json:"adaptation_note"`
	CreatedAt       time.Time `json:"created_at"`
}

// bridgeEnvelope wraps a single bridge.
type bridgeEnvelope struct {
	Bridge bridgeResponse `json:"bridge"`
}

// bridgeListResponse is the GET /comments/{id}/bridges body. Bridges is always an
// array, never null.
type bridgeListResponse struct {
	Bridges []bridgeResponse `json:"bridges"`
}

// createBridgeResponse is the POST /comments/{id}/bridges body: the bridge plus
// the two comments it joins, so a client does not have to fetch them.
type createBridgeResponse struct {
	Bridge        bridgeResponse  `json:"bridge"`
	SourceComment commentResponse `json:"source_comment"`
	TargetComment commentResponse `json:"target_comment"`
}

// CreateComment handles POST /versions/{id}/comments. The route is protected, so
// the author comes from the context rather than from the body.
func (h *ConversationsHandler) CreateComment(w http.ResponseWriter, r *http.Request) {
	authorID, ok := UserIDFromContext(r.Context())
	if !ok {
		// Unreachable while the route is wrapped by AuthMiddleware. It is kept
		// as a fail-closed guard so a future wiring mistake cannot create an
		// authorless comment.
		writeError(w, http.StatusUnauthorized, codeUnauthorized, "authentication required")
		return
	}

	var body createCommentRequest
	if err := decodeJSON(w, r, &body); err != nil {
		writeDecodeError(w, r, h.logger, err)
		return
	}

	created, err := h.service.CreateComment(r.Context(), conversations.CreateCommentInput{
		VersionID: r.PathValue("id"),
		AuthorID:  authorID,
		Language:  body.Language,
		Body:      body.Body,
	})
	if err != nil {
		h.writeServiceError(w, r, err, "version not found")
		return
	}

	writeJSON(w, http.StatusCreated, commentEnvelope{Comment: newCommentResponse(created)})
}

// ListComments handles GET /versions/{id}/comments. The route is public.
func (h *ConversationsHandler) ListComments(w http.ResponseWriter, r *http.Request) {
	query := r.URL.Query()

	limit, err := parseCommentsLimit(query.Get("limit"))
	if err != nil {
		h.writeServiceError(w, r, err, "version not found")
		return
	}

	page, next, err := h.service.ListComments(r.Context(), r.PathValue("id"), query.Get("cursor"), limit)
	if err != nil {
		h.writeServiceError(w, r, err, "version not found")
		return
	}

	items := make([]commentResponse, 0, len(page))
	for _, comment := range page {
		items = append(items, newCommentResponse(comment))
	}

	writeJSON(w, http.StatusOK, commentListResponse{Comments: items, NextCursor: next})
}

// CreateBridge handles POST /comments/{id}/bridges. The route is protected.
func (h *ConversationsHandler) CreateBridge(w http.ResponseWriter, r *http.Request) {
	authorID, ok := UserIDFromContext(r.Context())
	if !ok {
		writeError(w, http.StatusUnauthorized, codeUnauthorized, "authentication required")
		return
	}

	var body createBridgeRequest
	if err := decodeJSON(w, r, &body); err != nil {
		writeDecodeError(w, r, h.logger, err)
		return
	}

	bridge, source, target, err := h.service.CreateBridge(r.Context(), conversations.CreateBridgeInput{
		SourceCommentID: r.PathValue("id"),
		AuthorID:        authorID,
		TargetLanguage:  body.TargetLanguage,
		Body:            body.Body,
		AdaptationNote:  body.AdaptationNote,
	})
	if err != nil {
		h.writeServiceError(w, r, err, "source comment not found")
		return
	}

	writeJSON(w, http.StatusCreated, createBridgeResponse{
		Bridge:        newBridgeResponse(bridge),
		SourceComment: newCommentResponse(source),
		TargetComment: newCommentResponse(target),
	})
}

// ListBridges handles GET /comments/{id}/bridges. The route is public.
func (h *ConversationsHandler) ListBridges(w http.ResponseWriter, r *http.Request) {
	bridges, err := h.service.ListBridgesForComment(r.Context(), r.PathValue("id"))
	if err != nil {
		h.writeServiceError(w, r, err, "comment not found")
		return
	}

	items := make([]bridgeResponse, 0, len(bridges))
	for _, bridge := range bridges {
		items = append(items, newBridgeResponse(bridge))
	}

	writeJSON(w, http.StatusOK, bridgeListResponse{Bridges: items})
}

// GetBridge handles GET /bridges/{id}. The route is public.
func (h *ConversationsHandler) GetBridge(w http.ResponseWriter, r *http.Request) {
	bridge, err := h.service.GetBridge(r.Context(), r.PathValue("id"))
	if err != nil {
		h.writeServiceError(w, r, err, "bridge not found")
		return
	}

	writeJSON(w, http.StatusOK, bridgeEnvelope{Bridge: newBridgeResponse(bridge)})
}

// parseCommentsLimit reads the optional "limit" query parameter.
//
// An absent limit means DefaultListLimit. A limit above MaxListLimit is clamped
// to it rather than rejected, so a client asking for "as many as you can" gets a
// usable page instead of an error. A limit that is not a positive integer is a
// validation failure: it is a malformed request, not an over-large one.
func parseCommentsLimit(raw string) (int, error) {
	if raw == "" {
		return conversations.DefaultListLimit, nil
	}

	value, err := strconv.Atoi(raw)
	if err != nil {
		return 0, &conversations.ValidationError{Field: "limit", Message: "must be an integer"}
	}
	if value < 1 {
		return 0, &conversations.ValidationError{Field: "limit", Message: "must be at least 1"}
	}
	if value > conversations.MaxListLimit {
		return conversations.MaxListLimit, nil
	}

	return value, nil
}

// writeServiceError maps domain errors onto HTTP status codes. Only errors we
// recognise as safe are described to the client; everything else becomes a
// generic 500 and is logged with the request id. notFoundMessage lets each route
// name what was not found.
func (h *ConversationsHandler) writeServiceError(w http.ResponseWriter, r *http.Request, err error, notFoundMessage string) {
	var validation *conversations.ValidationError

	switch {
	case errors.As(err, &validation):
		writeError(w, http.StatusBadRequest, codeValidation, fmt.Sprintf("%s %s", validation.Field, validation.Message))
	case errors.Is(err, conversations.ErrNotFound):
		writeError(w, http.StatusNotFound, codeNotFound, notFoundMessage)
	default:
		h.logger.ErrorContext(
			r.Context(),
			"conversations request failed",
			slog.String("request_id", RequestIDFromContext(r.Context())),
			slog.String("path", r.URL.Path),
			slog.String("error", err.Error()),
		)
		writeError(w, http.StatusInternalServerError, codeInternal, "internal server error")
	}
}

// newCommentResponse projects a domain comment onto the wire format.
func newCommentResponse(comment conversations.Comment) commentResponse {
	return commentResponse{
		ID:        comment.ID,
		VersionID: comment.VersionID,
		AuthorID:  comment.AuthorID,
		Language:  comment.Language,
		Body:      comment.Body,
		CreatedAt: comment.CreatedAt,
		UpdatedAt: comment.UpdatedAt,
	}
}

// newBridgeResponse projects a domain bridge onto the wire format. An empty
// adaptation note becomes JSON null via optionalString.
func newBridgeResponse(bridge conversations.Bridge) bridgeResponse {
	return bridgeResponse{
		ID:              bridge.ID,
		SourceCommentID: bridge.SourceCommentID,
		TargetCommentID: bridge.TargetCommentID,
		AuthorID:        bridge.AuthorID,
		TargetLanguage:  bridge.TargetLanguage,
		AdaptationNote:  optionalString(bridge.AdaptationNote),
		CreatedAt:       bridge.CreatedAt,
	}
}
