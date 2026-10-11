package notifications

import (
	"context"
	"testing"

	"github.com/knot/backend/internal/moderation"
)

// notificationPageHasID reports whether a page contains the notification id.
func notificationPageHasID(page []Notification, id string) bool {
	for _, notification := range page {
		if notification.ID == id {
			return true
		}
	}
	return false
}

// TestPostgresStoreBlockFilterHidesActorNotifications proves the inbox hides an
// action by a blocked actor, in both the list and the unread badge.
func TestPostgresStoreBlockFilterHidesActorNotifications(t *testing.T) {
	env := integrationSetup(t)
	ctx := context.Background()
	blocked := env.actors[0]

	hidden := createNotification(t, env, blocked)
	visible := createNotification(t, env, env.actors[1])

	filtered := moderation.WithExcludedAuthors(ctx, []string{blocked})

	page, _, err := env.store.List(filtered, env.recipient, nil, 10)
	if err != nil {
		t.Fatalf("List() error = %v, want nil", err)
	}
	if notificationPageHasID(page, hidden.ID) {
		t.Error("a blocked actor's notification is visible")
	}
	if !notificationPageHasID(page, visible.ID) {
		t.Error("an unblocked actor's notification is missing")
	}

	// The badge agrees with the list, so it does not count a hidden action.
	unread, err := env.store.UnreadCount(filtered, env.recipient)
	if err != nil {
		t.Fatalf("UnreadCount() error = %v, want nil", err)
	}
	if unread != 1 {
		t.Errorf("filtered unread = %d, want 1 (only the unblocked actor)", unread)
	}

	// Without a block set both are present, and the badge counts both.
	unfiltered, _, err := env.store.List(ctx, env.recipient, nil, 10)
	if err != nil {
		t.Fatalf("List() error = %v, want nil", err)
	}
	if !notificationPageHasID(unfiltered, hidden.ID) {
		t.Error("unfiltered page is missing the blocked actor's notification")
	}
	allUnread, err := env.store.UnreadCount(ctx, env.recipient)
	if err != nil {
		t.Fatalf("UnreadCount() error = %v, want nil", err)
	}
	if allUnread != 2 {
		t.Errorf("unfiltered unread = %d, want 2", allUnread)
	}
}
