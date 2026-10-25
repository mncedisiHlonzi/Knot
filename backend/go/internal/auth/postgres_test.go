package auth

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"fmt"
	"os"
	"strings"
	"sync/atomic"
	"testing"
	"time"

	"github.com/jackc/pgx/v5/pgxpool"
)

// testUserSeq makes the fixture email unique across every user a single test run
// creates, so two tests that start in the same nanosecond cannot collide on the
// users email index.
var testUserSeq atomic.Int64

// newTestPool connects to the database named by KNOT_POSTGRES_DSN.
//
// The test is skipped, not failed, when KNOT_POSTGRES_DSN is unset, when the
// database is unreachable, or when the identity tables have not been migrated
// yet — none of those is a defect in the code under test.
func newTestPool(t *testing.T) *pgxpool.Pool {
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

	// The stores are useless against a schema that predates migration 0021, and a
	// missing table would surface as a confusing scan error in the test body.
	const query = `
		SELECT count(*)
		FROM unnest(ARRAY['users', 'sessions', 'credentials', 'verification_tokens']) AS name
		WHERE to_regclass('public.' || name) IS NOT NULL`

	var tables int
	if err := pool.QueryRow(ctx, query).Scan(&tables); err != nil {
		pool.Close()
		t.Skipf("could not inspect the schema: %v", err)
	}
	if tables < 4 {
		pool.Close()
		t.Skip("the identity tables do not exist; run `go run ./cmd/knot migrate up` first")
	}

	t.Cleanup(pool.Close)

	return pool
}

// newTestUser inserts a user row and returns its id.
//
// The row is deleted when the test ends, and ON DELETE CASCADE takes every
// session, credential and token that references it with it, so a test never
// leaves anything behind.
func newTestUser(t *testing.T, pool *pgxpool.Pool) string {
	t.Helper()

	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()

	email := fmt.Sprintf(
		"knot-auth-it-%d-%d@example.test",
		time.Now().UnixNano(),
		testUserSeq.Add(1),
	)

	const query = `
		INSERT INTO users (email, password_hash, display_name)
		VALUES ($1, $2, $3)
		RETURNING id`

	var id string
	if err := pool.QueryRow(ctx, query, email, "$argon2id$v=19$m=65536,t=1,p=4$c2FsdA$a2V5", "Auth Integration").Scan(&id); err != nil {
		t.Fatalf("inserting a test user: %v", err)
	}

	t.Cleanup(func() {
		cleanupCtx, cancelCleanup := context.WithTimeout(context.Background(), 10*time.Second)
		defer cancelCleanup()

		if _, err := pool.Exec(cleanupCtx, "DELETE FROM users WHERE id = $1", id); err != nil {
			t.Logf("cleanup: could not delete test user %s: %v", id, err)
		}
	})

	return id
}

// testHash returns a plausible hex-encoded SHA-256 for a seed, so a test never
// writes a token that could be mistaken for a real one.
func testHash(seed string) string {
	sum := sha256.Sum256([]byte(seed))

	return hex.EncodeToString(sum[:])
}

// missingUUID is a well-formed id that matches no row.
const missingUUID = "00000000-0000-4000-8000-000000000000"

// malformedUUID is a value that is not a UUID at all.
const malformedUUID = "not-a-uuid"
