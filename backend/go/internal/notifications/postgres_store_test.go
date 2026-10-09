package notifications

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

// integrationEnv is what the integration tests work with: the notifications
// store under test, the pool, one recipient and four distinct actors.
//
// The notifications table has a CHECK constraint that the actor and the recipient
// must differ, and an inbox is only interesting with several senders, so the
// fixture creates one recipient and four actors.
type integrationEnv struct {
	store     *PostgresStore
	pool      *pgxpool.Pool
	prefix    string
	recipient string
	actors    []string
}

// integrationSetup connects to the database named by KNOT_POSTGRES_DSN and creates
// the users the notifications point at.
//
// The test is skipped, not failed, when KNOT_POSTGRES_DSN is unset, when the
// database is unreachable, or when the tables have not been migrated yet — none of
// those is a defect in the code under test.
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

	// The notifications table (0010) and the users it references (0001) must both
	// exist for these tests to run.
	for _, table := range []string{"public.users", "public.notifications"} {
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

	prefix := fmt.Sprintf("knot-it-notif-%d-", time.Now().UnixNano())

	newUser := func(label string) string {
		t.Helper()

		var id string
		err := pool.QueryRow(
			ctx,
			`INSERT INTO users (email, password_hash, display_name, preferred_languages)
			 VALUES ($1, $2, $3, $4)
			 RETURNING id`,
			prefix+label+"@example.test",
			"$argon2id$v=19$m=65536,t=1,p=4$c2FsdA$a2V5",
			"Integration "+label,
			[]string{"eng"},
		).Scan(&id)
		if err != nil {
			t.Fatalf("could not create the integration %s: %v", label, err)
		}
		return id
	}

	recipient := newUser("recipient")
	actors := []string{newUser("actor1"), newUser("actor2"), newUser("actor3"), newUser("actor4")}

	t.Cleanup(func() {
		cleanupCtx, cancelCleanup := context.WithTimeout(context.Background(), 10*time.Second)
		defer cancelCleanup()

		// Deleting a user cascades to their inbox and to every notification they
		// acted on, so no notification row survives the cleanup.
		if _, err := pool.Exec(cleanupCtx, "DELETE FROM users WHERE email LIKE $1", prefix+"%"); err != nil {
			t.Logf("cleanup: could not delete test users: %v", err)
		}
		pool.Close()
	})

	return integrationEnv{store: store, pool: pool, prefix: prefix, recipient: recipient, actors: actors}
}

// createNotification stores one notification for the recipient from actor.
func createNotification(t *testing.T, env integrationEnv, actor string) Notification {
	t.Helper()

	created, err := env.store.Create(context.Background(), Notification{
		UserID:     env.recipient,
		ActorID:    actor,
		EventType:  EventVersionCreated,
		EntityType: EntityVersion,
		EntityID:   actor,
	})
	if err != nil {
		t.Fatalf("Create() error = %v, want nil", err)
	}

	return created
}

func TestPostgresStoreCreateReturnsStoredRow(t *testing.T) {
	env := integrationSetup(t)

	created := createNotification(t, env, env.actors[0])

	if !isUUID(created.ID) {
		t.Errorf("id = %q, want the generated UUID", created.ID)
	}
	if created.UserID != env.recipient {
		t.Errorf("user id = %q, want %q", created.UserID, env.recipient)
	}
	if created.ActorID != env.actors[0] {
		t.Errorf("actor id = %q, want %q", created.ActorID, env.actors[0])
	}
	if created.EventType != EventVersionCreated {
		t.Errorf("event type = %q, want %q", created.EventType, EventVersionCreated)
	}
	if created.EntityType != EntityVersion {
		t.Errorf("entity type = %q, want %q", created.EntityType, EntityVersion)
	}
	if created.IsRead() {
		t.Error("IsRead() = true, want false for a freshly stored notification")
	}
	if created.CreatedAt.IsZero() {
		t.Error("created at is zero, want the database timestamp")
	}
}

func TestPostgresStoreCreateRejectsSelfNotification(t *testing.T) {
	env := integrationSetup(t)

	// The CHECK constraint (actor_id <> user_id) is the last line of defence if a
	// caller ever bypasses the service's own rule.
	_, err := env.store.Create(context.Background(), Notification{
		UserID:     env.recipient,
		ActorID:    env.recipient,
		EventType:  EventVersionCreated,
		EntityType: EntityVersion,
		EntityID:   env.recipient,
	})
	if err == nil {
		t.Fatal("Create() error = nil, want the check constraint to reject a self-notification")
	}
}

func TestPostgresStoreCreateRejectsUnknownEventType(t *testing.T) {
	env := integrationSetup(t)

	_, err := env.store.Create(context.Background(), Notification{
		UserID:     env.recipient,
		ActorID:    env.actors[0],
		EventType:  EventType("story.published"),
		EntityType: EntityStory,
		EntityID:   env.actors[0],
	})
	if err == nil {
		t.Fatal("Create() error = nil, want the check constraint to reject an unknown event type")
	}
}

func TestPostgresStoreListReturnsNewestFirst(t *testing.T) {
	env := integrationSetup(t)

	first := createNotification(t, env, env.actors[0])
	second := createNotification(t, env, env.actors[1])

	page, next, err := env.store.List(context.Background(), env.recipient, nil, 10)
	if err != nil {
		t.Fatalf("List() error = %v, want nil", err)
	}

	if len(page) != 2 {
		t.Fatalf("len(page) = %d, want 2", len(page))
	}
	if page[0].ID != second.ID {
		t.Errorf("first row id = %q, want the newest %q", page[0].ID, second.ID)
	}
	if page[1].ID != first.ID {
		t.Errorf("second row id = %q, want the older %q", page[1].ID, first.ID)
	}
	if next != nil {
		t.Errorf("next cursor = %v, want nil on the last page", next)
	}
}

func TestPostgresStoreListIsScopedToOneUser(t *testing.T) {
	env := integrationSetup(t)

	createNotification(t, env, env.actors[0])

	// A notification for someone else's inbox must never appear in this one.
	if _, err := env.store.Create(context.Background(), Notification{
		UserID:     env.actors[1],
		ActorID:    env.actors[2],
		EventType:  EventVersionCreated,
		EntityType: EntityVersion,
		EntityID:   env.actors[2],
	}); err != nil {
		t.Fatalf("Create() error = %v, want nil", err)
	}

	page, _, err := env.store.List(context.Background(), env.recipient, nil, 10)
	if err != nil {
		t.Fatalf("List() error = %v, want nil", err)
	}

	if len(page) != 1 {
		t.Fatalf("len(page) = %d, want 1 — an inbox must contain only its owner's notifications", len(page))
	}
	if page[0].UserID != env.recipient {
		t.Errorf("row user id = %q, want %q", page[0].UserID, env.recipient)
	}
}

func TestPostgresStoreListPaginates(t *testing.T) {
	env := integrationSetup(t)

	// Five notifications from four distinct actors, paged two at a time: 2 + 2 + 1.
	stored := make([]Notification, 0, 5)
	for i := 0; i < 5; i++ {
		stored = append(stored, createNotification(t, env, env.actors[i%len(env.actors)]))
	}

	seen := make([]string, 0, len(stored))
	var cursor *Cursor

	for page := 0; page < 3; page++ {
		items, next, err := env.store.List(context.Background(), env.recipient, cursor, 2)
		if err != nil {
			t.Fatalf("List(page %d) error = %v, want nil", page, err)
		}
		if len(items) == 0 {
			t.Fatalf("page %d is empty, want notifications", page)
		}
		for _, item := range items {
			seen = append(seen, item.ID)
		}
		cursor = next
	}

	if len(seen) != len(stored) {
		t.Fatalf("saw %d notifications across the pages, want %d", len(seen), len(stored))
	}

	// Every id appears exactly once, and newest first.
	unique := make(map[string]bool, len(seen))
	for i, id := range seen {
		if unique[id] {
			t.Fatalf("id %q appeared twice; paging must not repeat a row", id)
		}
		unique[id] = true

		want := stored[len(stored)-1-i].ID
		if id != want {
			t.Fatalf("position %d = %q, want %q (newest first)", i, id, want)
		}
	}
}

func TestPostgresStoreListUnknownUserIsEmpty(t *testing.T) {
	env := integrationSetup(t)

	// A user id with no notifications is an empty page, not an error.
	page, next, err := env.store.List(context.Background(), env.actors[3], nil, 10)
	if err != nil {
		t.Fatalf("List() error = %v, want nil", err)
	}
	if len(page) != 0 {
		t.Errorf("len(page) = %d, want 0", len(page))
	}
	if next != nil {
		t.Errorf("next cursor = %v, want nil", next)
	}
}

func TestPostgresStoreUnreadCount(t *testing.T) {
	env := integrationSetup(t)

	first := createNotification(t, env, env.actors[0])
	createNotification(t, env, env.actors[1])

	count, err := env.store.UnreadCount(context.Background(), env.recipient)
	if err != nil {
		t.Fatalf("UnreadCount() error = %v, want nil", err)
	}
	if count != 2 {
		t.Errorf("count = %d, want 2", count)
	}

	if err := env.store.MarkRead(context.Background(), first.ID, env.recipient); err != nil {
		t.Fatalf("MarkRead() error = %v, want nil", err)
	}

	count, err = env.store.UnreadCount(context.Background(), env.recipient)
	if err != nil {
		t.Fatalf("UnreadCount() after marking one read error = %v, want nil", err)
	}
	if count != 1 {
		t.Errorf("count = %d, want 1 after marking one read", count)
	}
}

func TestPostgresStoreMarkRead(t *testing.T) {
	env := integrationSetup(t)

	created := createNotification(t, env, env.actors[0])

	if err := env.store.MarkRead(context.Background(), created.ID, env.recipient); err != nil {
		t.Fatalf("MarkRead() error = %v, want nil", err)
	}

	page, _, err := env.store.List(context.Background(), env.recipient, nil, 10)
	if err != nil {
		t.Fatalf("List() error = %v, want nil", err)
	}
	if len(page) != 1 {
		t.Fatalf("len(page) = %d, want 1", len(page))
	}
	if !page[0].IsRead() {
		t.Error("IsRead() = false, want true after MarkRead")
	}
	if page[0].ReadAt == nil || page[0].ReadAt.IsZero() {
		t.Error("read at is nil or zero, want the database timestamp")
	}

	// Marking an already-read notification again is not an error, and it does not
	// move the timestamp.
	firstRead := *page[0].ReadAt
	if err := env.store.MarkRead(context.Background(), created.ID, env.recipient); err != nil {
		t.Fatalf("second MarkRead() error = %v, want nil", err)
	}

	page, _, err = env.store.List(context.Background(), env.recipient, nil, 10)
	if err != nil {
		t.Fatalf("List() after second MarkRead error = %v, want nil", err)
	}
	if !page[0].ReadAt.Equal(firstRead) {
		t.Errorf("read at = %s, want the original %s — marking read is idempotent", page[0].ReadAt, firstRead)
	}
}

func TestPostgresStoreMarkReadRejectsAnotherUsersNotification(t *testing.T) {
	env := integrationSetup(t)

	created := createNotification(t, env, env.actors[0])

	// Another user marking it read must look like a missing notification, so the
	// endpoint cannot be used to probe whether an id exists.
	err := env.store.MarkRead(context.Background(), created.ID, env.actors[1])
	if !errors.Is(err, ErrNotFound) {
		t.Fatalf("MarkRead() error = %v, want ErrNotFound for a notification owned by someone else", err)
	}

	page, _, err := env.store.List(context.Background(), env.recipient, nil, 10)
	if err != nil {
		t.Fatalf("List() error = %v, want nil", err)
	}
	if page[0].IsRead() {
		t.Error("IsRead() = true, want false — another user must not be able to mark it read")
	}
}

func TestPostgresStoreMarkReadMissingNotification(t *testing.T) {
	env := integrationSetup(t)

	err := env.store.MarkRead(context.Background(), "00000000-0000-4000-8000-000000000000", env.recipient)
	if !errors.Is(err, ErrNotFound) {
		t.Errorf("MarkRead() error = %v, want ErrNotFound", err)
	}
}

func TestPostgresStoreMarkReadRejectsMalformedID(t *testing.T) {
	env := integrationSetup(t)

	for _, id := range []string{"", "not-a-uuid", "44444444-4444-4444-8444"} {
		t.Run(id, func(t *testing.T) {
			if err := env.store.MarkRead(context.Background(), id, env.recipient); !errors.Is(err, ErrNotFound) {
				t.Errorf("MarkRead(%q) error = %v, want ErrNotFound", id, err)
			}
		})
	}
}

func TestPostgresStoreMarkAllRead(t *testing.T) {
	env := integrationSetup(t)

	createNotification(t, env, env.actors[0])
	createNotification(t, env, env.actors[1])
	createNotification(t, env, env.actors[2])

	updated, err := env.store.MarkAllRead(context.Background(), env.recipient)
	if err != nil {
		t.Fatalf("MarkAllRead() error = %v, want nil", err)
	}
	if updated != 3 {
		t.Errorf("updated = %d, want 3", updated)
	}

	count, err := env.store.UnreadCount(context.Background(), env.recipient)
	if err != nil {
		t.Fatalf("UnreadCount() error = %v, want nil", err)
	}
	if count != 0 {
		t.Errorf("unread count = %d, want 0 after marking all read", count)
	}

	// A second call updates nothing, and is not an error.
	updated, err = env.store.MarkAllRead(context.Background(), env.recipient)
	if err != nil {
		t.Fatalf("second MarkAllRead() error = %v, want nil", err)
	}
	if updated != 0 {
		t.Errorf("second updated = %d, want 0", updated)
	}
}

func TestPostgresStoreMarkAllReadIsScopedToOneUser(t *testing.T) {
	env := integrationSetup(t)

	createNotification(t, env, env.actors[0])

	other, err := env.store.Create(context.Background(), Notification{
		UserID:     env.actors[1],
		ActorID:    env.actors[2],
		EventType:  EventVersionCreated,
		EntityType: EntityVersion,
		EntityID:   env.actors[2],
	})
	if err != nil {
		t.Fatalf("Create() error = %v, want nil", err)
	}

	if _, err := env.store.MarkAllRead(context.Background(), env.recipient); err != nil {
		t.Fatalf("MarkAllRead() error = %v, want nil", err)
	}

	count, err := env.store.UnreadCount(context.Background(), env.actors[1])
	if err != nil {
		t.Fatalf("UnreadCount() error = %v, want nil", err)
	}
	if count != 1 {
		t.Fatalf("other user's unread count = %d, want 1 — marking all read must not cross users", count)
	}

	page, _, err := env.store.List(context.Background(), env.actors[1], nil, 10)
	if err != nil {
		t.Fatalf("List() error = %v, want nil", err)
	}
	if len(page) != 1 || page[0].ID != other.ID {
		t.Fatalf("other user's page = %+v, want the untouched notification %q", page, other.ID)
	}
}
