package httpapi

// The story and version detail cache's HTTP boundary (KNOT-036-fix).
//
// The handler owns the two decisions the cache itself must not make: what is
// shared between readers and what is not, and whether a reader is allowed to see
// the entry at all. The cache only stores bytes under a key.

import (
	"context"
	"encoding/json"
	"log/slog"

	"github.com/knot/backend/internal/moderation"
	"github.com/knot/backend/internal/reactions"
)

// DetailCache is the HTTP layer's view of the story and version detail cache.
//
// It is an interface rather than the concrete service so a handler test can drive
// hits, misses, failures and invalidation without Redis.
//
// The payloads are the serialized response envelopes exactly as the handler writes
// them, with the reader-specific fields blanked: see cachedStoryDetail and
// cachedVersionDetail.
type DetailCache interface {
	GetStoryJSON(ctx context.Context, storyID string) (string, bool, error)
	SetStoryJSON(ctx context.Context, storyID, payload string) error
	GetVersionJSON(ctx context.Context, versionID string) (string, bool, error)
	SetVersionJSON(ctx context.Context, versionID, payload string) error
	InvalidateStory(ctx context.Context, storyID string) error
	InvalidateVersion(ctx context.Context, versionID string) error
}

// cachedStoryDetail returns a story detail response from the cache.
//
// The stored payload is the ANONYMOUS projection, so two things must happen before
// it can be served to whoever is asking:
//
//   - **The block check.** Visibility is a per-request decision taken from the
//     request context, and the database path applies it in the stories service. A
//     cache hit must apply it too, or a block would stop working the moment the
//     author's story was cached.
//   - **The reader's own reactions.** `my_reactions` belongs to the reader, so the
//     cached copy always has it empty and the reader's signals are read fresh. If a
//     future field becomes reader-specific, it belongs in this function (or the
//     cache has to be partitioned), which is the constraint KNOT-ADR-094 records.
//
// Anything unexpected — a miss, an unreadable payload, a block, a failed reaction
// read — answers "not cached", which sends the request down the database path. A
// cache that cannot be trusted is a slower read, never a wrong one.
func (h *StoriesHandler) cachedStoryDetail(ctx context.Context, storyID string) (*storyResponse, bool) {
	if h.caches.Details == nil || storyID == "" {
		return nil, false
	}

	payload, ok, err := h.caches.Details.GetStoryJSON(ctx, storyID)
	if err != nil {
		h.logger.WarnContext(
			ctx,
			"story detail cache read failed; serving the database",
			slog.String("request_id", RequestIDFromContext(ctx)),
			slog.String("error", err.Error()),
		)
		return nil, false
	}
	if !ok {
		return nil, false
	}

	var envelope storyEnvelope
	if err := json.Unmarshal([]byte(payload), &envelope); err != nil {
		h.logger.WarnContext(
			ctx,
			"cached story detail could not be decoded; serving the database",
			slog.String("request_id", RequestIDFromContext(ctx)),
			slog.String("error", err.Error()),
		)
		return nil, false
	}
	response := envelope.Story

	if moderation.IsExcludedAuthor(ctx, response.AuthorID) {
		return nil, false
	}

	if userID, authenticated := UserIDFromContext(ctx); authenticated && userID != "" {
		response.Reactions, response.MyReactions = reactionsForEntity(
			ctx, h.reactions, h.logger, reactions.EntityStory, userID, response.ID,
		)
	}

	return &response, true
}

// storeStoryDetail writes one story detail response into the cache.
//
// The payload is the same envelope the response is written from, with
// `my_reactions` blanked so one reader's signals can never be served to another.
// A marshal or Redis failure is logged at Warn and dropped: the response has
// already been sent, and a cache that failed to fill is a missed optimisation.
func (h *StoriesHandler) storeStoryDetail(ctx context.Context, response storyResponse) {
	if h.caches.Details == nil || response.ID == "" {
		return
	}

	response.MyReactions = []string{}

	payload, err := json.Marshal(storyEnvelope{Story: response})
	if err != nil {
		h.logger.WarnContext(
			ctx,
			"story detail could not be serialized for the cache",
			slog.String("request_id", RequestIDFromContext(ctx)),
			slog.String("error", err.Error()),
		)
		return
	}

	if err := h.caches.Details.SetStoryJSON(ctx, response.ID, string(payload)); err != nil {
		h.logger.WarnContext(
			ctx,
			"story detail cache write failed",
			slog.String("request_id", RequestIDFromContext(ctx)),
			slog.String("error", err.Error()),
		)
	}
}

// invalidateStoryDetail deletes one story's cached detail.
//
// It runs on the request path of the write that changed the story — a publish, or an
// adaptation (which changes version_count) — because a deterministic invalidation is
// worth one Redis DEL, and a failed DEL is only logged: the entry will expire.
func invalidateStoryDetail(ctx context.Context, logger *slog.Logger, details DetailCache, storyID string) {
	if details == nil || storyID == "" {
		return
	}

	if err := details.InvalidateStory(ctx, storyID); err != nil {
		logger.Warn(
			"story detail cache invalidation failed",
			"story_id", storyID,
			"error", err,
		)
	}
}

// invalidateVersionDetail deletes one version's cached detail.
func invalidateVersionDetail(ctx context.Context, logger *slog.Logger, details DetailCache, versionID string) {
	if details == nil || versionID == "" {
		return
	}

	if err := details.InvalidateVersion(ctx, versionID); err != nil {
		logger.Warn(
			"version detail cache invalidation failed",
			"version_id", versionID,
			"error", err,
		)
	}
}

// cachedVersionDetail returns a version detail response from the cache.
//
// It is the version counterpart of cachedStoryDetail and follows the same two
// rules: the block check comes from the request, and the reader's own reactions are
// read fresh. Everything else in the payload — the attribution sentence, the depth,
// the author, the Rooted summary — is public and shared.
func (h *VersionsHandler) cachedVersionDetail(ctx context.Context, versionID string) (*versionResponse, bool) {
	if h.caches.Details == nil || versionID == "" {
		return nil, false
	}

	payload, ok, err := h.caches.Details.GetVersionJSON(ctx, versionID)
	if err != nil {
		h.logger.WarnContext(
			ctx,
			"version detail cache read failed; serving the database",
			slog.String("request_id", RequestIDFromContext(ctx)),
			slog.String("error", err.Error()),
		)
		return nil, false
	}
	if !ok {
		return nil, false
	}

	var envelope versionEnvelope
	if err := json.Unmarshal([]byte(payload), &envelope); err != nil {
		h.logger.WarnContext(
			ctx,
			"cached version detail could not be decoded; serving the database",
			slog.String("request_id", RequestIDFromContext(ctx)),
			slog.String("error", err.Error()),
		)
		return nil, false
	}
	response := envelope.Version

	if moderation.IsExcludedAuthor(ctx, response.AuthorID) {
		return nil, false
	}

	if userID, authenticated := UserIDFromContext(ctx); authenticated && userID != "" {
		response.Reactions, response.MyReactions = reactionsForEntity(
			ctx, h.reactions, h.logger, reactions.EntityVersion, userID, response.ID,
		)
	}

	return &response, true
}

// storeVersionDetail writes one version detail response into the cache.
func (h *VersionsHandler) storeVersionDetail(ctx context.Context, response versionResponse) {
	if h.caches.Details == nil || response.ID == "" {
		return
	}

	response.MyReactions = []string{}

	payload, err := json.Marshal(versionEnvelope{Version: response})
	if err != nil {
		h.logger.WarnContext(
			ctx,
			"version detail could not be serialized for the cache",
			slog.String("request_id", RequestIDFromContext(ctx)),
			slog.String("error", err.Error()),
		)
		return
	}

	if err := h.caches.Details.SetVersionJSON(ctx, response.ID, string(payload)); err != nil {
		h.logger.WarnContext(
			ctx,
			"version detail cache write failed",
			slog.String("request_id", RequestIDFromContext(ctx)),
			slog.String("error", err.Error()),
		)
	}
}
