package versions

import (
	"context"
	"errors"
	"testing"
)

// adapterID is the author id used when a test needs an adapter who is not the
// author of the parent version.
const adapterID = "11111111-1111-4111-8111-111111111112"

// parentByOtherAuthor is a parent version authored by serviceAuthorID, so an
// adaptation by adapterID must notify serviceAuthorID.
func parentByOtherAuthor() StoryVersion {
	parent := rootParent()
	parent.AuthorID = serviceAuthorID
	return parent
}

func TestCreateAdaptationNotifiesTheParentAuthor(t *testing.T) {
	service, store, notifier := newTestServiceWithNotifier(t)
	store.getResult = parentByOtherAuthor()

	input := validAdaptationInput()
	input.AuthorID = adapterID

	created, err := service.CreateAdaptation(context.Background(), input)
	if err != nil {
		t.Fatalf("CreateAdaptation() error = %v, want nil", err)
	}

	if len(notifier.calls) != 1 {
		t.Fatalf("notifications = %d, want exactly 1", len(notifier.calls))
	}

	call := notifier.calls[0]
	if call.recipientID != serviceAuthorID {
		t.Errorf("recipient = %q, want the parent's author %q", call.recipientID, serviceAuthorID)
	}
	if call.actorID != adapterID {
		t.Errorf("actor = %q, want the adapter %q", call.actorID, adapterID)
	}
	if call.versionID != created.ID {
		t.Errorf("version id = %q, want the stored adaptation %q", call.versionID, created.ID)
	}
}

func TestCreateAdaptationDoesNotNotifyTheAdapterAboutTheirOwnVersion(t *testing.T) {
	service, store, notifier := newTestServiceWithNotifier(t)
	store.getResult = parentByOtherAuthor()

	// The parent is authored by serviceAuthorID and the adapter is the same user,
	// so adapting their own version must not notify them.
	created, err := service.CreateAdaptation(context.Background(), validAdaptationInput())
	if err != nil {
		t.Fatalf("CreateAdaptation() error = %v, want nil", err)
	}

	if len(notifier.calls) != 0 {
		t.Errorf("notifications = %+v, want none — acting on your own version is not news", notifier.calls)
	}
	if created.ID == "" {
		t.Error("id is empty, want the stored adaptation regardless of the notification")
	}
}

func TestCreateAdaptationSurvivesANotifierFailure(t *testing.T) {
	service, store, notifier := newTestServiceWithNotifier(t)
	store.getResult = parentByOtherAuthor()
	notifier.err = errors.New("notifications are unavailable")

	input := validAdaptationInput()
	input.AuthorID = adapterID

	created, err := service.CreateAdaptation(context.Background(), input)
	if err != nil {
		t.Fatalf("CreateAdaptation() error = %v, want nil — a notification is a non-critical side effect", err)
	}
	if created.ID == "" {
		t.Error("id is empty, want the stored adaptation even when the notification failed")
	}
	if store.createCalls != 1 {
		t.Errorf("store create calls = %d, want 1", store.createCalls)
	}
}

func TestCreateAdaptationDoesNotNotifyWhenTheParentLookupFails(t *testing.T) {
	service, store, notifier := newTestServiceWithNotifier(t)
	store.getErr = ErrNotFound

	if _, err := service.CreateAdaptation(context.Background(), validAdaptationInput()); !errors.Is(err, ErrNotFound) {
		t.Fatalf("CreateAdaptation() error = %v, want ErrNotFound", err)
	}

	if len(notifier.calls) != 0 {
		t.Errorf("notifications = %+v, want none when the adaptation itself failed", notifier.calls)
	}
}
