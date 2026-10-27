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
//   - the cached ids no longer resolve to entries (everything trimmed away);
//   - Redis or the follow-up read failed. A feed that cannot be cached is a slower
//     feed, never a broken one.
//
// The cursor has already been checked by the caller: the cached path has no cursor
// of its own (KNOT-ADR-093), so a paginated request is always answered by SQL.
func (h *StoriesHandler) precomputedFeedPage(ctx context.Context, limit int) ([]stories.FeedItem, bool) {
	if h.caches.Feed == nil {
		return nil, false
	}

	userID, ok := UserIDFromContext(ctx)
	if !ok || userID == "" {
		return nil, false
	}

	cached, err := h.caches.Feed.Read(ctx, userID, limit)
	if err != nil {
		h.logger.ErrorContext(
			ctx,
			"feed cache read failed; serving the postgres feed",
			slog.String("request_id", RequestIDFromContext(ctx)),
			slog.String("error", err.Error()),
		)
		return nil, false
	}
	if len(cached) == 0 {
		return nil, false
	}

	// Both kinds are resolved by the same call, through the same SQL branches the
	// feed query uses, so a cached page and a queried page carry identical fields
	// (KNOT-036-fix).
	items, err := h.service.FeedItemsByIDs(ctx, feedRefs(cached))
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
// BOTH kinds are seeded, each into its own set, so a cold reader's cached page
// carries the adaptations the SQL page carried. Before KNOT-036-fix only stories
// were seeded, which meant a reader's very first cached page silently lost every
// adaptation (KNOT-ADR-093 recorded that as a limitation; this is the fix).
func seedEntries(page []stories.FeedItem) []feedcache.Entry {
	entries := make([]feedcache.Entry, 0, len(page))
	for _, item := range page {
		if item.SortID() == "" {
			continue
		}

		entry := feedcache.Entry{
			ID:        item.SortID(),
			Kind:      feedcache.KindStory,
			CreatedAt: item.SortTime(),
		}
		if item.Kind == stories.FeedKindVersion {
			entry.Kind = feedcache.KindVersion
		}

		entries = append(entries, entry)
	}
	return entries
}

// feedRefs maps a page of cached entries onto the refs the stories service
// resolves.
//
// The two types stay separate on purpose: feedcache must not import the stories
// domain, and the domain must not import the cache.
func feedRefs(items []feedcache.Item) []stories.FeedRef {
	refs := make([]stories.FeedRef, 0, len(items))
	for _, item := range items {
		kind := stories.FeedKindStory
		if item.Kind == feedcache.KindVersion {
			kind = stories.FeedKindVersion
		}
		refs = append(refs, stories.FeedRef{Kind: kind, ID: item.ID})
	}
	return refs
}

// FeedCache is the HTTP layer's view of the precomputed feed.
//
// It is an interface rather than the concrete service so a handler test can drive
// the cached path — and every one of its failure modes — without Redis.
type FeedCache interface {
	// Read returns up to limit entries from one reader's feed, newest first, with
	// each entry's kind. An empty slice means "nothing cached", which is a cache
	// miss rather than an error.
	Read(ctx context.Context, userID string, limit int) ([]feedcache.Item, error)
	// FanoutStory pushes one newly created story into the feeds of the readers who
	// should see it.
	FanoutStory(ctx context.Context, storyID, authorID, language string, createdAt time.Time) (feedcache.Result, error)
	// FanoutVersion does the same for an adaptation, into the version feed set.
	FanoutVersion(ctx context.Context, versionID, authorID, language string, createdAt time.Time) (feedcache.Result, error)
	// Seed fills one reader's feeds from a page the SQL fallback just served, so a
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

// recordFeedFanout precomputes the feeds a new entry belongs in, without delaying
// or failing the request that created it.
//
// This is the write half of fan-out on write, and it is deliberately best-effort:
// the entry is already stored and already in the response, so a Redis failure here
// leaves the feed eventually consistent rather than making a successful publish
// look like a failure. A failed fan-out is logged at ERROR because it is the one
// case an operator must be able to see — the entry exists but nobody's feed knows.
//
// kind selects which of the two sets is written: a story id goes into the story set,
// a version id into the version set. A nil cache (the feature is switched off) is a
// silent no-op.
func recordFeedFanout(ctx context.Context, logger *slog.Logger, feed FeedCache, kind feedcache.Kind, id, authorID, language string, createdAt time.Time) {
	if feed == nil || id == "" || authorID == "" {
		return
	}

	detached := context.WithoutCancel(ctx)
	go func() {
		writeCtx, cancel := context.WithTimeout(detached, feedFanoutTimeout)
		defer cancel()

		fanout := feed.FanoutStory
		if kind == feedcache.KindVersion {
			fanout = feed.FanoutVersion
		}

		result, err := fanout(writeCtx, id, authorID, language, createdAt)
		if err != nil {
			logger.Error(
				"feed fan-out failed",
				"kind", string(kind),
				"id", id,
				"author_id", authorID,
				"error", err,
			)
			return
		}

		logger.Debug(
			"feed fan-out complete",
			"kind", string(kind),
			"id", id,
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
