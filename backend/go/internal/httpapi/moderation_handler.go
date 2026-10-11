package httpapi

import (
	"context"
	"errors"
	"log/slog"
	"net/http"
	"strconv"
	"time"

	"github.com/knot/backend/internal/moderation"
)

// codeBlocked is returned when a write targets content authored by a user on
// either side of a block (KNOT-ADR-060).
const codeBlocked = "blocked"

// codeConflict is returned when a request conflicts with existing state, such as
// reporting the same entity twice.
const codeConflict = "conflict"

// ModerationService is the slice of the moderation service that the HTTP layer
// needs. Depending on an interface keeps handler tests free of a database.
type ModerationService interface {
	// Block records that blockerID blocked blockedID. It is idempotent.
	Block(ctx context.Context, blockerID, blockedID string) error
	// Unblock removes a block. It is idempotent.
	Unblock(ctx context.Context, blockerID, blockedID string) error
	// ListBlocks returns one page of the blocks blockerID created, plus the
	// cursor that resumes after it ("" when the page is the last one).
	ListBlocks(ctx context.Context, blockerID, rawCursor string, limit int) ([]moderation.Block, string, error)
	// CreateReport stores a report and its moderation case.
	CreateReport(ctx context.Context, in moderation.CreateReportInput) (moderation.Report, error)
	// ListMyReports returns one page of the caller's own reports, plus the cursor
	// that resumes after it.
	ListMyReports(ctx context.Context, reporterID, rawCursor string, limit int) ([]moderation.Report, string, error)
}

// ModerationHandler serves the report and block endpoints (KNOT-017a). The
// moderator queue and its actions ship in KNOT-017b.
type ModerationHandler struct {
	service ModerationService
	// users resolves the blocked user behind each row of GET /blocks/mine, so the
	// list names and pictures them without a query per row.
	users  AuthorLookup
	logger *slog.Logger
}

// NewModerationHandler returns a handler backed by service.
func NewModerationHandler(service ModerationService, users AuthorLookup, logger *slog.Logger) (*ModerationHandler, error) {
	if service == nil {
		return nil, errNilHandler("moderation service")
	}
	if users == nil {
		return nil, errNilHandler("moderation user lookup")
	}
	if logger == nil {
		return nil, errNilHandler("logger")
	}
	return &ModerationHandler{service: service, users: users, logger: logger}, nil
}

// createReportRequest is the POST /reports body.
type createReportRequest struct {
	EntityType string `json:"entity_type"`
	EntityID   string `json:"entity_id"`
	Category   string `json:"category"`
	Reason     string `json:"reason"`
}

// reportResponse is the wire projection of a report.
type reportResponse struct {
	ID         string    `json:"id"`
	EntityType string    `json:"entity_type"`
	EntityID   string    `json:"entity_id"`
	Category   string    `json:"category"`
	Reason     *string   `json:"reason"`
	CreatedAt  time.Time `json:"created_at"`
}

// reportsResponse is the GET /reports/mine body. Reports is always an array,
// never null.
type reportsResponse struct {
	Reports    []reportResponse `json:"reports"`
	NextCursor string           `json:"next_cursor"`
}

// CreateReport handles POST /reports.
func (h *ModerationHandler) CreateReport(w http.ResponseWriter, r *http.Request) {
	reporterID, ok := UserIDFromContext(r.Context())
	if !ok {
		writeError(w, http.StatusUnauthorized, codeUnauthorized, "authentication required")
		return
	}

	var body createReportRequest
	if err := decodeJSON(w, r, &body); err != nil {
		writeDecodeError(w, r, h.logger, err)
		return
	}

	created, err := h.service.CreateReport(r.Context(), moderation.CreateReportInput{
		ReporterID: reporterID,
		EntityType: moderation.EntityType(body.EntityType),
		EntityID:   body.EntityID,
		Category:   moderation.Category(body.Category),
		Reason:     body.Reason,
	})
	if err != nil {
		h.writeServiceError(w, r, err)
		return
	}

	writeJSON(w, http.StatusCreated, newReportResponse(created))
}

// ListMyReports handles GET /reports/mine.
func (h *ModerationHandler) ListMyReports(w http.ResponseWriter, r *http.Request) {
	reporterID, ok := UserIDFromContext(r.Context())
	if !ok {
		writeError(w, http.StatusUnauthorized, codeUnauthorized, "authentication required")
		return
	}

	limit, err := parseModerationLimit(r.URL.Query().Get("limit"))
	if err != nil {
		writeError(w, http.StatusBadRequest, codeValidation, err.Error())
		return
	}

	page, nextCursor, err := h.service.ListMyReports(r.Context(), reporterID, r.URL.Query().Get("cursor"), limit)
	if err != nil {
		h.writeServiceError(w, r, err)
		return
	}

	out := make([]reportResponse, 0, len(page))
	for _, report := range page {
		out = append(out, newReportResponse(report))
	}

	writeJSON(w, http.StatusOK, reportsResponse{Reports: out, NextCursor: nextCursor})
}

// CreateBlock handles POST /blocks/{user_id}.
func (h *ModerationHandler) CreateBlock(w http.ResponseWriter, r *http.Request) {
	h.block(w, r, true)
}

// DeleteBlock handles DELETE /blocks/{user_id}.
func (h *ModerationHandler) DeleteBlock(w http.ResponseWriter, r *http.Request) {
	h.block(w, r, false)
}

// block is the shared body of the block and unblock routes. block adds the block
// when true and removes it when false; both answer 204 and both are idempotent.
func (h *ModerationHandler) block(w http.ResponseWriter, r *http.Request, create bool) {
	blockerID, ok := UserIDFromContext(r.Context())
	if !ok {
		writeError(w, http.StatusUnauthorized, codeUnauthorized, "authentication required")
		return
	}

	blockedID := r.PathValue("user_id")

	var err error
	if create {
		err = h.service.Block(r.Context(), blockerID, blockedID)
	} else {
		err = h.service.Unblock(r.Context(), blockerID, blockedID)
	}
	if err != nil {
		h.writeServiceError(w, r, err)
		return
	}

	w.WriteHeader(http.StatusNoContent)
}

// blockedUserResponse is the blocked user's public projection in the block list.
type blockedUserResponse struct {
	ID          string  `json:"id"`
	DisplayName string  `json:"display_name"`
	AvatarURL   *string `json:"avatar_url"`
	Role        string  `json:"role"`
}

// blockEntryResponse is one entry of GET /blocks/mine.
type blockEntryResponse struct {
	User      blockedUserResponse `json:"user"`
	CreatedAt time.Time           `json:"created_at"`
}

// blocksResponse is the GET /blocks/mine body. Blocks is always an array, never
// null.
type blocksResponse struct {
	Blocks     []blockEntryResponse `json:"blocks"`
	NextCursor string               `json:"next_cursor"`
}

// ListBlocks handles GET /blocks/mine.
func (h *ModerationHandler) ListBlocks(w http.ResponseWriter, r *http.Request) {
	blockerID, ok := UserIDFromContext(r.Context())
	if !ok {
		writeError(w, http.StatusUnauthorized, codeUnauthorized, "authentication required")
		return
	}

	limit, err := parseModerationLimit(r.URL.Query().Get("limit"))
	if err != nil {
		writeError(w, http.StatusBadRequest, codeValidation, err.Error())
		return
	}

	page, nextCursor, err := h.service.ListBlocks(r.Context(), blockerID, r.URL.Query().Get("cursor"), limit)
	if err != nil {
		h.writeServiceError(w, r, err)
		return
	}

	// Resolve every blocked user in one batch, so a page of blocks is not a query
	// per row.
	ids := make([]string, 0, len(page))
	for _, block := range page {
		ids = append(ids, block.BlockedID)
	}
	users := authorAttributions(r.Context(), h.users, h.logger, ids)

	out := make([]blockEntryResponse, 0, len(page))
	for _, block := range page {
		attribution := users[block.BlockedID]
		out = append(out, blockEntryResponse{
			User: blockedUserResponse{
				ID:          block.BlockedID,
				DisplayName: attribution.DisplayName,
				AvatarURL:   optionalString(attribution.AvatarURL),
				Role:        h.roleFor(r, block.BlockedID),
			},
			CreatedAt: block.CreatedAt,
		})
	}

	writeJSON(w, http.StatusOK, blocksResponse{Blocks: out, NextCursor: nextCursor})
}

// roleFor returns the blocked user's role. The role is on identity.User but the
// attribution helper only carries the display name and avatar, so it is looked up
// from the same batched result when available and defaults to "user" otherwise.
func (h *ModerationHandler) roleFor(r *http.Request, userID string) string {
	users, err := h.users.UsersByIDs(r.Context(), []string{userID})
	if err != nil {
		return string(moderation.RoleUser)
	}
	if user, ok := users[userID]; ok && user != nil && user.Role != "" {
		return user.Role
	}
	return string(moderation.RoleUser)
}

// newReportResponse projects a domain report onto the wire format.
func newReportResponse(report moderation.Report) reportResponse {
	return reportResponse{
		ID:         report.ID,
		EntityType: string(report.EntityType),
		EntityID:   report.EntityID,
		Category:   string(report.Category),
		Reason:     optionalString(report.Reason),
		CreatedAt:  report.CreatedAt,
	}
}

// parseModerationLimit reads the optional "limit" query parameter. An absent
// limit means the default; a value above the maximum is clamped; a non-positive
// or non-numeric value is a validation failure.
func parseModerationLimit(raw string) (int, error) {
	if raw == "" {
		return moderation.DefaultListLimit, nil
	}

	value, err := strconv.Atoi(raw)
	if err != nil {
		return 0, &moderation.ValidationError{Field: "limit", Message: "must be an integer"}
	}
	if value < 1 {
		return 0, &moderation.ValidationError{Field: "limit", Message: "must be at least 1"}
	}
	if value > moderation.MaxListLimit {
		return moderation.MaxListLimit, nil
	}

	return value, nil
}

// writeServiceError maps moderation domain errors onto HTTP status codes. Only
// errors we recognise as safe are described to the client; everything else
// becomes a generic 500 and is logged with the request id.
func (h *ModerationHandler) writeServiceError(w http.ResponseWriter, r *http.Request, err error) {
	var validation *moderation.ValidationError

	switch {
	case errors.As(err, &validation):
		writeError(w, http.StatusBadRequest, codeValidation, validation.Field+" "+validation.Message)
	case errors.Is(err, moderation.ErrSelfBlock):
		writeError(w, http.StatusBadRequest, codeValidation, "you cannot block yourself")
	case errors.Is(err, moderation.ErrEntityNotFound):
		writeError(w, http.StatusNotFound, codeNotFound, "entity not found")
	case errors.Is(err, moderation.ErrAlreadyReported):
		writeError(w, http.StatusConflict, codeConflict, "you have already reported this")
	default:
		h.logger.ErrorContext(
			r.Context(),
			"moderation request failed",
			slog.String("request_id", RequestIDFromContext(r.Context())),
			slog.String("path", r.URL.Path),
			slog.String("error", err.Error()),
		)
		writeError(w, http.StatusInternalServerError, codeInternal, "internal server error")
	}
}
