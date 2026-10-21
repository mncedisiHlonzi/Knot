package httpapi

import (
	"context"
	"errors"
	"testing"
	"time"
)

// recordedCall is one call the recorder received, plus the state of the context
// it was called with.
type recordedCall struct {
	userID          string
	entityType      string
	entityID        string
	interactionType string
	contextErr      error
}

// recordingRecorder captures calls on a channel so a test can wait for the
// background write without a sleep.
type recordingRecorder struct {
	calls chan recordedCall
	err   error
}

func (r *recordingRecorder) Log(ctx context.Context, userID, entityType, entityID, interactionType string) error {
	r.calls <- recordedCall{
		userID:          userID,
		entityType:      entityType,
		entityID:        entityID,
		interactionType: interactionType,
		contextErr:      ctx.Err(),
	}
	return r.err
}

// TestRecordInteractionWritesOffTheRequestContext proves the core promise of the
// pipeline: the log is written on its own goroutine, with a context that survives
// the request finishing (KNOT-026).
func TestRecordInteractionWritesOffTheRequestContext(t *testing.T) {
	recorder := &recordingRecorder{calls: make(chan recordedCall, 1)}
	ctx, cancel := context.WithCancel(context.Background())

	recordInteraction(ctx, discardLogger(), recorder, "user-1", "story", "story-1", "view")
	// The handler returns and its context is cancelled; the write must still land.
	cancel()

	select {
	case call := <-recorder.calls:
		if call.contextErr != nil {
			t.Errorf("recorder context error = %v, want nil (detached from the request)", call.contextErr)
		}
		if call.userID != "user-1" || call.entityType != "story" ||
			call.entityID != "story-1" || call.interactionType != "view" {
			t.Errorf("recorded call = %+v, want the caller's values", call)
		}
	case <-time.After(2 * time.Second):
		t.Fatal("recordInteraction() did not reach the recorder")
	}
}

// TestRecordInteractionIsANoOpWithoutSomethingToLog covers the three legitimate
// silences: no recorder wired, a signed-out reader, and an unresolved entity.
func TestRecordInteractionIsANoOpWithoutSomethingToLog(t *testing.T) {
	cases := []struct {
		name     string
		recorder InteractionRecorder
		userID   string
		entityID string
	}{
		{name: "no recorder", recorder: nil, userID: "user-1", entityID: "story-1"},
		{name: "anonymous reader", recorder: &recordingRecorder{calls: make(chan recordedCall, 1)}, userID: "", entityID: "story-1"},
		{name: "no entity", recorder: &recordingRecorder{calls: make(chan recordedCall, 1)}, userID: "user-1", entityID: ""},
	}

	for _, test := range cases {
		t.Run(test.name, func(t *testing.T) {
			recordInteraction(context.Background(), discardLogger(), test.recorder, test.userID, "story", test.entityID, "view")

			if recorder, ok := test.recorder.(*recordingRecorder); ok {
				select {
				case call := <-recorder.calls:
					t.Fatalf("recorder was called (%+v), want no call", call)
				case <-time.After(50 * time.Millisecond):
				}
			}
		})
	}
}

// TestRecordInteractionSwallowsFailures proves the caller's request cannot fail
// because telemetry did.
func TestRecordInteractionSwallowsFailures(t *testing.T) {
	recorder := &recordingRecorder{
		calls: make(chan recordedCall, 1),
		err:   errors.New("insert failed"),
	}

	// The call itself must not block or panic; the assert is that the test
	// completes and the call was attempted.
	recordInteraction(context.Background(), discardLogger(), recorder, "user-1", "version", "version-1", "reaction")

	select {
	case <-recorder.calls:
	case <-time.After(2 * time.Second):
		t.Fatal("recordInteraction() did not reach the recorder")
	}
}
