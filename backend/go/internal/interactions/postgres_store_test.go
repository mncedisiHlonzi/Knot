package interactions

import (
	"context"
	"fmt"
	"os"
	"strings"
	"testing"
	"time"

	"github.com/jackc/pgx/v5/pgxpool"

	"github.com/knot/backend/internal/reactions"
)

// integrationEnv is what the integration tests work with: the store under test,
// the pool, and the fixture ids.
type integrationEnv struct {
	store  *PostgresStore
	pool   *pgxpool.Pool
	prefix string
}

// integrationSetup connects to the database named by KNOT_POSTGRES_DSN. The test
// is skipped, not failed, when the DSN is unset, the database is unreachable, or
// the tables have not been migrated yet — none of those is a defect in the code
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

	for _, table := range []string{"public.users", "public.stories", "public.story_versions", "public.comments", "public.reactions"} {
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

	prefix := fmt.Sprintf("knot-it-interactions-%d-", time.Now().UnixNano())

	t.Cleanup(func() {
		cleanupCtx, cancelCleanup := context.WithTimeout(context.Background(), 10*time.Second)
		defer cancelCleanup()

		// Deleting the users cascades to their stories, versions, comments, and
		// reactions, so nothing the fixtures created outlives the test.
		if _, err := pool.Exec(cleanupCtx, "DELETE FROM users WHERE email LIKE $1", prefix+"%"); err != nil {
			t.Logf("cleanup: could not delete test users: %v", err)
		}
		pool.Close()
	})

	return integrationEnv{store: store, pool: pool, prefix: prefix}
}

// createUser inserts one fixture account and returns its id.
func createUser(t *testing.T, env integrationEnv, label string) string {
	t.Helper()

	var id string
	if err := env.pool.QueryRow(
		context.Background(),
		`INSERT INTO users (email, password_hash, display_name, preferred_languages)
		 VALUES ($1, $2, $3, $4)
		 RETURNING id`,
		env.prefix+label+"@example.test",
		"$argon2id$v=19$m=65536,t=1,p=4$c2FsdA$a2V5",
		"Interaction "+label,
		[]string{"eng"},
	).Scan(&id); err != nil {
		t.Fatalf("could not create the %s fixture user: %v", label, err)
	}

	return id
}

// createStoryWithRootVersion publishes a story and its root version in language,
// returning the story and root-version ids. The two rows reference each other, so
// one statement with two data-modifying CTEs writes them together.
func createStoryWithRootVersion(t *testing.T, env integrationEnv, authorID, language string) (string, string) {
	t.Helper()

	var storyID, rootVersionID string
	if err := env.pool.QueryRow(
		context.Background(),
		`WITH new_story AS (
			INSERT INTO stories (author_id, pillar, root_version_id)
			VALUES ($1, 'wonder', gen_random_uuid())
			RETURNING id, root_version_id, author_id
		), new_version AS (
			INSERT INTO story_versions (id, story_id, parent_version_id, author_id, language, title, body)
			SELECT root_version_id, id, NULL, author_id, $2, 'Integration', 'Body' FROM new_story
			RETURNING id
		)
		SELECT id, root_version_id FROM new_story`,
		authorID,
		language,
	).Scan(&storyID, &rootVersionID); err != nil {
		t.Fatalf("could not create the fixture story: %v", err)
	}

	return storyID, rootVersionID
}

// createAdaptation inserts a child version and returns its id.
func createAdaptation(t *testing.T, env integrationEnv, storyID, parentVersionID, authorID, language string) string {
	t.Helper()

	var id string
	if err := env.pool.QueryRow(
		context.Background(),
		`INSERT INTO story_versions (id, story_id, parent_version_id, author_id, language, title, body)
		 VALUES (gen_random_uuid(), $1, $2, $3, $4, 'Adaptation', 'Body')
		 RETURNING id`,
		storyID,
		parentVersionID,
		authorID,
		language,
	).Scan(&id); err != nil {
		t.Fatalf("could not create the fixture adaptation: %v", err)
	}

	return id
}

// addReaction inserts one reaction at an explicit time, so ordering is exact.
func addReaction(t *testing.T, env integrationEnv, userID string, entityType reactions.EntityType, entityID string, reactionType reactions.ReactionType, at time.Time) {
	t.Helper()

	if _, err := env.pool.Exec(
		context.Background(),
		`INSERT INTO reactions (user_id, entity_type, entity_id, reaction_type, created_at)
		 VALUES ($1, $2, $3, $4, $5)`,
		userID,
		string(entityType),
		entityID,
		string(reactionType),
		at,
	); err != nil {
		t.Fatalf("could not insert the %s reaction: %v", reactionType, err)
	}
}

// addComment inserts one comment at an explicit time.
func addComment(t *testing.T, env integrationEnv, versionID, authorID string, at time.Time) string {
	t.Helper()

	var id string
	if err := env.pool.QueryRow(
		context.Background(),
		`INSERT INTO comments (version_id, author_id, language, body, created_at)
		 VALUES ($1, $2, 'eng', 'A comment', $3)
		 RETURNING id`,
		versionID,
		authorID,
		at,
	).Scan(&id); err != nil {
		t.Fatalf("could not insert the fixture comment: %v", err)
	}

	return id
}

// TestPostgresStoreLatestMergesOnePerKind is the brief's scenario: a page with a
// story entry and an adaptation entry, each with older and newer reactions and a
// comment, resolves at most the newest reaction and the newest comment, newest
// first (KNOT-028).
func TestPostgresStoreLatestMergesOnePerKind(t *testing.T) {
	env := integrationSetup(t)

	author := createUser(t, env, "author")
	actorA := createUser(t, env, "actor-a")
	actorB := createUser(t, env, "actor-b")

	storyID, rootVersionID := createStoryWithRootVersion(t, env, author, "eng")
	versionID := createAdaptation(t, env, storyID, rootVersionID, author, "zul")

	base := time.Date(2026, 10, 1, 12, 0, 0, 0, time.UTC)

	// Story: two reactions (the newer one must win) and one comment.
	addReaction(t, env, actorA, reactions.EntityStory, storyID, reactions.RingsTrue, base.Add(1*time.Minute))
	storyReactionDetail := "needs_a_source"
	addReaction(t, env, actorB, reactions.EntityStory, storyID, reactions.NeedsASource, base.Add(3*time.Minute))
	rootCommentID := addComment(t, env, rootVersionID, actorB, base.Add(4*time.Minute))

	// Adaptation: one reaction and one newer comment.
	addReaction(t, env, actorA, reactions.EntityVersion, versionID, reactions.AddsSomethingNew, base.Add(2*time.Minute))
	versionCommentID := addComment(t, env, versionID, actorA, base.Add(5*time.Minute))

	latest, err := env.store.Latest(context.Background(), []Target{
		{
			Key:                "story-entry",
			ReactionEntityType: reactions.EntityStory,
			ReactionEntityID:   storyID,
			CommentVersionID:   rootVersionID,
		},
		{
			Key:                "version-entry",
			ReactionEntityType: reactions.EntityVersion,
			ReactionEntityID:   versionID,
			CommentVersionID:   versionID,
		},
	})
	if err != nil {
		t.Fatalf("Latest() error = %v, want nil", err)
	}

	storyInteractions := latest["story-entry"]
	if len(storyInteractions) != 2 {
		t.Fatalf("story interactions = %d, want 2 (one reaction, one comment)", len(storyInteractions))
	}
	// The comment is newer than the latest reaction, so it leads.
	if storyInteractions[0].Kind != KindComment || storyInteractions[0].ID != rootCommentID {
		t.Errorf("story[0] = %+v, want the newest comment %s", storyInteractions[0], rootCommentID)
	}
	if storyInteractions[0].ActorID != actorB {
		t.Errorf("story comment actor = %s, want %s", storyInteractions[0].ActorID, actorB)
	}
	if storyInteractions[1].Kind != KindReaction {
		t.Errorf("story[1].Kind = %s, want %s", storyInteractions[1].Kind, KindReaction)
	}
	if storyInteractions[1].Detail != storyReactionDetail {
		t.Errorf("story reaction detail = %q, want %q (the NEWER reaction)", storyInteractions[1].Detail, storyReactionDetail)
	}
	if storyInteractions[1].ActorID != actorB {
		t.Errorf("story reaction actor = %s, want %s", storyInteractions[1].ActorID, actorB)
	}

	versionInteractions := latest["version-entry"]
	if len(versionInteractions) != 2 {
		t.Fatalf("version interactions = %d, want 2", len(versionInteractions))
	}
	if versionInteractions[0].Kind != KindComment || versionInteractions[0].ID != versionCommentID {
		t.Errorf("version[0] = %+v, want the newest comment %s", versionInteractions[0], versionCommentID)
	}
	if versionInteractions[1].Kind != KindReaction || versionInteractions[1].Detail != string(reactions.AddsSomethingNew) {
		t.Errorf("version[1] = %+v, want the adds_something_new reaction", versionInteractions[1])
	}
}

// TestPostgresStoreLatestLeavesEmptyTargetsOut proves a target with no rows is
// simply absent, which the handler reads as "nothing to show".
func TestPostgresStoreLatestLeavesEmptyTargetsOut(t *testing.T) {
	env := integrationSetup(t)

	author := createUser(t, env, "author")
	storyID, rootVersionID := createStoryWithRootVersion(t, env, author, "eng")

	latest, err := env.store.Latest(context.Background(), []Target{
		{
			Key:                "quiet",
			ReactionEntityType: reactions.EntityStory,
			ReactionEntityID:   storyID,
			CommentVersionID:   rootVersionID,
		},
	})
	if err != nil {
		t.Fatalf("Latest() error = %v, want nil", err)
	}
	if _, present := latest["quiet"]; present {
		t.Errorf("latest[quiet] = %v, want the key to be absent", latest["quiet"])
	}
}

// TestPostgresStoreLatestEmptyInput proves an empty page is a no-op with no query.
func TestPostgresStoreLatestEmptyInput(t *testing.T) {
	env := integrationSetup(t)

	latest, err := env.store.Latest(context.Background(), nil)
	if err != nil {
		t.Fatalf("Latest(nil) error = %v, want nil", err)
	}
	if len(latest) != 0 {
		t.Errorf("Latest(nil) = %v, want an empty map", latest)
	}
}
