package feedcache

// The precomputed feed (KNOT-036).
//
// Knot's feed is read far more often than it is written, and until now every read
// was a live multi-statement query. This package turns it into fan-out on write:
// when a story is created, its id is pushed into the Redis feed of every reader
// who should see it — the readers who share the author's Rooted place, and the
// readers who speak its language — so the default feed becomes a single sorted-set
// read.
//
// The strategy is hybrid, because pure fan-out has a ceiling: an author with a
// very large audience would cost one write per reader. An author whose audience
// exceeds FanoutThreshold is therefore NOT fanned out. Their story is written once
// to a per-author feed that the read path can pull and merge, which keeps a large
// author's write cost constant.
//
// Nothing here knows what a story is: the package stores ids and numeric scores and
// leaves meaning to the caller. It is also deliberately best-effort — the caller
// runs it off the request path, and a Redis failure must never fail a story
// creation (the feed is eventually consistent, the story is not).

import (
	"context"
	"errors"
	"fmt"
	"sort"
	"time"

	"github.com/knot/backend/internal/cache"
)

// Bounds and defaults for the feed cache.
const (
	// FeedCap is how many entries one reader's feed holds. The sorted set is
	// trimmed to this size on every insert.
	FeedCap = 800
	// FanoutThreshold is the audience size past which an author is treated as
	// high-fanout: their stories are stored once instead of written to every
	// reader's feed.
	FanoutThreshold = 10000
)

// Key prefixes. Every key the backend writes names its owner, so two services can
// never collide on one Redis instance (internal/cache documents the rule).
const (
	feedKeyPrefix       = "knot:feed:"
	authorFeedKeyPrefix = "knot:author_feed:"
)

// FeedKey is the sorted-set key holding one reader's precomputed feed.
func FeedKey(userID string) string { return feedKeyPrefix + userID }

// AuthorFeedKey is the sorted-set key holding one high-fanout author's recent
// stories, which readers pull and merge at read time.
func AuthorFeedKey(authorID string) string { return authorFeedKeyPrefix + authorID }

// Options are the two numbers the feed is built with. They come from
// configuration, so an operator can tune a deployment without a code change.
type Options struct {
	// Cap is the maximum number of entries one feed holds.
	Cap int
	// FanoutThreshold is the audience size above which an author is not fanned
	// out.
	FanoutThreshold int
}

// Feed is the precomputed feed: the write path (FanoutStory) and the read path
// (Read/ReadMerged) over one Redis sorted set per reader.
type Feed struct {
	sets      cache.SortedSet
	audience  Audience
	cap       int
	threshold int
}

// New builds the feed cache over an existing sorted-set client and an audience
// lookup.
//
// The sorted set is the one already open for sessions and rate limiting
// (cache.NewSortedSet), so a deployment adds no second connection pool.
func New(sets cache.SortedSet, audience Audience, opts Options) (*Feed, error) {
	if sets == nil {
		return nil, fmt.Errorf("feedcache: requires a sorted set")
	}
	if audience == nil {
		return nil, fmt.Errorf("feedcache: requires an audience lookup")
	}
	if opts.Cap < 1 {
		return nil, fmt.Errorf("feedcache: cap must be positive (got %d)", opts.Cap)
	}
	if opts.FanoutThreshold < 1 {
		return nil, fmt.Errorf("feedcache: fanout threshold must be positive (got %d)", opts.FanoutThreshold)
	}

	return &Feed{
		sets:      sets,
		audience:  audience,
		cap:       opts.Cap,
		threshold: opts.FanoutThreshold,
	}, nil
}

// Result reports what one fan-out did, so the caller can log it.
type Result struct {
	// Recipients is how many readers' feeds were written. It is zero when the
	// story took the high-fanout path.
	Recipients int
	// HighFanout is true when the author's audience exceeded the threshold and the
	// story was stored in the author feed instead of being fanned out.
	HighFanout bool
}

// FanoutStory pushes one new story into the feed of every reader who should see
// it.
//
// The audience is resolved once, as a bounded read (threshold+1 ids), so a very
// large audience is NOT loaded into memory just to be counted. More than threshold
// candidates means the author is high-fanout: the story is written to the author's
// own feed and no per-reader write happens.
//
// The score is created_at in Unix seconds, which is the same ordering the
// PostgreSQL feed serves (created_at DESC, id DESC). The author is never a
// recipient: their own story must not appear in their feed as news
// (Audience.TargetUserIDs excludes them).
//
// Errors are returned rather than swallowed, but they are never fatal to the
// caller: a story that was stored is a story, whatever Redis thinks. A partial
// failure across recipients is joined so one slow key does not hide the rest.
func (f *Feed) FanoutStory(ctx context.Context, storyID, authorID, language string, createdAt time.Time) (Result, error) {
	if storyID == "" {
		return Result{}, fmt.Errorf("feedcache: fan-out requires a story id")
	}
	if authorID == "" {
		return Result{}, fmt.Errorf("feedcache: fan-out requires an author id")
	}

	// One extra id is asked for so "at or below the threshold" and "above it" are
	// distinguishable without counting the whole audience.
	targets, err := f.audience.TargetUserIDs(ctx, AudienceQuery{
		AuthorID: authorID,
		Language: language,
		Limit:    f.threshold + 1,
	})
	if err != nil {
		return Result{}, fmt.Errorf("feedcache: resolve audience: %w", err)
	}

	score := float64(createdAt.Unix())

	if len(targets) > f.threshold {
		if err := f.sets.Prepend(ctx, AuthorFeedKey(authorID), score, storyID, f.cap); err != nil {
			return Result{HighFanout: true}, fmt.Errorf("feedcache: store author feed: %w", err)
		}
		return Result{HighFanout: true}, nil
	}

	var (
		written int
		// capacity: one entry per recipient at most.
		failures []error
	)
	for _, userID := range targets {
		// Defensive: the audience query already excludes the author, and an empty
		// id would write to a key nobody reads.
		if userID == "" || userID == authorID {
			continue
		}

		if err := f.sets.Prepend(ctx, FeedKey(userID), score, storyID, f.cap); err != nil {
			failures = append(failures, err)
			continue
		}
		written++
	}

	if len(failures) > 0 {
		return Result{Recipients: written}, fmt.Errorf(
			"feedcache: fan-out to %d of %d readers: %w",
			len(failures), len(targets), errors.Join(failures...),
		)
	}

	return Result{Recipients: written}, nil
}

// Read returns up to limit story ids from one reader's precomputed feed, newest
// first, or an empty slice when it has none.
//
// It reads the single sorted set, which is the whole point of the task: one Redis
// command instead of the multi-statement SQL feed.
func (f *Feed) Read(ctx context.Context, userID string, limit int) ([]string, error) {
	return f.ReadMerged(ctx, userID, limit, nil)
}

// Entry is one story to write into a feed, as a cold-start seed supplies them.
type Entry struct {
	// ID is the story id.
	ID string
	// CreatedAt is the story's creation time, which becomes its score.
	CreatedAt time.Time
}

// Seed writes a page of stories into one reader's feed.
//
// It is the cache-miss half of the strategy: a reader the fan-out never reached — a
// new account, or one whose feed was never built — is served once by PostgreSQL and
// then seeded from that page, so their next read is a single Redis lookup. Seeding
// only ever ADDS: the scores are the stories' own created_at values, so a seeded
// entry sorts exactly where a fanned-out one would.
//
// It is best-effort like the fan-out: failures are joined and returned for the
// caller to log, and a partial seed is still useful.
func (f *Feed) Seed(ctx context.Context, userID string, entries []Entry) error {
	if userID == "" || len(entries) == 0 {
		return nil
	}

	var failures []error
	for _, entry := range entries {
		if entry.ID == "" {
			continue
		}
		if err := f.sets.Prepend(ctx, FeedKey(userID), float64(entry.CreatedAt.Unix()), entry.ID, f.cap); err != nil {
			failures = append(failures, err)
		}
	}

	return errors.Join(failures...)
}

// ReadMerged returns up to limit story ids from a reader's feed, merged with the
// recent stories of the given high-fanout authors, newest first.
//
// The merge exists because a high-fanout author is never fanned out: their stories
// live in one author feed that every reader pulls. The result is ordered by score
// (created_at) and then by id, which matches the PostgreSQL feed's
// (created_at DESC, id DESC) so a cached page and a fallback page agree about what
// "newest" means. Duplicate ids — the same story reachable twice — are collapsed.
//
// No cursor is supported: the first `limit` entries are the whole of this path
// for now, and the SQL path keeps pagination (KNOT-ADR-093).
func (f *Feed) ReadMerged(ctx context.Context, userID string, limit int, highFanoutAuthors []string) ([]string, error) {
	if limit < 1 {
		return nil, fmt.Errorf("feedcache: read requires a positive limit (got %d)", limit)
	}
	if userID == "" {
		return []string{}, nil
	}

	entries, err := f.sets.Recent(ctx, FeedKey(userID), limit)
	if err != nil {
		return nil, fmt.Errorf("feedcache: read feed: %w", err)
	}

	merged := make(map[string]float64, len(entries))
	for _, entry := range entries {
		merged[entry.Member] = entry.Score
	}

	for _, authorID := range highFanoutAuthors {
		if authorID == "" {
			continue
		}
		authored, err := f.sets.Recent(ctx, AuthorFeedKey(authorID), limit)
		if err != nil {
			return nil, fmt.Errorf("feedcache: read author feed %q: %w", authorID, err)
		}
		for _, entry := range authored {
			if current, ok := merged[entry.Member]; !ok || entry.Score > current {
				merged[entry.Member] = entry.Score
			}
		}
	}

	return orderByScoreDesc(merged, limit), nil
}

// orderByScoreDesc turns a member->score map into ids ordered newest first, with
// the id as the tie-break, and truncates to limit.
//
// The ordering is total, so two entries written in the same second never swap
// places between reads.
func orderByScoreDesc(merged map[string]float64, limit int) []string {
	type scored struct {
		id    string
		score float64
	}

	ordered := make([]scored, 0, len(merged))
	for id, score := range merged {
		ordered = append(ordered, scored{id: id, score: score})
	}

	sort.Slice(ordered, func(i, j int) bool {
		if ordered[i].score != ordered[j].score {
			return ordered[i].score > ordered[j].score
		}
		return ordered[i].id > ordered[j].id
	})

	if len(ordered) > limit {
		ordered = ordered[:limit]
	}

	ids := make([]string, 0, len(ordered))
	for _, entry := range ordered {
		ids = append(ids, entry.id)
	}

	return ids
}
