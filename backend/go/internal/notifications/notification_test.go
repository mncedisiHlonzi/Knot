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
	for _, event := range []EventType{EventVersionCreated, EventCommentCreated, EventBridgeCreated, EventReactionCreated} {
		if !event.Valid() {
			t.Errorf("%q.Valid() = false, want true", event)
		}
	}

	for _, event := range []EventType{"", "version.deleted", "VERSION.CREATED", "reaction.deleted"} {
		if event.Valid() {
			t.Errorf("%q.Valid() = true, want false", event)
		}
	}
}

func TestEventTypeAcceptsEntityType(t *testing.T) {
	// The three content events each accept exactly one entity kind.
	tests := []struct {
		event  EventType
		entity EntityType
	}{
		{EventVersionCreated, EntityVersion},
		{EventCommentCreated, EntityComment},
		{EventBridgeCreated, EntityBridge},
	}
	for _, test := range tests {
		if !test.event.acceptsEntityType(test.entity) {
			t.Errorf("%q.acceptsEntityType(%q) = false, want true", test.event, test.entity)
		}
		for _, other := range []EntityType{EntityStory, EntityVersion, EntityComment, EntityBridge} {
			if other == test.entity {
				continue
			}
			if test.event.acceptsEntityType(other) {
				t.Errorf("%q.acceptsEntityType(%q) = true, want false", test.event, other)
			}
		}
	}

	// reaction.created may target any of the four entity kinds.
	for _, entity := range []EntityType{EntityStory, EntityVersion, EntityComment, EntityBridge} {
		if !EventReactionCreated.acceptsEntityType(entity) {
			t.Errorf("reaction.created.acceptsEntityType(%q) = false, want true", entity)
		}
	}
	if EventReactionCreated.acceptsEntityType("profile") {
		t.Error("reaction.created.acceptsEntityType(profile) = true, want false")
	}
}

func TestValidateCreateReactionType(t *testing.T) {
	base := CreateInput{
		UserID:     "11111111-1111-4111-8111-111111111111",
		ActorID:    "22222222-2222-4222-8222-222222222222",
		EventType:  EventReactionCreated,
		EntityType: EntityComment,
		EntityID:   "33333333-3333-4333-8333-333333333333",
	}

	withType := base
	withType.ReactionType = "rings_true"
	if _, err := validateCreate(withType); err != nil {
		t.Errorf("validateCreate(reaction.created with a type) error = %v, want nil", err)
	}

	withoutType := base
	if _, err := validateCreate(withoutType); !errors.Is(err, ErrValidation) {
		t.Errorf("validateCreate(reaction.created without a type) error = %v, want ErrValidation", err)
	}

	wrongEvent := base
	wrongEvent.EventType = EventCommentCreated
	wrongEvent.EntityType = EntityComment
	wrongEvent.ReactionType = "rings_true"
	if _, err := validateCreate(wrongEvent); !errors.Is(err, ErrValidation) {
		t.Errorf("validateCreate(comment.created with a type) error = %v, want ErrValidation", err)
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
