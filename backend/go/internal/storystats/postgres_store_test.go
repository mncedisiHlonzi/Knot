package storystats

import (
	"context"
	"fmt"
	"os"
	"strings"
	"testing"
	"time"

	"github.com/jackc/pgx/v5/pgxpool"
)

// integrationEnv is what the integration tests work with: the store under test,
// the pool, and one author the fixtures are attributed to.
type integrationEnv struct {
	store  *PostgresStore
	pool   *pgxpool.Pool
	prefix string
	author string
}

// integrationSetup connects to the database named by KNOT_POSTGRES_DSN and
// creates an author to attribute fixtures to.
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

	for _, table := range []string{"public.users", "public.stories", "public.story_versions", "public.comments", "public.story_media"} {
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

	prefix := fmt.Sprintf("knot-it-storystats-%d-", time.Now().UnixNano())

	var author string
	if err := pool.QueryRow(
		ctx,
		`INSERT INTO users (email, password_hash, display_name, preferred_languages)
		 VALUES ($1, $2, $3, $4)
		 RETURNING id`,
		prefix+"author@example.test",
		"$argon2id$v=19$m=65536,t=1,p=4$c2FsdA$a2V5",
		"Story Stats Author",
		[]string{"eng"},
	).Scan(&author); err != nil {
		pool.Close()
		t.Fatalf("could not create the integration author: %v", err)
	}

	t.Cleanup(func() {
		cleanupCtx, cancelCleanup := context.WithTimeout(context.Background(), 10*time.Second)
		defer cancelCleanup()

		// Deleting the author cascades to their stories, versions, comments, and
		// media, so nothing the fixtures created outlives the test.
		if _, err := pool.Exec(cleanupCtx, "DELETE FROM users WHERE email LIKE $1", prefix+"%"); err != nil {
			t.Logf("cleanup: could not delete test users: %v", err)
		}
		pool.Close()
	})

	return integrationEnv{store: store, pool: pool, prefix: prefix, author: author}
}

// createStoryWithRootVersion publishes a story and its root version in language,
// returning the story id. The two rows reference each other, so one statement with
// two data-modifying CTEs writes them together.
func createStoryWithRootVersion(t *testing.T, env integrationEnv, language string) string {
	t.Helper()

	var storyID string
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
		SELECT id FROM new_story`,
		env.author,
		language,
	).Scan(&storyID); err != nil {
		t.Fatalf("could not create the integration story: %v", err)
	}

	return storyID
}

// rootVersionID returns the story's root version id.
func rootVersionID(t *testing.T, env integrationEnv, storyID string) string {
	t.Helper()

	var id string
	if err := env.pool.QueryRow(
		context.Background(),
		`SELECT id FROM story_versions WHERE story_id = $1 AND parent_version_id IS NULL`,
		storyID,
	).Scan(&id); err != nil {
		t.Fatalf("could not read the root version: %v", err)
	}

	return id
}

// addVersion adapts a story into another language, returning the new version id.
func addVersion(t *testing.T, env integrationEnv, storyID, language string) string {
	t.Helper()

	var id string
	if err := env.pool.QueryRow(
		context.Background(),
		`INSERT INTO story_versions (story_id, parent_version_id, author_id, language, title, body)
		 SELECT $1, root_version_id, $2, $3, 'Adaptation', 'Adapted body'
		 FROM stories WHERE id = $1
		 RETURNING id`,
		storyID,
		env.author,
		language,
	).Scan(&id); err != nil {
		t.Fatalf("could not add the integration version: %v", err)
	}

	return id
}

// addComment inserts a comment on a version, optionally as a reply, returning its id.
func addComment(t *testing.T, env integrationEnv, versionID string, parent *string) string {
	t.Helper()

	var id string
	if err := env.pool.QueryRow(
		context.Background(),
		`INSERT INTO comments (version_id, author_id, language, body, parent_comment_id)
		 VALUES ($1, $2, 'eng', 'Integration comment', $3)
		 RETURNING id`,
		versionID,
		env.author,
		parent,
	).Scan(&id); err != nil {
		t.Fatalf("could not add the integration comment: %v", err)
	}

	return id
}

// addMedia inserts count media rows for a story.
func addMedia(t *testing.T, env integrationEnv, storyID string, count int) {
	t.Helper()

	if _, err := env.pool.Exec(
		context.Background(),
		`INSERT INTO story_media (story_id, uploader_id, storage_key, media_type, mime_type, source, size_bytes, display_order)
		 SELECT $1, $2, 'story-media/' || $4 || '/' || g || '.jpg', 'image', 'image/jpeg', 'gallery', 1024, g
		 FROM generate_series(0, $3::int - 1) AS g`,
		storyID,
		env.author,
		count,
		storyID,
	); err != nil {
		t.Fatalf("could not add integration media: %v", err)
	}
}

// TestPostgresStoreCountsForStories is the brief's scenario for KNOT-036-fix5: one
// story whose ROOT version carries two comments and whose adaptation carries one
// comment plus a reply to it.
//
// **Replies are counted.** The brief asked for top-level comments only, on the
// condition that the destination agrees; it does not. `GET /versions/{id}/comments`
// returns a version's top-level comments AND their replies in one list, and the
// conversation header shows that list's length, so a reply is one of the entries the
// reader counts. Counting only top-level comments would make a card report 1 for a
// thread that says 2. The brief's own rule — match the version detail exactly —
// therefore wins, and the reply is included.
//
// So: root 2, adaptation 2 (1 comment + 1 reply), and no count ever spans versions.
func TestPostgresStoreCountsForStories(t *testing.T) {
	env := integrationSetup(t)

	storyID := createStoryWithRootVersion(t, env, "eng")
	adaptation := addVersion(t, env, storyID, "zul")

	root := rootVersionID(t, env, storyID)
	addComment(t, env, root, nil)
	addComment(t, env, root, nil)
	adaptationComment := addComment(t, env, adaptation, nil)
	addComment(t, env, adaptation, &adaptationComment)

	addMedia(t, env, storyID, 5)

	counts, err := env.store.CountsForStories(context.Background(), []string{storyID})
	if err != nil {
		t.Fatalf("CountsForStories() error = %v, want nil", err)
	}

	got := counts[storyID]
	if got.Versions != 2 || got.Languages != 2 || got.Media != 5 {
		t.Errorf(
			"versions/languages/media = %d/%d/%d, want 2/2/5",
			got.Versions, got.Languages, got.Media,
		)
	}

	wantByVersion := map[string]int{root: 2, adaptation: 2}
	if len(got.CommentCountByVersion) != len(wantByVersion) {
		t.Fatalf("comment counts = %v, want %v", got.CommentCountByVersion, wantByVersion)
	}
	for versionID, want := range wantByVersion {
		if count := got.CommentCountForVersion(versionID); count != want {
			t.Errorf("comments on version %s = %d, want %d", versionID, count, want)
		}
	}

	// The root version's thread is what a story entry reports, and the story-wide
	// total is the sum across versions.
	if got.RootComments != 2 {
		t.Errorf("root comments = %d, want 2", got.RootComments)
	}
	if got.Comments != 4 {
		t.Errorf("story-wide comments = %d, want 4 (2 root + 1 adaptation comment + 1 reply)", got.Comments)
	}
}

// TestPostgresStoreCommentCountForVersion proves the single-version read agrees
// with the batched map -- it is the read a version detail makes, so the two must
// never disagree -- and that an unknown version is zero.
func TestPostgresStoreCommentCountForVersion(t *testing.T) {
	env := integrationSetup(t)

	storyID := createStoryWithRootVersion(t, env, "eng")
	adaptation := addVersion(t, env, storyID, "zul")
	root := rootVersionID(t, env, storyID)

	addComment(t, env, root, nil)
	addComment(t, env, root, nil)
	top := addComment(t, env, adaptation, nil)
	addComment(t, env, adaptation, &top)

	counts, err := env.store.CountsForStories(context.Background(), []string{storyID})
	if err != nil {
		t.Fatalf("CountsForStories() error = %v, want nil", err)
	}

	for _, versionID := range []string{root, adaptation} {
		single, err := env.store.CommentCountForVersion(context.Background(), versionID)
		if err != nil {
			t.Fatalf("CommentCountForVersion(%s) error = %v, want nil", versionID, err)
		}
		if want := counts[storyID].CommentCountForVersion(versionID); single != want {
			t.Errorf("CommentCountForVersion(%s) = %d, want the batched %d", versionID, single, want)
		}
	}

	// The thread the reader opens is the number both reads agree on: two comments
	// on the root, one comment plus its reply on the adaptation.
	if count, err := env.store.CommentCountForVersion(context.Background(), adaptation); err != nil || count != 2 {
		t.Errorf("CommentCountForVersion(adaptation) = %d, %v, want 2 (the comment and its reply)", count, err)
	}

	unknown := "00000000-0000-4000-8000-000000000000"
	if count, err := env.store.CommentCountForVersion(context.Background(), unknown); err != nil || count != 0 {
		t.Errorf("CommentCountForVersion(unknown) = %d, %v, want 0, nil", count, err)
	}
}

// TestPostgresStoreCountsForStoriesDefaultsToZero proves a batched read over
// several ids returns each story's own counts, and that an id with no rows is
// simply absent from the map (the caller reads it as the zero Counts).
func TestPostgresStoreCountsForStoriesDefaultsToZero(t *testing.T) {
	env := integrationSetup(t)

	storyID := createStoryWithRootVersion(t, env, "fra")
	unknown := "00000000-0000-4000-8000-000000000000"

	counts, err := env.store.CountsForStories(context.Background(), []string{storyID, unknown})
	if err != nil {
		t.Fatalf("CountsForStories() error = %v, want nil", err)
	}

	got := counts[storyID]
	if got.Versions != 1 || got.Languages != 1 {
		t.Errorf("versions/languages = %d/%d, want one version in one language", got.Versions, got.Languages)
	}
	if got.RootComments != 0 || got.Comments != 0 || len(got.CommentCountByVersion) != 0 || got.Media != 0 {
		t.Errorf("counts = %+v, want no comments and no media", got)
	}
	if _, ok := counts[unknown]; ok {
		t.Errorf("counts[%s] is present, want absent for a story with no rows", unknown)
	}
}

// TestPostgresStoreCountsForStoriesEmptyInput proves an empty page is a no-op
// rather than a query with no placeholders.
func TestPostgresStoreCountsForStoriesEmptyInput(t *testing.T) {
	env := integrationSetup(t)

	counts, err := env.store.CountsForStories(context.Background(), nil)
	if err != nil {
		t.Fatalf("CountsForStories() error = %v, want nil", err)
	}
	if len(counts) != 0 {
		t.Errorf("len(counts) = %d, want 0", len(counts))
	}
}
