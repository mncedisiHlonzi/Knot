package inquiries

import (
	"context"
	"errors"
	"fmt"
	"os"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/jackc/pgx/v5/pgxpool"
)

// integrationSetup connects to the database named by KNOT_POSTGRES_DSN.
//
// The test is skipped, not failed, when KNOT_POSTGRES_DSN is unset, when the
// database is unreachable, or when the inquiries table has not been migrated yet —
// none of those is a defect in the code under test.
//
// It returns the store, the pool, and a per-run email prefix that cleanup uses to
// delete exactly the users this test created. Deleting a user cascades to the
// inquiries and answers it authored, so every row this test wrote goes with them.
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
	if err := pool.QueryRow(ctx, "SELECT to_regclass('public.inquiries') IS NOT NULL").Scan(&migrated); err != nil {
		pool.Close()
		t.Skipf("could not inspect the schema: %v", err)
	}
	if !migrated {
		pool.Close()
		t.Skip("the inquiries table does not exist; run `go run ./cmd/knot migrate up` first")
	}

	store, err := NewPostgresStore(pool)
	if err != nil {
		pool.Close()
		t.Fatalf("NewPostgresStore() error = %v, want nil", err)
	}

	prefix := fmt.Sprintf("knot-inquiries-it-%d-", time.Now().UnixNano())

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

// createUser inserts a user directly and returns its id. An inquiry references
// users(id), so a test user has to exist before one can be written.
func createUser(t *testing.T, pool *pgxpool.Pool, prefix, name string) string {
	t.Helper()

	var id string
	err := pool.QueryRow(
		context.Background(),
		`INSERT INTO users (email, password_hash, display_name) VALUES ($1, $2, $3) RETURNING id`,
		prefix+name+"@example.test",
		"$argon2id$v=19$m=65536,t=1,p=4$c2FsdA$a2V5",
		"Inquiries "+name,
	).Scan(&id)
	if err != nil {
		t.Fatalf("createUser() error = %v, want nil", err)
	}

	return id
}

// seedInquiry inserts an inquiry directly with an explicit created_at, so an
// ordering test is deterministic rather than depending on how close together two
// inserts happened to land.
func seedInquiry(t *testing.T, pool *pgxpool.Pool, authorID, title, place string, age time.Duration) string {
	t.Helper()

	var id string
	var placeArg *string
	if place != "" {
		placeArg = &place
	}

	err := pool.QueryRow(
		context.Background(),
		`INSERT INTO inquiries (author_id, title, body, language, place, created_at, updated_at)
		 VALUES ($1, $2, $3, 'eng', $4, now() - $5::interval, now() - $5::interval)
		 RETURNING id`,
		authorID,
		title,
		"body of "+title,
		placeArg,
		age.String(),
	).Scan(&id)
	if err != nil {
		t.Fatalf("seedInquiry() error = %v, want nil", err)
	}

	return id
}

func TestPostgresStoreCreateInquiryRoundTrips(t *testing.T) {
	store, pool, prefix := integrationSetup(t)
	ctx := context.Background()
	authorID := createUser(t, pool, prefix, "author")

	place := "Manguzi"
	latitude, longitude := -26.9, 32.7
	country := "South Africa"

	created, err := store.CreateInquiry(ctx, Inquiry{
		AuthorID:     authorID,
		Title:        "Why do the cattle come home at the same hour?",
		Body:         "Every evening, without anyone calling them.",
		Language:     "eng",
		Place:        &place,
		PlaceCountry: &country,
		Latitude:     &latitude,
		Longitude:    &longitude,
	})
	if err != nil {
		t.Fatalf("CreateInquiry() error = %v, want nil", err)
	}

	if created.ID == "" {
		t.Error("ID is empty, want the id PostgreSQL assigned")
	}
	if created.AuthorID != authorID {
		t.Errorf("AuthorID = %q, want %q", created.AuthorID, authorID)
	}
	// A brand-new inquiry has no answers, and the default is what makes that true
	// without the caller saying so.
	if created.AnswerCount != 0 {
		t.Errorf("AnswerCount = %d, want 0", created.AnswerCount)
	}
	if created.CreatedAt.IsZero() || created.UpdatedAt.IsZero() {
		t.Error("timestamps are zero, want them set by PostgreSQL")
	}
	if created.Place == nil || *created.Place != place {
		t.Errorf("Place = %v, want %q", created.Place, place)
	}
	if created.PlaceCountry == nil || *created.PlaceCountry != country {
		t.Errorf("PlaceCountry = %v, want %q", created.PlaceCountry, country)
	}
	if created.Latitude == nil || *created.Latitude != latitude {
		t.Errorf("Latitude = %v, want %v", created.Latitude, latitude)
	}

	fetched, err := store.GetInquiry(ctx, created.ID)
	if err != nil {
		t.Fatalf("GetInquiry() error = %v, want nil", err)
	}
	if fetched.ID != created.ID || fetched.Title != created.Title || fetched.Body != created.Body {
		t.Errorf("fetched inquiry = %+v, want it to match the created one", fetched)
	}
}

func TestPostgresStoreCreateInquiryWithoutPlace(t *testing.T) {
	store, pool, prefix := integrationSetup(t)
	ctx := context.Background()
	authorID := createUser(t, pool, prefix, "author")

	created, err := store.CreateInquiry(ctx, Inquiry{
		AuthorID: authorID,
		Title:    "A question about nowhere in particular",
		Body:     "Still a question.",
		Language: "eng",
	})
	if err != nil {
		t.Fatalf("CreateInquiry() error = %v, want nil", err)
	}
	if created.Place != nil {
		t.Errorf("Place = %v, want nil", created.Place)
	}
	if created.Latitude != nil || created.Longitude != nil {
		t.Errorf("coordinates = %v/%v, want nil/nil", created.Latitude, created.Longitude)
	}
}

func TestPostgresStoreCreateInquiryUnknownAuthor(t *testing.T) {
	store, _, _ := integrationSetup(t)

	_, err := store.CreateInquiry(context.Background(), Inquiry{
		AuthorID: "99999999-9999-4999-8999-999999999999",
		Title:    "x",
		Body:     "y",
		Language: "eng",
	})
	if !errors.Is(err, ErrUserNotFound) {
		t.Fatalf("CreateInquiry() error = %v, want ErrUserNotFound", err)
	}
}

func TestPostgresStoreGetInquiryNotFound(t *testing.T) {
	store, _, _ := integrationSetup(t)
	ctx := context.Background()

	if _, err := store.GetInquiry(ctx, "99999999-9999-4999-8999-999999999999"); !errors.Is(err, ErrNotFound) {
		t.Errorf("unknown id error = %v, want ErrNotFound", err)
	}
	if _, err := store.GetInquiry(ctx, "not-a-uuid"); !errors.Is(err, ErrNotFound) {
		t.Errorf("malformed id error = %v, want ErrNotFound", err)
	}
}

func TestPostgresStoreListInquiriesIsNewestFirstAndPaginates(t *testing.T) {
	store, pool, prefix := integrationSetup(t)
	ctx := context.Background()
	authorID := createUser(t, pool, prefix, "author")

	oldest := seedInquiry(t, pool, authorID, "oldest", "Manguzi", 3*time.Hour)
	middle := seedInquiry(t, pool, authorID, "middle", "Manguzi", 2*time.Hour)
	newest := seedInquiry(t, pool, authorID, "newest", "Manguzi", 1*time.Hour)

	firstPage, next, err := store.ListInquiries(ctx, nil, 2, "")
	if err != nil {
		t.Fatalf("ListInquiries() error = %v, want nil", err)
	}
	if len(firstPage) != 2 {
		t.Fatalf("first page length = %d, want 2", len(firstPage))
	}
	if firstPage[0].ID != newest || firstPage[1].ID != middle {
		t.Errorf("first page = %q, %q; want newest then middle", firstPage[0].Title, firstPage[1].Title)
	}
	if next == nil {
		t.Fatal("next cursor = nil, want one because a third inquiry remains")
	}

	secondPage, next, err := store.ListInquiries(ctx, next, 2, "")
	if err != nil {
		t.Fatalf("ListInquiries(page 2) error = %v, want nil", err)
	}
	if len(secondPage) != 1 {
		t.Fatalf("second page length = %d, want 1", len(secondPage))
	}
	if secondPage[0].ID != oldest {
		t.Errorf("second page = %q, want oldest", secondPage[0].Title)
	}
	if next != nil {
		t.Errorf("next cursor = %v, want nil at the end", next)
	}

	// Every row appears exactly once across the two pages.
	seen := map[string]bool{firstPage[0].ID: true, firstPage[1].ID: true, secondPage[0].ID: true}
	if len(seen) != 3 {
		t.Errorf("distinct ids across pages = %d, want 3 (a cursor must not repeat or skip a row)", len(seen))
	}
}

func TestPostgresStoreListInquiriesFiltersByPlace(t *testing.T) {
	store, pool, prefix := integrationSetup(t)
	ctx := context.Background()
	authorID := createUser(t, pool, prefix, "author")

	seedInquiry(t, pool, authorID, "manguzi one", "Manguzi", time.Hour)
	manguzi := seedInquiry(t, pool, authorID, "manguzi two", "Manguzi", 2*time.Hour)
	seedInquiry(t, pool, authorID, "elsewhere", "Durban", 3*time.Hour)

	page, next, err := store.ListInquiries(ctx, nil, 20, "Manguzi")
	if err != nil {
		t.Fatalf("ListInquiries(place) error = %v, want nil", err)
	}
	if len(page) != 2 {
		t.Fatalf("page length = %d, want 2 (only the Manguzi inquiries)", len(page))
	}
	for _, inquiry := range page {
		if inquiry.Place == nil || *inquiry.Place != "Manguzi" {
			t.Errorf("inquiry %q has place %v, want Manguzi", inquiry.Title, inquiry.Place)
		}
	}
	if next != nil {
		t.Errorf("next cursor = %v, want nil when the filtered page is complete", next)
	}

	found := false
	for _, inquiry := range page {
		if inquiry.ID == manguzi {
			found = true
		}
	}
	if !found {
		t.Error("the second Manguzi inquiry is missing from the filtered page")
	}

	// The place match is an exact match on the stored spelling.
	none, _, err := store.ListInquiries(ctx, nil, 20, "manguzi")
	if err != nil {
		t.Fatalf("ListInquiries(lower-case place) error = %v, want nil", err)
	}
	if len(none) != 0 {
		t.Errorf("lower-case place matched %d rows, want 0 (the match is exact)", len(none))
	}
}

func TestPostgresStoreListInquiriesEmptyPlaceFilterReturnsEveryInquiry(t *testing.T) {
	store, pool, prefix := integrationSetup(t)
	ctx := context.Background()
	authorID := createUser(t, pool, prefix, "author")

	withPlace := seedInquiry(t, pool, authorID, "with place", "Manguzi", time.Hour)
	withoutPlace := seedInquiry(t, pool, authorID, "without place", "", time.Hour)

	page, _, err := store.ListInquiries(ctx, nil, 50, "")
	if err != nil {
		t.Fatalf("ListInquiries() error = %v, want nil", err)
	}

	ids := map[string]bool{}
	for _, inquiry := range page {
		ids[inquiry.ID] = true
	}
	if !ids[withPlace] || !ids[withoutPlace] {
		t.Error("an unfiltered page must include both the place-less and the placed inquiry")
	}
}

func TestPostgresStoreCreateAnswerIncrementsTheCount(t *testing.T) {
	store, pool, prefix := integrationSetup(t)
	ctx := context.Background()
	askerID := createUser(t, pool, prefix, "asker")
	answererID := createUser(t, pool, prefix, "answerer")

	inquiry, err := store.CreateInquiry(ctx, Inquiry{
		AuthorID: askerID,
		Title:    "Why do the cattle come home at the same hour?",
		Body:     "Every evening.",
		Language: "eng",
	})
	if err != nil {
		t.Fatalf("CreateInquiry() error = %v, want nil", err)
	}

	answer, inquiryAuthorID, err := store.CreateAnswer(ctx, Answer{
		InquiryID: inquiry.ID,
		AuthorID:  answererID,
		Language:  "eng",
		Body:      "They follow the river.",
	})
	if err != nil {
		t.Fatalf("CreateAnswer() error = %v, want nil", err)
	}
	if answer.ID == "" || answer.InquiryID != inquiry.ID {
		t.Errorf("answer = %+v, want a stored answer for inquiry %q", answer, inquiry.ID)
	}
	// The author of the inquiry comes back from the same locked read, so the
	// caller can notify the asker without a second query.
	if inquiryAuthorID != askerID {
		t.Errorf("inquiry author = %q, want %q", inquiryAuthorID, askerID)
	}

	refetched, err := store.GetInquiry(ctx, inquiry.ID)
	if err != nil {
		t.Fatalf("GetInquiry() error = %v, want nil", err)
	}
	if refetched.AnswerCount != 1 {
		t.Errorf("AnswerCount = %d, want 1", refetched.AnswerCount)
	}
}

func TestPostgresStoreCreateAnswerUnknownInquiryOrAuthor(t *testing.T) {
	store, pool, prefix := integrationSetup(t)
	ctx := context.Background()
	authorID := createUser(t, pool, prefix, "author")

	_, _, err := store.CreateAnswer(ctx, Answer{
		InquiryID: "99999999-9999-4999-8999-999999999999",
		AuthorID:  authorID,
		Language:  "eng",
		Body:      "x",
	})
	if !errors.Is(err, ErrNotFound) {
		t.Errorf("unknown inquiry error = %v, want ErrNotFound", err)
	}

	inquiry, err := store.CreateInquiry(ctx, Inquiry{AuthorID: authorID, Title: "t", Body: "b", Language: "eng"})
	if err != nil {
		t.Fatalf("CreateInquiry() error = %v, want nil", err)
	}

	_, _, err = store.CreateAnswer(ctx, Answer{
		InquiryID: inquiry.ID,
		AuthorID:  "99999999-9999-4999-8999-999999999999",
		Language:  "eng",
		Body:      "x",
	})
	if !errors.Is(err, ErrUserNotFound) {
		t.Errorf("unknown author error = %v, want ErrUserNotFound", err)
	}

	// The failed author insert rolled the count back with it, so the inquiry still
	// has no answers.
	refetched, err := store.GetInquiry(ctx, inquiry.ID)
	if err != nil {
		t.Fatalf("GetInquiry() error = %v, want nil", err)
	}
	if refetched.AnswerCount != 0 {
		t.Errorf("AnswerCount = %d, want 0 after the rolled-back insert", refetched.AnswerCount)
	}
}

func TestPostgresStoreCreateAnswerRejectsMalformedInquiryID(t *testing.T) {
	store, pool, prefix := integrationSetup(t)
	authorID := createUser(t, pool, prefix, "author")

	_, _, err := store.CreateAnswer(context.Background(), Answer{
		InquiryID: "not-a-uuid",
		AuthorID:  authorID,
		Language:  "eng",
		Body:      "x",
	})
	if !errors.Is(err, ErrNotFound) {
		t.Fatalf("CreateAnswer() error = %v, want ErrNotFound", err)
	}
}

// The inquiry row is locked for the duration of the answer insert, so concurrent
// answers serialise on it and the count is exact (the KNOT-ADR-054 pattern).
func TestPostgresStoreConcurrentAnswersProduceAnExactCount(t *testing.T) {
	store, pool, prefix := integrationSetup(t)
	ctx := context.Background()
	askerID := createUser(t, pool, prefix, "asker")

	const answerers = 6
	answererIDs := make([]string, 0, answerers)
	for i := 0; i < answerers; i++ {
		answererIDs = append(answererIDs, createUser(t, pool, prefix, fmt.Sprintf("answerer%d", i)))
	}

	inquiry, err := store.CreateInquiry(ctx, Inquiry{AuthorID: askerID, Title: "t", Body: "b", Language: "eng"})
	if err != nil {
		t.Fatalf("CreateInquiry() error = %v, want nil", err)
	}

	var waitGroup sync.WaitGroup
	errs := make([]error, answerers)

	for i, answererID := range answererIDs {
		waitGroup.Add(1)
		go func(index int, id string) {
			defer waitGroup.Done()
			_, _, err := store.CreateAnswer(ctx, Answer{
				InquiryID: inquiry.ID,
				AuthorID:  id,
				Language:  "eng",
				Body:      fmt.Sprintf("answer %d", index),
			})
			errs[index] = err
		}(i, answererID)
	}
	waitGroup.Wait()

	for i, err := range errs {
		if err != nil {
			t.Fatalf("concurrent CreateAnswer(%d) error = %v, want nil", i, err)
		}
	}

	refetched, err := store.GetInquiry(ctx, inquiry.ID)
	if err != nil {
		t.Fatalf("GetInquiry() error = %v, want nil", err)
	}
	if refetched.AnswerCount != answerers {
		t.Errorf("AnswerCount = %d, want %d (the row lock must serialise the increments)", refetched.AnswerCount, answerers)
	}

	answers, _, err := store.ListAnswers(ctx, inquiry.ID, nil, 100)
	if err != nil {
		t.Fatalf("ListAnswers() error = %v, want nil", err)
	}
	if len(answers) != answerers {
		t.Errorf("stored answers = %d, want %d", len(answers), answerers)
	}
}

func TestPostgresStoreListAnswersIsOldestFirstAndPaginates(t *testing.T) {
	store, pool, prefix := integrationSetup(t)
	ctx := context.Background()
	askerID := createUser(t, pool, prefix, "asker")
	answererID := createUser(t, pool, prefix, "answerer")

	inquiry, err := store.CreateInquiry(ctx, Inquiry{AuthorID: askerID, Title: "t", Body: "b", Language: "eng"})
	if err != nil {
		t.Fatalf("CreateInquiry() error = %v, want nil", err)
	}

	// Explicit timestamps make the order deterministic.
	for i, age := range []time.Duration{3 * time.Hour, 2 * time.Hour, time.Hour} {
		if _, err := pool.Exec(
			ctx,
			`INSERT INTO inquiry_answers (inquiry_id, author_id, language, body, created_at, updated_at)
			 VALUES ($1, $2, 'eng', $3, now() - $4::interval, now() - $4::interval)`,
			inquiry.ID, answererID, fmt.Sprintf("answer %d", i), age.String(),
		); err != nil {
			t.Fatalf("seeding answer %d: %v", i, err)
		}
	}

	firstPage, next, err := store.ListAnswers(ctx, inquiry.ID, nil, 2)
	if err != nil {
		t.Fatalf("ListAnswers() error = %v, want nil", err)
	}
	if len(firstPage) != 2 {
		t.Fatalf("first page length = %d, want 2", len(firstPage))
	}
	// Oldest first: an answer thread reads as a conversation.
	if firstPage[0].Body != "answer 0" || firstPage[1].Body != "answer 1" {
		t.Errorf("first page = %q, %q; want answer 0 then answer 1", firstPage[0].Body, firstPage[1].Body)
	}
	if next == nil {
		t.Fatal("next cursor = nil, want one because a third answer remains")
	}

	secondPage, next, err := store.ListAnswers(ctx, inquiry.ID, next, 2)
	if err != nil {
		t.Fatalf("ListAnswers(page 2) error = %v, want nil", err)
	}
	if len(secondPage) != 1 {
		t.Fatalf("second page length = %d, want 1", len(secondPage))
	}
	if secondPage[0].Body != "answer 2" {
		t.Errorf("second page = %q, want answer 2", secondPage[0].Body)
	}
	if next != nil {
		t.Errorf("next cursor = %v, want nil at the end", next)
	}
}

func TestPostgresStoreListAnswersMalformedIDIsEmpty(t *testing.T) {
	store, _, _ := integrationSetup(t)

	page, next, err := store.ListAnswers(context.Background(), "not-a-uuid", nil, 20)
	if err != nil {
		t.Fatalf("ListAnswers() error = %v, want nil", err)
	}
	if len(page) != 0 || next != nil {
		t.Errorf("page/next = %v/%v, want an empty page and no cursor", page, next)
	}
}

func TestPostgresStoreGetAnswer(t *testing.T) {
	store, pool, prefix := integrationSetup(t)
	ctx := context.Background()
	askerID := createUser(t, pool, prefix, "asker")
	answererID := createUser(t, pool, prefix, "answerer")

	inquiry, err := store.CreateInquiry(ctx, Inquiry{AuthorID: askerID, Title: "t", Body: "b", Language: "eng"})
	if err != nil {
		t.Fatalf("CreateInquiry() error = %v, want nil", err)
	}

	answer, _, err := store.CreateAnswer(ctx, Answer{InquiryID: inquiry.ID, AuthorID: answererID, Language: "eng", Body: "They follow the river."})
	if err != nil {
		t.Fatalf("CreateAnswer() error = %v, want nil", err)
	}

	fetched, err := store.GetAnswer(ctx, answer.ID)
	if err != nil {
		t.Fatalf("GetAnswer() error = %v, want nil", err)
	}
	if fetched.ID != answer.ID || fetched.Body != "They follow the river." {
		t.Errorf("fetched answer = %+v, want it to match the created one", fetched)
	}

	if _, err := store.GetAnswer(ctx, "99999999-9999-4999-8999-999999999999"); !errors.Is(err, ErrNotFound) {
		t.Errorf("unknown id error = %v, want ErrNotFound", err)
	}
	if _, err := store.GetAnswer(ctx, "not-a-uuid"); !errors.Is(err, ErrNotFound) {
		t.Errorf("malformed id error = %v, want ErrNotFound", err)
	}
}

// Deleting an inquiry takes its answers with it, which is what keeps the schema
// honest when a row is removed by hand (there is no delete endpoint at MVP).
func TestPostgresStoreDeletingAnInquiryCascadesToItsAnswers(t *testing.T) {
	store, pool, prefix := integrationSetup(t)
	ctx := context.Background()
	askerID := createUser(t, pool, prefix, "asker")
	answererID := createUser(t, pool, prefix, "answerer")

	inquiry, err := store.CreateInquiry(ctx, Inquiry{AuthorID: askerID, Title: "t", Body: "b", Language: "eng"})
	if err != nil {
		t.Fatalf("CreateInquiry() error = %v, want nil", err)
	}
	if _, _, err := store.CreateAnswer(ctx, Answer{InquiryID: inquiry.ID, AuthorID: answererID, Language: "eng", Body: "x"}); err != nil {
		t.Fatalf("CreateAnswer() error = %v, want nil", err)
	}

	if _, err := pool.Exec(ctx, "DELETE FROM inquiries WHERE id = $1", inquiry.ID); err != nil {
		t.Fatalf("delete inquiry: %v", err)
	}

	var remaining int
	if err := pool.QueryRow(ctx, "SELECT COUNT(*) FROM inquiry_answers WHERE inquiry_id = $1", inquiry.ID).Scan(&remaining); err != nil {
		t.Fatalf("count answers: %v", err)
	}
	if remaining != 0 {
		t.Errorf("answers remaining = %d, want 0 after the inquiry was deleted", remaining)
	}
}
