package storymedia

import (
	"context"
	"encoding/json"
	"fmt"
)

// The Redis keys the media pipeline is built on (KNOT-038).
//
// The Go API is the producer and the Rust worker is the consumer, so these names
// are a cross-process contract: the worker's `src/queue.rs` spells the same four
// keys, and a change here without the same change there silently stops the
// pipeline. `knot:media:jobs` is the waiting line, `processing` holds jobs a worker
// has claimed, and `completed`/`failed` are the terminal lists an operator reads.
const (
	// JobsKey is where the API pushes and the worker blocks.
	JobsKey = "knot:media:jobs"
	// ProcessingKey holds the jobs a worker has claimed but not finished, so a
	// worker that dies leaves its job recoverable rather than lost.
	ProcessingKey = "knot:media:processing"
	// CompletedKey holds finished jobs, for auditing and for a drain check.
	CompletedKey = "knot:media:completed"
	// FailedKey holds jobs that exhausted their attempts.
	FailedKey = "knot:media:failed"
)

// Job is the payload of one unit of media work.
//
// It is deliberately the whole of what the worker needs: the row to finish, the
// story the row belongs to (for logging and for the key's prefix), the object to
// download, and which pipeline to run. The worker derives every derivative key
// from ObjectKey by replacing `_original.<ext>`, so the two processes agree on the
// naming scheme without sharing code (KNOT-038).
type Job struct {
	// MediaID is the story_media row the worker completes.
	MediaID string `json:"media_id"`
	// StoryID is the story the row belongs to.
	StoryID string `json:"story_id"`
	// ObjectKey is the uploaded file in the bucket.
	ObjectKey string `json:"object_key"`
	// MediaType is `image` or `video`, and picks the pipeline.
	MediaType MediaType `json:"media_type"`
}

// Valid reports whether the payload is complete enough to run. An incomplete job
// means the producer and the consumer disagree about the contract, which is worth
// failing loudly rather than processing half a job.
func (j Job) Valid() bool {
	return isUUID(j.MediaID) &&
		isUUID(j.StoryID) &&
		j.ObjectKey != "" &&
		j.MediaType.Valid()
}

// JobPublisher queues one unit of media work.
//
// The service depends on this interface rather than on Redis, exactly as it
// depends on Storage rather than on S3: a test can assert what was queued without
// a server, and the request path stays free of the queue's details (KNOT-038).
type JobPublisher interface {
	// Publish appends the job to the waiting line. It returns an error when the
	// queue is unreachable, which the caller treats as a failed upload: a row whose
	// job was never queued would stay `processing` forever.
	Publish(ctx context.Context, job Job) error
}

// ListPusher is the slice of the ephemeral store a queue needs: appending a value
// to a list. It is satisfied by cache.Queue, and depending on the narrow interface
// keeps this package free of a Redis client.
type ListPusher interface {
	Push(ctx context.Context, key, value string) error
}

// RedisJobPublisher publishes jobs onto the Redis list the worker blocks on.
type RedisJobPublisher struct {
	queue ListPusher
}

// Compile-time proof that the publisher satisfies the service's contract.
var _ JobPublisher = (*RedisJobPublisher)(nil)

// NewRedisJobPublisher returns a publisher backed by queue.
func NewRedisJobPublisher(queue ListPusher) (*RedisJobPublisher, error) {
	if queue == nil {
		return nil, fmt.Errorf("storymedia: job publisher requires a queue")
	}
	return &RedisJobPublisher{queue: queue}, nil
}

// Publish marshals job and appends it to JobsKey.
//
// The append is a plain RPUSH: the worker claims with `BLMOVE … LEFT LEFT`, so
// pushing right and popping left is what makes the queue first-in, first-out. The
// payload is marshalled here, once, so the wire format is the struct's own tags
// and cannot drift from the Rust worker's `serde` struct by accident (KNOT-038).
func (p *RedisJobPublisher) Publish(ctx context.Context, job Job) error {
	if !job.Valid() {
		return fmt.Errorf("storymedia: refusing to queue an incomplete job")
	}

	payload, err := json.Marshal(job)
	if err != nil {
		return fmt.Errorf("storymedia: marshal media job: %w", err)
	}

	if err := p.queue.Push(ctx, JobsKey, string(payload)); err != nil {
		return fmt.Errorf("storymedia: queue media job: %w", err)
	}

	return nil
}
