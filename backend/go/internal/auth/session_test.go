package auth

import (
	"context"
	"errors"
	"testing"
)

// TestNewPostgresSessionStoreRejectsNilPool covers the constructor's guard.
func TestNewPostgresSessionStoreRejectsNilPool(t *testing.T) {
	t.Parallel()

	store, err := NewPostgresSessionStore(nil)
	if err == nil {
		t.Fatalf("NewPostgresSessionStore(nil) error = nil, want an error")
	}
	if store != nil {
		t.Fatalf("NewPostgresSessionStore(nil) store = %v, want nil", store)
	}
}

// TestPostgresSessionStoreRejectsBadInput covers the argument guards that run
// before a query, so they need no database.
func TestPostgresSessionStoreRejectsBadInput(t *testing.T) {
	t.Parallel()

	// A zero-value store has a nil pool: every case below must be rejected before
	// the pool is touched, and a panic would mean the guard is missing.
	store := &PostgresSessionStore{}
	ctx := context.Background()

	if _, err := store.Create(ctx, nil); err == nil {
		t.Fatal("Create(nil) error = nil, want an error")
	}

	if _, err := store.Create(ctx, &Session{UserID: malformedUUID}); !errors.Is(err, ErrInvalidUserID) {
		t.Fatalf("Create() with a malformed user id error = %v, want ErrInvalidUserID", err)
	}

	if _, err := store.ListForUser(ctx, malformedUUID); !errors.Is(err, ErrInvalidUserID) {
		t.Fatalf("ListForUser() with a malformed user id error = %v, want ErrInvalidUserID", err)
	}

	if err := store.RevokeAllForUser(ctx, malformedUUID); !errors.Is(err, ErrInvalidUserID) {
		t.Fatalf("RevokeAllForUser() with a malformed user id error = %v, want ErrInvalidUserID", err)
	}

	if err := store.Revoke(ctx, malformedUUID); !errors.Is(err, ErrSessionNotFound) {
		t.Fatalf("Revoke() with a malformed id error = %v, want ErrSessionNotFound", err)
	}
}

// TestEncodeDeviceInfoEmpty proves an absent device info map becomes an empty
// JSON object rather than NULL, because the column is NOT NULL.
func TestEncodeDeviceInfoEmpty(t *testing.T) {
	t.Parallel()

	for _, info := range []map[string]any{nil, {}} {
		encoded, err := encodeDeviceInfo(info)
		if err != nil {
			t.Fatalf("encodeDeviceInfo(%v) error = %v, want nil", info, err)
		}
		if string(encoded) != "{}" {
			t.Fatalf("encodeDeviceInfo(%v) = %q, want {}", info, encoded)
		}
	}
}

// TestPostgresSessionStoreLifecycle exercises create, find, list, revoke and the
// idempotency of a second revoke.
func TestPostgresSessionStoreLifecycle(t *testing.T) {
	pool := newTestPool(t)
	userID := newTestUser(t, pool)

	store, err := NewPostgresSessionStore(pool)
	if err != nil {
		t.Fatalf("NewPostgresSessionStore() error = %v, want nil", err)
	}

	ctx := context.Background()
	hash := testHash("session-lifecycle")

	created, err := store.Create(ctx, &Session{
		UserID:           userID,
		RefreshTokenHash: hash,
		DeviceInfo:       map[string]any{"platform": "ios", "app_version": "0.1.0"},
		IPAddress:        "203.0.113.7",
	})
	if err != nil {
		t.Fatalf("Create() error = %v, want nil", err)
	}

	if !isUUID(created.ID) {
		t.Fatalf("Create() id = %q, want a UUID", created.ID)
	}
	if created.UserID != userID {
		t.Fatalf("Create() user id = %q, want %q", created.UserID, userID)
	}
	if !created.Active() {
		t.Fatalf("Create() revoked at = %v, want a live session", created.RevokedAt)
	}
	if created.CreatedAt.IsZero() || created.LastUsedAt.IsZero() {
		t.Fatalf("Create() timestamps = (%v, %v), want both set", created.CreatedAt, created.LastUsedAt)
	}
	if created.IPAddress != "203.0.113.7" {
		t.Fatalf("Create() ip address = %q, want %q", created.IPAddress, "203.0.113.7")
	}
	if created.DeviceInfo["platform"] != "ios" {
		t.Fatalf("Create() device info = %v, want platform ios", created.DeviceInfo)
	}

	found, err := store.FindByRefreshTokenHash(ctx, hash)
	if err != nil {
		t.Fatalf("FindByRefreshTokenHash() error = %v, want nil", err)
	}
	if found.ID != created.ID {
		t.Fatalf("FindByRefreshTokenHash() id = %q, want %q", found.ID, created.ID)
	}
	if found.DeviceInfo["app_version"] != "0.1.0" {
		t.Fatalf("FindByRefreshTokenHash() device info = %v, want app_version 0.1.0", found.DeviceInfo)
	}

	if _, err := store.FindByRefreshTokenHash(ctx, testHash("no-such-session")); !errors.Is(err, ErrSessionNotFound) {
		t.Fatalf("FindByRefreshTokenHash() for an unknown hash error = %v, want ErrSessionNotFound", err)
	}

	live, err := store.ListForUser(ctx, userID)
	if err != nil {
		t.Fatalf("ListForUser() error = %v, want nil", err)
	}
	if len(live) != 1 {
		t.Fatalf("ListForUser() = %d sessions, want 1", len(live))
	}

	if err := store.Revoke(ctx, created.ID); err != nil {
		t.Fatalf("Revoke() error = %v, want nil", err)
	}

	revoked, err := store.FindByRefreshTokenHash(ctx, hash)
	if err != nil {
		t.Fatalf("FindByRefreshTokenHash() after Revoke() error = %v, want nil", err)
	}
	if revoked.Active() {
		t.Fatal("session is still active after Revoke()")
	}
	firstRevokedAt := *revoked.RevokedAt

	// A second revoke is a no-op: the original timestamp stays, because it is
	// evidence of when the session actually ended.
	if err := store.Revoke(ctx, created.ID); err != nil {
		t.Fatalf("second Revoke() error = %v, want nil", err)
	}
	again, err := store.FindByRefreshTokenHash(ctx, hash)
	if err != nil {
		t.Fatalf("FindByRefreshTokenHash() after second Revoke() error = %v, want nil", err)
	}
	if !again.RevokedAt.Equal(firstRevokedAt) {
		t.Fatalf("second Revoke() moved revoked_at from %s to %s", firstRevokedAt, *again.RevokedAt)
	}

	if err := store.Revoke(ctx, missingUUID); !errors.Is(err, ErrSessionNotFound) {
		t.Fatalf("Revoke() for an unknown id error = %v, want ErrSessionNotFound", err)
	}

	live, err = store.ListForUser(ctx, userID)
	if err != nil {
		t.Fatalf("ListForUser() after Revoke() error = %v, want nil", err)
	}
	if len(live) != 0 {
		t.Fatalf("ListForUser() after Revoke() = %d sessions, want 0", len(live))
	}
}

// TestPostgresSessionStoreRevokeAllForUser proves signing out everywhere ends
// every live session and is safe to repeat.
func TestPostgresSessionStoreRevokeAllForUser(t *testing.T) {
	pool := newTestPool(t)
	userID := newTestUser(t, pool)

	store, err := NewPostgresSessionStore(pool)
	if err != nil {
		t.Fatalf("NewPostgresSessionStore() error = %v, want nil", err)
	}

	ctx := context.Background()

	hashes := []string{testHash("revoke-all-1"), testHash("revoke-all-2")}
	for _, hash := range hashes {
		if _, err := store.Create(ctx, &Session{UserID: userID, RefreshTokenHash: hash}); err != nil {
			t.Fatalf("Create() error = %v, want nil", err)
		}
	}

	live, err := store.ListForUser(ctx, userID)
	if err != nil {
		t.Fatalf("ListForUser() error = %v, want nil", err)
	}
	if len(live) != len(hashes) {
		t.Fatalf("ListForUser() = %d sessions, want %d", len(live), len(hashes))
	}

	if err := store.RevokeAllForUser(ctx, userID); err != nil {
		t.Fatalf("RevokeAllForUser() error = %v, want nil", err)
	}

	for _, hash := range hashes {
		session, findErr := store.FindByRefreshTokenHash(ctx, hash)
		if findErr != nil {
			t.Fatalf("FindByRefreshTokenHash() error = %v, want nil", findErr)
		}
		if session.Active() {
			t.Fatalf("session %q is still active after RevokeAllForUser()", hash)
		}
	}

	live, err = store.ListForUser(ctx, userID)
	if err != nil {
		t.Fatalf("ListForUser() after RevokeAllForUser() error = %v, want nil", err)
	}
	if len(live) != 0 {
		t.Fatalf("ListForUser() after RevokeAllForUser() = %d sessions, want 0", len(live))
	}

	// Nothing left to revoke is not a failure.
	if err := store.RevokeAllForUser(ctx, userID); err != nil {
		t.Fatalf("second RevokeAllForUser() error = %v, want nil", err)
	}

	// Another user's sessions are untouched by any of the above.
	otherUserID := newTestUser(t, pool)
	otherHash := testHash("revoke-all-other")
	if _, err := store.Create(ctx, &Session{UserID: otherUserID, RefreshTokenHash: otherHash}); err != nil {
		t.Fatalf("Create() for the second user error = %v, want nil", err)
	}
	other, err := store.FindByRefreshTokenHash(ctx, otherHash)
	if err != nil {
		t.Fatalf("FindByRefreshTokenHash() for the second user error = %v, want nil", err)
	}
	if !other.Active() {
		t.Fatal("RevokeAllForUser() revoked another user's session")
	}
}
