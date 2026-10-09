package identity

import (
	"context"
	"errors"
	"fmt"
	"os"
	"strings"
	"testing"
	"time"

	"github.com/jackc/pgx/v5/pgxpool"
)

// integrationSetup connects to the database named by KNOT_POSTGRES_DSN.
//
// The test is skipped, not failed, when KNOT_POSTGRES_DSN is unset, when the
// database is unreachable, or when the users table has not been migrated yet —
// none of those is a defect in the code under test.
//
// It returns the store, the pool, and a per-run email prefix that cleanup uses to
// delete exactly the rows this test created.
func integrationSetup(t *testing.T) (*PostgresStore, *pgxpool.Pool, string) {
	t.Helper()

	dsn := strings.TrimSpace(os.Getenv("KNOT_POSTGRES_DSN"))
	if dsn == "" {
		t.Skip("KNOT_POSTGRES_DSN is not set; skipping PostgreSQL integration test")
	}

	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()

	pool, err := pgxpool.New(ctx, dsn)
	if err != nil {
		t.Skipf("could not build a pool from KNOT_POSTGRES_DSN: %v", err)
	}
	if err := pool.Ping(ctx); err != nil {
		pool.Close()
		t.Skipf("PostgreSQL at KNOT_POSTGRES_DSN is not reachable: %v", err)
	}

	var migrated bool
	if err := pool.QueryRow(ctx, "SELECT to_regclass('public.users') IS NOT NULL").Scan(&migrated); err != nil {
		pool.Close()
		t.Skipf("could not inspect the schema: %v", err)
	}
	if !migrated {
		pool.Close()
		t.Skip("the users table does not exist; run `go run ./cmd/knot migrate up` first")
	}

	store, err := NewPostgresStore(pool)
	if err != nil {
		pool.Close()
		t.Fatalf("NewPostgresStore() error = %v, want nil", err)
	}

	prefix := fmt.Sprintf("knot-it-%d-", time.Now().UnixNano())

	t.Cleanup(func() {
		cleanupCtx, cancelCleanup := context.WithTimeout(context.Background(), 10*time.Second)
		defer cancelCleanup()

		if _, err := pool.Exec(cleanupCtx, "DELETE FROM users WHERE email LIKE $1", prefix+"%"); err != nil {
			t.Logf("cleanup: could not delete test users: %v", err)
		}
		pool.Close()
	})

	return store, pool, prefix
}

// newIntegrationUser builds a user with a unique email under prefix.
func newIntegrationUser(prefix, name string) *User {
	return &User{
		Email:              prefix + name + "@example.test",
		PasswordHash:       "$argon2id$v=19$m=65536,t=1,p=4$c2FsdA$a2V5",
		DisplayName:        "Integration " + name,
		PreferredLanguages: []string{"en"},
	}
}

func TestPostgresStoreCreateUser(t *testing.T) {
	store, _, prefix := integrationSetup(t)
	ctx := context.Background()

	created, err := store.CreateUser(ctx, newIntegrationUser(prefix, "create"))
	if err != nil {
		t.Fatalf("CreateUser() error = %v, want nil", err)
	}

	if created.ID == "" {
		t.Error("id is empty, want a database-generated uuid")
	}
	if !isUUID(created.ID) {
		t.Errorf("id = %q, want a canonical uuid", created.ID)
	}
	if created.Email != prefix+"create@example.test" {
		t.Errorf("email = %q, want the normalised address", created.Email)
	}
	if created.DisplayName != "Integration create" {
		t.Errorf("display name = %q, want %q", created.DisplayName, "Integration create")
	}
	if created.CreatedAt.IsZero() || created.UpdatedAt.IsZero() {
		t.Error("timestamps are zero, want database-generated values")
	}
	if len(created.PreferredLanguages) != 1 || created.PreferredLanguages[0] != "en" {
		t.Errorf("languages = %v, want [en]", created.PreferredLanguages)
	}
	if created.Phone != "" || created.ApproximateLocation != "" {
		t.Errorf("nullable columns = (%q, %q), want empty strings", created.Phone, created.ApproximateLocation)
	}
}

func TestPostgresStoreCreateUserRoundTripsOptionalFields(t *testing.T) {
	store, _, prefix := integrationSetup(t)
	ctx := context.Background()

	user := newIntegrationUser(prefix, "optional")
	user.Phone = "+27000000000"
	user.ApproximateLocation = "Cape Town"
	user.PreferredLanguages = []string{"en", "zu", "fr"}

	created, err := store.CreateUser(ctx, user)
	if err != nil {
		t.Fatalf("CreateUser() error = %v, want nil", err)
	}

	if created.Phone != "+27000000000" {
		t.Errorf("phone = %q, want %q", created.Phone, "+27000000000")
	}
	if created.ApproximateLocation != "Cape Town" {
		t.Errorf("location = %q, want %q", created.ApproximateLocation, "Cape Town")
	}
	if len(created.PreferredLanguages) != 3 {
		t.Errorf("languages = %v, want three entries", created.PreferredLanguages)
	}
}

func TestPostgresStoreCreateUserStoresEmptyLanguageList(t *testing.T) {
	store, _, prefix := integrationSetup(t)
	ctx := context.Background()

	user := newIntegrationUser(prefix, "nolangs")
	user.PreferredLanguages = nil

	created, err := store.CreateUser(ctx, user)
	if err != nil {
		t.Fatalf("CreateUser() error = %v, want nil", err)
	}

	if created.PreferredLanguages == nil {
		t.Error("languages = nil, want an empty slice rather than NULL")
	}
	if len(created.PreferredLanguages) != 0 {
		t.Errorf("languages = %v, want empty", created.PreferredLanguages)
	}
}

func TestPostgresStoreCreateUserRejectsDuplicateEmail(t *testing.T) {
	store, _, prefix := integrationSetup(t)
	ctx := context.Background()

	user := newIntegrationUser(prefix, "duplicate")
	if _, err := store.CreateUser(ctx, user); err != nil {
		t.Fatalf("first CreateUser() error = %v, want nil", err)
	}

	// A different local-part case is still the same address.
	second := newIntegrationUser(prefix, "duplicate")
	second.Email = strings.ToUpper(second.Email)

	_, err := store.CreateUser(ctx, second)
	if !errors.Is(err, ErrEmailTaken) {
		t.Errorf("second CreateUser() error = %v, want ErrEmailTaken", err)
	}
}

func TestPostgresStoreFindUserByEmail(t *testing.T) {
	store, _, prefix := integrationSetup(t)
	ctx := context.Background()

	created, err := store.CreateUser(ctx, newIntegrationUser(prefix, "byemail"))
	if err != nil {
		t.Fatalf("CreateUser() error = %v, want nil", err)
	}

	found, err := store.FindUserByEmail(ctx, created.Email)
	if err != nil {
		t.Fatalf("FindUserByEmail() error = %v, want nil", err)
	}
	if found.ID != created.ID {
		t.Errorf("id = %q, want %q", found.ID, created.ID)
	}

	// Lookup is case-insensitive.
	upper, err := store.FindUserByEmail(ctx, strings.ToUpper(created.Email))
	if err != nil {
		t.Fatalf("FindUserByEmail(upper) error = %v, want nil", err)
	}
	if upper.ID != created.ID {
		t.Errorf("id = %q, want %q for a differently-cased lookup", upper.ID, created.ID)
	}
}

func TestPostgresStoreFindUserByEmailMissing(t *testing.T) {
	store, _, prefix := integrationSetup(t)

	_, err := store.FindUserByEmail(context.Background(), prefix+"absent@example.test")
	if !errors.Is(err, ErrUserNotFound) {
		t.Errorf("FindUserByEmail() error = %v, want ErrUserNotFound", err)
	}
}

func TestPostgresStoreFindUserByID(t *testing.T) {
	store, _, prefix := integrationSetup(t)
	ctx := context.Background()

	created, err := store.CreateUser(ctx, newIntegrationUser(prefix, "byid"))
	if err != nil {
		t.Fatalf("CreateUser() error = %v, want nil", err)
	}

	found, err := store.FindUserByID(ctx, created.ID)
	if err != nil {
		t.Fatalf("FindUserByID() error = %v, want nil", err)
	}
	if found.Email != created.Email {
		t.Errorf("email = %q, want %q", found.Email, created.Email)
	}
}

func TestPostgresStoreFindUserByIDMissing(t *testing.T) {
	store, _, _ := integrationSetup(t)

	// A well-formed but unused uuid.
	_, err := store.FindUserByID(context.Background(), "00000000-0000-4000-8000-000000000000")
	if !errors.Is(err, ErrUserNotFound) {
		t.Errorf("FindUserByID() error = %v, want ErrUserNotFound", err)
	}
}

func TestPostgresStoreFindUserByIDRejectsMalformedID(t *testing.T) {
	store, _, _ := integrationSetup(t)

	for _, id := range []string{"", "not-a-uuid", "12345", "11111111-1111-4111-8111-11111111111"} {
		if _, err := store.FindUserByID(context.Background(), id); !errors.Is(err, ErrUserNotFound) {
			t.Errorf("FindUserByID(%q) error = %v, want ErrUserNotFound", id, err)
		}
	}
}

func TestPostgresStoreUpdateAvatarURL(t *testing.T) {
	store, _, prefix := integrationSetup(t)
	ctx := context.Background()

	created, err := store.CreateUser(ctx, newIntegrationUser(prefix, "avatar"))
	if err != nil {
		t.Fatalf("CreateUser() error = %v, want nil", err)
	}
	if created.AvatarURL != "" {
		t.Errorf("avatar_url = %q, want empty for a new user", created.AvatarURL)
	}

	key := "avatars/" + created.ID + "/01234567-89ab-4def-8123-456789abcdef.png"

	updated, err := store.UpdateAvatarURL(ctx, created.ID, key)
	if err != nil {
		t.Fatalf("UpdateAvatarURL() error = %v, want nil", err)
	}
	if updated.AvatarURL != key {
		t.Errorf("avatar_url = %q, want %q", updated.AvatarURL, key)
	}
	// The column is refreshed by the database, so the two transactions must not
	// produce a timestamp that moves backwards.
	if updated.UpdatedAt.Before(created.UpdatedAt) {
		t.Errorf("updated_at = %v, want it not to precede %v", updated.UpdatedAt, created.UpdatedAt)
	}

	// A later read sees the same value, so the column really is persisted rather
	// than only echoed back by RETURNING.
	reread, err := store.FindUserByID(ctx, created.ID)
	if err != nil {
		t.Fatalf("FindUserByID() error = %v, want nil", err)
	}
	if reread.AvatarURL != key {
		t.Errorf("re-read avatar_url = %q, want %q", reread.AvatarURL, key)
	}

	// Replacing it overwrites the single column rather than accumulating.
	replacement := "avatars/" + created.ID + "/fedcba98-7654-4321-8fed-cba987654321.jpg"
	replaced, err := store.UpdateAvatarURL(ctx, created.ID, replacement)
	if err != nil {
		t.Fatalf("second UpdateAvatarURL() error = %v, want nil", err)
	}
	if replaced.AvatarURL != replacement {
		t.Errorf("avatar_url = %q, want %q", replaced.AvatarURL, replacement)
	}
}

func TestPostgresStoreUpdateAvatarURLClearsWhenEmpty(t *testing.T) {
	store, _, prefix := integrationSetup(t)
	ctx := context.Background()

	created, err := store.CreateUser(ctx, newIntegrationUser(prefix, "avatarclear"))
	if err != nil {
		t.Fatalf("CreateUser() error = %v, want nil", err)
	}

	if _, err := store.UpdateAvatarURL(ctx, created.ID, "avatars/"+created.ID+"/a.png"); err != nil {
		t.Fatalf("UpdateAvatarURL() error = %v, want nil", err)
	}

	cleared, err := store.UpdateAvatarURL(ctx, created.ID, "")
	if err != nil {
		t.Fatalf("clearing UpdateAvatarURL() error = %v, want nil", err)
	}
	if cleared.AvatarURL != "" {
		t.Errorf("avatar_url = %q, want empty after clearing", cleared.AvatarURL)
	}
}

func TestPostgresStoreUpdateAvatarURLMissingUser(t *testing.T) {
	store, _, _ := integrationSetup(t)

	_, err := store.UpdateAvatarURL(context.Background(), "00000000-0000-4000-8000-000000000000", "avatars/x/y.png")
	if !errors.Is(err, ErrUserNotFound) {
		t.Errorf("UpdateAvatarURL() error = %v, want ErrUserNotFound", err)
	}
}

func TestPostgresStoreUpdateAvatarURLRejectsMalformedID(t *testing.T) {
	store, _, _ := integrationSetup(t)

	for _, id := range []string{"", "not-a-uuid", "12345"} {
		if _, err := store.UpdateAvatarURL(context.Background(), id, "avatars/x/y.png"); !errors.Is(err, ErrUserNotFound) {
			t.Errorf("UpdateAvatarURL(%q) error = %v, want ErrUserNotFound", id, err)
		}
	}
}

func TestNewPostgresStoreRejectsNilPool(t *testing.T) {
	if _, err := NewPostgresStore(nil); err == nil {
		t.Error("NewPostgresStore(nil) error = nil, want an error")
	}
}
