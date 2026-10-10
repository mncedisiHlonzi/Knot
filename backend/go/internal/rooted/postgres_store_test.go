package rooted

import (
	"context"
	"errors"
	"fmt"
	"os"
	"strings"
	"testing"
	"time"

	"github.com/jackc/pgx/v5/pgconn"
	"github.com/jackc/pgx/v5/pgxpool"
)

// integrationSetup connects to the database named by KNOT_POSTGRES_DSN.
//
// The test is skipped, not failed, when KNOT_POSTGRES_DSN is unset, when the
// database is unreachable, or when the rooted_signals table has not been migrated
// yet — none of those is a defect in the code under test.
//
// It returns the store, the pool, and a per-run email prefix that cleanup uses to
// delete exactly the users this test created. Deleting a user cascades to its
// rooted_signals rows, so the signals go with them.
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
	if err := pool.QueryRow(ctx, "SELECT to_regclass('public.rooted_signals') IS NOT NULL").Scan(&migrated); err != nil {
		pool.Close()
		t.Skipf("could not inspect the schema: %v", err)
	}
	if !migrated {
		pool.Close()
		t.Skip("the rooted_signals table does not exist; run `go run ./cmd/knot migrate up` first")
	}

	store, err := NewPostgresStore(pool)
	if err != nil {
		pool.Close()
		t.Fatalf("NewPostgresStore() error = %v, want nil", err)
	}

	prefix := fmt.Sprintf("knot-rooted-it-%d-", time.Now().UnixNano())

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

// createUser inserts a user directly and returns its id. A signal references
// users(id), so a test user has to exist before a signal can be written.
func createUser(t *testing.T, pool *pgxpool.Pool, prefix, name string) string {
	t.Helper()

	var id string
	err := pool.QueryRow(
		context.Background(),
		`INSERT INTO users (email, password_hash, display_name) VALUES ($1, $2, $3) RETURNING id`,
		prefix+name+"@example.test",
		"$argon2id$v=19$m=65536,t=1,p=4$c2FsdA$a2V5",
		"Rooted "+name,
	).Scan(&id)
	if err != nil {
		t.Fatalf("createUser() error = %v, want nil", err)
	}

	return id
}

func publicSignal(place string) Signal {
	return Signal{Place: place, DurationBucket: DurationLifelong, IsPublic: true, IsPrimary: true}
}

func TestPostgresStoreSetPrimaryInserts(t *testing.T) {
	store, pool, prefix := integrationSetup(t)
	ctx := context.Background()
	userID := createUser(t, pool, prefix, "insert")

	stored, err := store.SetPrimary(ctx, userID, publicSignal("Cape Town"))
	if err != nil {
		t.Fatalf("SetPrimary() error = %v, want nil", err)
	}

	if !isUUID(stored.ID) {
		t.Errorf("id = %q, want a canonical uuid", stored.ID)
	}
	if stored.UserID != userID {
		t.Errorf("user id = %q, want %q", stored.UserID, userID)
	}
	if stored.Place != "Cape Town" {
		t.Errorf("place = %q, want %q", stored.Place, "Cape Town")
	}
	if stored.DurationBucket != DurationLifelong {
		t.Errorf("duration bucket = %q, want %q", stored.DurationBucket, DurationLifelong)
	}
	if !stored.IsPublic || !stored.IsPrimary {
		t.Errorf("flags = (public %v, primary %v), want both true", stored.IsPublic, stored.IsPrimary)
	}
	if stored.CreatedAt.IsZero() || stored.UpdatedAt.IsZero() {
		t.Error("timestamps are zero, want database-generated values")
	}
}

func TestPostgresStoreSetPrimaryReplaces(t *testing.T) {
	store, pool, prefix := integrationSetup(t)
	ctx := context.Background()
	userID := createUser(t, pool, prefix, "replace")

	if _, err := store.SetPrimary(ctx, userID, publicSignal("Cape Town")); err != nil {
		t.Fatalf("first SetPrimary() error = %v, want nil", err)
	}

	second, err := store.SetPrimary(ctx, userID, Signal{
		Place:          "Johannesburg",
		DurationBucket: DurationManyYears,
		IsPublic:       true,
		IsPrimary:      true,
	})
	if err != nil {
		t.Fatalf("second SetPrimary() error = %v, want nil", err)
	}
	if second.Place != "Johannesburg" {
		t.Errorf("place = %q, want the replacement", second.Place)
	}

	var count int
	if err := pool.QueryRow(ctx, `SELECT count(*) FROM rooted_signals WHERE user_id = $1`, userID).Scan(&count); err != nil {
		t.Fatalf("counting signals: %v", err)
	}
	if count != 1 {
		t.Errorf("signal count = %d, want exactly 1 after a replacement", count)
	}
}

func TestPostgresStorePartialUniqueIndexPreventsTwoPrimaries(t *testing.T) {
	_, pool, prefix := integrationSetup(t)
	ctx := context.Background()
	userID := createUser(t, pool, prefix, "unique")

	if _, err := pool.Exec(ctx, `
		INSERT INTO rooted_signals (user_id, place, duration_bucket, is_primary)
		VALUES ($1, 'Cape Town', 'lifelong', true)`, userID); err != nil {
		t.Fatalf("first primary insert error = %v, want nil", err)
	}

	_, err := pool.Exec(ctx, `
		INSERT INTO rooted_signals (user_id, place, duration_bucket, is_primary)
		VALUES ($1, 'Johannesburg', 'many_years', true)`, userID)

	var pgErr *pgconn.PgError
	if !errors.As(err, &pgErr) || pgErr.Code != "23505" {
		t.Fatalf("second primary insert error = %v, want a unique violation (23505)", err)
	}
	if pgErr.ConstraintName != "rooted_signals_one_primary_per_user" {
		t.Errorf("violated constraint = %q, want rooted_signals_one_primary_per_user", pgErr.ConstraintName)
	}

	// A non-primary row is still allowed: the index is partial.
	if _, err := pool.Exec(ctx, `
		INSERT INTO rooted_signals (user_id, place, duration_bucket, is_primary)
		VALUES ($1, 'Durban', 'recently', false)`, userID); err != nil {
		t.Errorf("non-primary insert error = %v, want nil (the unique index is partial)", err)
	}
}

func TestPostgresStoreListPublicByUserFilters(t *testing.T) {
	store, pool, prefix := integrationSetup(t)
	ctx := context.Background()
	userID := createUser(t, pool, prefix, "filter")

	if _, err := pool.Exec(ctx, `
		INSERT INTO rooted_signals (user_id, place, duration_bucket, is_public, is_primary)
		VALUES ($1, 'Cape Town', 'lifelong', true, true)`, userID); err != nil {
		t.Fatalf("public insert error = %v, want nil", err)
	}
	if _, err := pool.Exec(ctx, `
		INSERT INTO rooted_signals (user_id, place, duration_bucket, is_public, is_primary)
		VALUES ($1, 'Durban', 'recently', false, false)`, userID); err != nil {
		t.Fatalf("private insert error = %v, want nil", err)
	}

	public, err := store.ListPublicByUser(ctx, userID)
	if err != nil {
		t.Fatalf("ListPublicByUser() error = %v, want nil", err)
	}
	if len(public) != 1 || public[0].Place != "Cape Town" {
		t.Errorf("public signals = %+v, want only the public one", public)
	}

	all, err := store.ListByUser(ctx, userID)
	if err != nil {
		t.Fatalf("ListByUser() error = %v, want nil", err)
	}
	if len(all) != 2 {
		t.Errorf("all signals = %d, want 2 (the owner sees the private one too)", len(all))
	}
	if len(all) > 0 && !all[0].IsPrimary {
		t.Error("first signal is not primary, want the primary signal first")
	}
}

func TestPostgresStoreBatchPrimaryPublicFiveUsers(t *testing.T) {
	store, pool, prefix := integrationSetup(t)
	ctx := context.Background()

	ids := make([]string, 0, 5)
	for i := 0; i < 5; i++ {
		ids = append(ids, createUser(t, pool, prefix, fmt.Sprintf("batch%d", i)))
	}

	// Three of the five have a primary public signal.
	for i := 0; i < 3; i++ {
		if _, err := store.SetPrimary(ctx, ids[i], publicSignal(fmt.Sprintf("Place %d", i))); err != nil {
			t.Fatalf("SetPrimary(%d) error = %v, want nil", i, err)
		}
	}
	// One has a private signal only, which must not surface.
	if _, err := store.SetPrimary(ctx, ids[3], Signal{
		Place:          "Hidden",
		DurationBucket: DurationRecently,
		IsPublic:       false,
		IsPrimary:      true,
	}); err != nil {
		t.Fatalf("SetPrimary(private) error = %v, want nil", err)
	}

	result, err := store.BatchPrimaryPublic(ctx, ids)
	if err != nil {
		t.Fatalf("BatchPrimaryPublic() error = %v, want nil", err)
	}

	if len(result) != 3 {
		t.Fatalf("result = %d entries, want 3 public signals", len(result))
	}
	for i := 0; i < 3; i++ {
		signal, ok := result[ids[i]]
		if !ok {
			t.Errorf("result is missing user %d", i)
			continue
		}
		if signal.Place != fmt.Sprintf("Place %d", i) {
			t.Errorf("place = %q, want %q", signal.Place, fmt.Sprintf("Place %d", i))
		}
	}
	if _, ok := result[ids[3]]; ok {
		t.Error("result contains a user whose only signal is private")
	}

	// The whole batch is one round trip regardless of the order or the count, so a
	// reversed list returns the same set.
	reversed := []string{ids[4], ids[3], ids[2], ids[1], ids[0]}
	again, err := store.BatchPrimaryPublic(ctx, reversed)
	if err != nil {
		t.Fatalf("BatchPrimaryPublic(reversed) error = %v, want nil", err)
	}
	if len(again) != 3 {
		t.Errorf("reversed result = %d entries, want 3", len(again))
	}
}

func TestPostgresStoreSetPrimaryUnknownUserIsNotFound(t *testing.T) {
	store, _, _ := integrationSetup(t)

	_, err := store.SetPrimary(context.Background(), "00000000-0000-4000-8000-000000000000", publicSignal("Nowhere"))
	if !errors.Is(err, ErrUserNotFound) {
		t.Errorf("SetPrimary() error = %v, want ErrUserNotFound (the FK to users)", err)
	}
}

func TestPostgresStoreSetPrimaryRejectsMalformedUserID(t *testing.T) {
	store, _, _ := integrationSetup(t)

	if _, err := store.SetPrimary(context.Background(), "not-a-uuid", publicSignal("Nowhere")); !errors.Is(err, ErrUserNotFound) {
		t.Errorf("SetPrimary() error = %v, want ErrUserNotFound", err)
	}
}

func TestPostgresStoreUserExists(t *testing.T) {
	store, pool, prefix := integrationSetup(t)
	ctx := context.Background()
	userID := createUser(t, pool, prefix, "exists")

	exists, err := store.UserExists(ctx, userID)
	if err != nil {
		t.Fatalf("UserExists() error = %v, want nil", err)
	}
	if !exists {
		t.Error("UserExists() = false, want true for a created user")
	}

	missing, err := store.UserExists(ctx, "00000000-0000-4000-8000-000000000000")
	if err != nil {
		t.Fatalf("UserExists(missing) error = %v, want nil", err)
	}
	if missing {
		t.Error("UserExists(missing) = true, want false")
	}

	malformed, err := store.UserExists(ctx, "not-a-uuid")
	if err != nil {
		t.Fatalf("UserExists(malformed) error = %v, want nil", err)
	}
	if malformed {
		t.Error("UserExists(malformed) = true, want false")
	}
}

func TestNewPostgresStoreRejectsNilPool(t *testing.T) {
	if _, err := NewPostgresStore(nil); err == nil {
		t.Error("NewPostgresStore(nil) error = nil, want an error")
	}
}

// TestPostgresStoreRootedUserIDsByPlaceReturnsEarliestFirst proves the routing
// read: the first Rooted users of a place, earliest declarer first, capped at the
// limit.
func TestPostgresStoreRootedUserIDsByPlaceReturnsEarliestFirst(t *testing.T) {
	store, pool, prefix := integrationSetup(t)
	ctx := context.Background()

	first := createUser(t, pool, prefix, "first")
	second := createUser(t, pool, prefix, "second")
	third := createUser(t, pool, prefix, "third")
	other := createUser(t, pool, prefix, "other")

	// A place unique to this run. A local development database is not empty — it
	// holds real accounts with real rooted signals — so a test that asserted on a
	// well-known place name would be reading someone else's data.
	place := prefix + "Manguzi"

	for _, userID := range []string{first, second, third} {
		if _, err := store.SetPrimary(ctx, userID, publicSignal(place)); err != nil {
			t.Fatalf("SetPrimary(%s) error = %v, want nil", userID, err)
		}
		// The order is by created_at, and now() has microsecond resolution; a small
		// pause keeps the three inserts strictly ordered without depending on it.
		time.Sleep(2 * time.Millisecond)
	}
	if _, err := store.SetPrimary(ctx, other, publicSignal(place+" Elsewhere")); err != nil {
		t.Fatalf("SetPrimary(other) error = %v, want nil", err)
	}

	ids, err := store.RootedUserIDsByPlace(ctx, place, 10)
	if err != nil {
		t.Fatalf("RootedUserIDsByPlace() error = %v, want nil", err)
	}
	if len(ids) != 3 {
		t.Fatalf("ids = %v, want the three users rooted in %q", ids, place)
	}
	if ids[0] != first || ids[1] != second || ids[2] != third {
		t.Errorf("ids = %v, want [%s %s %s] (earliest declarer first)", ids, first, second, third)
	}
	for _, id := range ids {
		if id == other {
			t.Error("a user rooted elsewhere was returned")
		}
	}

	limited, err := store.RootedUserIDsByPlace(ctx, place, 2)
	if err != nil {
		t.Fatalf("RootedUserIDsByPlace(limit 2) error = %v, want nil", err)
	}
	if len(limited) != 2 || limited[0] != first || limited[1] != second {
		t.Errorf("limited ids = %v, want the first two: [%s %s]", limited, first, second)
	}
}

// A hidden signal is one its owner chose not to be findable by, so it must never
// route someone else's question to them.
func TestPostgresStoreRootedUserIDsByPlaceExcludesHiddenAndNonPrimary(t *testing.T) {
	store, pool, prefix := integrationSetup(t)
	ctx := context.Background()

	visible := createUser(t, pool, prefix, "visible")
	hidden := createUser(t, pool, prefix, "hidden")

	// A place unique to this run, so ambient signals cannot appear in the result.
	place := prefix + "Hidden"

	if _, err := store.SetPrimary(ctx, visible, publicSignal(place)); err != nil {
		t.Fatalf("SetPrimary(visible) error = %v, want nil", err)
	}
	hiddenSignal := publicSignal(place)
	hiddenSignal.IsPublic = false
	if _, err := store.SetPrimary(ctx, hidden, hiddenSignal); err != nil {
		t.Fatalf("SetPrimary(hidden) error = %v, want nil", err)
	}

	ids, err := store.RootedUserIDsByPlace(ctx, place, 10)
	if err != nil {
		t.Fatalf("RootedUserIDsByPlace() error = %v, want nil", err)
	}
	if len(ids) != 1 || ids[0] != visible {
		t.Errorf("ids = %v, want only the visible signal's owner %s", ids, visible)
	}
}

func TestPostgresStoreRootedUserIDsByPlaceUnknownPlaceIsEmpty(t *testing.T) {
	store, _, prefix := integrationSetup(t)

	ids, err := store.RootedUserIDsByPlace(context.Background(), prefix+"Nowhere At All", 10)
	if err != nil {
		t.Fatalf("RootedUserIDsByPlace() error = %v, want nil", err)
	}
	if len(ids) != 0 {
		t.Errorf("ids = %v, want empty", ids)
	}
}

func TestPostgresStoreRootedUserIDsByPlaceEmptyPlaceOrBadLimit(t *testing.T) {
	store, _, prefix := integrationSetup(t)
	ctx := context.Background()

	empty, err := store.RootedUserIDsByPlace(ctx, "", 10)
	if err != nil {
		t.Fatalf("RootedUserIDsByPlace(\"\") error = %v, want nil", err)
	}
	if len(empty) != 0 {
		t.Errorf("ids = %v, want empty for a blank place", empty)
	}

	zero, err := store.RootedUserIDsByPlace(ctx, prefix+"Manguzi", 0)
	if err != nil {
		t.Fatalf("RootedUserIDsByPlace(limit 0) error = %v, want nil", err)
	}
	if len(zero) != 0 {
		t.Errorf("ids = %v, want empty for a non-positive limit", zero)
	}
}
