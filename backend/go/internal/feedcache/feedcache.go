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
//
// There are two feed sets per reader, not one: a story id and a version id live in
// different tables and are resolved by different queries, so keeping them apart
// keeps every existing story write and the story set itself unchanged. The merge
// happens at read time, which is where the SQL feed's UNION also happens
// (KNOT-036-fix).
const (
	feedKeyPrefix               = "knot:feed:"
	feedVersionsKeyPrefix       = "knot:feed_versions:"
	authorFeedKeyPrefix         = "knot:author_feed:"
	authorFeedVersionsKeyPrefix = "knot:author_feed_versions:"
)

// FeedKey is the sorted-set key holding one reader's precomputed story feed.
func FeedKey(userID string) string { return feedKeyPrefix + userID }

// FeedVersionsKey is the sorted-set key holding one reader's precomputed
// adaptation feed: version ids, scored by the version's own created_at.
//
// It is a second set rather than a member prefix in the first one, so a story
// fan-out writes exactly what it always wrote and an adaptation cannot be
// mistaken for a story by a reader that only understands the old format.
func FeedVersionsKey(userID string) string { return feedVersionsKeyPrefix + userID }

// AuthorFeedKey is the sorted-set key holding one high-fanout author's recent
// stories, which readers pull and merge at read time.
func AuthorFeedKey(authorID string) string { return authorFeedKeyPrefix + authorID }

// AuthorFeedVersionsKey is the version counterpart of AuthorFeedKey: one
// high-fanout author's recent adaptations.
func AuthorFeedVersionsKey(authorID string) string {
	return authorFeedVersionsKeyPrefix + authorID
}

// Kind discriminates the two entry kinds a precomputed feed carries. Its values
// mirror stories.FeedKind on purpose: the strings travel no further than this
// package and the handler maps them onto the domain's own type.
type Kind string

// The precomputed feed's entry kinds.
const (
	// KindStory is a story entry: a story id, resolved from the stories table.
	KindStory Kind = "story"
	// KindVersion is an adaptation entry: a version id, resolved from
	// story_versions.
	KindVersion Kind = "version"
)

// Item is one entry of a reader's precomputed feed: which kind of entry it is and
// the id to resolve.
//
// The kind is not decoration. A cached page has to be assembled from stories AND
// adaptations, and only the set an id was read from says which table resolves it
// (KNOT-036-fix).
type Item struct {
	// ID is the story id or the version id.
	ID string
	// Kind says which table resolves ID.
	Kind Kind
}

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

	return f.fanout(ctx, KindStory, storyID, authorID, language, createdAt)
}

// FanoutVersion is FanoutStory for an adaptation.
//
// It writes the VERSION id into the version set with the version's own created_at
// as its score, so an adaptation appears where it was written rather than where
// its story was — the same rule the SQL feed follows (KNOT-022). The audience is
// resolved by exactly the same query: a reader who would see a story by this
// author is a reader who should see their adaptation of someone else's.
func (f *Feed) FanoutVersion(ctx context.Context, versionID, authorID, language string, createdAt time.Time) (Result, error) {
	if versionID == "" {
		return Result{}, fmt.Errorf("feedcache: fan-out requires a version id")
	}
	if authorID == "" {
		return Result{}, fmt.Errorf("feedcache: fan-out requires an author id")
	}

	return f.fanout(ctx, KindVersion, versionID, authorID, language, createdAt)
}

// fanout is the one implementation both entry kinds share: resolve the audience
// once, then either write every reader's set (normal authors) or the author's own
// set once (high-fanout authors).
//
// Only the two key functions differ by kind, which is what keeps the story and
// version rules from drifting apart.
func (f *Feed) fanout(ctx context.Context, kind Kind, id, authorID, language string, createdAt time.Time) (Result, error) {
	feedKey, authorKey := FeedKey, AuthorFeedKey
	if kind == KindVersion {
		feedKey, authorKey = FeedVersionsKey, AuthorFeedVersionsKey
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
		if err := f.sets.Prepend(ctx, authorKey(authorID), score, id, f.cap); err != nil {
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

		if err := f.sets.Prepend(ctx, feedKey(userID), score, id, f.cap); err != nil {
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

// Read returns up to limit entries from one reader's precomputed feed, newest
// first, or an empty slice when it has none.
//
// It reads one key per entry kind (two Redis commands), which is still the point
// of the task: a fixed, tiny number of commands instead of the multi-statement SQL
// feed.
func (f *Feed) Read(ctx context.Context, userID string, limit int) ([]Item, error) {
	return f.ReadMerged(ctx, userID, limit, nil)
}

// Entry is one feed entry to write, as a cold-start seed supplies them.
type Entry struct {
	// ID is the story id or the version id.
	ID string
	// Kind says which set the entry belongs in. An empty Kind means KindStory,
	// which is what the story-only seed wrote before adaptations were cached.
	Kind Kind
	// CreatedAt is the entry's creation time, which becomes its score.
	CreatedAt time.Time
}

// Seed writes a page of feed entries into one reader's feeds.
//
// It is the cache-miss half of the strategy: a reader the fan-out never reached — a
// new account, or one whose feed was never built — is served once by PostgreSQL and
// then seeded from that page, so their next read is a single pair of Redis
// lookups. Seeding only ever ADDS: the scores are the entries' own created_at
// values, so a seeded entry sorts exactly where a fanned-out one would.
//
// Both kinds are seeded, each into its own set, so a cold reader's cached page
// carries the adaptations the SQL page carried (KNOT-036-fix).
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

		key := FeedKey(userID)
		if entry.Kind == KindVersion {
			key = FeedVersionsKey(userID)
		}

		if err := f.sets.Prepend(ctx, key, float64(entry.CreatedAt.Unix()), entry.ID, f.cap); err != nil {
			failures = append(failures, err)
		}
	}

	return errors.Join(failures...)
}

// ReadMerged returns up to limit entries from a reader's feeds, merged with the
// recent entries of the given high-fanout authors, newest first.
//
// The merge is the read-time equivalent of the SQL feed's UNION: the story set and
// the version set are read independently and interleaved by score, so an adaptation
// sits exactly where it was written (KNOT-036-fix). Ordering is by score
// (created_at) then id, which matches the PostgreSQL feed's
// (created_at DESC, id DESC) so a cached page and a fallback page agree about what
// "newest" means. The kind is part of the identity, so the same id in two sets is
// two entries rather than a collision.
//
// The author feeds exist because a high-fanout author is never fanned out: their
// entries live in one set that every reader pulls.
//
// No cursor is supported: the first `limit` entries are the whole of this path,
// and the SQL path keeps pagination (KNOT-ADR-093).
func (f *Feed) ReadMerged(ctx context.Context, userID string, limit int, highFanoutAuthors []string) ([]Item, error) {
	if limit < 1 {
		return nil, fmt.Errorf("feedcache: read requires a positive limit (got %d)", limit)
	}
	if userID == "" {
		return []Item{}, nil
	}

	merged := make(map[string]scoredItem, limit)

	add := func(key string, kind Kind) error {
		entries, err := f.sets.Recent(ctx, key, limit)
		if err != nil {
			return err
		}
		for _, entry := range entries {
			identity := string(kind) + "\x00" + entry.Member
			if current, ok := merged[identity]; !ok || entry.Score > current.score {
				merged[identity] = scoredItem{
					item:  Item{ID: entry.Member, Kind: kind},
					score: entry.Score,
				}
			}
		}
		return nil
	}

	if err := add(FeedKey(userID), KindStory); err != nil {
		return nil, fmt.Errorf("feedcache: read feed: %w", err)
	}
	if err := add(FeedVersionsKey(userID), KindVersion); err != nil {
		return nil, fmt.Errorf("feedcache: read version feed: %w", err)
	}

	for _, authorID := range highFanoutAuthors {
		if authorID == "" {
			continue
		}
		if err := add(AuthorFeedKey(authorID), KindStory); err != nil {
			return nil, fmt.Errorf("feedcache: read author feed %q: %w", authorID, err)
		}
		if err := add(AuthorFeedVersionsKey(authorID), KindVersion); err != nil {
			return nil, fmt.Errorf("feedcache: read author version feed %q: %w", authorID, err)
		}
	}

	return orderByScoreDesc(merged, limit), nil
}

// scoredItem is one merged entry with the score it is ordered by.
type scoredItem struct {
	item  Item
	score float64
}

// orderByScoreDesc interleaves the two kinds and truncates to limit.
//
// The ordering is total — score, then id, then kind — so two entries written in
// the same second never swap places between reads, and two kinds can never tie.
func orderByScoreDesc(merged map[string]scoredItem, limit int) []Item {
	ordered := make([]scoredItem, 0, len(merged))
	for _, entry := range merged {
		ordered = append(ordered, entry)
	}

	sort.Slice(ordered, func(i, j int) bool {
		if ordered[i].score != ordered[j].score {
			return ordered[i].score > ordered[j].score
		}
		if ordered[i].item.ID != ordered[j].item.ID {
			return ordered[i].item.ID > ordered[j].item.ID
		}
		return ordered[i].item.Kind > ordered[j].item.Kind
	})

	if len(ordered) > limit {
		ordered = ordered[:limit]
	}

	items := make([]Item, 0, len(ordered))
	for _, entry := range ordered {
		items = append(items, entry.item)
	}

	return items
}
