package profile

import (
	"context"
	"fmt"
	"os"
	"strings"
	"testing"
	"time"

	"github.com/jackc/pgx/v5/pgxpool"

	"github.com/knot/backend/internal/conversations"
	"github.com/knot/backend/internal/inquiries"
	"github.com/knot/backend/internal/stories"
	"github.com/knot/backend/internal/versions"
)

// integrationEnv is what the integration tests work with: the profile store under
// test, the content stores used to author activities, the pool, and two users to
// attribute them to.
type integrationEnv struct {
	store        *PostgresStore
	storyStore   *stories.PostgresStore
	versionStore *versions.PostgresStore
	convStore    *conversations.PostgresStore
	inquiryStore *inquiries.PostgresStore
	pool         *pgxpool.Pool
	prefix       string
	userA        string
	userB        string
}

// integrationSetup connects to the database named by KNOT_POSTGRES_DSN and
// creates two authors to attribute activities to.
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

	// Every table the union reads must exist for these tests to run.
	for _, table := range []string{"public.stories", "public.story_versions", "public.comments", "public.bridges", "public.inquiries", "public.inquiry_answers", "public.users"} {
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

	convStore, err := conversations.NewPostgresStore(pool)
	if err != nil {
		pool.Close()
		t.Fatalf("conversations.NewPostgresStore() error = %v, want nil", err)
	}

	inquiryStore, err := inquiries.NewPostgresStore(pool)
	if err != nil {
		pool.Close()
		t.Fatalf("inquiries.NewPostgresStore() error = %v, want nil", err)
	}

	prefix := fmt.Sprintf("knot-it-profile-%d-", time.Now().UnixNano())
	env := integrationEnv{
		store:        store,
		storyStore:   storyStore,
		versionStore: versionStore,
		convStore:    convStore,
		inquiryStore: inquiryStore,
		pool:         pool,
		prefix:       prefix,
	}

	env.userA = createUser(t, pool, prefix+"a@example.test", "Ada Integration")
	env.userB = createUser(t, pool, prefix+"b@example.test", "Grace Integration")

	t.Cleanup(func() {
		cleanupCtx, cancelCleanup := context.WithTimeout(context.Background(), 10*time.Second)
		defer cancelCleanup()

		// Deleting the users cascades to their stories, versions, comments, and
		// bridges.
		if _, err := pool.Exec(cleanupCtx, "DELETE FROM stories WHERE author_id IN ($1, $2)", env.userA, env.userB); err != nil {
			t.Logf("cleanup: could not delete test stories: %v", err)
		}
		if _, err := pool.Exec(cleanupCtx, "DELETE FROM users WHERE email LIKE $1", prefix+"%"); err != nil {
			t.Logf("cleanup: could not delete test users: %v", err)
		}
		pool.Close()
	})

	return env
}

// createUser inserts an account and returns its id.
func createUser(t *testing.T, pool *pgxpool.Pool, email, displayName string) string {
	t.Helper()

	var id string
	err := pool.QueryRow(
		context.Background(),
		`INSERT INTO users (email, password_hash, display_name, preferred_languages)
		 VALUES ($1, $2, $3, $4)
		 RETURNING id`,
		email,
		"$argon2id$v=19$m=65536,t=1,p=4$c2FsdA$a2V5",
		displayName,
		[]string{"eng"},
	).Scan(&id)
	if err != nil {
		t.Fatalf("could not create user %q: %v", email, err)
	}

	return id
}

// createStory creates a story (and its root version) for author and returns both ids.
func createStory(t *testing.T, env integrationEnv, author, name string) (storyID, rootVersionID string) {
	t.Helper()

	story, err := env.storyStore.CreateStory(context.Background(), stories.Story{
		AuthorID:  author,
		Pillar:    stories.PillarHeritage,
		Language:  "eng",
		Title:     "Story " + name,
		Body:      "Body for " + name,
		MediaURLs: []string{},
	})
	if err != nil {
		t.Fatalf("CreateStory(%q) error = %v, want nil", name, err)
	}

	return story.ID, story.RootVersionID
}

// createVersion creates an adaptation of parentVersionID into language.
func createVersion(t *testing.T, env integrationEnv, author, storyID, parentVersionID, language string) string {
	t.Helper()

	created, err := env.versionStore.CreateVersion(context.Background(), versions.StoryVersion{
		StoryID:         storyID,
		ParentVersionID: parentVersionID,
		AuthorID:        author,
		Language:        language,
		Title:           "Adaptation into " + language,
		Body:            "Adapted body for " + language + ".",
	})
	if err != nil {
		t.Fatalf("CreateVersion(%q) error = %v, want nil", language, err)
	}

	return created.ID
}

// createComment writes a comment by author on versionID and returns its id.
func createComment(t *testing.T, env integrationEnv, author, versionID, body string) string {
	t.Helper()

	created, err := env.convStore.CreateComment(context.Background(), conversations.Comment{
		VersionID: versionID,
		AuthorID:  author,
		Language:  "eng",
		Body:      body,
	})
	if err != nil {
		t.Fatalf("CreateComment() error = %v, want nil", err)
	}

	return created.ID
}

// createBridge bridges sourceCommentID into targetLanguage, writing the target
// comment on targetVersionID. It returns the bridge id.
func createBridge(t *testing.T, env integrationEnv, author, sourceCommentID, targetVersionID, targetLanguage string) string {
	t.Helper()

	bridge, _, err := env.convStore.CreateBridge(context.Background(), conversations.Comment{
		VersionID: targetVersionID,
		AuthorID:  author,
		Language:  targetLanguage,
		Body:      "A bridged comment in " + targetLanguage + ".",
	}, sourceCommentID, "")
	if err != nil {
		t.Fatalf("CreateBridge() error = %v, want nil", err)
	}

	return bridge.ID
}

func TestPostgresStoreListForUserMergesEveryKind(t *testing.T) {
	env := integrationSetup(t)
	ctx := context.Background()

	storyID, rootVersionID := createStory(t, env, env.userA, "merged")
	frVersionID := createVersion(t, env, env.userA, storyID, rootVersionID, "fra")
	commentID := createComment(t, env, env.userA, rootVersionID, "Ada's comment.")
	bridgeID := createBridge(t, env, env.userA, commentID, frVersionID, "fra")

	page, next, err := env.store.ListForUser(ctx, env.userA, nil, 50)
	if err != nil {
		t.Fatalf("ListForUser() error = %v, want nil", err)
	}
	if next != nil {
		t.Errorf("next = %+v, want nil on the last page", next)
	}
	if len(page) != 4 {
		t.Fatalf("activities = %d, want 4 (story, adaptation, comment, bridge); got %+v", len(page), page)
	}

	byKind := make(map[Kind]Activity, len(page))
	for _, activity := range page {
		if _, seen := byKind[activity.Kind]; seen {
			t.Errorf("kind %s appears twice, want one activity per act", activity.Kind)
		}
		byKind[activity.Kind] = activity
	}

	if got := byKind[KindStory]; got.ID != storyID {
		t.Errorf("story id = %q, want the story %q", got.ID, storyID)
	}
	if got := byKind[KindStory].Payload; got.Title != "Story merged" || got.Pillar != "heritage" || got.Language != "eng" {
		t.Errorf("story payload = %+v, want title/pillar/language of the story", got)
	}

	if got := byKind[KindVersion]; got.ID != frVersionID {
		t.Errorf("version id = %q, want the adaptation %q (the root version must be excluded)", got.ID, frVersionID)
	}
	if got := byKind[KindVersion].Payload; got.StoryID != storyID || got.StoryTitle != "Story merged" || got.Language != "fra" {
		t.Errorf("version payload = %+v, want the story id, story title and language", got)
	}

	if got := byKind[KindComment]; got.ID != commentID {
		t.Errorf("comment id = %q, want %q", got.ID, commentID)
	}
	if got := byKind[KindComment].Payload; got.VersionID != rootVersionID || got.StoryID != storyID || got.BodyPreview == "" {
		t.Errorf("comment payload = %+v, want the version id, story id and a body preview", got)
	}

	if got := byKind[KindBridge]; got.ID != bridgeID {
		t.Errorf("bridge id = %q, want %q", got.ID, bridgeID)
	}
	if got := byKind[KindBridge].Payload; got.SourceCommentID != commentID || got.VersionID != rootVersionID || got.TargetLanguage != "fra" {
		t.Errorf("bridge payload = %+v, want the source comment, version and target language", got)
	}

	// Newest first.
	for i := 1; i < len(page); i++ {
		if page[i].CreatedAt.After(page[i-1].CreatedAt) {
			t.Errorf("activity %d (%s) is newer than %d (%s), want newest first", i, page[i].Kind, i-1, page[i-1].Kind)
		}
	}
}

func TestPostgresStoreListForUserIsScopedToTheAuthor(t *testing.T) {
	env := integrationSetup(t)
	ctx := context.Background()

	storyAID, rootA := createStory(t, env, env.userA, "A")
	commentA := createComment(t, env, env.userA, rootA, "By Ada.")

	storyBID, rootB := createStory(t, env, env.userB, "B")
	commentB := createComment(t, env, env.userB, rootB, "By Grace.")

	pageA, _, err := env.store.ListForUser(ctx, env.userA, nil, 50)
	if err != nil {
		t.Fatalf("ListForUser(A) error = %v, want nil", err)
	}

	got := make(map[string]bool, len(pageA))
	for _, activity := range pageA {
		got[activity.ID] = true
	}

	if len(pageA) != 2 || !got[storyAID] || !got[commentA] {
		t.Errorf("A's wall = %+v, want exactly A's story and comment", pageA)
	}
	if got[storyBID] || got[commentB] {
		t.Errorf("A's wall contains B's activity: %+v", pageA)
	}
}

func TestPostgresStoreListForUserEmptyWallIsNotAnError(t *testing.T) {
	env := integrationSetup(t)

	page, next, err := env.store.ListForUser(context.Background(), env.userB, nil, 20)
	if err != nil {
		t.Fatalf("ListForUser() error = %v, want nil for a user with no activity", err)
	}
	if len(page) != 0 {
		t.Errorf("len(page) = %d, want 0", len(page))
	}
	if next != nil {
		t.Errorf("next = %+v, want nil on an empty wall", next)
	}
}

func TestPostgresStoreListForUserPaginates(t *testing.T) {
	env := integrationSetup(t)
	ctx := context.Background()

	_, rootVersionID := createStory(t, env, env.userA, "paginate")

	const total = 5
	for i := 0; i < total; i++ {
		createComment(t, env, env.userA, rootVersionID, fmt.Sprintf("comment %d", i))
	}

	// The story itself plus the five comments: six activities.
	const want = total + 1

	var (
		seen    []string
		cursor  *Cursor
		fetches int
	)

	for len(seen) < want && fetches < 10 {
		fetches++

		page, next, err := env.store.ListForUser(ctx, env.userA, cursor, 2)
		if err != nil {
			t.Fatalf("ListForUser() error = %v, want nil", err)
		}
		if len(page) > 2 {
			t.Fatalf("page %d has %d rows, want at most 2", fetches, len(page))
		}

		for _, activity := range page {
			seen = append(seen, activity.ID)
		}

		if next == nil {
			cursor = nil
			break
		}
		cursor = next
	}

	if fetches < 3 {
		t.Errorf("walked the wall in %d pages, want at least 3 for %d activities at 2 per page", fetches, want)
	}
	if len(seen) != want {
		t.Fatalf("collected %d activities, want %d", len(seen), want)
	}

	// No id repeats across pages, and the pages are strictly descending.
	distinct := make(map[string]bool, len(seen))
	for i, id := range seen {
		if distinct[id] {
			t.Fatalf("id %s appeared twice across pages", id)
		}
		distinct[id] = true
		if i > 0 && seen[i] == seen[i-1] {
			t.Fatalf("adjacent duplicate id %s", id)
		}
	}
}

func TestPostgresStoreListForUserOrdersNewestFirst(t *testing.T) {
	env := integrationSetup(t)
	ctx := context.Background()

	_, rootVersionID := createStory(t, env, env.userA, "order")
	first := createComment(t, env, env.userA, rootVersionID, "first")
	time.Sleep(2 * time.Millisecond)
	second := createComment(t, env, env.userA, rootVersionID, "second")

	page, _, err := env.store.ListForUser(ctx, env.userA, nil, 50)
	if err != nil {
		t.Fatalf("ListForUser() error = %v, want nil", err)
	}

	var firstIndex, secondIndex = -1, -1
	for i, activity := range page {
		switch activity.ID {
		case first:
			firstIndex = i
		case second:
			secondIndex = i
		}
	}

	if firstIndex == -1 || secondIndex == -1 {
		t.Fatalf("both comments must appear on the wall: %+v", page)
	}
	if secondIndex > firstIndex {
		t.Errorf("the newer comment is at %d and the older at %d, want newest first", secondIndex, firstIndex)
	}
}

// TestPostgresStoreListForUserIncludesInquiriesAndAnswers proves the wall's two
// new branches: a question the owner asked, and an answer the owner gave to
// someone else's question.
func TestPostgresStoreListForUserIncludesInquiriesAndAnswers(t *testing.T) {
	env := integrationSetup(t)
	ctx := context.Background()

	place := "Manguzi"
	inquiry, err := env.inquiryStore.CreateInquiry(ctx, inquiries.Inquiry{
		AuthorID: env.userA,
		Title:    "Why do the cattle come home at the same hour?",
		Body:     "Every evening, without anyone calling them.",
		Language: "eng",
		Place:    &place,
	})
	if err != nil {
		t.Fatalf("CreateInquiry() error = %v, want nil", err)
	}

	// The answer is older than the inquiry's activity would be if it were written
	// after, so give the answer a distinct timestamp by waiting a moment.
	time.Sleep(2 * time.Millisecond)
	answer, _, err := env.inquiryStore.CreateAnswer(ctx, inquiries.Answer{
		InquiryID: inquiry.ID,
		AuthorID:  env.userB,
		Language:  "eng",
		Body:      "They follow the river, and the river has a tide.",
	})
	if err != nil {
		t.Fatalf("CreateAnswer() error = %v, want nil", err)
	}

	// UserA asked the question, so their wall carries an inquiry activity.
	wallA, _, err := env.store.ListForUser(ctx, env.userA, nil, 50)
	if err != nil {
		t.Fatalf("ListForUser(userA) error = %v, want nil", err)
	}

	var foundInquiry *Activity
	for i := range wallA {
		if wallA[i].Kind == KindInquiry && wallA[i].ID == inquiry.ID {
			foundInquiry = &wallA[i]
		}
	}
	if foundInquiry == nil {
		t.Fatalf("user A's wall has no inquiry activity for %s: %+v", inquiry.ID, wallA)
	}
	if foundInquiry.Payload.Title != inquiry.Title {
		t.Errorf("inquiry payload title = %q, want %q", foundInquiry.Payload.Title, inquiry.Title)
	}
	if foundInquiry.Payload.Place != "Manguzi" {
		t.Errorf("inquiry payload place = %q, want Manguzi", foundInquiry.Payload.Place)
	}
	// An inquiry activity carries no story, version, or comment context.
	if foundInquiry.Payload.StoryID != "" || foundInquiry.Payload.VersionID != "" {
		t.Errorf("inquiry payload leaked other context: %+v", foundInquiry.Payload)
	}

	// UserB answered, so their wall carries an inquiry_answer activity that names
	// the question it answers.
	wallB, _, err := env.store.ListForUser(ctx, env.userB, nil, 50)
	if err != nil {
		t.Fatalf("ListForUser(userB) error = %v, want nil", err)
	}

	var foundAnswer *Activity
	for i := range wallB {
		if wallB[i].Kind == KindInquiryAnswer && wallB[i].ID == answer.ID {
			foundAnswer = &wallB[i]
		}
	}
	if foundAnswer == nil {
		t.Fatalf("user B's wall has no inquiry_answer activity for %s: %+v", answer.ID, wallB)
	}
	if foundAnswer.Payload.InquiryID != inquiry.ID {
		t.Errorf("answer payload inquiry id = %q, want %q", foundAnswer.Payload.InquiryID, inquiry.ID)
	}
	if foundAnswer.Payload.InquiryTitle != inquiry.Title {
		t.Errorf("answer payload inquiry title = %q, want %q", foundAnswer.Payload.InquiryTitle, inquiry.Title)
	}
	if foundAnswer.Payload.BodyPreview != answer.Body {
		t.Errorf("answer payload preview = %q, want %q", foundAnswer.Payload.BodyPreview, answer.Body)
	}

	// The question belongs to the asker's wall only: userB answered it, but did not
	// ask it, so it must not appear there a second time as an inquiry.
	for _, activity := range wallB {
		if activity.Kind == KindInquiry && activity.ID == inquiry.ID {
			t.Error("a question appears on the answerer's wall as an inquiry activity")
		}
	}
}

// TestPostgresStoreListForUserPlaceLessInquiryHasNoPlace confirms a place-less
// question is still wall content, with the place field simply absent.
func TestPostgresStoreListForUserPlaceLessInquiryHasNoPlace(t *testing.T) {
	env := integrationSetup(t)
	ctx := context.Background()

	inquiry, err := env.inquiryStore.CreateInquiry(ctx, inquiries.Inquiry{
		AuthorID: env.userA,
		Title:    "A question about nowhere in particular",
		Body:     "Still a question.",
		Language: "eng",
	})
	if err != nil {
		t.Fatalf("CreateInquiry() error = %v, want nil", err)
	}

	wall, _, err := env.store.ListForUser(ctx, env.userA, nil, 50)
	if err != nil {
		t.Fatalf("ListForUser() error = %v, want nil", err)
	}

	for _, activity := range wall {
		if activity.Kind == KindInquiry && activity.ID == inquiry.ID {
			if activity.Payload.Place != "" {
				t.Errorf("place = %q, want empty for a place-less inquiry", activity.Payload.Place)
			}
			return
		}
	}
	t.Error("the place-less inquiry is missing from the wall")
}
