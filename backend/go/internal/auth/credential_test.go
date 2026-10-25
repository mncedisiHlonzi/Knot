package auth

import (
	"context"
	"errors"
	"testing"
)

// TestNewPostgresCredentialStoreRejectsNilPool covers the constructor's guard.
func TestNewPostgresCredentialStoreRejectsNilPool(t *testing.T) {
	t.Parallel()

	store, err := NewPostgresCredentialStore(nil)
	if err == nil {
		t.Fatalf("NewPostgresCredentialStore(nil) error = nil, want an error")
	}
	if store != nil {
		t.Fatalf("NewPostgresCredentialStore(nil) store = %v, want nil", store)
	}
}

// TestPostgresCredentialStoreRejectsBadInput covers the argument guards that run
// before a query, so they need no database.
func TestPostgresCredentialStoreRejectsBadInput(t *testing.T) {
	t.Parallel()

	store := &PostgresCredentialStore{}
	ctx := context.Background()

	if _, err := store.Create(ctx, nil); err == nil {
		t.Fatal("Create(nil) error = nil, want an error")
	}

	if _, err := store.Create(ctx, &Credential{UserID: malformedUUID, CredentialType: CredentialTypePassword}); !errors.Is(err, ErrInvalidUserID) {
		t.Fatalf("Create() with a malformed user id error = %v, want ErrInvalidUserID", err)
	}

	if _, err := store.Create(ctx, &Credential{UserID: missingUUID, CredentialType: "sms"}); !errors.Is(err, ErrInvalidCredentialType) {
		t.Fatalf("Create() with an unknown type error = %v, want ErrInvalidCredentialType", err)
	}

	if _, err := store.FindByUserIDAndType(ctx, missingUUID, "sms"); !errors.Is(err, ErrInvalidCredentialType) {
		t.Fatalf("FindByUserIDAndType() with an unknown type error = %v, want ErrInvalidCredentialType", err)
	}

	if _, err := store.FindByUserIDAndType(ctx, malformedUUID, CredentialTypePassword); !errors.Is(err, ErrInvalidUserID) {
		t.Fatalf("FindByUserIDAndType() with a malformed user id error = %v, want ErrInvalidUserID", err)
	}

	if _, err := store.Update(ctx, nil); err == nil {
		t.Fatal("Update(nil) error = nil, want an error")
	}

	if _, err := store.Update(ctx, &Credential{ID: malformedUUID}); !errors.Is(err, ErrCredentialNotFound) {
		t.Fatalf("Update() with a malformed id error = %v, want ErrCredentialNotFound", err)
	}
}

// TestIsCredentialType covers the closed set, which the migration's CHECK
// constraint and the Go constants have to agree on.
func TestIsCredentialType(t *testing.T) {
	t.Parallel()

	tests := []struct {
		value string
		want  bool
	}{
		{value: CredentialTypePassword, want: true},
		{value: CredentialTypeOAuthGoogle, want: true},
		{value: CredentialTypeMagicLink, want: true},
		{value: "sms", want: false},
		{value: "", want: false},
		{value: "Password", want: false},
	}

	for _, test := range tests {
		if got := IsCredentialType(test.value); got != test.want {
			t.Fatalf("IsCredentialType(%q) = %t, want %t", test.value, got, test.want)
		}
	}
}

// TestPostgresCredentialStoreLifecycle exercises create, find, replace and the
// duplicate rejection.
func TestPostgresCredentialStoreLifecycle(t *testing.T) {
	pool := newTestPool(t)
	userID := newTestUser(t, pool)

	store, err := NewPostgresCredentialStore(pool)
	if err != nil {
		t.Fatalf("NewPostgresCredentialStore() error = %v, want nil", err)
	}

	ctx := context.Background()

	const originalHash = "$argon2id$v=19$m=65536,t=1,p=4$c2FsdA$b3JpZ2luYWw"
	created, err := store.Create(ctx, &Credential{
		UserID:         userID,
		CredentialType: CredentialTypePassword,
		CredentialData: originalHash,
	})
	if err != nil {
		t.Fatalf("Create() error = %v, want nil", err)
	}

	if !isUUID(created.ID) {
		t.Fatalf("Create() id = %q, want a UUID", created.ID)
	}
	if created.CreatedAt.IsZero() || created.UpdatedAt.IsZero() {
		t.Fatalf("Create() timestamps = (%v, %v), want both set", created.CreatedAt, created.UpdatedAt)
	}

	found, err := store.FindByUserIDAndType(ctx, userID, CredentialTypePassword)
	if err != nil {
		t.Fatalf("FindByUserIDAndType() error = %v, want nil", err)
	}
	if found.ID != created.ID {
		t.Fatalf("FindByUserIDAndType() id = %q, want %q", found.ID, created.ID)
	}
	if found.CredentialData != originalHash {
		t.Fatalf("FindByUserIDAndType() data = %q, want %q", found.CredentialData, originalHash)
	}

	// A user may hold several methods, but only one of each.
	if _, err := store.Create(ctx, &Credential{
		UserID:         userID,
		CredentialType: CredentialTypeOAuthGoogle,
		CredentialData: "google-subject-1234",
	}); err != nil {
		t.Fatalf("Create() for a second type error = %v, want nil", err)
	}

	if _, err := store.Create(ctx, &Credential{
		UserID:         userID,
		CredentialType: CredentialTypePassword,
		CredentialData: originalHash,
	}); !errors.Is(err, ErrCredentialExists) {
		t.Fatalf("Create() duplicate error = %v, want ErrCredentialExists", err)
	}

	if _, err := store.FindByUserIDAndType(ctx, userID, CredentialTypeMagicLink); !errors.Is(err, ErrCredentialNotFound) {
		t.Fatalf("FindByUserIDAndType() for an absent type error = %v, want ErrCredentialNotFound", err)
	}

	// Replacing the secret is an Update, and it keeps the row's identity.
	const rotatedHash = "$argon2id$v=19$m=65536,t=1,p=4$c2FsdA$cm90YXRlZA"
	updated, err := store.Update(ctx, &Credential{ID: created.ID, CredentialData: rotatedHash})
	if err != nil {
		t.Fatalf("Update() error = %v, want nil", err)
	}
	if updated.ID != created.ID {
		t.Fatalf("Update() id = %q, want %q", updated.ID, created.ID)
	}
	if updated.CredentialData != rotatedHash {
		t.Fatalf("Update() data = %q, want %q", updated.CredentialData, rotatedHash)
	}

	reloaded, err := store.FindByUserIDAndType(ctx, userID, CredentialTypePassword)
	if err != nil {
		t.Fatalf("FindByUserIDAndType() after Update() error = %v, want nil", err)
	}
	if reloaded.CredentialData != rotatedHash {
		t.Fatalf("stored data = %q, want %q", reloaded.CredentialData, rotatedHash)
	}

	if _, err := store.Update(ctx, &Credential{ID: missingUUID, CredentialData: rotatedHash}); !errors.Is(err, ErrCredentialNotFound) {
		t.Fatalf("Update() for an unknown id error = %v, want ErrCredentialNotFound", err)
	}
}

// TestPostgresCredentialStoreIsPerUser proves one user's credentials are
// invisible to another.
func TestPostgresCredentialStoreIsPerUser(t *testing.T) {
	pool := newTestPool(t)
	firstUserID := newTestUser(t, pool)
	secondUserID := newTestUser(t, pool)

	store, err := NewPostgresCredentialStore(pool)
	if err != nil {
		t.Fatalf("NewPostgresCredentialStore() error = %v, want nil", err)
	}

	ctx := context.Background()

	if _, err := store.Create(ctx, &Credential{
		UserID:         firstUserID,
		CredentialType: CredentialTypePassword,
		CredentialData: "$argon2id$v=19$m=65536,t=1,p=4$c2FsdA$Zmlyc3Q",
	}); err != nil {
		t.Fatalf("Create() error = %v, want nil", err)
	}

	if _, err := store.FindByUserIDAndType(ctx, secondUserID, CredentialTypePassword); !errors.Is(err, ErrCredentialNotFound) {
		t.Fatalf("FindByUserIDAndType() for the other user error = %v, want ErrCredentialNotFound", err)
	}

	// The same type for a different user is not a duplicate.
	if _, err := store.Create(ctx, &Credential{
		UserID:         secondUserID,
		CredentialType: CredentialTypePassword,
		CredentialData: "$argon2id$v=19$m=65536,t=1,p=4$c2FsdA$c2Vjb25k",
	}); err != nil {
		t.Fatalf("Create() for the second user error = %v, want nil", err)
	}
}
