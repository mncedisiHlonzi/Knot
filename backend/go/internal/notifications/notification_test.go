package notifications

import (
	"errors"
	"testing"
	"time"
)

func TestEventTypeEntityType(t *testing.T) {
	tests := []struct {
		event  EventType
		entity EntityType
	}{
		{EventVersionCreated, EntityVersion},
		{EventCommentCreated, EntityComment},
		{EventBridgeCreated, EntityBridge},
	}

	for _, test := range tests {
		t.Run(string(test.event), func(t *testing.T) {
			entity, ok := test.event.EntityType()
			if !ok {
				t.Fatalf("EntityType() ok = false, want true for %q", test.event)
			}
			if entity != test.entity {
				t.Errorf("EntityType() = %q, want %q", entity, test.entity)
			}
		})
	}
}

func TestEventTypeEntityTypeRejectsUnknownEvents(t *testing.T) {
	for _, event := range []EventType{"", "story.published", "version.deleted"} {
		t.Run(string(event), func(t *testing.T) {
			entity, ok := event.EntityType()
			if ok {
				t.Errorf("EntityType() ok = true, want false for %q", event)
			}
			if entity != "" {
				t.Errorf("EntityType() = %q, want empty for an unknown event", entity)
			}
		})
	}
}

func TestEventTypeValid(t *testing.T) {
	for _, event := range []EventType{EventVersionCreated, EventCommentCreated, EventBridgeCreated} {
		if !event.Valid() {
			t.Errorf("%q.Valid() = false, want true", event)
		}
	}

	for _, event := range []EventType{"", "version.deleted", "VERSION.CREATED"} {
		if event.Valid() {
			t.Errorf("%q.Valid() = true, want false", event)
		}
	}
}

func TestEntityTypeValid(t *testing.T) {
	for _, entity := range []EntityType{EntityStory, EntityVersion, EntityComment, EntityBridge} {
		if !entity.Valid() {
			t.Errorf("%q.Valid() = false, want true", entity)
		}
	}

	for _, entity := range []EntityType{"", "user", "STORY"} {
		if entity.Valid() {
			t.Errorf("%q.Valid() = true, want false", entity)
		}
	}
}

func TestNotificationIsRead(t *testing.T) {
	unread := Notification{ID: testID}
	if unread.IsRead() {
		t.Error("IsRead() = true, want false while read_at is unset")
	}

	readAt := time.Date(2026, 10, 9, 13, 0, 0, 0, time.UTC)
	read := Notification{ID: testID, ReadAt: &readAt}
	if !read.IsRead() {
		t.Error("IsRead() = false, want true once read_at is set")
	}
}

func TestValidationErrorSatisfiesErrValidation(t *testing.T) {
	err := &ValidationError{Field: "cursor", Message: "has a malformed id"}

	if !errors.Is(err, ErrValidation) {
		t.Error("errors.Is(err, ErrValidation) = false, want true for every ValidationError")
	}
	if err.Error() != "notifications: invalid cursor: has a malformed id" {
		t.Errorf("Error() = %q, want the field and the message", err.Error())
	}
}
