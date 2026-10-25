package auth

import (
	"bytes"
	"context"
	"log/slog"
	"testing"
	"time"

	"github.com/alicebob/miniredis/v2"

	"github.com/knot/backend/internal/cache"
)

// newTestLimiter builds a limiter over an in-process Redis.
func newTestLimiter(t *testing.T) (*Limiter, *miniredis.Miniredis) {
	t.Helper()

	server := miniredis.RunT(t)

	redisCache, err := cache.NewRedisCache(server.Addr(), "", 0)
	if err != nil {
		t.Fatalf("NewRedisCache() error = %v, want nil", err)
	}
	t.Cleanup(func() { _ = redisCache.Close() })

	limiter, err := NewLimiter(redisCache, slog.New(slog.NewTextHandler(&bytes.Buffer{}, nil)))
	if err != nil {
		t.Fatalf("NewLimiter() error = %v, want nil", err)
	}

	return limiter, server
}

func TestNewLimiterRejectsMissingDependencies(t *testing.T) {
	t.Parallel()

	logger := slog.New(slog.NewTextHandler(&bytes.Buffer{}, nil))

	if _, err := NewLimiter(nil, logger); err == nil {
		t.Fatal("NewLimiter(nil, logger) error = nil, want an error")
	}

	server := miniredis.RunT(t)
	redisCache, err := cache.NewRedisCache(server.Addr(), "", 0)
	if err != nil {
		t.Fatalf("NewRedisCache() error = %v, want nil", err)
	}
	t.Cleanup(func() { _ = redisCache.Close() })

	if _, err := NewLimiter(redisCache, nil); err == nil {
		t.Fatal("NewLimiter(cache, nil) error = nil, want an error")
	}
}

// TestLimiterBlocksAfterTheThreshold is the rate-limit contract: the Nth attempt
// is allowed, the one after it is not.
func TestLimiterBlocksAfterTheThreshold(t *testing.T) {
	t.Parallel()

	limiter, _ := newTestLimiter(t)
	ctx := context.Background()

	limit := Limit{Name: "test", Attempts: 3, Window: time.Minute}

	for i := int64(1); i <= limit.Attempts; i++ {
		result := limiter.Allow(ctx, limit, "203.0.113.7")
		if !result.Allowed {
			t.Fatalf("attempt %d was blocked, want it allowed", i)
		}
		if result.Count != i {
			t.Fatalf("count = %d, want %d", result.Count, i)
		}
	}

	blocked := limiter.Allow(ctx, limit, "203.0.113.7")
	if blocked.Allowed {
		t.Fatalf("attempt %d was allowed, want it blocked", limit.Attempts+1)
	}
	if blocked.RetryAfter != limit.Window {
		t.Errorf("retry after = %s, want the window %s", blocked.RetryAfter, limit.Window)
	}

	// A different key has its own counter.
	if other := limiter.Allow(ctx, limit, "198.51.100.9"); !other.Allowed {
		t.Fatal("a second key was blocked by the first key's counter")
	}
}

// TestLimiterWindowExpires proves the counter is a fixed window: once it lapses the
// caller is allowed again.
func TestLimiterWindowExpires(t *testing.T) {
	t.Parallel()

	limiter, server := newTestLimiter(t)
	ctx := context.Background()

	limit := Limit{Name: "test-window", Attempts: 1, Window: time.Minute}

	if !limiter.Allow(ctx, limit, "key").Allowed {
		t.Fatal("the first attempt was blocked")
	}
	if limiter.Allow(ctx, limit, "key").Allowed {
		t.Fatal("the second attempt was allowed")
	}

	server.FastForward(limit.Window)

	if !limiter.Allow(ctx, limit, "key").Allowed {
		t.Fatal("an attempt after the window lapsed was blocked")
	}
}

// TestLimiterAllowsAnEmptyKey proves a request that cannot be attributed is not
// put into a shared bucket with everybody else.
func TestLimiterAllowsAnEmptyKey(t *testing.T) {
	t.Parallel()

	limiter, _ := newTestLimiter(t)

	limit := Limit{Name: "test-empty", Attempts: 1, Window: time.Minute}

	for i := 0; i < 5; i++ {
		if result := limiter.Allow(context.Background(), limit, ""); !result.Allowed {
			t.Fatalf("attempt %d with an empty key was blocked", i+1)
		}
	}
}

// TestLimiterFailsOpenWhenTheCacheIsDown pins the deliberate trade recorded in
// docs/SECURITY.md: a Redis outage must not become a sign-in outage.
func TestLimiterFailsOpenWhenTheCacheIsDown(t *testing.T) {
	t.Parallel()

	limiter, server := newTestLimiter(t)
	server.Close()

	result := limiter.Allow(context.Background(), LimitRegister, "203.0.113.7")
	if !result.Allowed {
		t.Fatal("the limiter failed closed while the cache was down")
	}
}

// TestLimitRulesMatchTheDocumentedNumbers pins the numbers docs/SECURITY.md and
// docs/API.md state, so the documentation cannot drift from the code.
func TestLimitRulesMatchTheDocumentedNumbers(t *testing.T) {
	t.Parallel()

	tests := []struct {
		limit    Limit
		attempts int64
		window   time.Duration
	}{
		{limit: LimitRegister, attempts: 5, window: time.Hour},
		{limit: LimitLoginByIP, attempts: 10, window: 15 * time.Minute},
		{limit: LimitLoginByEmail, attempts: 10, window: 15 * time.Minute},
		{limit: LimitRefresh, attempts: 30, window: 15 * time.Minute},
		{limit: LimitForgotPassword, attempts: 3, window: time.Hour},
	}

	for _, test := range tests {
		if test.limit.Attempts != test.attempts {
			t.Errorf("%s attempts = %d, want %d", test.limit.Name, test.limit.Attempts, test.attempts)
		}
		if test.limit.Window != test.window {
			t.Errorf("%s window = %s, want %s", test.limit.Name, test.limit.Window, test.window)
		}
	}
}
