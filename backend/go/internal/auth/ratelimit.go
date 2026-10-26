package auth

import (
	"context"
	"fmt"
	"log/slog"
	"time"

	"github.com/knot/backend/internal/cache"
)

// Rate limits for the authentication endpoints (KNOT-ADR-082's machinery, applied
// to sign-in).
//
// Every limit is a fixed window counted in Redis, so the numbers below are the
// contract the API advertises: N attempts per window per key. They are chosen to
// be invisible to a person and hostile to a script — a person does not log in
// eleven times in a quarter of an hour, and an attacker who is allowed five
// attempts an hour per address needs years to walk a password space.
//
// The keys are deliberately *not* the account: `register` is per address because
// the attacker's resource is addresses, `login` is per address because that is
// where a credential-stuffing run originates, and the extra per-email login limit
// is what stops a distributed attack from focusing on one account.
const (
	// rateLimitKeyPrefix namespaces every limiter key in Redis.
	rateLimitKeyPrefix = "knot:auth:rl:"
)

// Limit is one rate-limit rule: how many attempts, over how long.
type Limit struct {
	// Name is the key segment, so limits never share a counter.
	Name string
	// Attempts is how many attempts are allowed inside the window.
	Attempts int64
	// Window is the fixed window's length, and the value reported in Retry-After.
	Window time.Duration
}

// The rules the API enforces. They are exported because the mobile client, the
// tests, and docs/SECURITY.md all have to state the same numbers.
var (
	// LimitRegister is 5 registrations per hour per address.
	LimitRegister = Limit{Name: "register", Attempts: 5, Window: time.Hour}
	// LimitLoginByIP is 10 sign-in attempts per 15 minutes per address.
	LimitLoginByIP = Limit{Name: "login", Attempts: 10, Window: 15 * time.Minute}
	// LimitLoginByEmail is 10 sign-in attempts per 15 minutes per account, so a
	// distributed attack cannot focus on one address.
	//
	// It matches the per-address limit on purpose: a person on a sixth device must be
	// able to sign in, and the session cap of five (KNOT-ADR-084) is unreachable if
	// the limiter refuses the attempt that would demonstrate it.
	LimitLoginByEmail = Limit{Name: "login_email", Attempts: 10, Window: 15 * time.Minute}
	// LimitRefresh is 30 refreshes per 15 minutes per session. It is the loosest
	// limit because a healthy client refreshes on a timer, and it is still low
	// enough that a flood is stopped.
	LimitRefresh = Limit{Name: "refresh", Attempts: 30, Window: 15 * time.Minute}
	// LimitForgotPassword is 3 reset requests per hour per address.
	LimitForgotPassword = Limit{Name: "forgot", Attempts: 3, Window: time.Hour}
	// LimitResendVerification is 3 verification resends per hour per address.
	//
	// It matches the reset limit because it protects the same resource: one
	// person's inbox. A person who mistyped their address fixes it in one or two
	// attempts, and a script trying to use the endpoint as a mail relay is stopped
	// on the fourth (KNOT-ADR-090).
	LimitResendVerification = Limit{Name: "resend_verification", Attempts: 3, Window: time.Hour}
)

// LimitResult is what one attempt cost.
type LimitResult struct {
	// Allowed reports whether the attempt is within the limit.
	Allowed bool
	// RetryAfter is how long the caller should wait, and is what the API sends in
	// Retry-After when it refuses. It is the whole window rather than the
	// remaining time, which is deliberate: it is the longest the caller can
	// possibly need to wait, so a client that honours it is correct without the
	// server having to expose its counter's internal clock.
	RetryAfter time.Duration
	// Count is how many attempts this key has now made in the window.
	Count int64
}

// Limiter counts attempts against the cache.
//
// It holds no state of its own: the count lives in Redis, which is what makes the
// limit apply across every instance of the API rather than per process.
type Limiter struct {
	cache  cache.Cache
	logger *slog.Logger
}

// NewLimiter returns a limiter backed by c.
func NewLimiter(c cache.Cache, logger *slog.Logger) (*Limiter, error) {
	if c == nil {
		return nil, fmt.Errorf("auth: limiter requires a cache")
	}
	if logger == nil {
		return nil, fmt.Errorf("auth: limiter requires a logger")
	}

	return &Limiter{cache: c, logger: logger}, nil
}

// Allow counts one attempt for key and reports whether it is within limit.
//
// An empty key is always allowed: a limit that cannot be attributed to anything
// would otherwise put every caller without an address into one shared bucket and
// lock out the honest ones first.
//
// A cache failure is also allowed, and logged. This is a deliberate trade — the
// limiter is a defence against abuse, not a condition of correctness, and
// failing closed would turn a Redis outage into a total sign-in outage for every
// user. The trade and its consequence are recorded in docs/SECURITY.md: an
// attacker who can take Redis down can also remove the limits, so Redis must be
// protected like any other security dependency.
func (l *Limiter) Allow(ctx context.Context, limit Limit, key string) LimitResult {
	if key == "" {
		return LimitResult{Allowed: true}
	}

	count, err := l.cache.Incr(ctx, rateLimitKeyPrefix+limit.Name+":"+key, limit.Window)
	if err != nil {
		l.logger.WarnContext(ctx, "rate limit check failed; allowing the request",
			slog.String("limit", limit.Name), slog.String("error", err.Error()))
		return LimitResult{Allowed: true}
	}

	return LimitResult{
		Allowed:    count <= limit.Attempts,
		RetryAfter: limit.Window,
		Count:      count,
	}
}
