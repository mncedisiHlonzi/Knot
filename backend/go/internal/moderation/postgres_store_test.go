package moderation

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

// integrationEnv is the pool plus the users a test attaches its rows to.
type integrationEnv struct {
	store  *PostgresStore
	pool   *pgxpool.Pool
	prefix string
	userA  string
	userB  string
	userC  string
}

// integrationSetup connects to KNOT_POSTGRES_DSN and creates three users.
//
// It is skipped, not failed, when the DSN is unset, the database is unreachable,
// or migration 0015 has not been applied — none of those is a defect in the code
// under test.
func integrationSetup(t *testing.T) integrationEnv {
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
	if err := pool.QueryRow(ctx, "SELECT to_regclass('public.blocks') IS NOT NULL").Scan(&migrated); err != nil {
		pool.Close()
		t.Skipf("could not inspect the schema: %v", err)
	}
	if !migrated {
		pool.Close()
		t.Skip("the moderation tables do not exist; run `go run ./cmd/knot migrate up` first")
	}

	store, err := NewPostgresStore(pool)
	if err != nil {
		pool.Close()
		t.Fatalf("NewPostgresStore() error = %v, want nil", err)
	}

	prefix := fmt.Sprintf("knot-mod-it-%d-", time.Now().UnixNano())

	createUser := func(label string) string {
		var id string
		err := pool.QueryRow(
			ctx,
			`INSERT INTO users (email, password_hash, display_name, preferred_languages)
			 VALUES ($1, $2, $3, $4)
			 RETURNING id`,
			prefix+label+"@example.test",
			"$argon2id$v=19$m=65536,t=1,p=4$c2FsdA$a2V5",
			"Moderation IT "+label,
			[]string{"eng"},
		).Scan(&id)
		if err != nil {
			pool.Close()
			t.Fatalf("could not create user %s: %v", label, err)
		}
		return id
	}

	env := integrationEnv{
		store:  store,
		pool:   pool,
		prefix: prefix,
		userA:  createUser("a"),
		userB:  createUser("b"),
		userC:  createUser("c"),
	}

	t.Cleanup(func() {
		cleanupCtx, cancelCleanup := context.WithTimeout(context.Background(), 10*time.Second)
		defer cancelCleanup()

		// Deleting the users cascades to blocks, reports, and audit_log. Cases have
		// no user FK, so they are removed separately, matched on the entity ids the
		// tests created.
		if _, err := pool.Exec(cleanupCtx, "DELETE FROM moderation_cases WHERE entity_id IN (SELECT entity_id FROM reports WHERE reporter_id IN ($1, $2, $3))", env.userA, env.userB, env.userC); err != nil {
			t.Logf("cleanup: could not delete test cases: %v", err)
		}
		if _, err := pool.Exec(cleanupCtx, "DELETE FROM users WHERE email LIKE $1", prefix+"%"); err != nil {
			t.Logf("cleanup: could not delete test users: %v", err)
		}
		pool.Close()
	})

	return env
}

// newEntityID returns a fresh random UUID for a reported entity, so a run never
// collides with a leftover case from a previous run.
func newEntityID(t *testing.T, env integrationEnv) string {
	t.Helper()

	var id string
	if err := env.pool.QueryRow(context.Background(), "SELECT gen_random_uuid()").Scan(&id); err != nil {
		t.Fatalf("could not generate an entity id: %v", err)
	}
	return id
}

func TestPostgresStoreBlockLifecycle(t *testing.T) {
	env := integrationSetup(t)
	ctx := context.Background()

	created, err := env.store.CreateBlock(ctx, env.userA, env.userB)
	if err != nil {
		t.Fatalf("CreateBlock() error = %v, want nil", err)
	}
	if !created {
		t.Error("CreateBlock() created = false, want true")
	}

	again, err := env.store.CreateBlock(ctx, env.userA, env.userB)
	if err != nil {
		t.Fatalf("CreateBlock() second error = %v, want nil", err)
	}
	if again {
		t.Error("CreateBlock() second created = true, want false (idempotent)")
	}

	// Both directions are blocked.
	for _, pair := range [][2]string{{env.userA, env.userB}, {env.userB, env.userA}} {
		blocked, err := env.store.IsBlocked(ctx, pair[0], pair[1])
		if err != nil {
			t.Fatalf("IsBlocked(%s, %s) error = %v", pair[0], pair[1], err)
		}
		if !blocked {
			t.Errorf("IsBlocked(%s, %s) = false, want true", pair[0], pair[1])
		}
	}

	// The mutual set of each side contains the other.
	ids, err := env.store.BlockedUserIDs(ctx, env.userA)
	if err != nil {
		t.Fatalf("BlockedUserIDs() error = %v, want nil", err)
	}
	if !contains(ids, env.userB) {
		t.Errorf("BlockedUserIDs(A) = %v, want to contain B", ids)
	}

	reverse, err := env.store.BlockedUserIDs(ctx, env.userB)
	if err != nil {
		t.Fatalf("BlockedUserIDs(B) error = %v, want nil", err)
	}
	if !contains(reverse, env.userA) {
		t.Errorf("BlockedUserIDs(B) = %v, want to contain A", reverse)
	}

	pairs, err := env.store.BlockedPairs(ctx, env.userA, []string{env.userB, env.userC})
	if err != nil {
		t.Fatalf("BlockedPairs() error = %v, want nil", err)
	}
	if !pairs[env.userB] {
		t.Errorf("BlockedPairs(A) = %v, want B true", pairs)
	}
	if pairs[env.userC] {
		t.Errorf("BlockedPairs(A) = %v, want C absent", pairs)
	}

	page, _, err := env.store.ListBlocks(ctx, env.userA, nil, 20)
	if err != nil {
		t.Fatalf("ListBlocks() error = %v, want nil", err)
	}
	if len(page) != 1 || page[0].BlockedID != env.userB {
		t.Errorf("ListBlocks() = %+v, want one block on B", page)
	}

	if err := env.store.DeleteBlock(ctx, env.userA, env.userB); err != nil {
		t.Fatalf("DeleteBlock() error = %v, want nil", err)
	}
	blocked, err := env.store.IsBlocked(ctx, env.userA, env.userB)
	if err != nil {
		t.Fatalf("IsBlocked() after delete error = %v", err)
	}
	if blocked {
		t.Error("IsBlocked() after delete = true, want false")
	}
}

func TestPostgresStoreReportAggregatesIntoOneCase(t *testing.T) {
	env := integrationSetup(t)
	ctx := context.Background()
	entity := newEntityID(t, env)

	first, err := env.store.CreateReport(ctx, Report{
		ReporterID: env.userA,
		EntityType: EntityStory,
		EntityID:   entity,
		Category:   CategoryHarassment,
	})
	if err != nil {
		t.Fatalf("CreateReport() error = %v, want nil", err)
	}
	if first.ID == "" {
		t.Error("CreateReport() returned an empty id")
	}

	// A second report by the same user on the same entity is a unique violation.
	if _, err := env.store.CreateReport(ctx, Report{
		ReporterID: env.userA,
		EntityType: EntityStory,
		EntityID:   entity,
		Category:   CategorySpam,
	}); !errors.Is(err, ErrAlreadyReported) {
		t.Fatalf("duplicate CreateReport() error = %v, want ErrAlreadyReported", err)
	}

	// A different user's report aggregates into the same case.
	if _, err := env.store.CreateReport(ctx, Report{
		ReporterID: env.userB,
		EntityType: EntityStory,
		EntityID:   entity,
		Category:   CategorySpam,
	}); err != nil {
		t.Fatalf("second-user CreateReport() error = %v, want nil", err)
	}

	var reportCount int
	if err := env.pool.QueryRow(ctx, "SELECT report_count FROM moderation_cases WHERE entity_type = $1 AND entity_id = $2", string(EntityStory), entity).Scan(&reportCount); err != nil {
		t.Fatalf("could not read the case: %v", err)
	}
	if reportCount != 2 {
		t.Errorf("case report_count = %d, want 2", reportCount)
	}

	var caseRows int
	if err := env.pool.QueryRow(ctx, "SELECT count(*) FROM moderation_cases WHERE entity_id = $1", entity).Scan(&caseRows); err != nil {
		t.Fatalf("could not count cases: %v", err)
	}
	if caseRows != 1 {
		t.Errorf("cases for the entity = %d, want 1 (aggregated)", caseRows)
	}

	mine, _, err := env.store.ListMyReports(ctx, env.userA, nil, 20)
	if err != nil {
		t.Fatalf("ListMyReports() error = %v, want nil", err)
	}
	if len(mine) != 1 || mine[0].EntityID != entity {
		t.Errorf("ListMyReports() = %+v, want one report on the entity", mine)
	}
}

func TestPostgresStoreAuditLogWrites(t *testing.T) {
	env := integrationSetup(t)
	ctx := context.Background()

	if err := env.store.Log(ctx, env.userA, "report.created", "story", env.userB, map[string]any{"category": "spam"}); err != nil {
		t.Fatalf("Log() error = %v, want nil", err)
	}

	var meta []byte
	if err := env.pool.QueryRow(ctx, "SELECT metadata FROM audit_log WHERE actor_id = $1 AND action = $2", env.userA, "report.created").Scan(&meta); err != nil {
		t.Fatalf("could not read the audit row: %v", err)
	}
	if !strings.Contains(string(meta), "spam") {
		t.Errorf("audit metadata = %s, want it to carry the category", meta)
	}

	// A nil target id is stored as NULL, and nil metadata as an empty object.
	if err := env.store.Log(ctx, env.userA, "role.changed", "user", "", nil); err != nil {
		t.Fatalf("Log() with empty target error = %v, want nil", err)
	}
	var targetID *string
	if err := env.pool.QueryRow(ctx, "SELECT target_id::text FROM audit_log WHERE actor_id = $1 AND action = $2", env.userA, "role.changed").Scan(&targetID); err != nil {
		t.Fatalf("could not read the second audit row: %v", err)
	}
	if targetID != nil {
		t.Errorf("audit target_id = %v, want NULL", *targetID)
	}
}

func contains(values []string, want string) bool {
	for _, value := range values {
		if value == want {
			return true
		}
	}
	return false
}
