// Package cache is Knot's ephemeral store.
//
// It puts Redis behind a deliberately small interface: seven operations, each
// taking a context and returning an error. The rest of the backend depends on
// that interface rather than on a Redis client, which is what lets tests run
// against an in-process server (miniredis) and what will let a later service
// swap the backing store without touching a single call site.
//
// Nothing here knows what a session, a rate limit or a token is. It stores bytes
// under a key and forgets them on a deadline; the meaning of a key belongs to
// the package that writes it, and every key a caller writes should carry a
// prefix that names its owner so one service's keys can never collide with
// another's.
//
// Redis is the backbone for sessions, rate limiting, caching and token
// management (KNOT-ADR-082). This package is the only place that opens a socket
// to it (KNOT-030).
package cache

import (
	"context"
	"errors"
	"fmt"
	"runtime"
	"strings"
	"time"

	"github.com/redis/go-redis/v9"
)

// ErrMiss is returned by Get when the key does not exist.
//
// A missing key is not a failure: for a cache it is the normal case on a cold
// start, and for a rate limiter it is the first request of a window. It is
// nonetheless returned as an error, because a Get that returned ("", nil) for a
// missing key could not be told apart from a Get that returned an empty string,
// and callers would silently treat "no session" as "a session with no data".
// Callers test for it with errors.Is, so wrapping keeps working.
var ErrMiss = errors.New("cache: key not found")

// Cache is the ephemeral store shared by the backend's services.
//
// Implementations must be safe for concurrent use: a single instance is created
// by the composition root and handed to every service.
type Cache interface {
	// Get returns the value stored at key, or an error wrapping ErrMiss when
	// the key holds no value.
	Get(ctx context.Context, key string) (string, error)

	// Set stores value at key. A ttl of zero or less stores the value with no
	// expiry; use SetEX when a value must expire.
	Set(ctx context.Context, key, value string, ttl time.Duration) error

	// SetEX stores value at key and always sets an expiry. A ttl of zero or
	// less is rejected rather than silently treated as "no expiry".
	SetEX(ctx context.Context, key, value string, ttl time.Duration) error

	// Delete removes key. Deleting a key that does not exist is not an error,
	// so a revoke is idempotent.
	Delete(ctx context.Context, key string) error

	// Incr increments the counter at key and returns its new value, creating
	// the counter at 1 when it is absent. When ttl is positive the expiry is set
	// as the counter is created, so a fixed window keeps its length instead of
	// sliding on every request.
	Incr(ctx context.Context, key string, ttl time.Duration) (int64, error)

	// Ping verifies the connection, and is how a caller checks readiness.
	Ping(ctx context.Context) error

	// Close releases the client's connections. The Cache is unusable after it,
	// and closing twice is not an error.
	Close() error
}

// Compile-time proof that the Redis implementation satisfies the contract.
var _ Cache = (*redisCache)(nil)

// Connection tuning.
//
// The values are deliberately modest. A cache that cannot answer in three
// seconds is not slow, it is broken, and waiting longer only turns one stalled
// Redis into a stalled API: every caller fails at roughly the same moment,
// which is far easier to reason about than a request that hangs.
const (
	// dialTimeout bounds establishing a new connection.
	dialTimeout = 5 * time.Second
	// opTimeout bounds a single read or write.
	opTimeout = 3 * time.Second
	// poolTimeout bounds waiting for a free connection from a saturated pool.
	// It is longer than opTimeout so a busy pool reports as pool exhaustion
	// rather than as a command timeout.
	poolTimeout = 4 * time.Second
	// retries is how many times one command is retried after a transport
	// failure. Two is deliberate: a cache is never worth a long retry, because
	// the caller has its own answer to fall back on and a request held open for
	// seconds is worse than a request that fails quickly.
	retries = 2
	// retryBackoff bounds the wait between those retries.
	minRetryBackoff = 10 * time.Millisecond
	maxRetryBackoff = 200 * time.Millisecond
	// minIdleConns is how many connections are kept warm. Redis connections are
	// cheap, and one round trip of dial latency per burst is not.
	minIdleConns = 2
	// connsPerCPU is the pool multiplier. A connection per core per ten is
	// generous for a workload that is mostly one round trip, and it bounds the
	// pool on a large machine where 10*NumCPU would otherwise open hundreds.
	connsPerCPU = 10
)

// redisCache is the go-redis implementation of Cache.
type redisCache struct {
	client *redis.Client
}

// NewRedisCache returns a Cache backed by the Redis server at addr.
//
// addr is host:port. password may be empty, which is the local docker-compose
// server's configuration. db selects the logical database and is normally 0.
//
// It does not dial: go-redis connects lazily on first use, so a Redis that is
// down does not stop the API from starting. Ping is how a caller checks
// reachability, and it is the caller's decision whether an unreachable cache is
// fatal.
func NewRedisCache(addr, password string, db int) (Cache, error) {
	trimmedAddr := strings.TrimSpace(addr)
	if trimmedAddr == "" {
		return nil, fmt.Errorf("cache: redis address must not be empty")
	}
	if db < 0 {
		return nil, fmt.Errorf("cache: redis db must not be negative (got %d)", db)
	}

	client := redis.NewClient(&redis.Options{
		Addr:            trimmedAddr,
		Password:        password,
		DB:              db,
		PoolSize:        poolSize(),
		MinIdleConns:    minIdleConns,
		PoolTimeout:     poolTimeout,
		DialTimeout:     dialTimeout,
		ReadTimeout:     opTimeout,
		WriteTimeout:    opTimeout,
		ConnMaxIdleTime: opTimeout * 10,
		MaxRetries:      retries,
		MinRetryBackoff: minRetryBackoff,
		MaxRetryBackoff: maxRetryBackoff,
	})

	return &redisCache{client: client}, nil
}

// poolSize is the maximum number of connections the client keeps open.
//
// It scales with the machine because each connection serves one command at a
// time; on a single-core developer machine that is ten, which is already more
// than the whole local stack uses.
func poolSize() int {
	size := connsPerCPU * runtime.NumCPU()
	if size < connsPerCPU {
		// runtime.NumCPU is at least 1, but the guard states the floor.
		size = connsPerCPU
	}
	return size
}

// Get returns the value at key, or an error wrapping ErrMiss when it is absent.
func (c *redisCache) Get(ctx context.Context, key string) (string, error) {
	value, err := c.client.Get(ctx, key).Result()
	if err != nil {
		if errors.Is(err, redis.Nil) {
			return "", fmt.Errorf("cache: get %q: %w", key, ErrMiss)
		}
		return "", fmt.Errorf("cache: get %q: %w", key, err)
	}

	return value, nil
}

// Set stores value at key. A ttl of zero or less means no expiry.
func (c *redisCache) Set(ctx context.Context, key, value string, ttl time.Duration) error {
	if err := c.client.Set(ctx, key, value, ttl).Err(); err != nil {
		return fmt.Errorf("cache: set %q: %w", key, err)
	}

	return nil
}

// SetEX stores value at key with an expiry, rejecting a ttl that is not positive.
func (c *redisCache) SetEX(ctx context.Context, key, value string, ttl time.Duration) error {
	if ttl <= 0 {
		return fmt.Errorf("cache: setex %q: ttl must be positive (got %s)", key, ttl)
	}

	if err := c.client.Set(ctx, key, value, ttl).Err(); err != nil {
		return fmt.Errorf("cache: setex %q: %w", key, err)
	}

	return nil
}

// Delete removes key. Removing an absent key is not an error.
func (c *redisCache) Delete(ctx context.Context, key string) error {
	if err := c.client.Del(ctx, key).Err(); err != nil {
		return fmt.Errorf("cache: delete %q: %w", key, err)
	}

	return nil
}

// Incr increments the counter at key, creating it at 1 and arming the expiry
// when it does.
func (c *redisCache) Incr(ctx context.Context, key string, ttl time.Duration) (int64, error) {
	count, err := c.client.Incr(ctx, key).Result()
	if err != nil {
		return 0, fmt.Errorf("cache: incr %q: %w", key, err)
	}

	// The expiry is armed only when the counter is created: that is what makes
	// this a fixed window rather than a window that restarts with every request.
	// A concurrent creator that loses the race sees a count above 1 here and
	// leaves the expiry alone, so the window cannot be extended by load.
	if ttl > 0 && count == 1 {
		if err := c.client.Expire(ctx, key, ttl).Err(); err != nil {
			return count, fmt.Errorf("cache: incr %q: set ttl: %w", key, err)
		}
	}

	return count, nil
}

// Ping reports whether the server answers.
func (c *redisCache) Ping(ctx context.Context) error {
	if err := c.client.Ping(ctx).Err(); err != nil {
		return fmt.Errorf("cache: ping: %w", err)
	}

	return nil
}

// Close releases the client's connections. It is idempotent: closing an already
// closed cache is not an error, because both a deferred Close in the composition
// root and an explicit Close in a test are allowed to run.
func (c *redisCache) Close() error {
	if err := c.client.Close(); err != nil && !errors.Is(err, redis.ErrClosed) {
		return fmt.Errorf("cache: close: %w", err)
	}

	return nil
}
