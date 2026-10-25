package cache

import (
	"context"
	"errors"
	"strings"
	"testing"
	"time"

	"github.com/alicebob/miniredis/v2"
)

// testTTL is long enough that no assertion races the clock, and short enough
// that a failure to arm an expiry is obvious.
const testTTL = 30 * time.Second

// newTestCache starts an in-process Redis (miniredis) and returns a Cache
// pointing at it, together with the server so a test can inspect or break it.
//
// miniredis is the reason these tests need neither Docker nor a running Redis:
// it speaks the RESP protocol over a real socket, so the code under test is the
// production code path — a real client, real commands, real errors.
func newTestCache(t *testing.T) (*redisCache, *miniredis.Miniredis) {
	t.Helper()

	server := miniredis.RunT(t)

	built, err := NewRedisCache(server.Addr(), "", 0)
	if err != nil {
		t.Fatalf("NewRedisCache() error = %v, want nil", err)
	}

	concrete, ok := built.(*redisCache)
	if !ok {
		t.Fatalf("NewRedisCache() returned %T, want *redisCache", built)
	}

	t.Cleanup(func() {
		if err := concrete.Close(); err != nil {
			t.Logf("close: %v", err)
		}
	})

	return concrete, server
}

// TestNewRedisCacheValidation covers the constructor's rejections.
func TestNewRedisCacheValidation(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name     string
		addr     string
		db       int
		wantErr  bool
		wantWord string
	}{
		{name: "empty address", addr: "", db: 0, wantErr: true, wantWord: "address"},
		{name: "blank address", addr: "   ", db: 0, wantErr: true, wantWord: "address"},
		{name: "negative db", addr: "localhost:6379", db: -1, wantErr: true, wantWord: "db"},
		{name: "zero db", addr: "localhost:6379", db: 0, wantErr: false},
	}

	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			t.Parallel()

			built, err := NewRedisCache(test.addr, "", test.db)

			if test.wantErr {
				if err == nil {
					t.Fatalf("NewRedisCache(%q, %d) error = nil, want an error", test.addr, test.db)
				}
				if !strings.Contains(err.Error(), test.wantWord) {
					t.Fatalf("NewRedisCache() error = %q, want it to mention %q", err, test.wantWord)
				}
				if built != nil {
					t.Fatalf("NewRedisCache() cache = %v, want nil", built)
				}
				return
			}

			if err != nil {
				t.Fatalf("NewRedisCache() error = %v, want nil", err)
			}
			t.Cleanup(func() {
				if closeErr := built.Close(); closeErr != nil {
					t.Logf("close: %v", closeErr)
				}
			})
		})
	}
}

// TestSetGetRoundTrip proves a stored value comes back unchanged.
func TestSetGetRoundTrip(t *testing.T) {
	t.Parallel()

	c, _ := newTestCache(t)
	ctx := context.Background()

	const (
		key   = "knot:test:greeting"
		value = "hello — Γειά σου — مرحبا"
	)

	if err := c.Set(ctx, key, value, 0); err != nil {
		t.Fatalf("Set() error = %v, want nil", err)
	}

	got, err := c.Get(ctx, key)
	if err != nil {
		t.Fatalf("Get() error = %v, want nil", err)
	}
	if got != value {
		t.Fatalf("Get() = %q, want %q", got, value)
	}
}

// TestGetMissingReportsMiss proves an absent key is a distinguishable ErrMiss
// rather than a bare error or an empty string.
func TestGetMissingReportsMiss(t *testing.T) {
	t.Parallel()

	c, _ := newTestCache(t)

	got, err := c.Get(context.Background(), "knot:test:absent")
	if err == nil {
		t.Fatalf("Get() error = nil, want a miss from %q", got)
	}
	if !errors.Is(err, ErrMiss) {
		t.Fatalf("Get() error = %v, want it to wrap ErrMiss", err)
	}
	if got != "" {
		t.Fatalf("Get() = %q, want the empty string", got)
	}
}

// TestDelete proves deletion removes the value and is idempotent.
func TestDelete(t *testing.T) {
	t.Parallel()

	c, server := newTestCache(t)
	ctx := context.Background()

	const key = "knot:test:delete"

	if err := c.Set(ctx, key, "value", 0); err != nil {
		t.Fatalf("Set() error = %v, want nil", err)
	}

	if err := c.Delete(ctx, key); err != nil {
		t.Fatalf("Delete() error = %v, want nil", err)
	}

	if _, err := c.Get(ctx, key); !errors.Is(err, ErrMiss) {
		t.Fatalf("Get() after Delete() error = %v, want ErrMiss", err)
	}

	// Deleting what is already gone must not fail: a revoke that runs twice is
	// normal when a client retries a request.
	if err := c.Delete(ctx, key); err != nil {
		t.Fatalf("second Delete() error = %v, want nil", err)
	}

	if server.Exists(key) {
		t.Fatalf("server still holds %q after Delete()", key)
	}
}

// TestSetWithoutTTLPersists proves a zero ttl really means "no expiry".
func TestSetWithoutTTLPersists(t *testing.T) {
	t.Parallel()

	c, server := newTestCache(t)
	ctx := context.Background()

	const key = "knot:test:persistent"

	if err := c.Set(ctx, key, "value", 0); err != nil {
		t.Fatalf("Set() error = %v, want nil", err)
	}

	if ttl := server.TTL(key); ttl != 0 {
		t.Fatalf("TTL() = %s, want no expiry", ttl)
	}
}

// TestSetWithTTLExpires proves a positive ttl is armed, and that the value is
// gone once the window closes.
func TestSetWithTTLExpires(t *testing.T) {
	t.Parallel()

	c, server := newTestCache(t)
	ctx := context.Background()

	const key = "knot:test:expiring"

	if err := c.Set(ctx, key, "value", testTTL); err != nil {
		t.Fatalf("Set() error = %v, want nil", err)
	}

	if ttl := server.TTL(key); ttl != testTTL {
		t.Fatalf("TTL() = %s, want %s", ttl, testTTL)
	}

	server.FastForward(testTTL)

	if _, err := c.Get(ctx, key); !errors.Is(err, ErrMiss) {
		t.Fatalf("Get() after expiry error = %v, want ErrMiss", err)
	}
}

// TestSetEX covers the explicit-expiry variant, including its rejection of a
// non-positive ttl.
func TestSetEX(t *testing.T) {
	t.Parallel()

	c, server := newTestCache(t)
	ctx := context.Background()

	const key = "knot:test:setex"

	if err := c.SetEX(ctx, key, "value", testTTL); err != nil {
		t.Fatalf("SetEX() error = %v, want nil", err)
	}

	if ttl := server.TTL(key); ttl != testTTL {
		t.Fatalf("TTL() = %s, want %s", ttl, testTTL)
	}

	got, err := c.Get(ctx, key)
	if err != nil {
		t.Fatalf("Get() error = %v, want nil", err)
	}
	if got != "value" {
		t.Fatalf("Get() = %q, want %q", got, "value")
	}

	for _, ttl := range []time.Duration{0, -time.Second} {
		if err := c.SetEX(ctx, "knot:test:setex:bad", "value", ttl); err == nil {
			t.Fatalf("SetEX() with ttl %s error = nil, want an error", ttl)
		}
	}
}

// TestIncr proves the counter counts from one, arms its expiry once, and keeps
// that expiry while the window is open.
func TestIncr(t *testing.T) {
	t.Parallel()

	c, server := newTestCache(t)
	ctx := context.Background()

	const key = "knot:test:counter"

	for want := int64(1); want <= 3; want++ {
		got, err := c.Incr(ctx, key, testTTL)
		if err != nil {
			t.Fatalf("Incr() error = %v, want nil", err)
		}
		if got != want {
			t.Fatalf("Incr() = %d, want %d", got, want)
		}
		// The expiry is armed when the counter is created and never extended, so
		// the window length stays testTTL no matter how many increments land.
		if ttl := server.TTL(key); ttl != testTTL {
			t.Fatalf("after increment %d TTL() = %s, want %s", want, ttl, testTTL)
		}
	}

	server.FastForward(testTTL)

	got, err := c.Incr(ctx, key, testTTL)
	if err != nil {
		t.Fatalf("Incr() after expiry error = %v, want nil", err)
	}
	if got != 1 {
		t.Fatalf("Incr() after expiry = %d, want 1 (a new window)", got)
	}
}

// TestIncrWithoutTTL proves a zero ttl leaves the counter without an expiry.
func TestIncrWithoutTTL(t *testing.T) {
	t.Parallel()

	c, server := newTestCache(t)

	if _, err := c.Incr(context.Background(), "knot:test:counter:no-ttl", 0); err != nil {
		t.Fatalf("Incr() error = %v, want nil", err)
	}

	if ttl := server.TTL("knot:test:counter:no-ttl"); ttl != 0 {
		t.Fatalf("TTL() = %s, want no expiry", ttl)
	}
}

// TestPingAndClose proves Ping reflects reachability and that Close releases the
// client.
func TestPingAndClose(t *testing.T) {
	t.Parallel()

	c, server := newTestCache(t)
	ctx := context.Background()

	if err := c.Ping(ctx); err != nil {
		t.Fatalf("Ping() error = %v, want nil", err)
	}

	server.Close()

	if err := c.Ping(ctx); err == nil {
		t.Fatal("Ping() after the server closed error = nil, want an error")
	}

	if err := c.Close(); err != nil {
		t.Fatalf("Close() error = %v, want nil", err)
	}

	// A cache used after Close is a bug in the caller, and it must surface as an
	// error rather than as a panic or a silent success.
	if err := c.Ping(ctx); err == nil {
		t.Fatal("Ping() after Close() error = nil, want an error")
	}
}

// TestBackendFailureIsNotAMiss proves a broken connection is reported as a
// failure rather than disguised as a cache miss. Everything that treats a miss
// as "not present" — a session lookup, a rate limit — must be able to tell the
// two apart, or an outage would look like every user having been signed out.
func TestBackendFailureIsNotAMiss(t *testing.T) {
	t.Parallel()

	c, server := newTestCache(t)
	ctx := context.Background()

	if err := c.Set(ctx, "knot:test:broken", "value", 0); err != nil {
		t.Fatalf("Set() error = %v, want nil", err)
	}

	server.Close()

	if _, err := c.Get(ctx, "knot:test:broken"); err == nil || errors.Is(err, ErrMiss) {
		t.Fatalf("Get() against a closed server error = %v, want a failure that is not ErrMiss", err)
	}

	if err := c.Set(ctx, "knot:test:broken", "value", 0); err == nil {
		t.Fatal("Set() against a closed server error = nil, want an error")
	}

	if err := c.Delete(ctx, "knot:test:broken"); err == nil {
		t.Fatal("Delete() against a closed server error = nil, want an error")
	}

	if _, err := c.Incr(ctx, "knot:test:broken", testTTL); err == nil {
		t.Fatal("Incr() against a closed server error = nil, want an error")
	}
}
