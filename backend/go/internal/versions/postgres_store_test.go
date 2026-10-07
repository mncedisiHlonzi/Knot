package versions

import (
	"context"
	"errors"
	"fmt"
	"os"
	"strings"
	"testing"
	"time"

	"github.com/jackc/pgx/v5/pgxpool"

	"github.com/knot/backend/internal/stories"
)

// integrationEnv is what the integration tests work with: the versions store
// under test, a stories store used to create the stories versions attach to, the
// pool, and the author they share.
type integrationEnv struct {
	store      *PostgresStore
	storyStore *stories.PostgresStore
	pool       *pgxpool.Pool
	prefix     string
	author     string
}

// integrationSetup connects to the database named by KNOT_POSTGRES_DSN and
// creates an author to attach stories to.
//
// The test is skipped, not failed, when KNOT_POSTGRES_DSN is unset, when the
// database is unreachable, or when the tables have not been migrated yet — none
// of those is a defect in the code under test.
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
	if err := pool.QueryRow(ctx, "SELECT to_regclass('public.story_versions') IS NOT NULL").Scan(&migrated); err != nil {
		pool.Close()
		t.Skipf("could not inspect the schema: %v", err)
	}
	if !migrated {
		pool.Close()
		t.Skip("the story_versions table does not exist; run `go run ./cmd/knot migrate up` first")
	}

	store, err := NewPostgresStore(pool)
	if err != nil {
		pool.Close()
		t.Fatalf("NewPostgresStore() error = %v, want nil", err)
	}

	storyStore, err := stories.NewPostgresStore(pool)
	if err != nil {
		pool.Close()
		t.Fatalf("stories.NewPostgresStore() error = %v, want nil", err)
	}

	prefix := fmt.Sprintf("knot-it-versions-%d-", time.Now().UnixNano())

	var author string
	err = pool.QueryRow(
		ctx,
		`INSERT INTO users (email, password_hash, display_name, preferred_languages)
		 VALUES ($1, $2, $3, $4)
		 RETURNING id`,
		prefix+"author@example.test",
		"$argon2id$v=19$m=65536,t=1,p=4$c2FsdA$a2V5",
		"Integration Author",
		[]string{"en"},
	).Scan(&author)
	if err != nil {
		pool.Close()
		t.Fatalf("could not create the integration author: %v", err)
	}

	t.Cleanup(func() {
		cleanupCtx, cancelCleanup := context.WithTimeout(context.Background(), 10*time.Second)
		defer cancelCleanup()

		// Deleting the author's stories cascades to their versions; the users
		// delete then removes the author. Stories go first so the
		// root_version_id RESTRICT constraint never blocks the user delete.
		if _, err := pool.Exec(cleanupCtx, "DELETE FROM stories WHERE author_id = $1", author); err != nil {
			t.Logf("cleanup: could not delete test stories: %v", err)
		}
		if _, err := pool.Exec(cleanupCtx, "DELETE FROM users WHERE email LIKE $1", prefix+"%"); err != nil {
			t.Logf("cleanup: could not delete test users: %v", err)
		}
		pool.Close()
	})

	return integrationEnv{store: store, storyStore: storyStore, pool: pool, prefix: prefix, author: author}
}

// newIntegrationStory creates a story (and, through the stories store, its root
// version) to attach versions to.
func newIntegrationStory(t *testing.T, env integrationEnv, name string) stories.Story {
	t.Helper()

	story, err := env.storyStore.CreateStory(context.Background(), stories.Story{
		AuthorID:  env.author,
		Pillar:    stories.PillarHeritage,
		Language:  "en",
		Title:     "Integration " + name,
		Body:      "Body for " + name,
		MediaURLs: []string{},
	})
	if err != nil {
		t.Fatalf("CreateStory(%q) error = %v, want nil", name, err)
	}
	return story
}

// countVersions counts every version of one story, read with an independent query
// so the assertions are not circular.
func countVersions(t *testing.T, env integrationEnv, storyID string) int {
	t.Helper()

	var total int
	if err := env.pool.QueryRow(
		context.Background(),
		"SELECT count(*) FROM story_versions WHERE story_id = $1",
		storyID,
	).Scan(&total); err != nil {
		t.Fatalf("could not count versions: %v", err)
	}
	return total
}

func TestPostgresStoreCreateVersionAsChildRoundTrip(t *testing.T) {
	env := integrationSetup(t)
	ctx := context.Background()

	story := newIntegrationStory(t, env, "child")

	created, err := env.store.CreateVersion(ctx, StoryVersion{
		StoryID:         story.ID,
		ParentVersionID: story.RootVersionID,
		AuthorID:        env.author,
		Language:        "fr",
		Title:           "La première pluie",
		Body:            "Grand-mère disait que la première pluie se souvient de chaque nom.",
		AdaptationNote:  "Rendered for French-speaking listeners.",
	})
	if err != nil {
		t.Fatalf("CreateVersion() error = %v, want nil", err)
	}

	if !isUUID(created.ID) {
		t.Errorf("id = %q, want a canonical uuid generated by the database", created.ID)
	}
	if created.StoryID != story.ID {
		t.Errorf("story id = %q, want %q", created.StoryID, story.ID)
	}
	if created.ParentVersionID != story.RootVersionID {
		t.Errorf("parent version id = %q, want the root %q", created.ParentVersionID, story.RootVersionID)
	}
	if created.Language != "fr" {
		t.Errorf("language = %q, want %q", created.Language, "fr")
	}
	if created.AdaptationNote != "Rendered for French-speaking listeners." {
		t.Errorf("adaptation note = %q, want the stored note", created.AdaptationNote)
	}
	if created.CreatedAt.IsZero() || created.UpdatedAt.IsZero() {
		t.Error("timestamps are zero, want database-generated values")
	}

	fetched, err := env.store.GetVersion(ctx, created.ID)
	if err != nil {
		t.Fatalf("GetVersion() error = %v, want nil", err)
	}
	if fetched.ID != created.ID || fetched.Body != created.Body || fetched.Title != created.Title {
		t.Errorf("fetched = %+v, want it to match the created version %+v", fetched, created)
	}
	if !fetched.IsRoot() && fetched.ParentVersionID != created.ParentVersionID {
		t.Errorf("parent version id = %q, want %q", fetched.ParentVersionID, created.ParentVersionID)
	}
}

func TestPostgresStoreCreateVersionWithoutOptionalNote(t *testing.T) {
	env := integrationSetup(t)
	ctx := context.Background()

	story := newIntegrationStory(t, env, "no-note")

	created, err := env.store.CreateVersion(ctx, StoryVersion{
		StoryID:         story.ID,
		ParentVersionID: story.RootVersionID,
		AuthorID:        env.author,
		Language:        "en",
		Title:           "A retelling",
		Body:            "Same story, plainer words.",
	})
	if err != nil {
		t.Fatalf("CreateVersion() error = %v, want nil", err)
	}
	if created.AdaptationNote != "" {
		t.Errorf("adaptation note = %q, want empty for a NULL column", created.AdaptationNote)
	}
}

func TestPostgresStoreRejectsASecondRootVersion(t *testing.T) {
	// Every story already has a root version (created with the story), so the
	// partial unique index story_versions_story_id_root_unique must refuse a
	// second parentless version. This is the constraint that makes "exactly one
	// root per story" true rather than merely intended.
	env := integrationSetup(t)
	ctx := context.Background()

	story := newIntegrationStory(t, env, "second-root")

	_, err := env.store.CreateVersion(ctx, StoryVersion{
		StoryID:  story.ID,
		AuthorID: env.author,
		Language: "en",
		Title:    "Second root",
		Body:     "Should never be stored.",
	})
	if err == nil {
		t.Fatal("CreateVersion() error = nil, want the partial unique index to reject a second root")
	}

	if got := countVersions(t, env, story.ID); got != 1 {
		t.Errorf("story has %d versions, want only its one root", got)
	}
}

func TestPostgresStoreListByStoryReturnsOnlyThatStorysVersions(t *testing.T) {
	env := integrationSetup(t)
	ctx := context.Background()

	storyA := newIntegrationStory(t, env, "list-a")
	storyB := newIntegrationStory(t, env, "list-b")

	for i := 0; i < 2; i++ {
		if _, err := env.store.CreateVersion(ctx, StoryVersion{
			StoryID:         storyA.ID,
			ParentVersionID: storyA.RootVersionID,
			AuthorID:        env.author,
			Language:        "fr",
			Title:           fmt.Sprintf("A child %d", i),
			Body:            "Body A",
		}); err != nil {
			t.Fatalf("CreateVersion(A, %d) error = %v, want nil", i, err)
		}
	}
	if _, err := env.store.CreateVersion(ctx, StoryVersion{
		StoryID:         storyB.ID,
		ParentVersionID: storyB.RootVersionID,
		AuthorID:        env.author,
		Language:        "pt",
		Title:           "B child",
		Body:            "Body B",
	}); err != nil {
		t.Fatalf("CreateVersion(B) error = %v, want nil", err)
	}

	versions, err := env.store.ListByStory(ctx, storyA.ID)
	if err != nil {
		t.Fatalf("ListByStory() error = %v, want nil", err)
	}
	if len(versions) != 3 {
		t.Fatalf("len(versions) = %d, want 3 (the root and two children)", len(versions))
	}
	for _, version := range versions {
		if version.StoryID != storyA.ID {
			t.Errorf("version %s belongs to story %s, want only %s", version.ID, version.StoryID, storyA.ID)
		}
	}
	if versions[0].ID != storyA.RootVersionID {
		t.Errorf("first version = %s, want the root %s (oldest first)", versions[0].ID, storyA.RootVersionID)
	}
}

func TestPostgresStoreListByStoryUnknownStory(t *testing.T) {
	env := integrationSetup(t)

	_, err := env.store.ListByStory(context.Background(), "99999999-9999-4999-8999-999999999999")
	if !errors.Is(err, ErrNotFound) {
		t.Errorf("errors.Is(err, ErrNotFound) = false, want true (err = %v)", err)
	}
}

func TestPostgresStoreListByStoryMalformedId(t *testing.T) {
	env := integrationSetup(t)

	_, err := env.store.ListByStory(context.Background(), "not-a-uuid")
	if !errors.Is(err, ErrNotFound) {
		t.Errorf("errors.Is(err, ErrNotFound) = false, want true (err = %v)", err)
	}
}

func TestPostgresStoreGetVersionUnknownAndMalformedIds(t *testing.T) {
	env := integrationSetup(t)

	for _, id := range []string{"99999999-9999-4999-8999-999999999999", "not-a-uuid"} {
		_, err := env.store.GetVersion(context.Background(), id)
		if !errors.Is(err, ErrNotFound) {
			t.Errorf("GetVersion(%q) error = %v, want ErrNotFound", id, err)
		}
	}
}

func TestPostgresStoreCreateVersionRejectsUnknownStory(t *testing.T) {
	env := integrationSetup(t)

	_, err := env.store.CreateVersion(context.Background(), StoryVersion{
		StoryID:  "99999999-9999-4999-8999-999999999999",
		AuthorID: env.author,
		Language: "en",
		Title:    "Orphan",
		Body:     "No such story.",
	})
	if err == nil {
		t.Fatal("CreateVersion() error = nil, want the story foreign key to reject an unknown story")
	}
}

func TestPostgresStoreCreateVersionRejectsUnknownParent(t *testing.T) {
	env := integrationSetup(t)
	ctx := context.Background()

	story := newIntegrationStory(t, env, "bad-parent")

	_, err := env.store.CreateVersion(ctx, StoryVersion{
		StoryID:         story.ID,
		ParentVersionID: "99999999-9999-4999-8999-999999999999",
		AuthorID:        env.author,
		Language:        "en",
		Title:           "Dangling",
		Body:            "No such parent.",
	})
	if err == nil {
		t.Fatal("CreateVersion() error = nil, want the parent foreign key to reject an unknown parent")
	}

	if got := countVersions(t, env, story.ID); got != 1 {
		t.Errorf("story has %d versions, want only its one root — the failed insert must not persist", got)
	}
}

func TestPostgresStoreRejectsNilPool(t *testing.T) {
	if _, err := NewPostgresStore(nil); err == nil {
		t.Error("NewPostgresStore(nil) error = nil, want an error")
	}
}
