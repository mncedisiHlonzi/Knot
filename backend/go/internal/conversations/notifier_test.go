package conversations

import (
	"context"
	"errors"
	"testing"
)

// otherAuthorID is the author id used when a test needs an affected user who is
// not the actor.
const otherAuthorID = "11111111-1111-4111-8111-111111111112"

func TestCreateCommentNotifiesTheVersionAuthor(t *testing.T) {
	service, comments, notifier := newTestServiceWithNotifier(t)
	comments.versionAuthorResult = otherAuthorID

	created, err := service.CreateComment(context.Background(), validCommentInput())
	if err != nil {
		t.Fatalf("CreateComment() error = %v, want nil", err)
	}

	if len(notifier.calls) != 1 {
		t.Fatalf("notifications = %d, want exactly 1", len(notifier.calls))
	}

	call := notifier.calls[0]
	if call.kind != "comment.created" {
		t.Errorf("kind = %q, want %q", call.kind, "comment.created")
	}
	if call.recipientID != otherAuthorID {
		t.Errorf("recipient = %q, want the version's author %q", call.recipientID, otherAuthorID)
	}
	if call.actorID != authorID {
		t.Errorf("actor = %q, want the commenter %q", call.actorID, authorID)
	}
	if call.entityID != created.ID {
		t.Errorf("entity id = %q, want the stored comment %q", call.entityID, created.ID)
	}
}

func TestCreateCommentDoesNotNotifyTheVersionAuthorAboutTheirOwnComment(t *testing.T) {
	service, comments, notifier := newTestServiceWithNotifier(t)
	comments.versionAuthorResult = authorID

	if _, err := service.CreateComment(context.Background(), validCommentInput()); err != nil {
		t.Fatalf("CreateComment() error = %v, want nil", err)
	}

	if len(notifier.calls) != 0 {
		t.Errorf("notifications = %+v, want none — commenting on your own version is not news", notifier.calls)
	}
}

func TestCreateCommentSkipsNotificationWhenTheVersionAuthorIsUnknown(t *testing.T) {
	service, comments, notifier := newTestServiceWithNotifier(t)
	comments.versionAuthorErr = ErrNotFound

	if _, err := service.CreateComment(context.Background(), validCommentInput()); err != nil {
		t.Fatalf("CreateComment() error = %v, want nil", err)
	}

	if len(notifier.calls) != 0 {
		t.Errorf("notifications = %+v, want none when the author cannot be resolved", notifier.calls)
	}
}

func TestCreateCommentSurvivesANotifierFailure(t *testing.T) {
	service, comments, notifier := newTestServiceWithNotifier(t)
	comments.versionAuthorResult = otherAuthorID
	notifier.err = errors.New("notifications are unavailable")

	created, err := service.CreateComment(context.Background(), validCommentInput())
	if err != nil {
		t.Fatalf("CreateComment() error = %v, want nil — a notification is a non-critical side effect", err)
	}
	if created.ID == "" {
		t.Error("id is empty, want the stored comment even when the notification failed")
	}
	if comments.createCalls != 1 {
		t.Errorf("store create calls = %d, want 1", comments.createCalls)
	}
}
