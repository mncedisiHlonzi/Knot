package httpapi

import (
	"context"
	"errors"
	"fmt"
	"log/slog"
	"net/http"
	"time"

	"github.com/knot/backend/internal/conversations"
	"github.com/knot/backend/internal/reactions"
	"github.com/knot/backend/internal/stories"
	"github.com/knot/backend/internal/versions"
)

// ReactionsService is the slice of the reactions service that the HTTP layer
// needs. Depending on an interface (rather than the concrete service) keeps
// handler tests free of a database.
type ReactionsService interface {
	// Toggle adds or removes the reaction and returns the entity's updated
	// summary, plus whether the reaction is now held.
	Toggle(ctx context.Context, in reactions.ToggleInput) (reactions.Summary, bool, error)
	// ListForEntity returns every reaction on one entity, with its reactor
	// resolved.
	ListForEntity(ctx context.Context, entityType reactions.EntityType, entityID string) ([]reactions.ReactionWithActor, error)
}

// ReactionsNotifier records that a user reacted to another user's content. It is
// the one-method hook the notifications service satisfies.
type ReactionsNotifier interface {
	NotifyReactionCreated(ctx context.Context, recipientID, actorID, entityType, reactionType, entityID string) error
}

// authorResolver resolves the author of the entity a reaction targets, and
// reports whether that entity exists. It is how each toggle route verifies its
// target through that target's own domain service, so the reactions domain never
// needs a polymorphic existence check (KNOT-ADR-050).
type authorResolver func(ctx context.Context, id string) (string, error)

// errReactionOnReply refuses a reaction on a reply. Reactions are a top-level
// signal: a reply is answered in its own thread, and the thread as a whole is
// covered by the reactions on its top-level comments (KNOT-ADR-052).
var errReactionOnReply = errors.New("httpapi: reactions are not supported on replies")

// ReactionsHandler serves the per-entity reaction endpoints.
//
// There is one toggle and one list route per entity kind rather than a single
// polymorphic pair. The path already names the kind, so the handler knows it; the
// existence check is then an ordinary call into that domain's own service, which
// is simpler and safer than a polymorphic lookup (KNOT-ADR-050).
type ReactionsHandler struct {
	reactions     ReactionsService
	stories       StoriesService
	versions      VersionsService
	conversations ConversationsService
	notifier      ReactionsNotifier
	logger        *slog.Logger
}

// NewReactionsHandler returns a handler. Each content service is used only to
// resolve an entity's author and confirm it exists; the notifier fires the
// reaction.created event.
func NewReactionsHandler(reactionsService ReactionsService, storiesService StoriesService, versionsService VersionsService, conversationsService ConversationsService, notifier ReactionsNotifier, logger *slog.Logger) (*ReactionsHandler, error) {
	if reactionsService == nil {
		return nil, fmt.Errorf("httpapi: reactions handler requires a reactions service")
	}
	if storiesService == nil {
		return nil, fmt.Errorf("httpapi: reactions handler requires a stories service")
	}
	if versionsService == nil {
		return nil, fmt.Errorf("httpapi: reactions handler requires a versions service")
	}
	if conversationsService == nil {
		return nil, fmt.Errorf("httpapi: reactions handler requires a conversations service")
	}
	if notifier == nil {
		return nil, fmt.Errorf("httpapi: reactions handler requires a notifier")
	}
	if logger == nil {
		return nil, fmt.Errorf("httpapi: reactions handler requires a logger")
	}
	return &ReactionsHandler{
		reactions:     reactionsService,
		stories:       storiesService,
		versions:      versionsService,
		conversations: conversationsService,
		notifier:      notifier,
		logger:        logger,
	}, nil
}

// toggleReactionRequest is the POST /<entity>/{id}/reactions body.
type toggleReactionRequest struct {
	ReactionType string `json:"reaction_type"`
}

// reactionCountsEnvelope is the toggle response body: the entity's updated
// counts, in the same shape the entity responses carry.
type reactionCountsEnvelope struct {
	Reactions reactionCountsResponse `json:"reactions"`
}

// reactionActorResponse is the reactor's public projection, matching the
// attribution every other response exposes about an author.
type reactionActorResponse struct {
	ID          string `json:"id"`
	DisplayName string `json:"display_name"`
	AvatarURL   string `json:"avatar_url"`
}

// reactionItemResponse is one entry of a reaction list.
type reactionItemResponse struct {
	ID           string                `json:"id"`
	User         reactionActorResponse `json:"user"`
	ReactionType string                `json:"reaction_type"`
	CreatedAt    time.Time             `json:"created_at"`
}

// reactionListResponse is the GET /<entity>/{id}/reactions body. Reactions is
// always an array, never null.
type reactionListResponse struct {
	Reactions []reactionItemResponse `json:"reactions"`
}

// ToggleStory handles POST /stories/{id}/reactions.
func (h *ReactionsHandler) ToggleStory(w http.ResponseWriter, r *http.Request) {
	h.toggle(w, r, reactions.EntityStory, h.storyAuthor)
}

// ToggleVersion handles POST /versions/{id}/reactions.
func (h *ReactionsHandler) ToggleVersion(w http.ResponseWriter, r *http.Request) {
	h.toggle(w, r, reactions.EntityVersion, h.versionAuthor)
}

// ToggleComment handles POST /comments/{id}/reactions.
func (h *ReactionsHandler) ToggleComment(w http.ResponseWriter, r *http.Request) {
	h.toggle(w, r, reactions.EntityComment, h.commentAuthor)
}

// ToggleBridge handles POST /bridges/{id}/reactions.
func (h *ReactionsHandler) ToggleBridge(w http.ResponseWriter, r *http.Request) {
	h.toggle(w, r, reactions.EntityBridge, h.bridgeAuthor)
}

// ListStory handles GET /stories/{id}/reactions.
func (h *ReactionsHandler) ListStory(w http.ResponseWriter, r *http.Request) {
	h.list(w, r, reactions.EntityStory)
}

// ListVersion handles GET /versions/{id}/reactions.
func (h *ReactionsHandler) ListVersion(w http.ResponseWriter, r *http.Request) {
	h.list(w, r, reactions.EntityVersion)
}

// ListComment handles GET /comments/{id}/reactions.
func (h *ReactionsHandler) ListComment(w http.ResponseWriter, r *http.Request) {
	h.list(w, r, reactions.EntityComment)
}

// ListBridge handles GET /bridges/{id}/reactions.
func (h *ReactionsHandler) ListBridge(w http.ResponseWriter, r *http.Request) {
	h.list(w, r, reactions.EntityBridge)
}

// toggle is the shared body of the four toggle routes.
func (h *ReactionsHandler) toggle(w http.ResponseWriter, r *http.Request, entityType reactions.EntityType, authorOf authorResolver) {
	actorID, ok := UserIDFromContext(r.Context())
	if !ok {
		// Unreachable while the route is wrapped by AuthMiddleware. It is kept as
		// a fail-closed guard so a wiring mistake cannot react with no user.
		writeError(w, http.StatusUnauthorized, codeUnauthorized, "authentication required")
		return
	}

	var body toggleReactionRequest
	if err := decodeJSON(w, r, &body); err != nil {
		writeDecodeError(w, r, h.logger, err)
		return
	}

	reactionType := reactions.ReactionType(body.ReactionType)
	if !reactionType.Valid() {
		writeError(w, http.StatusBadRequest, codeValidation,
			"reaction_type must be one of rings_true, know_it_differently, adds_something_new, needs_a_source")
		return
	}

	// Resolve the entity through its own domain first: this both proves it exists
	// (404) and yields its author, who is the notification's recipient.
	entityID := r.PathValue("id")
	authorID, err := authorOf(r.Context(), entityID)
	if err != nil {
		h.writeResolveError(w, r, err, entityType)
		return
	}

	summary, active, err := h.reactions.Toggle(r.Context(), reactions.ToggleInput{
		UserID:       actorID,
		EntityType:   entityType,
		EntityID:     entityID,
		ReactionType: reactionType,
	})
	if err != nil {
		h.writeServiceError(w, r, err)
		return
	}

	// Only creating a reaction notifies anyone: toggling off is silent, and
	// reacting to your own content notifies nobody (KNOT-ADR-050). The notifier
	// logs its own failures and the toggle has already succeeded, so the error is
	// deliberately dropped.
	if active && authorID != actorID {
		_ = h.notifier.NotifyReactionCreated(r.Context(), authorID, actorID, string(entityType), string(reactionType), entityID)
	}

	writeJSON(w, http.StatusOK, reactionCountsEnvelope{Reactions: newReactionCounts(summary)})
}

// list is the shared body of the four list routes.
//
// An entity with no reactions — or an id that names nothing — returns an empty
// array. The list route is a read of signals that exist, so an absent entity is
// not distinguished from one nobody has reacted to.
func (h *ReactionsHandler) list(w http.ResponseWriter, r *http.Request, entityType reactions.EntityType) {
	items, err := h.reactions.ListForEntity(r.Context(), entityType, r.PathValue("id"))
	if err != nil {
		h.writeServiceError(w, r, err)
		return
	}

	out := make([]reactionItemResponse, 0, len(items))
	for _, item := range items {
		out = append(out, reactionItemResponse{
			ID: item.Reaction.ID,
			User: reactionActorResponse{
				ID:          item.Actor.ID,
				DisplayName: item.Actor.DisplayName,
				AvatarURL:   item.Actor.AvatarURL,
			},
			ReactionType: string(item.Reaction.ReactionType),
			CreatedAt:    item.Reaction.CreatedAt,
		})
	}

	writeJSON(w, http.StatusOK, reactionListResponse{Reactions: out})
}

// storyAuthor, versionAuthor, commentAuthor, and bridgeAuthor resolve an entity's
// author through that entity's own service.
func (h *ReactionsHandler) storyAuthor(ctx context.Context, id string) (string, error) {
	story, err := h.stories.GetStory(ctx, id)
	if err != nil {
		return "", err
	}
	return story.AuthorID, nil
}

func (h *ReactionsHandler) versionAuthor(ctx context.Context, id string) (string, error) {
	version, err := h.versions.GetVersion(ctx, id)
	if err != nil {
		return "", err
	}
	return version.AuthorID, nil
}

func (h *ReactionsHandler) commentAuthor(ctx context.Context, id string) (string, error) {
	comment, err := h.conversations.GetComment(ctx, id)
	if err != nil {
		return "", err
	}
	if comment.ParentCommentID != nil {
		return "", errReactionOnReply
	}
	return comment.AuthorID, nil
}

func (h *ReactionsHandler) bridgeAuthor(ctx context.Context, id string) (string, error) {
	bridge, err := h.conversations.GetBridge(ctx, id)
	if err != nil {
		return "", err
	}
	return bridge.AuthorID, nil
}

// writeResolveError maps a failed entity resolution onto a status: a reply is a
// 400, a missing entity is a type-named 404, and anything else is a 500.
func (h *ReactionsHandler) writeResolveError(w http.ResponseWriter, r *http.Request, err error, entityType reactions.EntityType) {
	switch {
	case errors.Is(err, errReactionOnReply):
		writeError(w, http.StatusBadRequest, codeValidation, "replies cannot be reacted to; react to the thread instead")
	case isDomainNotFound(err):
		writeError(w, http.StatusNotFound, codeNotFound, notFoundMessage(entityType))
	default:
		h.logger.ErrorContext(
			r.Context(),
			"reactions request failed",
			slog.String("request_id", RequestIDFromContext(r.Context())),
			slog.String("path", r.URL.Path),
			slog.String("error", err.Error()),
		)
		writeError(w, http.StatusInternalServerError, codeInternal, "internal server error")
	}
}

// writeServiceError maps a reactions domain error onto a status code.
func (h *ReactionsHandler) writeServiceError(w http.ResponseWriter, r *http.Request, err error) {
	var validation *reactions.ValidationError

	switch {
	case errors.As(err, &validation):
		writeError(w, http.StatusBadRequest, codeValidation, fmt.Sprintf("%s %s", validation.Field, validation.Message))
	default:
		h.logger.ErrorContext(
			r.Context(),
			"reactions request failed",
			slog.String("request_id", RequestIDFromContext(r.Context())),
			slog.String("path", r.URL.Path),
			slog.String("error", err.Error()),
		)
		writeError(w, http.StatusInternalServerError, codeInternal, "internal server error")
	}
}

// isDomainNotFound reports whether err is a not-found from any content domain.
func isDomainNotFound(err error) bool {
	return errors.Is(err, stories.ErrNotFound) ||
		errors.Is(err, versions.ErrNotFound) ||
		errors.Is(err, conversations.ErrNotFound)
}

// notFoundMessage names what was not found for each entity kind.
func notFoundMessage(entityType reactions.EntityType) string {
	switch entityType {
	case reactions.EntityStory:
		return "story not found"
	case reactions.EntityVersion:
		return "version not found"
	case reactions.EntityComment:
		return "comment not found"
	case reactions.EntityBridge:
		return "bridge not found"
	default:
		return "not found"
	}
}
