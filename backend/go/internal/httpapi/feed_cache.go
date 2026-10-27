package httpapi

// The precomputed feed's HTTP boundary (KNOT-036).
//
// The handler owns three decisions the feed cache itself must not make: when a
// request is allowed to be served from Redis, what happens when it cannot be, and
// that both the fan-out on write and the cold-start seed run OFF the request path.
// The cache package only knows how to store and read ids.

import (
	"context"
	"log/slog"
	"time"

	"github.com/knot/backend/internal/feedcache"
	"github.com/knot/backend/internal/stories"
)

// precomputedFeedPage serves the first page of the default feed from Redis.
//
// The second return value is "this page came from the cache": false means the
// caller must fall through to the SQL feed, which is the pre-KNOT-036 behaviour and
// the answer in every one of these cases:
//
//   - the cache is not wired (the feature is switched off);
//   - the reader is anonymous — no feed exists, and an anonymous read must not
//     populate one;
//   - the reader's feed is empty (a new account, or a cache miss);
//   - the cached ids no longer resolve to stories (everything trimmed away);
//   - Redis or the follow-up read failed. A feed that cannot be cached is a slower
//     feed, never a broken one.
//
// The cursor has already been checked by the caller: the cached path has no cursor
// of its own (KNOT-ADR-093), so a paginated request is always answered by SQL.
func (h *StoriesHandler) precomputedFeedPage(ctx context.Context, limit int) ([]stories.FeedItem, bool) {
	if h.feed == nil {
		return nil, false
	}

	userID, ok := UserIDFromContext(ctx)
	if !ok || userID == "" {
		return nil, false
	}

	ids, err := h.feed.Read(ctx, userID, limit)
	if err != nil {
		h.logger.ErrorContext(
			ctx,
			"feed cache read failed; serving the postgres feed",
			slog.String("request_id", RequestIDFromContext(ctx)),
			slog.String("error", err.Error()),
		)
		return nil, false
	}
	if len(ids) == 0 {
		return nil, false
	}

	items, err := h.service.StoriesByIDs(ctx, ids)
	if err != nil {
		h.logger.ErrorContext(
			ctx,
			"feed cache rows could not be read; serving the postgres feed",
			slog.String("request_id", RequestIDFromContext(ctx)),
			slog.String("error", err.Error()),
		)
		return nil, false
	}
	if len(items) == 0 {
		return nil, false
	}

	return items, true
}

// seedEntries projects a feed page onto the entries a cold-start seed writes.
//
// Only story entries are seeded: the sorted set stores story ids, and an adaptation
// entry's id is a version id, which would be read back as a story and found
// missing. Adaptations therefore stay a SQL-path capability for now, which is the
// known limitation of the cached feed (KNOT-ADR-093).
func seedEntries(page []stories.FeedItem) []feedcache.Entry {
	entries := make([]feedcache.Entry, 0, len(page))
	for _, item := range page {
		if item.Kind != stories.FeedKindStory || item.Story.ID == "" {
			continue
		}
		entries = append(entries, feedcache.Entry{
			ID:        item.Story.ID,
			CreatedAt: item.Story.CreatedAt,
		})
	}
	return entries
}

// FeedCache is the HTTP layer's view of the precomputed feed.
//
// It is an interface rather than the concrete service so a handler test can drive
// the cached path — and every one of its failure modes — without Redis.
type FeedCache interface {
	// Read returns up to limit story ids from one reader's feed, newest first.
	// An empty slice means "nothing cached", which is a cache miss rather than an
	// error.
	Read(ctx context.Context, userID string, limit int) ([]string, error)
	// FanoutStory pushes one newly created story into the feeds of the readers who
	// should see it.
	FanoutStory(ctx context.Context, storyID, authorID, language string, createdAt time.Time) (feedcache.Result, error)
	// Seed fills one reader's feed from a page the SQL fallback just served, so a
	// reader who was never fanned out to (a new account) stops hitting PostgreSQL
	// on every read.
	Seed(ctx context.Context, userID string, entries []feedcache.Entry) error
}

// Background bounds.
//
// Both are generous compared with the interaction log's five seconds, because both
// are writes that scale with an audience rather than with one row: a fan-out to an
// author's whole audience is one Redis round trip per reader, and a seed is one per
// entry on a page. Both stay inside a detached, bounded context so a slow Redis
// cannot accumulate goroutines behind a busy server.
const (
	// feedFanoutTimeout bounds one fan-out.
	feedFanoutTimeout = 30 * time.Second
	// feedSeedTimeout bounds one cold-start seed.
	feedSeedTimeout = 10 * time.Second
)

// recordFeedFanout precomputes the feeds a new story belongs in, without delaying
// or failing the request that created it.
//
// This is the write half of fan-out on write, and it is deliberately best-effort:
// the story is already stored and already in the response, so a Redis failure here
// leaves the feed eventually consistent rather than making a successful publish
// look like a failure. A failed fan-out is logged at ERROR because it is the one
// case an operator must be able to see — the story exists but nobody's feed knows.
//
// A nil cache (the feature is switched off) is a silent no-op.
func recordFeedFanout(ctx context.Context, logger *slog.Logger, feed FeedCache, storyID, authorID, language string, createdAt time.Time) {
	if feed == nil || storyID == "" || authorID == "" {
		return
	}

	detached := context.WithoutCancel(ctx)
	go func() {
		writeCtx, cancel := context.WithTimeout(detached, feedFanoutTimeout)
		defer cancel()

		result, err := feed.FanoutStory(writeCtx, storyID, authorID, language, createdAt)
		if err != nil {
			logger.Error(
				"feed fan-out failed",
				"story_id", storyID,
				"author_id", authorID,
				"error", err,
			)
			return
		}

		logger.Debug(
			"feed fan-out complete",
			"story_id", storyID,
			"recipients", result.Recipients,
			"high_fanout", result.HighFanout,
		)
	}()
}

// seedPrecomputedFeed writes a page the SQL fallback just served into the reader's
// feed, so the next read is a cache hit.
//
// It runs off the request path for the same reason the fan-out does: the reader has
// their answer, and how long their next read takes must not extend this one. The
// seed is only ever called for an authenticated reader on the first page — an
// anonymous reader has no feed to seed, and a later page is not the newest entries
// a feed is ordered by.
func seedPrecomputedFeed(ctx context.Context, logger *slog.Logger, feed FeedCache, userID string, entries []feedcache.Entry) {
	if feed == nil || userID == "" || len(entries) == 0 {
		return
	}

	detached := context.WithoutCancel(ctx)
	go func() {
		writeCtx, cancel := context.WithTimeout(detached, feedSeedTimeout)
		defer cancel()

		if err := feed.Seed(writeCtx, userID, entries); err != nil {
			logger.Warn(
				"feed cache seed failed",
				"user_id", userID,
				"error", err,
			)
		}
	}()
}
