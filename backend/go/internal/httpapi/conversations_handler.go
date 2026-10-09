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
	GetComment(ctx context.Context, id string) (conversations.Comment, error)
	ListComments(ctx context.Context, versionID string, rawCursor string, limit int) ([]conversations.Comment, string, error)
	CreateBridge(ctx context.Context, in conversations.CreateBridgeInput) (conversations.Bridge, conversations.Comment, conversations.Comment, error)
	GetBridge(ctx context.Context, id string) (conversations.Bridge, error)
	ListBridgesForComment(ctx context.Context, commentID string) ([]conversations.Bridge, error)
}

// ConversationsHandler serves the comment and bridge endpoints.
type ConversationsHandler struct {
	service ConversationsService
	authors AuthorLookup
	rooted  RootedLookup
	logger  *slog.Logger
}

// NewConversationsHandler returns a handler backed by service. The author lookup
// names and pictures every author named by a response (one batched read per
// response), and the rooted lookup attaches each author's inline Rooted summary.
func NewConversationsHandler(service ConversationsService, authors AuthorLookup, rooted RootedLookup, logger *slog.Logger) (*ConversationsHandler, error) {
	if service == nil {
		return nil, fmt.Errorf("httpapi: conversations handler requires a service")
	}
	if authors == nil {
		return nil, fmt.Errorf("httpapi: conversations handler requires an author lookup")
	}
	if rooted == nil {
		return nil, fmt.Errorf("httpapi: conversations handler requires a rooted lookup")
	}
	if logger == nil {
		return nil, fmt.Errorf("httpapi: conversations handler requires a logger")
	}
	return &ConversationsHandler{service: service, authors: authors, rooted: rooted, logger: logger}, nil
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
	// AuthorDisplayName and AuthorAvatarURL are the author's inline attribution,
	// filled by one batched lookup per response. AuthorAvatarURL is the backend
	// path to fetch an avatar from, or null when the author has none, so a client
	// renders initials (KNOT-ADR-041).
	AuthorDisplayName string  `json:"author_display_name"`
	AuthorAvatarURL   *string `json:"author_avatar_url"`
	// AuthorRooted is the comment author's primary public Rooted signal, or null
	// when they have none. It is a summary (place and duration only), attached at
	// the HTTP layer; see KNOT-ADR-017.
	AuthorRooted *rootedSummary `json:"author_rooted"`
}

// commentEnvelope wraps a single comment so the response shape can gain sibling
// fields later without breaking clients.
type commentEnvelope struct {
	Comment commentResponse `json:"comment"`
}

// commentDetailResponse is the GET /comments/{id} projection of a comment. It is
// a separate type from commentResponse because it also carries story_id, the story
// the comment's version belongs to, which the service resolves so a client can open
// the comment's thread without a second lookup. It carries the same author
// attribution as every other comment response (KNOT-ADR-041).
type commentDetailResponse struct {
	ID        string    `json:"id"`
	VersionID string    `json:"version_id"`
	StoryID   string    `json:"story_id"`
	AuthorID  string    `json:"author_id"`
	Language  string    `json:"language"`
	Body      string    `json:"body"`
	CreatedAt time.Time `json:"created_at"`
	UpdatedAt time.Time `json:"updated_at"`
	// AuthorDisplayName and AuthorAvatarURL are the author's inline attribution,
	// filled by one batched lookup. AuthorAvatarURL is null when the author has no
	// avatar (KNOT-ADR-041).
	AuthorDisplayName string  `json:"author_display_name"`
	AuthorAvatarURL   *string `json:"author_avatar_url"`
	// AuthorRooted is the comment author's primary public Rooted signal, or null.
	AuthorRooted *rootedSummary `json:"author_rooted"`
}

// commentDetailEnvelope wraps a single resolved comment.
type commentDetailEnvelope struct {
	Comment commentDetailResponse `json:"comment"`
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
	// AuthorDisplayName and AuthorAvatarURL are the bridger's inline attribution,
	// filled by one batched lookup per response. AuthorAvatarURL is the backend
	// path to fetch an avatar from, or null when the bridger has none (KNOT-ADR-041).
	AuthorDisplayName string  `json:"author_display_name"`
	AuthorAvatarURL   *string `json:"author_avatar_url"`
	// AuthorRooted is the bridger's primary public Rooted signal, or null when they
	// have none. It is a summary (place and duration only), attached at the HTTP
	// layer; see KNOT-ADR-017.
	AuthorRooted *rootedSummary `json:"author_rooted"`
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

	response := newCommentResponse(created)
	response.AuthorRooted = authorRootedSummaries(r.Context(), h.rooted, h.logger, []string{created.AuthorID})[created.AuthorID]
	response.AuthorDisplayName, response.AuthorAvatarURL = authorFields(r.Context(), h.authors, h.logger, created.AuthorID)

	writeJSON(w, http.StatusCreated, commentEnvelope{Comment: response})
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
	authorIDs := make([]string, 0, len(page))
	for _, comment := range page {
		authorIDs = append(authorIDs, comment.AuthorID)
	}
	// One batched read per lookup names and pictures every author on the page.
	summaries := authorRootedSummaries(r.Context(), h.rooted, h.logger, authorIDs)
	attributions := authorAttributions(r.Context(), h.authors, h.logger, authorIDs)
	for _, comment := range page {
		item := newCommentResponse(comment)
		item.AuthorRooted = summaries[comment.AuthorID]
		attribution := attributions[comment.AuthorID]
		item.AuthorDisplayName = attribution.DisplayName
		item.AuthorAvatarURL = optionalString(attribution.AvatarURL)
		items = append(items, item)
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

	bridgeItem := newBridgeResponse(bridge)
	sourceItem := newCommentResponse(source)
	targetItem := newCommentResponse(target)
	authorIDs := []string{bridge.AuthorID, source.AuthorID, target.AuthorID}
	summaries := authorRootedSummaries(r.Context(), h.rooted, h.logger, authorIDs)
	attributions := authorAttributions(r.Context(), h.authors, h.logger, authorIDs)
	bridgeItem.AuthorRooted = summaries[bridge.AuthorID]
	sourceItem.AuthorRooted = summaries[source.AuthorID]
	targetItem.AuthorRooted = summaries[target.AuthorID]
	bridgeItem.AuthorDisplayName, bridgeItem.AuthorAvatarURL = attributions[bridge.AuthorID].DisplayName, optionalString(attributions[bridge.AuthorID].AvatarURL)
	sourceItem.AuthorDisplayName, sourceItem.AuthorAvatarURL = attributions[source.AuthorID].DisplayName, optionalString(attributions[source.AuthorID].AvatarURL)
	targetItem.AuthorDisplayName, targetItem.AuthorAvatarURL = attributions[target.AuthorID].DisplayName, optionalString(attributions[target.AuthorID].AvatarURL)

	writeJSON(w, http.StatusCreated, createBridgeResponse{
		Bridge:        bridgeItem,
		SourceComment: sourceItem,
		TargetComment: targetItem,
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
	authorIDs := make([]string, 0, len(bridges))
	for _, bridge := range bridges {
		authorIDs = append(authorIDs, bridge.AuthorID)
	}
	summaries := authorRootedSummaries(r.Context(), h.rooted, h.logger, authorIDs)
	attributions := authorAttributions(r.Context(), h.authors, h.logger, authorIDs)
	for _, bridge := range bridges {
		item := newBridgeResponse(bridge)
		item.AuthorRooted = summaries[bridge.AuthorID]
		attribution := attributions[bridge.AuthorID]
		item.AuthorDisplayName = attribution.DisplayName
		item.AuthorAvatarURL = optionalString(attribution.AvatarURL)
		items = append(items, item)
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

	response := newBridgeResponse(bridge)
	response.AuthorRooted = authorRootedSummaries(r.Context(), h.rooted, h.logger, []string{bridge.AuthorID})[bridge.AuthorID]
	response.AuthorDisplayName, response.AuthorAvatarURL = authorFields(r.Context(), h.authors, h.logger, bridge.AuthorID)

	writeJSON(w, http.StatusOK, bridgeEnvelope{Bridge: response})
}

// GetComment handles GET /comments/{id}. The route is public.
//
// It resolves a comment id to the comment, its version, and its story, so a
// notification tap can open the comment's thread. It is the only comment route
// that requires the version's story to be resolved, hence its own response type.
func (h *ConversationsHandler) GetComment(w http.ResponseWriter, r *http.Request) {
	comment, err := h.service.GetComment(r.Context(), r.PathValue("id"))
	if err != nil {
		h.writeServiceError(w, r, err, "comment not found")
		return
	}

	response := newCommentDetailResponse(comment)
	response.AuthorRooted = authorRootedSummaries(r.Context(), h.rooted, h.logger, []string{comment.AuthorID})[comment.AuthorID]
	response.AuthorDisplayName, response.AuthorAvatarURL = authorFields(r.Context(), h.authors, h.logger, comment.AuthorID)

	writeJSON(w, http.StatusOK, commentDetailEnvelope{Comment: response})
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

// newCommentDetailResponse projects a resolved comment onto the wire format. It
// carries the story id the service resolved, which the thread and list
// projections do not.
func newCommentDetailResponse(comment conversations.Comment) commentDetailResponse {
	return commentDetailResponse{
		ID:        comment.ID,
		VersionID: comment.VersionID,
		StoryID:   comment.StoryID,
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
