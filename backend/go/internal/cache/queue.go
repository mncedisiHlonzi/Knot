package cache

// Lists are the second Redis data structure the backend uses outside the seven
// key/value operations: the media pipeline is a job queue, and a Redis list is
// what both processes agree on (KNOT-038).
//
// The producer is this backend, which only ever appends; the consumer is the Rust
// media worker, which claims jobs with BLMOVE and acknowledges them with LREM. The
// surface is therefore one method wide on this side. Keeping it beside the
// key/value contract preserves the package's one promise: internal/cache is the
// only place that opens a socket to Redis, and the pipeline shares the existing
// client and pool instead of dialling a second one.

import (
	"context"
	"fmt"

	"github.com/redis/go-redis/v9"
)

// Queue is a narrow view of one Redis list per key.
//
// It is safe for concurrent use for the same reason Cache is: it holds the shared
// client and no state of its own.
type Queue interface {
	// Push appends value to the TAIL of the list at key.
	//
	// The direction is part of a cross-process contract: the worker claims with
	// `BLMOVE <jobs> <processing> LEFT LEFT`, so a producer that pushed to the head
	// would silently reverse the queue's order. Appending at the tail keeps the
	// pipeline first-in, first-out (KNOT-038).
	Push(ctx context.Context, key, value string) error

	// Length returns how many values the list at key holds, for an operator check
	// that the queue is draining.
	Length(ctx context.Context, key string) (int64, error)
}

// Compile-time proof that the Redis implementation satisfies the contract.
var _ Queue = (*redisQueue)(nil)

// redisQueue is the go-redis implementation of Queue. It shares the client the
// Cache was built with, so the pipeline adds no connections.
type redisQueue struct {
	client *redis.Client
}

// NewQueue returns the list view of c.
//
// It returns the SAME client c already owns. A non-Redis Cache fails loudly here
// rather than silently accepting jobs that nothing would ever consume.
func NewQueue(c Cache) (Queue, error) {
	concrete, ok := c.(*redisCache)
	if !ok {
		return nil, fmt.Errorf("cache: queues require the redis-backed cache")
	}
	return &redisQueue{client: concrete.client}, nil
}

// Push appends value to the tail of the list at key.
func (q *redisQueue) Push(ctx context.Context, key, value string) error {
	if err := q.client.RPush(ctx, key, value).Err(); err != nil {
		return fmt.Errorf("cache: push %q: %w", key, err)
	}

	return nil
}

// Length reports the list's length, and 0 for a key that does not exist.
func (q *redisQueue) Length(ctx context.Context, key string) (int64, error) {
	length, err := q.client.LLen(ctx, key).Result()
	if err != nil {
		return 0, fmt.Errorf("cache: length %q: %w", key, err)
	}

	return length, nil
}
