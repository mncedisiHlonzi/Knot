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
	service, comments, _, notifier := newTestServiceWithNotifier(t)
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
	service, comments, _, notifier := newTestServiceWithNotifier(t)
	comments.versionAuthorResult = authorID

	if _, err := service.CreateComment(context.Background(), validCommentInput()); err != nil {
		t.Fatalf("CreateComment() error = %v, want nil", err)
	}

	if len(notifier.calls) != 0 {
		t.Errorf("notifications = %+v, want none — commenting on your own version is not news", notifier.calls)
	}
}

func TestCreateCommentSkipsNotificationWhenTheVersionAuthorIsUnknown(t *testing.T) {
	service, comments, _, notifier := newTestServiceWithNotifier(t)
	comments.versionAuthorErr = ErrNotFound

	if _, err := service.CreateComment(context.Background(), validCommentInput()); err != nil {
		t.Fatalf("CreateComment() error = %v, want nil", err)
	}

	if len(notifier.calls) != 0 {
		t.Errorf("notifications = %+v, want none when the author cannot be resolved", notifier.calls)
	}
}

func TestCreateCommentSurvivesANotifierFailure(t *testing.T) {
	service, comments, _, notifier := newTestServiceWithNotifier(t)
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

func TestCreateBridgeNotifiesTheSourceCommentAuthor(t *testing.T) {
	service, comments, bridges, notifier := newTestServiceWithNotifier(t)
	comments.getResult = sourceComment()
	bridges.findTargetResult = otherVersionID

	input := validBridgeInput()
	input.AuthorID = otherAuthorID

	bridge, _, _, err := service.CreateBridge(context.Background(), input)
	if err != nil {
		t.Fatalf("CreateBridge() error = %v, want nil", err)
	}

	if len(notifier.calls) != 1 {
		t.Fatalf("notifications = %d, want exactly 1", len(notifier.calls))
	}

	call := notifier.calls[0]
	if call.kind != "bridge.created" {
		t.Errorf("kind = %q, want %q", call.kind, "bridge.created")
	}
	if call.recipientID != authorID {
		t.Errorf("recipient = %q, want the source comment's author %q", call.recipientID, authorID)
	}
	if call.actorID != otherAuthorID {
		t.Errorf("actor = %q, want the bridger %q", call.actorID, otherAuthorID)
	}
	if call.entityID != bridge.ID {
		t.Errorf("entity id = %q, want the stored bridge %q", call.entityID, bridge.ID)
	}
}

func TestCreateBridgeDoesNotNotifyTheAuthorAboutTheirOwnBridge(t *testing.T) {
	service, comments, bridges, notifier := newTestServiceWithNotifier(t)
	comments.getResult = sourceComment()
	bridges.findTargetResult = otherVersionID

	// The source comment is authored by authorID and the bridger is the same user.
	if _, _, _, err := service.CreateBridge(context.Background(), validBridgeInput()); err != nil {
		t.Fatalf("CreateBridge() error = %v, want nil", err)
	}

	if len(notifier.calls) != 0 {
		t.Errorf("notifications = %+v, want none — bridging your own comment is not news", notifier.calls)
	}
}

func TestCreateBridgeSurvivesANotifierFailure(t *testing.T) {
	service, comments, bridges, notifier := newTestServiceWithNotifier(t)
	comments.getResult = sourceComment()
	bridges.findTargetResult = otherVersionID
	notifier.err = errors.New("notifications are unavailable")

	input := validBridgeInput()
	input.AuthorID = otherAuthorID

	bridge, _, _, err := service.CreateBridge(context.Background(), input)
	if err != nil {
		t.Fatalf("CreateBridge() error = %v, want nil — a notification is a non-critical side effect", err)
	}
	if bridge.ID == "" {
		t.Error("bridge id is empty, want the stored bridge even when the notification failed")
	}
	if bridges.createCalls != 1 {
		t.Errorf("store create calls = %d, want 1", bridges.createCalls)
	}
}

func TestCreateBridgeDoesNotNotifyWhenTheBridgeFails(t *testing.T) {
	service, comments, bridges, notifier := newTestServiceWithNotifier(t)
	comments.getResult = sourceComment()
	bridges.findTargetResult = otherVersionID
	bridges.createErr = errors.New("write failed")

	input := validBridgeInput()
	input.AuthorID = otherAuthorID

	if _, _, _, err := service.CreateBridge(context.Background(), input); err == nil {
		t.Fatal("CreateBridge() error = nil, want the store failure")
	}

	if len(notifier.calls) != 0 {
		t.Errorf("notifications = %+v, want none when the bridge itself failed", notifier.calls)
	}
}
