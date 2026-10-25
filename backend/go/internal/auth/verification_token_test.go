package auth

import (
	"context"
	"errors"
	"testing"
	"time"
)

// TestNewPostgresVerificationTokenStoreRejectsNilPool covers the constructor's
// guard.
func TestNewPostgresVerificationTokenStoreRejectsNilPool(t *testing.T) {
	t.Parallel()

	store, err := NewPostgresVerificationTokenStore(nil)
	if err == nil {
		t.Fatalf("NewPostgresVerificationTokenStore(nil) error = nil, want an error")
	}
	if store != nil {
		t.Fatalf("NewPostgresVerificationTokenStore(nil) store = %v, want nil", store)
	}
}

// TestPostgresVerificationTokenStoreRejectsBadInput covers the argument guards
// that run before a query, so they need no database.
func TestPostgresVerificationTokenStoreRejectsBadInput(t *testing.T) {
	t.Parallel()

	store := &PostgresVerificationTokenStore{}
	ctx := context.Background()

	if _, err := store.Create(ctx, nil); err == nil {
		t.Fatal("Create(nil) error = nil, want an error")
	}

	if _, err := store.Create(ctx, &VerificationToken{UserID: malformedUUID}); !errors.Is(err, ErrInvalidUserID) {
		t.Fatalf("Create() with a malformed user id error = %v, want ErrInvalidUserID", err)
	}

	if _, err := store.Create(ctx, &VerificationToken{
		UserID:    missingUUID,
		TokenType: "sms_verification",
		ExpiresAt: time.Now().Add(time.Hour),
	}); !errors.Is(err, ErrInvalidVerificationTokenType) {
		t.Fatalf("Create() with an unknown type error = %v, want ErrInvalidVerificationTokenType", err)
	}

	if _, err := store.Create(ctx, &VerificationToken{
		UserID:    missingUUID,
		TokenType: VerificationTokenEmailVerification,
	}); err == nil {
		t.Fatal("Create() without an expiry error = nil, want an error")
	}

	if err := store.Consume(ctx, malformedUUID); !errors.Is(err, ErrVerificationTokenNotFound) {
		t.Fatalf("Consume() with a malformed id error = %v, want ErrVerificationTokenNotFound", err)
	}
}

// TestVerificationTokenExpiredAndUsed covers the two predicates the service will
// decide with, which need no database.
func TestVerificationTokenExpiredAndUsed(t *testing.T) {
	t.Parallel()

	now := time.Date(2026, time.October, 25, 12, 0, 0, 0, time.UTC)
	consumedAt := now.Add(-time.Minute)

	tests := []struct {
		name       string
		token      VerificationToken
		wantExpiry bool
		wantUsed   bool
	}{
		{
			name:  "live and unused",
			token: VerificationToken{ExpiresAt: now.Add(time.Hour)},
		},
		{
			name:     "used",
			token:    VerificationToken{ExpiresAt: now.Add(time.Hour), ConsumedAt: &consumedAt},
			wantUsed: true,
		},
		{
			name:       "expired",
			token:      VerificationToken{ExpiresAt: now.Add(-time.Second)},
			wantExpiry: true,
		},
		{
			name:       "expiring exactly now is expired",
			token:      VerificationToken{ExpiresAt: now},
			wantExpiry: true,
		},
	}

	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			t.Parallel()

			if got := test.token.Expired(now); got != test.wantExpiry {
				t.Fatalf("Expired() = %t, want %t", got, test.wantExpiry)
			}
			if got := test.token.Used(); got != test.wantUsed {
				t.Fatalf("Used() = %t, want %t", got, test.wantUsed)
			}
		})
	}
}

// TestIsVerificationTokenType covers the closed set.
func TestIsVerificationTokenType(t *testing.T) {
	t.Parallel()

	tests := []struct {
		value string
		want  bool
	}{
		{value: VerificationTokenEmailVerification, want: true},
		{value: VerificationTokenPasswordReset, want: true},
		{value: "sms_verification", want: false},
		{value: "", want: false},
	}

	for _, test := range tests {
		if got := IsVerificationTokenType(test.value); got != test.want {
			t.Fatalf("IsVerificationTokenType(%q) = %t, want %t", test.value, got, test.want)
		}
	}
}

// TestPostgresVerificationTokenStoreLifecycle exercises issue, find, redeem and
// the double-redeem rejection.
func TestPostgresVerificationTokenStoreLifecycle(t *testing.T) {
	pool := newTestPool(t)
	userID := newTestUser(t, pool)

	store, err := NewPostgresVerificationTokenStore(pool)
	if err != nil {
		t.Fatalf("NewPostgresVerificationTokenStore() error = %v, want nil", err)
	}

	ctx := context.Background()
	hash := testHash("verification-token-lifecycle")
	expiresAt := time.Now().Add(time.Hour).UTC().Truncate(time.Microsecond)

	created, err := store.Create(ctx, &VerificationToken{
		UserID:    userID,
		TokenHash: hash,
		TokenType: VerificationTokenEmailVerification,
		ExpiresAt: expiresAt,
	})
	if err != nil {
		t.Fatalf("Create() error = %v, want nil", err)
	}

	if !isUUID(created.ID) {
		t.Fatalf("Create() id = %q, want a UUID", created.ID)
	}
	if created.Used() {
		t.Fatalf("Create() consumed at = %v, want an unused token", created.ConsumedAt)
	}
	if !created.ExpiresAt.Equal(expiresAt) {
		t.Fatalf("Create() expires at = %s, want %s", created.ExpiresAt, expiresAt)
	}
	if created.Expired(time.Now()) {
		t.Fatal("Create() produced a token that is already expired")
	}

	found, err := store.FindByTokenHash(ctx, hash)
	if err != nil {
		t.Fatalf("FindByTokenHash() error = %v, want nil", err)
	}
	if found.ID != created.ID {
		t.Fatalf("FindByTokenHash() id = %q, want %q", found.ID, created.ID)
	}
	if found.TokenType != VerificationTokenEmailVerification {
		t.Fatalf("FindByTokenHash() type = %q, want %q", found.TokenType, VerificationTokenEmailVerification)
	}

	if _, err := store.FindByTokenHash(ctx, testHash("no-such-token")); !errors.Is(err, ErrVerificationTokenNotFound) {
		t.Fatalf("FindByTokenHash() for an unknown hash error = %v, want ErrVerificationTokenNotFound", err)
	}

	if err := store.Consume(ctx, created.ID); err != nil {
		t.Fatalf("Consume() error = %v, want nil", err)
	}

	consumed, err := store.FindByTokenHash(ctx, hash)
	if err != nil {
		t.Fatalf("FindByTokenHash() after Consume() error = %v, want nil", err)
	}
	if !consumed.Used() {
		t.Fatal("token is still unused after Consume()")
	}
	firstConsumedAt := *consumed.ConsumedAt

	// A second redemption is refused, and does not move the timestamp: the first
	// use is the fact worth keeping.
	if err := store.Consume(ctx, created.ID); !errors.Is(err, ErrVerificationTokenConsumed) {
		t.Fatalf("second Consume() error = %v, want ErrVerificationTokenConsumed", err)
	}
	stillConsumed, err := store.FindByTokenHash(ctx, hash)
	if err != nil {
		t.Fatalf("FindByTokenHash() after second Consume() error = %v, want nil", err)
	}
	if !stillConsumed.ConsumedAt.Equal(firstConsumedAt) {
		t.Fatalf("second Consume() moved consumed_at from %s to %s", firstConsumedAt, *stillConsumed.ConsumedAt)
	}

	if err := store.Consume(ctx, missingUUID); !errors.Is(err, ErrVerificationTokenNotFound) {
		t.Fatalf("Consume() for an unknown id error = %v, want ErrVerificationTokenNotFound", err)
	}
}

// TestPostgresVerificationTokenStoreHidesOtherUsersTokens proves a token is
// scoped to its owner.
func TestPostgresVerificationTokenStoreHidesOtherUsersTokens(t *testing.T) {
	pool := newTestPool(t)
	firstUserID := newTestUser(t, pool)
	secondUserID := newTestUser(t, pool)

	store, err := NewPostgresVerificationTokenStore(pool)
	if err != nil {
		t.Fatalf("NewPostgresVerificationTokenStore() error = %v, want nil", err)
	}

	ctx := context.Background()

	first, err := store.Create(ctx, &VerificationToken{
		UserID:    firstUserID,
		TokenHash: testHash("scoped-token-first"),
		TokenType: VerificationTokenPasswordReset,
		ExpiresAt: time.Now().Add(30 * time.Minute),
	})
	if err != nil {
		t.Fatalf("Create() error = %v, want nil", err)
	}

	second, err := store.Create(ctx, &VerificationToken{
		UserID:    secondUserID,
		TokenHash: testHash("scoped-token-second"),
		TokenType: VerificationTokenPasswordReset,
		ExpiresAt: time.Now().Add(30 * time.Minute),
	})
	if err != nil {
		t.Fatalf("Create() for the second user error = %v, want nil", err)
	}

	if first.UserID != firstUserID {
		t.Fatalf("Create() user id = %q, want %q", first.UserID, firstUserID)
	}
	if second.UserID != secondUserID {
		t.Fatalf("Create() user id = %q, want %q", second.UserID, secondUserID)
	}

	// Redeeming one user's token must not touch the other's.
	if err := store.Consume(ctx, first.ID); err != nil {
		t.Fatalf("Consume() error = %v, want nil", err)
	}
	other, err := store.FindByTokenHash(ctx, testHash("scoped-token-second"))
	if err != nil {
		t.Fatalf("FindByTokenHash() error = %v, want nil", err)
	}
	if other.Used() {
		t.Fatal("redeeming one user's token consumed another user's token")
	}
}
