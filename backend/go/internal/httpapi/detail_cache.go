package httpapi

// The story and version detail cache's HTTP boundary (KNOT-036-fix).
//
// The handler owns the two decisions the cache itself must not make: what is
// shared between readers and what is not, and whether a reader is allowed to see
// the entry at all. The cache only stores bytes under a key.

import (
	"context"
	"encoding/json"
	"errors"
	"log/slog"

	"github.com/knot/backend/internal/conversations"
	"github.com/knot/backend/internal/moderation"
	"github.com/knot/backend/internal/reactions"
	"github.com/knot/backend/internal/versions"
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
func (h *VersionsHandler) cachedVersionDetail(ctx context.Context, versionID string) (*versionDetailResponse, bool) {
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

	// The stored payload is the DETAIL envelope, which carries the story's media
	// array as well as the version fields (KNOT-036-fix3). Decoding it into the
	// plain versionEnvelope would silently drop that array.
	var envelope versionDetailEnvelope
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
func (h *VersionsHandler) storeVersionDetail(ctx context.Context, response versionDetailResponse) {
	if h.caches.Details == nil || response.ID == "" {
		return
	}

	response.MyReactions = []string{}

	payload, err := json.Marshal(versionDetailEnvelope{Version: response})
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

// VersionParentLookup resolves a version to the story it belongs to. It is the
// narrow slice of the versions service the invalidator needs, so the invalidator
// can be driven by a stub whose only job is to answer that question.
type VersionParentLookup interface {
	GetVersion(ctx context.Context, id string) (versions.StoryVersion, error)
}

// CommentThreadLookup resolves a comment to its version and that version's story.
// Both are already carried by conversations.Comment: GetComment populates
// VersionID and StoryID, so resolving a comment costs one read, not two.
type CommentThreadLookup interface {
	GetComment(ctx context.Context, id string) (conversations.Comment, error)
}

// DetailInvalidator deletes the detail cache keys a content write makes stale.
//
// It exists so the key format and the parent-lookup rules live in one place: a
// handler says "this entity changed" and does not have to know that a version key
// implies its parent story, or that a comment implies both its version and that
// version's story (KNOT-036-fix2).
//
// Every deletion is best-effort. A Redis failure is logged at Warn and never
// returned: the write that triggered it has already succeeded, and the TTL is the
// backstop (KNOT-ADR-094).
//
// A nil *DetailInvalidator is a valid "caching is switched off" value: every
// method is a no-op, so a handler can call it unconditionally.
type DetailInvalidator struct {
	details  DetailCache
	versions VersionParentLookup
	comments CommentThreadLookup
	logger   *slog.Logger
}

// NewDetailInvalidator returns an invalidator, or nil when there is no detail
// cache to invalidate (the feature is switched off).
func NewDetailInvalidator(details DetailCache, versions VersionParentLookup, comments CommentThreadLookup, logger *slog.Logger) *DetailInvalidator {
	if details == nil {
		return nil
	}

	return &DetailInvalidator{details: details, versions: versions, comments: comments, logger: logger}
}

// InvalidateForEntity deletes the detail keys a reaction on entityKind makes stale.
//
// The rules are the same for every write: the target's own detail key, plus its
// parent's when the parent's counts include the target (KNOT-036-fix2).
//
//   - a story deletes `knot:story:{id}`;
//   - a version deletes `knot:version:{id}` and `knot:story:{parent_story_id}`,
//     because the story's version and language counts include it;
//   - a comment deletes `knot:version:{version_id}` and
//     `knot:story:{parent_story_id}`;
//   - anything else (an inquiry, or a kind the detail cache does not hold) deletes
//     nothing. Inquiries are not cached, so a reaction on one cannot make a cached
//     detail stale.
//
// Bridge reactions no longer exist (KNOT-023 retired bridges), so there is no
// bridge case: a bridge used to create a target comment, and the comment create
// invalidation covers that path.
func (d *DetailInvalidator) InvalidateForEntity(ctx context.Context, entityKind, entityID string) {
	if d == nil || entityID == "" {
		return
	}

	switch entityKind {
	case string(reactions.EntityStory):
		invalidateStoryDetail(ctx, d.logger, d.details, entityID)
	case string(reactions.EntityVersion):
		d.InvalidateForVersion(ctx, entityID)
	case string(reactions.EntityComment):
		d.InvalidateForComment(ctx, entityID)
	}
}

// InvalidateForVersion deletes a version's detail key and its parent story's.
//
// The parent story is resolved through the versions service: a version row names
// its story, so this is one read rather than a second cache lookup. A version
// whose story cannot be resolved still has its own key deleted — the resolution
// failure only costs the parent's key, which the TTL then heals.
func (d *DetailInvalidator) InvalidateForVersion(ctx context.Context, versionID string) {
	if d == nil || versionID == "" {
		return
	}

	invalidateVersionDetail(ctx, d.logger, d.details, versionID)

	storyID, err := d.parentStoryOfVersion(ctx, versionID)
	if err != nil {
		d.warn(ctx, "version", versionID, "the parent story could not be resolved", err)
		return
	}

	invalidateStoryDetail(ctx, d.logger, d.details, storyID)
}

// InvalidateForComment deletes the comment's version key and that version's
// story key.
//
// A comment is not itself cached, so there is no comment key to delete: the thread
// a reader sees is the version detail's, and the comment count a reader sees is the
// story detail's.
func (d *DetailInvalidator) InvalidateForComment(ctx context.Context, commentID string) {
	if d == nil || commentID == "" {
		return
	}
	if d.comments == nil {
		d.warn(ctx, "comment", commentID, "no comment lookup is wired", errNoDetailLookup)
		return
	}

	comment, err := d.comments.GetComment(ctx, commentID)
	if err != nil {
		d.warn(ctx, "comment", commentID, "the comment's thread could not be resolved", err)
		return
	}

	invalidateVersionDetail(ctx, d.logger, d.details, comment.VersionID)

	storyID := comment.StoryID
	if storyID == "" {
		// CreateComment leaves StoryID empty because its response does not need it;
		// a caller holding a freshly created comment therefore falls back to the
		// version's own story pointer.
		storyID, err = d.parentStoryOfVersion(ctx, comment.VersionID)
		if err != nil {
			d.warn(ctx, "comment", commentID, "the parent story could not be resolved", err)
			return
		}
	}

	invalidateStoryDetail(ctx, d.logger, d.details, storyID)
}

// parentStoryOfVersion resolves a version id to the story it belongs to.
func (d *DetailInvalidator) parentStoryOfVersion(ctx context.Context, versionID string) (string, error) {
	if versionID == "" {
		return "", nil
	}
	if d.versions == nil {
		return "", errNoDetailLookup
	}

	version, err := d.versions.GetVersion(ctx, versionID)
	if err != nil {
		return "", err
	}

	return version.StoryID, nil
}

// warn records a failed invalidation. The message is deliberately one string for
// every branch, so an operator greps once, and the attributes say which entity and
// why.
func (d *DetailInvalidator) warn(ctx context.Context, entityKind, entityID, reason string, err error) {
	if d.logger == nil {
		return
	}

	attributes := []any{
		slog.String("request_id", RequestIDFromContext(ctx)),
		slog.String("entity_kind", entityKind),
		slog.String("entity_id", entityID),
		slog.String("reason", reason),
	}
	if err != nil {
		attributes = append(attributes, slog.String("error", err.Error()))
	}

	d.logger.WarnContext(ctx, "detail cache invalidation failed", attributes...)
}

// errNoDetailLookup is the reason recorded when an invalidator was built without
// the lookup a parent resolution needs.
var errNoDetailLookup = errors.New("no detail-cache lookup is wired")
