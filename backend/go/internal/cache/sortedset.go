package cache

// Sorted sets are the one Redis data structure the backend uses outside the
// seven key/value operations above: a precomputed feed is a list of ids ordered
// by time, and Redis's sorted set is exactly that (KNOT-036).
//
// The surface is deliberately two methods wide. Everything else a sorted set can
// do — intersections, unions, ranges by score, temporary keys — is left out,
// because a caller that needs it can say so in a review. Keeping this beside the
// key/value contract (rather than in the feed package) preserves the package's
// one promise: internal/cache is the only place that opens a socket to Redis, and
// a feature built on it shares the existing client and connection pool instead
// of dialling a second one.

import (
	"context"
	"fmt"

	"github.com/redis/go-redis/v9"
)

// SortedSetEntry is one member of a sorted set, with the score it is ordered by.
//
// The feed stores a story id as the member and the story's created_at as the
// score, so the score is what a reader's ordering is built from and what lets two
// sets be merged correctly.
type SortedSetEntry struct {
	// Member is the stored identifier. For a feed it is a story id.
	Member string
	// Score is the ordering value the member was written with.
	Score float64
}

// SortedSet is a narrow view of one Redis sorted set per key.
//
// It is safe for concurrent use for the same reason Cache is: it holds the shared
// client and no state of its own.
type SortedSet interface {
	// Prepend inserts member with score and then trims the set to its highest
	// keep members, in one round trip.
	//
	// Writing a new entry is always "add, then forget the tail": the feed is
	// bounded by keep rather than by a time to live, so it can never grow without
	// limit and never has to be rebuilt from scratch after expiring (KNOT-036).
	// Re-inserting a member replaces its score rather than duplicating it, which
	// makes a repeated fan-out idempotent.
	Prepend(ctx context.Context, key string, score float64, member string, keep int) error

	// Recent returns up to limit members of the set, highest score first, or an
	// empty slice when the key does not exist.
	//
	// A missing key is an empty feed, not an error: a reader who has never been
	// fanned out to is the ordinary cold-start case.
	Recent(ctx context.Context, key string, limit int) ([]SortedSetEntry, error)
}

// Compile-time proof that the Redis implementation satisfies the contract.
var _ SortedSet = (*redisCache)(nil)

// NewSortedSet returns the sorted-set view of c.
//
// It returns the SAME client c already owns, so a service built on it costs no
// extra connection and no extra configuration. The type assertion is deliberate:
// the alternative is either a second pool (which the brief for KNOT-036 forbids)
// or widening Cache with sorted-set methods every key/value caller would have to
// implement. A non-Redis Cache fails loudly here rather than silently serving an
// empty feed.
func NewSortedSet(c Cache) (SortedSet, error) {
	concrete, ok := c.(*redisCache)
	if !ok {
		return nil, fmt.Errorf("cache: sorted sets require the redis-backed cache")
	}
	return concrete, nil
}

// Prepend inserts member with score and trims the set to its highest keep
// members.
//
// The two commands travel as one pipeline, so a fan-out costs one round trip per
// recipient rather than two. Trimming by rank removes the LOWEST-scoring members
// — ranks 0 through -(keep+1) — which is what bounds the feed at its newest keep
// entries.
func (c *redisCache) Prepend(ctx context.Context, key string, score float64, member string, keep int) error {
	if keep < 1 {
		return fmt.Errorf("cache: prepend %q: keep must be positive (got %d)", key, keep)
	}

	pipe := c.client.Pipeline()
	pipe.ZAdd(ctx, key, redis.Z{Score: score, Member: member})
	pipe.ZRemRangeByRank(ctx, key, 0, int64(-keep-1))

	if _, err := pipe.Exec(ctx); err != nil {
		return fmt.Errorf("cache: prepend %q: %w", key, err)
	}

	return nil
}

// Recent returns up to limit members of the set, highest score first.
//
// ZREVRANGE is the read the feed is built on: newest first is the order the
// chronological feed serves, and the rank range is what makes "the first page" a
// single O(log n) command rather than a sort.
func (c *redisCache) Recent(ctx context.Context, key string, limit int) ([]SortedSetEntry, error) {
	if limit < 1 {
		return nil, fmt.Errorf("cache: recent %q: limit must be positive (got %d)", key, limit)
	}

	rows, err := c.client.ZRevRangeWithScores(ctx, key, 0, int64(limit-1)).Result()
	if err != nil {
		return nil, fmt.Errorf("cache: recent %q: %w", key, err)
	}

	entries := make([]SortedSetEntry, 0, len(rows))
	for _, row := range rows {
		member, ok := row.Member.(string)
		if !ok {
			// A member is always written by us as a string. Anything else cannot be
			// an id, so it is skipped rather than misreported.
			continue
		}
		entries = append(entries, SortedSetEntry{Member: member, Score: row.Score})
	}

	return entries, nil
}
