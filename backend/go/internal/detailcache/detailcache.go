package detailcache

// The story and version detail cache (KNOT-036-fix).
//
// The feed is precomputed (KNOT-036), but a tap on an entry still cost a handful of
// statements: the row, the author, the Rooted summary, the media, four count
// queries, the reactions. This package stores the serialized detail response under
// its own key so a repeat tap is one Redis GET.
//
// Two things this package deliberately does NOT decide:
//
//   - **What is reader-specific.** A detail response carries `my_reactions`, which
//     belongs to the reader who asked. The handler stores the ANONYMOUS projection
//     (an empty `my_reactions`) and re-applies the reader's own signals on a hit, so
//     one reader can never be shown another's.
//   - **Whether a reader may see the entry at all.** Visibility is a per-request
//     block decision made from the request context, and the handler re-applies it on
//     a hit rather than trusting the cached bytes.
//
// The TTL is the honesty valve: reactions, comments and adaptations change the
// response and are not all invalidated yet, so a cached detail is allowed to be up
// to the TTL stale rather than invalidated from every write site that could touch
// it (KNOT-ADR-094).

import (
	"context"
	"errors"
	"fmt"
	"time"

	"github.com/knot/backend/internal/cache"
)

// Key prefixes. Every key names its owner, so two services can never collide on
// one Redis instance (internal/cache documents the rule).
const (
	storyKeyPrefix   = "knot:story:"
	versionKeyPrefix = "knot:version:"
)

// DefaultTTL is how long a cached detail response lives.
//
// Ten minutes is long enough that a browse of the feed hits warm entries and short
// enough that a stale reaction count or version count self-heals without an
// operator doing anything.
const DefaultTTL = 10 * time.Minute

// StoryKey is the key holding one story's serialized detail response.
func StoryKey(storyID string) string { return storyKeyPrefix + storyID }

// VersionKey is the key holding one version's serialized detail response.
func VersionKey(versionID string) string { return versionKeyPrefix + versionID }

// Cache stores serialized detail responses in Redis.
//
// It reads and writes opaque strings: marshalling and unmarshalling belong to the
// HTTP layer that owns the response shape, so this package never has to import it.
type Cache struct {
	store cache.Cache
	ttl   time.Duration
}

// Compile-time proof that the Redis-backed cache satisfies the contract.
var _ Store = (*Cache)(nil)

// Store is what the HTTP layer depends on, so a handler test needs no Redis.
type Store interface {
	// GetStoryJSON returns a story's cached payload. A miss is an empty payload
	// with false: an absent key is the ordinary cold case, not an error.
	GetStoryJSON(ctx context.Context, storyID string) (string, bool, error)
	// SetStoryJSON stores a story's payload for the configured TTL.
	SetStoryJSON(ctx context.Context, storyID, payload string) error
	// GetVersionJSON returns a version's cached payload, with the same miss
	// convention.
	GetVersionJSON(ctx context.Context, versionID string) (string, bool, error)
	// SetVersionJSON stores a version's payload for the configured TTL.
	SetVersionJSON(ctx context.Context, versionID, payload string) error
	// InvalidateStory deletes a story's cached payload. Deleting an absent key is
	// not an error, so a write path can call it unconditionally.
	InvalidateStory(ctx context.Context, storyID string) error
	// InvalidateVersion deletes a version's cached payload, with the same
	// convention.
	InvalidateVersion(ctx context.Context, versionID string) error
}

// New returns a detail cache over an existing Redis client.
//
// The client is the one already open for sessions, rate limiting and the
// precomputed feed, so a deployment adds no second connection pool.
func New(store cache.Cache, ttl time.Duration) (*Cache, error) {
	if store == nil {
		return nil, fmt.Errorf("detailcache: requires a cache")
	}
	if ttl <= 0 {
		return nil, fmt.Errorf("detailcache: ttl must be positive (got %s)", ttl)
	}

	return &Cache{store: store, ttl: ttl}, nil
}

// GetStoryJSON returns a story's cached payload, or false on a miss.
func (c *Cache) GetStoryJSON(ctx context.Context, storyID string) (string, bool, error) {
	return c.get(ctx, StoryKey(storyID), storyID)
}

// SetStoryJSON stores a story's payload for the cache's TTL.
func (c *Cache) SetStoryJSON(ctx context.Context, storyID, payload string) error {
	return c.set(ctx, StoryKey(storyID), storyID, payload)
}

// GetVersionJSON returns a version's cached payload, or false on a miss.
func (c *Cache) GetVersionJSON(ctx context.Context, versionID string) (string, bool, error) {
	return c.get(ctx, VersionKey(versionID), versionID)
}

// SetVersionJSON stores a version's payload for the cache's TTL.
func (c *Cache) SetVersionJSON(ctx context.Context, versionID, payload string) error {
	return c.set(ctx, VersionKey(versionID), versionID, payload)
}

// InvalidateStory deletes a story's cached payload.
func (c *Cache) InvalidateStory(ctx context.Context, storyID string) error {
	return c.invalidate(ctx, StoryKey(storyID), storyID)
}

// InvalidateVersion deletes a version's cached payload.
func (c *Cache) InvalidateVersion(ctx context.Context, versionID string) error {
	return c.invalidate(ctx, VersionKey(versionID), versionID)
}

// get reads one key, translating cache.ErrMiss into a miss rather than an error.
func (c *Cache) get(ctx context.Context, key, id string) (string, bool, error) {
	if id == "" {
		return "", false, nil
	}

	payload, err := c.store.Get(ctx, key)
	if err != nil {
		if errors.Is(err, cache.ErrMiss) {
			return "", false, nil
		}
		return "", false, fmt.Errorf("detailcache: get %q: %w", key, err)
	}

	return payload, true, nil
}

// set writes one key with the cache's TTL.
func (c *Cache) set(ctx context.Context, key, id, payload string) error {
	if id == "" || payload == "" {
		return nil
	}

	if err := c.store.SetEX(ctx, key, payload, c.ttl); err != nil {
		return fmt.Errorf("detailcache: set %q: %w", key, err)
	}

	return nil
}

// invalidate deletes one key. Deleting an absent key is not an error, so this is
// idempotent and safe to call on every write.
func (c *Cache) invalidate(ctx context.Context, key, id string) error {
	if id == "" {
		return nil
	}

	if err := c.store.Delete(ctx, key); err != nil {
		return fmt.Errorf("detailcache: invalidate %q: %w", key, err)
	}

	return nil
}
