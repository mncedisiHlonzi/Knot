package conversations

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
	"github.com/knot/backend/internal/versions"
)

// integrationEnv is what the integration tests work with: the conversations
// store under test, a stories store and a versions store used to create the
// story versions comments attach to, the pool, and the author they share.
type integrationEnv struct {
	store        *PostgresStore
	storyStore   *stories.PostgresStore
	versionStore *versions.PostgresStore
	pool         *pgxpool.Pool
	prefix       string
	author       string
}

// integrationSetup connects to the database named by KNOT_POSTGRES_DSN and
// creates an author to attach stories and comments to.
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

	// Both the comments table (0004) and the story_versions table it references
	// (0003) must exist for these tests to run.
	for _, table := range []string{"public.story_versions", "public.comments"} {
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

	storyStore, err := stories.NewPostgresStore(pool)
	if err != nil {
		pool.Close()
		t.Fatalf("stories.NewPostgresStore() error = %v, want nil", err)
	}

	versionStore, err := versions.NewPostgresStore(pool)
	if err != nil {
		pool.Close()
		t.Fatalf("versions.NewPostgresStore() error = %v, want nil", err)
	}

	prefix := fmt.Sprintf("knot-it-conv-%d-", time.Now().UnixNano())

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

		// Deleting the author's stories cascades to versions, then to comments,
		// then to bridges. The users delete then removes the author.
		if _, err := pool.Exec(cleanupCtx, "DELETE FROM stories WHERE author_id = $1", author); err != nil {
			t.Logf("cleanup: could not delete test stories: %v", err)
		}
		if _, err := pool.Exec(cleanupCtx, "DELETE FROM users WHERE email LIKE $1", prefix+"%"); err != nil {
			t.Logf("cleanup: could not delete test users: %v", err)
		}
		pool.Close()
	})

	return integrationEnv{store: store, storyStore: storyStore, versionStore: versionStore, pool: pool, prefix: prefix, author: author}
}

// newIntegrationStory creates a story (and, through the stories store, its root
// version) and returns the story id and root version id.
func newIntegrationStory(t *testing.T, env integrationEnv, name string) (storyID, versionID string) {
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
	return story.ID, story.RootVersionID
}

// adaptToLanguage creates an adaptation of a story's root version into language
// and returns the new version id. A bridge into that language writes its target
// comment on this version.
func adaptToLanguage(t *testing.T, env integrationEnv, storyID, rootVersionID, language string) string {
	t.Helper()

	adapted, err := env.versionStore.CreateVersion(context.Background(), versions.StoryVersion{
		StoryID:         storyID,
		ParentVersionID: rootVersionID,
		AuthorID:        env.author,
		Language:        language,
		Title:           "Adaptation into " + language,
		Body:            "Adapted body for " + language + ".",
	})
	if err != nil {
		t.Fatalf("CreateVersion(%q) error = %v, want nil", language, err)
	}
	return adapted.ID
}

// countComments counts the comments of one version with an independent query.
func countComments(t *testing.T, env integrationEnv, versionID string) int {
	t.Helper()

	var total int
	if err := env.pool.QueryRow(
		context.Background(),
		"SELECT count(*) FROM comments WHERE version_id = $1",
		versionID,
	).Scan(&total); err != nil {
		t.Fatalf("could not count comments: %v", err)
	}
	return total
}

func TestPostgresStoreCreateCommentRoundTripAndGet(t *testing.T) {
	env := integrationSetup(t)
	ctx := context.Background()

	storyID, versionID := newIntegrationStory(t, env, "comment")

	created, err := env.store.CreateComment(ctx, Comment{
		VersionID: versionID,
		AuthorID:  env.author,
		Language:  "en",
		Body:      "The first rain remembers every name.",
	})
	if err != nil {
		t.Fatalf("CreateComment() error = %v, want nil", err)
	}

	if !isUUID(created.ID) {
		t.Errorf("id = %q, want a canonical uuid generated by the database", created.ID)
	}
	if created.VersionID != versionID {
		t.Errorf("version id = %q, want %q", created.VersionID, versionID)
	}
	if created.CreatedAt.IsZero() || created.UpdatedAt.IsZero() {
		t.Error("timestamps are zero, want database-generated values")
	}

	fetched, err := env.store.GetComment(ctx, created.ID)
	if err != nil {
		t.Fatalf("GetComment() error = %v, want nil", err)
	}
	if fetched.ID != created.ID || fetched.Body != created.Body || fetched.Language != created.Language {
		t.Errorf("fetched = %+v, want it to match the created comment %+v", fetched, created)
	}
	// GetComment resolves the comment's version's story, which the comment row
	// does not store, so a notification tap can open the thread without a second
	// lookup.
	if fetched.StoryID != storyID {
		t.Errorf("fetched story id = %q, want %q (the version's story)", fetched.StoryID, storyID)
	}
}

func TestPostgresStoreCreateCommentForeignKeyConstraints(t *testing.T) {
	env := integrationSetup(t)

	tests := []struct {
		name     string
		comment  Comment
		wantCode func(error) bool
	}{
		{
			name: "unknown version is not-found",
			comment: Comment{
				VersionID: "99999999-9999-4999-8999-999999999999",
				AuthorID:  env.author,
				Language:  "en",
				Body:      "Orphan comment.",
			},
			wantCode: func(err error) bool { return errors.Is(err, ErrNotFound) },
		},
		{
			name: "unknown author is rejected",
			comment: Comment{
				VersionID: "99999999-9999-4999-8999-999999999999",
				AuthorID:  "99999999-9999-4999-8999-999999999998",
				Language:  "en",
				Body:      "No such author.",
			},
			wantCode: func(err error) bool { return err != nil },
		},
	}

	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			_, err := env.store.CreateComment(context.Background(), test.comment)
			if err == nil {
				t.Fatal("CreateComment() error = nil, want a foreign key rejection")
			}
			if !test.wantCode(err) {
				t.Errorf("err = %v, want the expected classification", err)
			}
		})
	}
}

func TestPostgresStoreListCommentsPaginatesAcrossPages(t *testing.T) {
	env := integrationSetup(t)
	ctx := context.Background()

	_, versionID := newIntegrationStory(t, env, "paginate")

	const total = 7
	for i := 0; i < total; i++ {
		if _, err := env.store.CreateComment(ctx, Comment{
			VersionID: versionID,
			AuthorID:  env.author,
			Language:  "en",
			Body:      fmt.Sprintf("comment %d", i),
		}); err != nil {
			t.Fatalf("CreateComment(%d) error = %v, want nil", i, err)
		}
	}

	// The expected order is read with an independent query so the pagination
	// assertions are not circular.
	rows, err := env.pool.Query(
		ctx,
		`SELECT id FROM comments WHERE version_id = $1 ORDER BY created_at DESC, id DESC`,
		versionID,
	)
	if err != nil {
		t.Fatalf("could not read the expected order: %v", err)
	}
	defer rows.Close()

	var want []string
	for rows.Next() {
		var id string
		if err := rows.Scan(&id); err != nil {
			t.Fatalf("could not scan an id: %v", err)
		}
		want = append(want, id)
	}
	if err := rows.Err(); err != nil {
		t.Fatalf("could not read the expected order: %v", err)
	}
	if len(want) != total {
		t.Fatalf("created %d comments but found %d", total, len(want))
	}

	var (
		seen    []string
		cursor  *Cursor
		fetches int
	)

	for len(seen) < len(want) && fetches < 10 {
		fetches++

		page, next, err := env.store.ListComments(ctx, versionID, cursor, 3)
		if err != nil {
			t.Fatalf("ListComments() error = %v, want nil", err)
		}
		if len(page) > 3 {
			t.Fatalf("page %d has %d rows, want at most 3", fetches, len(page))
		}
		for _, comment := range page {
			seen = append(seen, comment.ID)
		}
		if next == nil {
			break
		}
		cursor = next
	}

	if fetches < 3 {
		t.Errorf("walked the thread in %d pages, want at least 3 for %d comments at 3 per page", fetches, total)
	}
	if len(seen) != len(want) {
		t.Fatalf("collected %d comments, want %d", len(seen), len(want))
	}
	for i := range want {
		if seen[i] != want[i] {
			t.Fatalf("collected position %d = %s, want %s (pages must tile the thread in created_at DESC, id DESC order)", i, seen[i], want[i])
		}
	}
}

func TestPostgresStoreListCommentsEmptyThreadIsNotNotFound(t *testing.T) {
	env := integrationSetup(t)

	_, versionID := newIntegrationStory(t, env, "empty-thread")

	page, next, err := env.store.ListComments(context.Background(), versionID, nil, 20)
	if err != nil {
		t.Fatalf("ListComments() error = %v, want nil for a version with no comments", err)
	}
	if len(page) != 0 {
		t.Errorf("len(page) = %d, want 0", len(page))
	}
	if next != nil {
		t.Errorf("next cursor = %+v, want nil on the last page", next)
	}
}

func TestPostgresStoreListCommentsUnknownVersionIsNotFound(t *testing.T) {
	env := integrationSetup(t)

	for _, id := range []string{"99999999-9999-4999-8999-999999999999", "not-a-uuid"} {
		_, _, err := env.store.ListComments(context.Background(), id, nil, 20)
		if !errors.Is(err, ErrNotFound) {
			t.Errorf("ListComments(%q) error = %v, want ErrNotFound", id, err)
		}
	}
}

func TestPostgresStoreCreateBridgeCommitsBothRows(t *testing.T) {
	env := integrationSetup(t)
	ctx := context.Background()

	storyID, versionID := newIntegrationStory(t, env, "bridge")
	frenchVersionID := adaptToLanguage(t, env, storyID, versionID, "fr")

	source, err := env.store.CreateComment(ctx, Comment{
		VersionID: versionID,
		AuthorID:  env.author,
		Language:  "en",
		Body:      "The first rain remembers every name.",
	})
	if err != nil {
		t.Fatalf("CreateComment(source) error = %v, want nil", err)
	}

	bridge, target, err := env.store.CreateBridge(ctx, Comment{
		VersionID: frenchVersionID,
		AuthorID:  env.author,
		Language:  "fr",
		Body:      "La première pluie se souvient de chaque nom.",
	}, source.ID, "Rendered for French-speaking listeners.")
	if err != nil {
		t.Fatalf("CreateBridge() error = %v, want nil", err)
	}

	// Both rows must be committed: the target comment and the bridge that joins it.
	if !isUUID(target.ID) {
		t.Errorf("target id = %q, want a canonical uuid", target.ID)
	}
	if bridge.TargetCommentID != target.ID {
		t.Errorf("bridge target id = %q, want the created target %q", bridge.TargetCommentID, target.ID)
	}
	if bridge.SourceCommentID != source.ID {
		t.Errorf("bridge source id = %q, want %q", bridge.SourceCommentID, source.ID)
	}
	// Each conversation gains exactly its own comment: the source stays on the
	// English version, the bridged comment lands on the French one.
	if got := countComments(t, env, versionID); got != 1 {
		t.Errorf("source version has %d comments, want 1 (the source only)", got)
	}
	if got := countComments(t, env, frenchVersionID); got != 1 {
		t.Errorf("target version has %d comments, want 1 (the bridged target)", got)
	}

	fetchedTarget, err := env.store.GetComment(ctx, target.ID)
	if err != nil {
		t.Fatalf("GetComment(target) error = %v, want nil", err)
	}
	if fetchedTarget.Language != "fr" || fetchedTarget.VersionID != frenchVersionID {
		t.Errorf("target = %+v, want a French comment on the French version", fetchedTarget)
	}
}

func TestPostgresStoreCreateBridgeRejectsSecondBridgeIntoTheSameLanguage(t *testing.T) {
	env := integrationSetup(t)
	ctx := context.Background()

	storyID, versionID := newIntegrationStory(t, env, "one-per-language")
	frenchVersionID := adaptToLanguage(t, env, storyID, versionID, "fr")

	source, err := env.store.CreateComment(ctx, Comment{
		VersionID: versionID,
		AuthorID:  env.author,
		Language:  "en",
		Body:      "The first rain remembers every name.",
	})
	if err != nil {
		t.Fatalf("CreateComment(source) error = %v, want nil", err)
	}

	target := Comment{VersionID: frenchVersionID, AuthorID: env.author, Language: "fr", Body: "Première traduction."}
	if _, _, err := env.store.CreateBridge(ctx, target, source.ID, ""); err != nil {
		t.Fatalf("first CreateBridge() error = %v, want nil", err)
	}

	before := countComments(t, env, frenchVersionID)

	// A second bridge of the same source into the same language must be refused
	// by bridges_one_per_target_language, and the transaction must roll back the
	// target comment it inserted first.
	target2 := Comment{VersionID: frenchVersionID, AuthorID: env.author, Language: "fr", Body: "Deuxième traduction."}
	if _, _, err := env.store.CreateBridge(ctx, target2, source.ID, ""); !errors.Is(err, ErrAlreadyBridged) {
		t.Fatalf("second CreateBridge() error = %v, want ErrAlreadyBridged", err)
	}

	if after := countComments(t, env, frenchVersionID); after != before {
		t.Errorf("target version has %d comments, want %d — the rolled-back target must not persist", after, before)
	}
}

func TestPostgresStoreBridgesUniquePairRejectsDuplicatePair(t *testing.T) {
	env := integrationSetup(t)
	ctx := context.Background()

	_, versionID := newIntegrationStory(t, env, "unique-pair")

	source, err := env.store.CreateComment(ctx, Comment{
		VersionID: versionID,
		AuthorID:  env.author,
		Language:  "en",
		Body:      "Source.",
	})
	if err != nil {
		t.Fatalf("CreateComment(source) error = %v, want nil", err)
	}
	target, err := env.store.CreateComment(ctx, Comment{
		VersionID: versionID,
		AuthorID:  env.author,
		Language:  "fr",
		Body:      "Cible.",
	})
	if err != nil {
		t.Fatalf("CreateComment(target) error = %v, want nil", err)
	}

	if _, err := env.pool.Exec(
		ctx,
		`INSERT INTO bridges (source_comment_id, target_comment_id, author_id, target_language)
		 VALUES ($1, $2, $3, 'fr')`,
		source.ID, target.ID, env.author,
	); err != nil {
		t.Fatalf("could not insert the first bridge: %v", err)
	}

	// The same (source, target) pair must be refused by bridges_unique_pair.
	// CreateBridge cannot produce this case — it always makes a fresh target —
	// so the constraint is exercised directly.
	if _, err := env.pool.Exec(
		ctx,
		`INSERT INTO bridges (source_comment_id, target_comment_id, author_id, target_language)
		 VALUES ($1, $2, $3, 'de')`,
		source.ID, target.ID, env.author,
	); err == nil {
		t.Fatal("inserting a duplicate (source, target) pair succeeded, want bridges_unique_pair to reject it")
	}
}

func TestPostgresStoreFindTargetVersion(t *testing.T) {
	env := integrationSetup(t)
	ctx := context.Background()

	storyID, rootVersionID := newIntegrationStory(t, env, "find-target")
	frenchVersionID := adaptToLanguage(t, env, storyID, rootVersionID, "fr")

	found, err := env.store.FindTargetVersion(ctx, rootVersionID, "fr")
	if err != nil {
		t.Fatalf("FindTargetVersion() error = %v, want nil", err)
	}
	if found != frenchVersionID {
		t.Errorf("found = %q, want the French version %q", found, frenchVersionID)
	}

	tests := []struct {
		name            string
		sourceVersionID string
		targetLanguage  string
	}{
		{name: "no version in the language", sourceVersionID: rootVersionID, targetLanguage: "de"},
		{name: "malformed source version", sourceVersionID: "not-a-uuid", targetLanguage: "fr"},
		{name: "unknown source version", sourceVersionID: "99999999-9999-4999-8999-999999999999", targetLanguage: "fr"},
	}

	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			if _, err := env.store.FindTargetVersion(ctx, test.sourceVersionID, test.targetLanguage); !errors.Is(err, ErrNotFound) {
				t.Errorf("FindTargetVersion() error = %v, want ErrNotFound", err)
			}
		})
	}
}

func TestPostgresStoreFindTargetVersionIsDeterministicWhenAmbiguous(t *testing.T) {
	// A story may hold several versions in one language. The choice must not
	// depend on physical row order, so the same call must answer the same way.
	env := integrationSetup(t)
	ctx := context.Background()

	storyID, rootVersionID := newIntegrationStory(t, env, "ambiguous-target")
	first := adaptToLanguage(t, env, storyID, rootVersionID, "fr")
	second := adaptToLanguage(t, env, storyID, rootVersionID, "fr")

	got, err := env.store.FindTargetVersion(ctx, rootVersionID, "fr")
	if err != nil {
		t.Fatalf("FindTargetVersion() error = %v, want nil", err)
	}
	again, err := env.store.FindTargetVersion(ctx, rootVersionID, "fr")
	if err != nil {
		t.Fatalf("FindTargetVersion() error = %v, want nil", err)
	}

	if got != again {
		t.Errorf("FindTargetVersion() returned %q then %q, want a deterministic choice", got, again)
	}
	if got != first && got != second {
		t.Errorf("found = %q, want one of the two French versions %q or %q", got, first, second)
	}
}

func TestPostgresStoreGetBridgeRoundTrip(t *testing.T) {
	env := integrationSetup(t)
	ctx := context.Background()

	storyID, versionID := newIntegrationStory(t, env, "get-bridge")
	frenchVersionID := adaptToLanguage(t, env, storyID, versionID, "fr")

	source, err := env.store.CreateComment(ctx, Comment{
		VersionID: versionID, AuthorID: env.author, Language: "en", Body: "Source.",
	})
	if err != nil {
		t.Fatalf("CreateComment() error = %v, want nil", err)
	}

	created, _, err := env.store.CreateBridge(ctx, Comment{
		VersionID: frenchVersionID, AuthorID: env.author, Language: "fr", Body: "Cible.",
	}, source.ID, "note")
	if err != nil {
		t.Fatalf("CreateBridge() error = %v, want nil", err)
	}

	fetched, err := env.store.GetBridge(ctx, created.ID)
	if err != nil {
		t.Fatalf("GetBridge() error = %v, want nil", err)
	}
	if fetched.ID != created.ID || fetched.TargetLanguage != "fr" || fetched.AdaptationNote != "note" {
		t.Errorf("fetched = %+v, want it to match the created bridge %+v", fetched, created)
	}

	for _, id := range []string{"99999999-9999-4999-8999-999999999999", "not-a-uuid"} {
		if _, err := env.store.GetBridge(ctx, id); !errors.Is(err, ErrNotFound) {
			t.Errorf("GetBridge(%q) error = %v, want ErrNotFound", id, err)
		}
	}
}

func TestPostgresStoreListBridgesForCommentSeesBothEnds(t *testing.T) {
	env := integrationSetup(t)
	ctx := context.Background()

	storyID, versionID := newIntegrationStory(t, env, "bridges-for-comment")
	frenchVersionID := adaptToLanguage(t, env, storyID, versionID, "fr")

	source, err := env.store.CreateComment(ctx, Comment{
		VersionID: versionID, AuthorID: env.author, Language: "en", Body: "Source.",
	})
	if err != nil {
		t.Fatalf("CreateComment() error = %v, want nil", err)
	}

	created, target, err := env.store.CreateBridge(ctx, Comment{
		VersionID: frenchVersionID, AuthorID: env.author, Language: "fr", Body: "Cible.",
	}, source.ID, "")
	if err != nil {
		t.Fatalf("CreateBridge() error = %v, want nil", err)
	}

	for _, commentID := range []string{source.ID, target.ID} {
		bridges, err := env.store.ListBridgesForComment(ctx, commentID)
		if err != nil {
			t.Fatalf("ListBridgesForComment(%s) error = %v, want nil", commentID, err)
		}
		if len(bridges) != 1 || bridges[0].ID != created.ID {
			t.Errorf("ListBridgesForComment(%s) = %+v, want the one bridge %s", commentID, bridges, created.ID)
		}
	}

	if _, err := env.store.ListBridgesForComment(ctx, "99999999-9999-4999-8999-999999999999"); !errors.Is(err, ErrNotFound) {
		t.Errorf("ListBridgesForComment(unknown) error = %v, want ErrNotFound", err)
	}
}

func TestPostgresStoreListBridgesForStory(t *testing.T) {
	env := integrationSetup(t)
	ctx := context.Background()

	storyID, versionID := newIntegrationStory(t, env, "bridges-for-story")
	frenchVersionID := adaptToLanguage(t, env, storyID, versionID, "fr")
	otherStoryID, _ := newIntegrationStory(t, env, "other-story")

	source, err := env.store.CreateComment(ctx, Comment{
		VersionID: versionID, AuthorID: env.author, Language: "en", Body: "Source.",
	})
	if err != nil {
		t.Fatalf("CreateComment() error = %v, want nil", err)
	}
	created, _, err := env.store.CreateBridge(ctx, Comment{
		VersionID: frenchVersionID, AuthorID: env.author, Language: "fr", Body: "Cible.",
	}, source.ID, "")
	if err != nil {
		t.Fatalf("CreateBridge() error = %v, want nil", err)
	}

	bridges, err := env.store.ListBridgesForStory(ctx, storyID)
	if err != nil {
		t.Fatalf("ListBridgesForStory() error = %v, want nil", err)
	}
	if len(bridges) != 1 || bridges[0].ID != created.ID {
		t.Errorf("bridges = %+v, want only the bridge on this story", bridges)
	}

	// A different story must not see it.
	other, err := env.store.ListBridgesForStory(ctx, otherStoryID)
	if err != nil {
		t.Fatalf("ListBridgesForStory(other) error = %v, want nil", err)
	}
	if len(other) != 0 {
		t.Errorf("other story sees %d bridges, want 0", len(other))
	}

	if _, err := env.store.ListBridgesForStory(ctx, "99999999-9999-4999-8999-999999999999"); !errors.Is(err, ErrNotFound) {
		t.Errorf("ListBridgesForStory(unknown) error = %v, want ErrNotFound", err)
	}
}

func TestPostgresStoreRejectsNilPool(t *testing.T) {
	if _, err := NewPostgresStore(nil); err == nil {
		t.Error("NewPostgresStore(nil) error = nil, want an error")
	}
}
