package reactions

import (
	"context"
	"fmt"
	"os"
	"strings"
	"testing"
	"time"

	"github.com/jackc/pgx/v5/pgxpool"
)

// Fixed entity ids. Reactions hold a bare UUID with no foreign key, so any
// canonical UUID is a valid target.
const (
	itEntityA = "aa111111-1111-4111-8111-111111111111"
	itEntityB = "bb222222-2222-4222-8222-222222222222"
)

// integrationEnv is what the integration tests work with: the store under test,
// the pool, one user, and two entity ids to react to.
type integrationEnv struct {
	store  *PostgresStore
	pool   *pgxpool.Pool
	prefix string
	userID string
}

// integrationSetup connects to the database named by KNOT_POSTGRES_DSN and creates
// the user the reactions point at.
//
// The test is skipped, not failed, when the DSN is unset, when the database is
// unreachable, or when the tables have not been migrated: none of those is a
// defect in the code under test.
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

	// The reactions table (0013) and the users it references (0001) must both
	// exist.
	for _, table := range []string{"public.users", "public.reactions"} {
		var present bool
		if err := pool.QueryRow(ctx, "SELECT to_regclass($1) IS NOT NULL", table).Scan(&present); err != nil {
			pool.Close()
			t.Skipf("could not inspect the schema: %v", err)
		}
		if !present {
			pool.Close()
			t.Skipf("the %s table does not exist; run `go run ./cmd/knot migrate up` first", table)
		}
	}

	store, err := NewPostgresStore(pool)
	if err != nil {
		pool.Close()
		t.Fatalf("NewPostgresStore() error = %v, want nil", err)
	}

	prefix := fmt.Sprintf("knot-it-reactions-%d-", time.Now().UnixNano())

	var userID string
	if err := pool.QueryRow(
		ctx,
		`INSERT INTO users (email, password_hash, display_name, preferred_languages)
		 VALUES ($1, $2, $3, $4)
		 RETURNING id`,
		prefix+"reactor@example.test",
		"$argon2id$v=19$m=65536,t=1,p=4$c2FsdA$a2V5",
		"Integration Reactor",
		[]string{"eng"},
	).Scan(&userID); err != nil {
		pool.Close()
		t.Fatalf("could not create the integration user: %v", err)
	}

	t.Cleanup(func() {
		cleanupCtx, cancelCleanup := context.WithTimeout(context.Background(), 10*time.Second)
		defer cancelCleanup()

		// Deleting the user cascades to their reactions, so no row survives.
		if _, err := pool.Exec(cleanupCtx, "DELETE FROM users WHERE email LIKE $1", prefix+"%"); err != nil {
			t.Logf("cleanup: could not delete test users: %v", err)
		}
		pool.Close()
	})

	return integrationEnv{store: store, pool: pool, prefix: prefix, userID: userID}
}

func TestPostgresStoreToggleInsertsThenRemoves(t *testing.T) {
	env := integrationSetup(t)
	ctx := context.Background()

	reaction := Reaction{UserID: env.userID, EntityType: EntityStory, EntityID: itEntityA, ReactionType: RingsTrue}

	active, err := env.store.Toggle(ctx, reaction)
	if err != nil {
		t.Fatalf("Toggle() error = %v, want nil", err)
	}
	if !active {
		t.Error("first Toggle() active = false, want true")
	}

	summary, err := env.store.Summaries(ctx, EntityStory, []string{itEntityA})
	if err != nil {
		t.Fatalf("Summaries() error = %v, want nil", err)
	}
	if summary[itEntityA].RingsTrue != 1 {
		t.Errorf("rings_true after insert = %d, want 1", summary[itEntityA].RingsTrue)
	}

	active, err = env.store.Toggle(ctx, reaction)
	if err != nil {
		t.Fatalf("second Toggle() error = %v, want nil", err)
	}
	if active {
		t.Error("second Toggle() active = true, want false when the reaction is removed")
	}

	summary, err = env.store.Summaries(ctx, EntityStory, []string{itEntityA})
	if err != nil {
		t.Fatalf("Summaries() error = %v, want nil", err)
	}
	if summary[itEntityA].Total() != 0 {
		t.Errorf("total after removal = %d, want 0", summary[itEntityA].Total())
	}
}

func TestPostgresStoreToggleIsScopedPerReactionType(t *testing.T) {
	env := integrationSetup(t)
	ctx := context.Background()

	base := Reaction{UserID: env.userID, EntityType: EntityStory, EntityID: itEntityA}

	ringsTrue := base
	ringsTrue.ReactionType = RingsTrue
	needsASource := base
	needsASource.ReactionType = NeedsASource

	if _, err := env.store.Toggle(ctx, ringsTrue); err != nil {
		t.Fatalf("Toggle(rings_true) error = %v, want nil", err)
	}
	if _, err := env.store.Toggle(ctx, needsASource); err != nil {
		t.Fatalf("Toggle(needs_a_source) error = %v, want nil", err)
	}

	// Both signals are held at once: they do not compete (KNOT-ADR-050).
	held, err := env.store.MyReactions(ctx, env.userID, EntityStory, []string{itEntityA})
	if err != nil {
		t.Fatalf("MyReactions() error = %v, want nil", err)
	}
	if len(held[itEntityA]) != 2 {
		t.Errorf("held = %v, want two signals", held[itEntityA])
	}

	// Removing one leaves the other.
	if active, err := env.store.Toggle(ctx, ringsTrue); err != nil || active {
		t.Fatalf("Toggle(rings_true) again = (%v, %v), want (false, nil)", active, err)
	}
	held, err = env.store.MyReactions(ctx, env.userID, EntityStory, []string{itEntityA})
	if err != nil {
		t.Fatalf("MyReactions() error = %v, want nil", err)
	}
	if len(held[itEntityA]) != 1 || held[itEntityA][0] != NeedsASource {
		t.Errorf("held after removal = %v, want [needs_a_source]", held[itEntityA])
	}
}

func TestPostgresStoreUniqueIndexRejectsDuplicate(t *testing.T) {
	env := integrationSetup(t)
	ctx := context.Background()

	const insert = `
		INSERT INTO reactions (user_id, entity_type, entity_id, reaction_type)
		VALUES ($1, $2, $3, $4)`

	if _, err := env.pool.Exec(ctx, insert, env.userID, "story", itEntityA, "rings_true"); err != nil {
		t.Fatalf("first insert error = %v, want nil", err)
	}
	if _, err := env.pool.Exec(ctx, insert, env.userID, "story", itEntityA, "rings_true"); err == nil {
		t.Error("second insert error = nil, want a unique-violation error")
	}
}

func TestPostgresStoreSummariesAggregatePerTypeAndBatchAcrossEntities(t *testing.T) {
	env := integrationSetup(t)
	ctx := context.Background()

	// A second user so the counts are more than one, exercising count(*).
	var otherID string
	if err := env.pool.QueryRow(
		ctx,
		`INSERT INTO users (email, password_hash, display_name, preferred_languages)
		 VALUES ($1, $2, $3, $4)
		 RETURNING id`,
		env.prefix+"other@example.test",
		"$argon2id$v=19$m=65536,t=1,p=4$c2FsdA$a2V5",
		"Integration Other",
		[]string{"eng"},
	).Scan(&otherID); err != nil {
		t.Fatalf("could not create the second user: %v", err)
	}

	// ItEntityA: two rings_true (one per user) and one adds_something_new.
	// ItEntityB: one needs_a_source.
	for _, reaction := range []Reaction{
		{UserID: env.userID, EntityType: EntityStory, EntityID: itEntityA, ReactionType: RingsTrue},
		{UserID: otherID, EntityType: EntityStory, EntityID: itEntityA, ReactionType: RingsTrue},
		{UserID: env.userID, EntityType: EntityStory, EntityID: itEntityA, ReactionType: AddsSomethingNew},
		{UserID: env.userID, EntityType: EntityStory, EntityID: itEntityB, ReactionType: NeedsASource},
		// A reaction of a different entity type must not leak into the summary.
		{UserID: env.userID, EntityType: EntityComment, EntityID: itEntityA, ReactionType: NeedsASource},
	} {
		if _, err := env.store.Toggle(ctx, reaction); err != nil {
			t.Fatalf("Toggle(%+v) error = %v, want nil", reaction, err)
		}
	}

	summaries, err := env.store.Summaries(ctx, EntityStory, []string{itEntityA, itEntityB})
	if err != nil {
		t.Fatalf("Summaries() error = %v, want nil", err)
	}

	a := summaries[itEntityA]
	if a.RingsTrue != 2 || a.AddsSomethingNew != 1 || a.NeedsASource != 0 {
		t.Errorf("summary[A] = %+v, want rings_true 2 / adds_something_new 1 / needs_a_source 0", a)
	}
	b := summaries[itEntityB]
	if b.NeedsASource != 1 || b.RingsTrue != 0 {
		t.Errorf("summary[B] = %+v, want needs_a_source 1", b)
	}
	if a.Total() != 3 {
		t.Errorf("summary[A].Total() = %d, want 3", a.Total())
	}
}

func TestPostgresStoreMyReactionsIsPerUser(t *testing.T) {
	env := integrationSetup(t)
	ctx := context.Background()

	if _, err := env.store.Toggle(ctx, Reaction{UserID: env.userID, EntityType: EntityStory, EntityID: itEntityA, ReactionType: RingsTrue}); err != nil {
		t.Fatalf("Toggle() error = %v, want nil", err)
	}

	held, err := env.store.MyReactions(ctx, env.userID, EntityStory, []string{itEntityA, itEntityB})
	if err != nil {
		t.Fatalf("MyReactions() error = %v, want nil", err)
	}
	if len(held[itEntityA]) != 1 || held[itEntityA][0] != RingsTrue {
		t.Errorf("held[A] = %v, want [rings_true]", held[itEntityA])
	}
	if _, ok := held[itEntityB]; ok {
		t.Errorf("held[B] = %v, want no entry for an entity the user did not react to", held[itEntityB])
	}
}

func TestPostgresStoreListForEntityNewestFirst(t *testing.T) {
	env := integrationSetup(t)
	ctx := context.Background()

	// Insert with explicit timestamps so the order is deterministic: the older
	// signal is an hour behind the newer one.
	const insert = `
		INSERT INTO reactions (user_id, entity_type, entity_id, reaction_type, created_at)
		VALUES ($1, 'story', $2, $3, $4)`

	if _, err := env.pool.Exec(ctx, insert, env.userID, itEntityA, "rings_true", time.Now().Add(-time.Hour)); err != nil {
		t.Fatalf("older insert error = %v, want nil", err)
	}
	if _, err := env.pool.Exec(ctx, insert, env.userID, itEntityA, "adds_something_new", time.Now()); err != nil {
		t.Fatalf("newer insert error = %v, want nil", err)
	}

	list, err := env.store.ListForEntity(ctx, EntityStory, itEntityA)
	if err != nil {
		t.Fatalf("ListForEntity() error = %v, want nil", err)
	}
	if len(list) != 2 {
		t.Fatalf("len(list) = %d, want 2", len(list))
	}
	// Newest first: the newer insertion leads.
	if list[0].ReactionType != AddsSomethingNew {
		t.Errorf("list[0] = %q, want adds_something_new (newest first)", list[0].ReactionType)
	}
	if list[1].ReactionType != RingsTrue {
		t.Errorf("list[1] = %q, want rings_true", list[1].ReactionType)
	}
}
