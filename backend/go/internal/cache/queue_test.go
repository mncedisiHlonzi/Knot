package cache

import (
	"context"
	"testing"
)

// TestQueuePushAppendsToTheTail pins the direction of the media queue's write.
//
// It is not cosmetic: the worker claims with `BLMOVE <jobs> <processing> LEFT
// LEFT`, so a producer that pushed to the head would reverse the pipeline's order
// and let one slow upload jump ahead of everything (KNOT-038).
func TestQueuePushAppendsToTheTail(t *testing.T) {
	redis, server := newTestCache(t)
	queue, err := NewQueue(redis)
	if err != nil {
		t.Fatalf("NewQueue() error = %v, want nil", err)
	}

	for _, value := range []string{"first", "second", "third"} {
		if err := queue.Push(context.Background(), "knot:test:jobs", value); err != nil {
			t.Fatalf("Push(%q) error = %v, want nil", value, err)
		}
	}

	values, err := server.List("knot:test:jobs")
	if err != nil {
		t.Fatalf("reading the list: %v", err)
	}
	want := []string{"first", "second", "third"}
	if len(values) != len(want) {
		t.Fatalf("list = %v, want %v", values, want)
	}
	for i := range want {
		if values[i] != want[i] {
			t.Fatalf("list = %v, want %v (head first)", values, want)
		}
	}
}

// TestQueueLengthCountsWhatIsWaiting covers the operator check that the queue is
// draining.
func TestQueueLengthCountsWhatIsWaiting(t *testing.T) {
	redis, _ := newTestCache(t)
	queue, err := NewQueue(redis)
	if err != nil {
		t.Fatalf("NewQueue() error = %v, want nil", err)
	}

	// A key that does not exist is an empty queue, not an error.
	emptied, err := queue.Length(context.Background(), "knot:test:jobs")
	if err != nil {
		t.Fatalf("Length(missing) error = %v, want nil", err)
	}
	if emptied != 0 {
		t.Errorf("Length(missing) = %d, want 0", emptied)
	}

	if err := queue.Push(context.Background(), "knot:test:jobs", "job"); err != nil {
		t.Fatalf("Push() error = %v, want nil", err)
	}

	length, err := queue.Length(context.Background(), "knot:test:jobs")
	if err != nil {
		t.Fatalf("Length() error = %v, want nil", err)
	}
	if length != 1 {
		t.Errorf("Length() = %d, want 1", length)
	}
}

// TestNewQueueRejectsANonRedisCache proves the constructor fails loudly rather
// than accepting a cache whose queues nothing would ever read.
func TestNewQueueRejectsANonRedisCache(t *testing.T) {
	var missing Cache

	if _, err := NewQueue(missing); err == nil {
		t.Error("NewQueue(nil) error = nil, want an error")
	}
}
