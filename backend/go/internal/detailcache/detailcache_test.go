package detailcache

import (
	"context"
	"strings"
	"testing"
	"time"

	"github.com/alicebob/miniredis/v2"

	"github.com/knot/backend/internal/cache"
)

// Canonical UUID text, because the real callers only ever pass ids PostgreSQL
// generated.
const (
	storyID   = "11111111-1111-4111-8111-111111111111"
	versionID = "22222222-2222-4222-8222-222222222222"
)

// payload is what the handler stores: it is opaque to this package, which is the
// point — the cache never has to understand the response shape.
const payload = `{"story":{"id":"11111111-1111-4111-8111-111111111111","title":"The first rain"}}`

// newTestCache returns a cache over an in-process Redis, so the TTL behaviour is
// real rather than mocked.
func newTestCache(t *testing.T, ttl time.Duration) (*Cache, *miniredis.Miniredis) {
	t.Helper()

	server := miniredis.RunT(t)
	redisCache, err := cache.NewRedisCache(server.Addr(), "", 0)
	if err != nil {
		t.Fatalf("NewRedisCache() error = %v, want nil", err)
	}
	t.Cleanup(func() { _ = redisCache.Close() })

	details, err := New(redisCache, ttl)
	if err != nil {
		t.Fatalf("New() error = %v, want nil", err)
	}

	return details, server
}

func TestKeysAreNamespaced(t *testing.T) {
	if got, want := StoryKey(storyID), "knot:story:"+storyID; got != want {
		t.Errorf("StoryKey() = %q, want %q", got, want)
	}
	if got, want := VersionKey(versionID), "knot:version:"+versionID; got != want {
		t.Errorf("VersionKey() = %q, want %q", got, want)
	}
}

func TestStoryJSONRoundTrip(t *testing.T) {
	details, server := newTestCache(t, DefaultTTL)
	ctx := context.Background()

	if err := details.SetStoryJSON(ctx, storyID, payload); err != nil {
		t.Fatalf("SetStoryJSON() error = %v, want nil", err)
	}

	if !server.Exists(StoryKey(storyID)) {
		t.Fatalf("key %q was not written", StoryKey(storyID))
	}

	got, ok, err := details.GetStoryJSON(ctx, storyID)
	if err != nil {
		t.Fatalf("GetStoryJSON() error = %v, want nil", err)
	}
	if !ok {
		t.Fatal("GetStoryJSON() ok = false, want the stored payload")
	}
	if got != payload {
		t.Errorf("payload = %q, want it back unchanged", got)
	}

	// The TTL is armed, not just the value.
	if ttl := server.TTL(StoryKey(storyID)); ttl <= 0 || ttl > DefaultTTL {
		t.Errorf("ttl = %s, want (0, %s]", ttl, DefaultTTL)
	}
}

func TestVersionJSONRoundTrip(t *testing.T) {
	details, server := newTestCache(t, DefaultTTL)
	ctx := context.Background()

	if err := details.SetVersionJSON(ctx, versionID, payload); err != nil {
		t.Fatalf("SetVersionJSON() error = %v, want nil", err)
	}
	if !server.Exists(VersionKey(versionID)) {
		t.Fatalf("key %q was not written", VersionKey(versionID))
	}

	got, ok, err := details.GetVersionJSON(ctx, versionID)
	if err != nil {
		t.Fatalf("GetVersionJSON() error = %v, want nil", err)
	}
	if !ok || got != payload {
		t.Errorf("GetVersionJSON() = (%q, %v), want the stored payload", got, ok)
	}
}

// TestEntryExpiresAfterTheTTL is the reason the cache has a deadline at all: the
// write paths that can change a detail (reactions, comments, adaptations) do not
// all invalidate it, so it must heal on its own.
func TestEntryExpiresAfterTheTTL(t *testing.T) {
	const ttl = 3 * time.Second

	details, server := newTestCache(t, ttl)
	ctx := context.Background()

	if err := details.SetStoryJSON(ctx, storyID, payload); err != nil {
		t.Fatalf("SetStoryJSON() error = %v, want nil", err)
	}
	if err := details.SetVersionJSON(ctx, versionID, payload); err != nil {
		t.Fatalf("SetVersionJSON() error = %v, want nil", err)
	}

	// Just before the deadline the entry is still there.
	server.FastForward(ttl - time.Second)
	if _, ok, err := details.GetStoryJSON(ctx, storyID); err != nil || !ok {
		t.Fatalf("GetStoryJSON() = (ok %v, err %v) before the ttl, want a hit", ok, err)
	}

	server.FastForward(2 * time.Second)

	if _, ok, err := details.GetStoryJSON(ctx, storyID); err != nil || ok {
		t.Errorf("GetStoryJSON() = (ok %v, err %v) after the ttl, want a miss", ok, err)
	}
	if _, ok, err := details.GetVersionJSON(ctx, versionID); err != nil || ok {
		t.Errorf("GetVersionJSON() = (ok %v, err %v) after the ttl, want a miss", ok, err)
	}
}

func TestMissIsNotAnError(t *testing.T) {
	details, _ := newTestCache(t, DefaultTTL)
	ctx := context.Background()

	got, ok, err := details.GetStoryJSON(ctx, storyID)
	if err != nil {
		t.Fatalf("GetStoryJSON() error = %v, want nil for a miss", err)
	}
	if ok || got != "" {
		t.Errorf("GetStoryJSON() = (%q, %v), want the zero result", got, ok)
	}
}

func TestInvalidateRemovesBothKinds(t *testing.T) {
	details, server := newTestCache(t, DefaultTTL)
	ctx := context.Background()

	if err := details.SetStoryJSON(ctx, storyID, payload); err != nil {
		t.Fatalf("SetStoryJSON() error = %v, want nil", err)
	}
	if err := details.SetVersionJSON(ctx, versionID, payload); err != nil {
		t.Fatalf("SetVersionJSON() error = %v, want nil", err)
	}

	if err := details.InvalidateStory(ctx, storyID); err != nil {
		t.Fatalf("InvalidateStory() error = %v, want nil", err)
	}
	if server.Exists(StoryKey(storyID)) {
		t.Error("the story key survived invalidation")
	}
	if !server.Exists(VersionKey(versionID)) {
		t.Error("invalidating a story removed the version key")
	}

	if err := details.InvalidateVersion(ctx, versionID); err != nil {
		t.Fatalf("InvalidateVersion() error = %v, want nil", err)
	}
	if server.Exists(VersionKey(versionID)) {
		t.Error("the version key survived invalidation")
	}

	// Deleting an absent key is idempotent: a write path may call this blindly.
	if err := details.InvalidateStory(ctx, storyID); err != nil {
		t.Errorf("InvalidateStory() on an absent key error = %v, want nil", err)
	}
}

func TestEmptyIDsAreNoOps(t *testing.T) {
	details, server := newTestCache(t, DefaultTTL)
	ctx := context.Background()

	if err := details.SetStoryJSON(ctx, "", payload); err != nil {
		t.Errorf("SetStoryJSON(\"\") error = %v, want nil", err)
	}
	if err := details.SetVersionJSON(ctx, "", ""); err != nil {
		t.Errorf("SetVersionJSON(\"\", \"\") error = %v, want nil", err)
	}
	if _, ok, err := details.GetStoryJSON(ctx, ""); err != nil || ok {
		t.Errorf("GetStoryJSON(\"\") = (ok %v, err %v), want a miss", ok, err)
	}
	if err := details.InvalidateVersion(ctx, ""); err != nil {
		t.Errorf("InvalidateVersion(\"\") error = %v, want nil", err)
	}

	// Nothing was written, and in particular no key with an empty id.
	for _, key := range []string{storyKeyPrefix, versionKeyPrefix} {
		if server.Exists(key) {
			t.Errorf("key %q was written for an empty id", key)
		}
	}
	if server.Exists(StoryKey("")) || server.Exists(VersionKey("")) {
		t.Error("a key with an empty id was written")
	}
}

func TestNewValidatesItsInputs(t *testing.T) {
	server := miniredis.RunT(t)
	redisCache, err := cache.NewRedisCache(server.Addr(), "", 0)
	if err != nil {
		t.Fatalf("NewRedisCache() error = %v, want nil", err)
	}
	t.Cleanup(func() { _ = redisCache.Close() })

	if _, err := New(nil, DefaultTTL); err == nil {
		t.Error("New(nil) error = nil, want an error")
	}
	if _, err := New(redisCache, 0); err == nil {
		t.Error("New(ttl 0) error = nil, want an error: a cache with no deadline is not a cache")
	}
	if _, err := New(redisCache, -time.Minute); err == nil {
		t.Error("New(negative ttl) error = nil, want an error")
	}
}

// TestStoreContractIsSatisfied is a compile-time-ish guard with a runtime message,
// so a future method added to Store has to be implemented here.
func TestStoreContractIsSatisfied(t *testing.T) {
	details, _ := newTestCache(t, DefaultTTL)

	var store Store = details
	if !strings.HasPrefix(StoryKey(storyID), "knot:story:") {
		t.Fatal("StoryKey lost its namespace")
	}
	if store == nil {
		t.Fatal("the Redis cache does not satisfy Store")
	}
}
