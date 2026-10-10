package httpapi

import (
	"context"
	"errors"
	"fmt"
	"log/slog"
	"net/http"
	"strconv"
	"time"

	"github.com/knot/backend/internal/identity"
	"github.com/knot/backend/internal/notifications"
)

// NotificationsService is the slice of the notification service that the HTTP
// layer needs. Depending on an interface (rather than the concrete service) keeps
// handler tests free of a database.
type NotificationsService interface {
	List(ctx context.Context, userID string, rawCursor string, limit int) ([]notifications.Notification, string, error)
	UnreadCount(ctx context.Context, userID string) (int, error)
	MarkRead(ctx context.Context, id, userID string) error
	MarkAllRead(ctx context.Context, userID string) (int, error)
}

// NotificationActors resolves the users behind a page of notification actors.
//
// It is the shared AuthorLookup contract: one batched read, so rendering an inbox
// page costs one query for every distinct actor rather than one per notification.
// It is a named alias rather than a second interface so the inbox and the content
// handlers depend on one enrichment contract (KNOT-ADR-041).
type NotificationActors = AuthorLookup

// NotificationsHandler serves the in-app inbox.
type NotificationsHandler struct {
	service NotificationsService
	users   NotificationActors
	rooted  RootedLookup
	logger  *slog.Logger
}

// NewNotificationsHandler returns a handler backed by service. The user lookup
// names each notification's actor, and the rooted lookup attaches that actor's
// inline Rooted summary.
func NewNotificationsHandler(service NotificationsService, users NotificationActors, rooted RootedLookup, logger *slog.Logger) (*NotificationsHandler, error) {
	if service == nil {
		return nil, fmt.Errorf("httpapi: notifications handler requires a service")
	}
	if users == nil {
		return nil, fmt.Errorf("httpapi: notifications handler requires a user lookup")
	}
	if rooted == nil {
		return nil, fmt.Errorf("httpapi: notifications handler requires a rooted lookup")
	}
	if logger == nil {
		return nil, fmt.Errorf("httpapi: notifications handler requires a logger")
	}
	return &NotificationsHandler{service: service, users: users, rooted: rooted, logger: logger}, nil
}

// notificationActorResponse is the public projection of the user who acted.
//
// It carries the actor's id, display name, avatar URL, and inline Rooted
// summary — the same facts a story or comment response exposes about its author,
// so an inbox row can render exactly like the content it points at.
type notificationActorResponse struct {
	ID           string         `json:"id"`
	DisplayName  string         `json:"display_name"`
	AvatarURL    string         `json:"avatar_url"`
	AuthorRooted *rootedSummary `json:"author_rooted"`
}

// notificationResponse is the public projection of one inbox entry. It is an
// explicit type, not the domain Notification, so the wire format is a deliberate
// choice.
//
// Read is derived from read_at rather than exposing the timestamp: the client
// renders "unread dot" or not, and the exact instant is not part of the contract.
type notificationResponse struct {
	ID         string `json:"id"`
	EventType  string `json:"event_type"`
	EntityType string `json:"entity_type"`
	EntityID   string `json:"entity_id"`
	// ReactionType is the perspective signal a reaction.created notification names,
	// and "" for every other event, so the inbox can render the emoji and label.
	ReactionType string                     `json:"reaction_type"`
	Read         bool                       `json:"read"`
	CreatedAt    time.Time                  `json:"created_at"`
	Actor        *notificationActorResponse `json:"actor"`
}

// listNotificationsResponse is the GET /notifications body. Notifications is
// always an array, never null, and NextCursor is the empty string on the last
// page.
type listNotificationsResponse struct {
	Notifications []notificationResponse `json:"notifications"`
	NextCursor    string                 `json:"next_cursor"`
}

// unreadCountResponse is the GET /notifications/unread_count body.
type unreadCountResponse struct {
	Count int `json:"count"`
}

// markAllReadResponse is the POST /notifications/read_all body. It reports how
// many rows were actually updated, so a client can tell "nothing was unread" from
// "the call did not land".
type markAllReadResponse struct {
	Updated int `json:"updated"`
}

// List handles GET /notifications. The route is protected, so the inbox is always
// the authenticated user's own.
func (h *NotificationsHandler) List(w http.ResponseWriter, r *http.Request) {
	userID, ok := UserIDFromContext(r.Context())
	if !ok {
		// Unreachable while the route is wrapped by AuthMiddleware. It is kept as
		// a fail-closed guard so that a future wiring mistake cannot serve an
		// inbox with no owner.
		writeError(w, http.StatusUnauthorized, codeUnauthorized, "authentication required")
		return
	}

	query := r.URL.Query()

	limit, err := parseNotificationLimit(query.Get("limit"))
	if err != nil {
		h.writeServiceError(w, r, err)
		return
	}

	page, next, err := h.service.List(r.Context(), userID, query.Get("cursor"), limit)
	if err != nil {
		h.writeServiceError(w, r, err)
		return
	}

	items := newNotificationResponses(r.Context(), h.users, h.rooted, h.logger, page)

	writeJSON(w, http.StatusOK, listNotificationsResponse{Notifications: items, NextCursor: next})
}

// UnreadCount handles GET /notifications/unread_count. It exists as its own route
// so the app can badge the feed's bell without downloading a page of
// notifications.
func (h *NotificationsHandler) UnreadCount(w http.ResponseWriter, r *http.Request) {
	userID, ok := UserIDFromContext(r.Context())
	if !ok {
		writeError(w, http.StatusUnauthorized, codeUnauthorized, "authentication required")
		return
	}

	count, err := h.service.UnreadCount(r.Context(), userID)
	if err != nil {
		h.writeServiceError(w, r, err)
		return
	}

	writeJSON(w, http.StatusOK, unreadCountResponse{Count: count})
}

// MarkRead handles POST /notifications/{id}/read.
//
// It answers 204 with no body: the caller asked for a state, not for a
// representation. A notification that does not exist, or that belongs to another
// user, is a 404 — the same answer for both, so the route cannot be used to probe
// whether an id exists (KNOT-ADR-038).
func (h *NotificationsHandler) MarkRead(w http.ResponseWriter, r *http.Request) {
	userID, ok := UserIDFromContext(r.Context())
	if !ok {
		writeError(w, http.StatusUnauthorized, codeUnauthorized, "authentication required")
		return
	}

	if err := h.service.MarkRead(r.Context(), r.PathValue("id"), userID); err != nil {
		h.writeServiceError(w, r, err)
		return
	}

	w.WriteHeader(http.StatusNoContent)
}

// MarkAllRead handles POST /notifications/read_all.
func (h *NotificationsHandler) MarkAllRead(w http.ResponseWriter, r *http.Request) {
	userID, ok := UserIDFromContext(r.Context())
	if !ok {
		writeError(w, http.StatusUnauthorized, codeUnauthorized, "authentication required")
		return
	}

	updated, err := h.service.MarkAllRead(r.Context(), userID)
	if err != nil {
		h.writeServiceError(w, r, err)
		return
	}

	writeJSON(w, http.StatusOK, markAllReadResponse{Updated: updated})
}

// parseNotificationLimit reads the optional "limit" query parameter.
//
// An absent limit means the notification domain's default. A limit above the
// maximum is clamped rather than rejected, so a client asking for "as many as you
// can" gets a usable page. A limit that is not a positive integer is a validation
// failure: it is a malformed request, not an over-large one.
func parseNotificationLimit(raw string) (int, error) {
	if raw == "" {
		return notifications.DefaultListLimit, nil
	}

	value, err := strconv.Atoi(raw)
	if err != nil {
		return 0, &notifications.ValidationError{Field: "limit", Message: "must be an integer"}
	}
	if value < 1 {
		return 0, &notifications.ValidationError{Field: "limit", Message: "must be at least 1"}
	}
	if value > notifications.MaxListLimit {
		return notifications.MaxListLimit, nil
	}

	return value, nil
}

// writeServiceError maps domain errors onto HTTP status codes. Only errors we
// recognise as safe are described to the client; everything else becomes a
// generic 500 and is logged with the request id.
func (h *NotificationsHandler) writeServiceError(w http.ResponseWriter, r *http.Request, err error) {
	var validation *notifications.ValidationError

	switch {
	case errors.As(err, &validation):
		writeError(w, http.StatusBadRequest, codeValidation, fmt.Sprintf("%s %s", validation.Field, validation.Message))
	case errors.Is(err, notifications.ErrNotFound):
		writeError(w, http.StatusNotFound, codeNotFound, "notification not found")
	default:
		h.logger.ErrorContext(
			r.Context(),
			"notifications request failed",
			slog.String("request_id", RequestIDFromContext(r.Context())),
			slog.String("path", r.URL.Path),
			slog.String("error", err.Error()),
		)
		writeError(w, http.StatusInternalServerError, codeInternal, "internal server error")
	}
}

// newNotificationResponses projects a page of notifications onto the wire format,
// enriching every actor in two batched reads: one for the users, one for their
// Rooted summaries.
//
// Enrichment is supplementary, so a lookup failure is logged and swallowed rather
// than failing the whole page: an inbox whose actor names could not be resolved is
// still better than no inbox. An actor that could not be resolved becomes null,
// which the client renders as an unknown user.
func newNotificationResponses(ctx context.Context, users NotificationActors, rooted RootedLookup, logger *slog.Logger, page []notifications.Notification) []notificationResponse {
	items := make([]notificationResponse, 0, len(page))

	actorIDs := make([]string, 0, len(page))
	for _, notification := range page {
		actorIDs = append(actorIDs, notification.ActorID)
	}

	resolved := resolveNotificationActors(ctx, users, logger, actorIDs)
	summaries := authorRootedSummaries(ctx, rooted, logger, actorIDs)

	for _, notification := range page {
		response := notificationResponse{
			ID:           notification.ID,
			EventType:    string(notification.EventType),
			EntityType:   string(notification.EntityType),
			EntityID:     notification.EntityID,
			ReactionType: notification.ReactionType,
			Read:         notification.IsRead(),
			CreatedAt:    notification.CreatedAt,
		}

		if actor, ok := resolved[notification.ActorID]; ok {
			response.Actor = &notificationActorResponse{
				ID:           actor.ID,
				DisplayName:  actor.DisplayName,
				AvatarURL:    avatarPathFor(actor),
				AuthorRooted: summaries[actor.ID],
			}
		}

		items = append(items, response)
	}

	return items
}

// resolveNotificationActors resolves the distinct actors of a page in one call and
// returns them keyed by id.
func resolveNotificationActors(ctx context.Context, users NotificationActors, logger *slog.Logger, ids []string) map[string]*identity.User {
	distinct := uniqueAuthorIDs(ids)
	if len(distinct) == 0 {
		return nil
	}

	resolved, err := users.UsersByIDs(ctx, distinct)
	if err != nil {
		logger.WarnContext(
			ctx,
			"notification actor enrichment failed",
			slog.String("request_id", RequestIDFromContext(ctx)),
			slog.String("error", err.Error()),
		)
		return nil
	}

	return resolved
}
