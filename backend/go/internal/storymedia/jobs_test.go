package storymedia

import (
	"context"
	"encoding/json"
	"errors"
	"testing"
)

// fakeList records what a publisher pushed.
type fakeList struct {
	key    string
	values []string
	err    error
}

func (l *fakeList) Push(_ context.Context, key, value string) error {
	if l.err != nil {
		return l.err
	}
	l.key = key
	l.values = append(l.values, value)
	return nil
}

func TestRedisJobPublisherPushesTheWorkerContract(t *testing.T) {
	list := &fakeList{}
	publisher, err := NewRedisJobPublisher(list)
	if err != nil {
		t.Fatalf("NewRedisJobPublisher() error = %v, want nil", err)
	}

	job := Job{
		MediaID:   "00000000-0000-4000-8000-000000000001",
		StoryID:   storyID,
		ObjectKey: StoryMediaKeyPrefix(storyID) + "abc_original.png",
		MediaType: MediaTypeImage,
	}
	if err := publisher.Publish(context.Background(), job); err != nil {
		t.Fatalf("Publish() error = %v, want nil", err)
	}

	if list.key != JobsKey {
		t.Errorf("pushed to %q, want %q", list.key, JobsKey)
	}
	if len(list.values) != 1 {
		t.Fatalf("pushed %d values, want 1", len(list.values))
	}

	// The JSON field names are the contract the Rust worker deserializes, so this
	// test is deliberately about the literal keys rather than about the Go struct:
	// a rename on either side must fail something (KNOT-038).
	var payload map[string]any
	if err := json.Unmarshal([]byte(list.values[0]), &payload); err != nil {
		t.Fatalf("the payload is not JSON: %v", err)
	}

	for _, field := range []string{"media_id", "story_id", "object_key", "media_type"} {
		if _, ok := payload[field]; !ok {
			t.Errorf("payload has no %q: %s", field, list.values[0])
		}
	}
	if len(payload) != 4 {
		t.Errorf("payload has %d fields, want exactly 4: %s", len(payload), list.values[0])
	}
	if payload["media_id"] != job.MediaID || payload["story_id"] != job.StoryID {
		t.Errorf("payload ids = %v/%v, want %q/%q", payload["media_id"], payload["story_id"], job.MediaID, job.StoryID)
	}
	if payload["object_key"] != job.ObjectKey {
		t.Errorf("payload object_key = %v, want %q", payload["object_key"], job.ObjectKey)
	}
	if payload["media_type"] != "image" {
		t.Errorf("payload media_type = %v, want \"image\"", payload["media_type"])
	}
}

func TestRedisJobPublisherQueuesAVideo(t *testing.T) {
	list := &fakeList{}
	publisher, err := NewRedisJobPublisher(list)
	if err != nil {
		t.Fatalf("NewRedisJobPublisher() error = %v, want nil", err)
	}

	if err := publisher.Publish(context.Background(), Job{
		MediaID:   "00000000-0000-4000-8000-000000000002",
		StoryID:   storyID,
		ObjectKey: StoryMediaKeyPrefix(storyID) + "clip_original.mp4",
		MediaType: MediaTypeVideo,
	}); err != nil {
		t.Fatalf("Publish() error = %v, want nil", err)
	}

	var payload Job
	if err := json.Unmarshal([]byte(list.values[0]), &payload); err != nil {
		t.Fatalf("the payload is not a job: %v", err)
	}
	if payload.MediaType != MediaTypeVideo {
		t.Errorf("media type = %q, want video", payload.MediaType)
	}
}

func TestRedisJobPublisherRefusesAnIncompleteJob(t *testing.T) {
	list := &fakeList{}
	publisher, err := NewRedisJobPublisher(list)
	if err != nil {
		t.Fatalf("NewRedisJobPublisher() error = %v, want nil", err)
	}

	// A job with no row would be claimed and then fail to update anything; it is
	// refused here, where the caller can still turn it into a failed upload.
	if err := publisher.Publish(context.Background(), Job{StoryID: storyID}); err == nil {
		t.Fatal("Publish(incomplete) error = nil, want an error")
	}
	if len(list.values) != 0 {
		t.Errorf("pushed %d values, want none", len(list.values))
	}
}

func TestRedisJobPublisherReportsAFullQueue(t *testing.T) {
	list := &fakeList{err: errors.New("redis is down")}
	publisher, err := NewRedisJobPublisher(list)
	if err != nil {
		t.Fatalf("NewRedisJobPublisher() error = %v, want nil", err)
	}

	err = publisher.Publish(context.Background(), Job{
		MediaID:   "00000000-0000-4000-8000-000000000003",
		StoryID:   storyID,
		ObjectKey: StoryMediaKeyPrefix(storyID) + "abc_original.png",
		MediaType: MediaTypeImage,
	})
	if err == nil {
		t.Fatal("Publish() error = nil, want the queue failure")
	}
}

func TestNewRedisJobPublisherRejectsAMissingQueue(t *testing.T) {
	if _, err := NewRedisJobPublisher(nil); err == nil {
		t.Error("NewRedisJobPublisher(nil) error = nil, want an error")
	}
}
