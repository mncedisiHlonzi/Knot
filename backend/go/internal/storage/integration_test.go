package storage

import (
	"bytes"
	"context"
	"errors"
	"io"
	"os"
	"strings"
	"testing"
	"time"
)

// integrationSetup builds an S3Storage against the endpoint named by
// KNOT_S3_ENDPOINT, backed by the same bucket and credentials configuration uses.
//
// The test is skipped, not failed, when the endpoint is unset or unreachable, so
// `go test ./...` passes on a machine without MinIO (the same pattern the
// PostgreSQL stores use for KNOT_POSTGRES_DSN).
func integrationSetup(t *testing.T) *S3Storage {
	t.Helper()

	endpoint := strings.TrimSpace(os.Getenv("KNOT_S3_ENDPOINT"))
	if endpoint == "" {
		t.Skip("KNOT_S3_ENDPOINT is not set; skipping object-storage integration test")
	}

	region := envOr("KNOT_S3_REGION", "us-east-1")
	accessKey := envOr("KNOT_S3_ACCESS_KEY", "knot")
	secretKey := envOr("KNOT_S3_SECRET_KEY", "knot_local_only_change_me")
	bucket := envOr("KNOT_S3_BUCKET", "knot-media")

	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()

	store, err := NewS3Storage(ctx, endpoint, region, accessKey, secretKey, bucket)
	if err != nil {
		t.Skipf("could not build an S3 client for KNOT_S3_ENDPOINT: %v", err)
	}

	// Prove the endpoint is reachable (and the bucket exists) before asserting on
	// storage behaviour, so an absent MinIO skips rather than fails.
	if _, err := store.Exists(ctx, "integration/probe"); err != nil {
		t.Skipf("object store at KNOT_S3_ENDPOINT is not reachable, or bucket %q is missing: %v", bucket, err)
	}

	return store
}

func envOr(key, fallback string) string {
	if value := strings.TrimSpace(os.Getenv(key)); value != "" {
		return value
	}
	return fallback
}

// TestS3StorageSatisfiesContract runs the shared contract against the real
// object store, so the S3 implementation and the in-memory double are held to the
// same promises.
func TestS3StorageSatisfiesContract(t *testing.T) {
	store := integrationSetup(t)

	runStorageContract(t, store, "integration/contract")
}

// multipartPayloadBytes is one byte more than one part, so the uploader has to
// split the body and the server has to reassemble it. A file smaller than a part
// would be a plain PutObject and would prove nothing about the multipart path.
const multipartPayloadBytes = uploadPartSize + (1 << 20)

// TestS3StorageMultipartUploadRoundTrips proves the multipart path end to end
// against a real S3-compatible server: a body larger than one part is uploaded,
// reassembled by the store, and read back byte for byte (KNOT-037-fix2).
//
// The in-memory contract test cannot cover this — it has no parts — and the unit
// test only proves the uploader is called. This is the one test that would catch a
// manager misconfiguration (a part size below S3's minimum, or a silently
// truncated assembly).
func TestS3StorageMultipartUploadRoundTrips(t *testing.T) {
	store := integrationSetup(t)

	ctx, cancel := context.WithTimeout(context.Background(), 2*time.Minute)
	defer cancel()

	// A deterministic, non-repeating pattern: all-zero bytes would still match if
	// the store padded or truncated the object.
	payload := make([]byte, multipartPayloadBytes)
	for i := range payload {
		payload[i] = byte(i*7 + i/251)
	}

	key := "integration/multipart-large.bin"
	t.Cleanup(func() {
		cleanupCtx, cleanupCancel := context.WithTimeout(context.Background(), 30*time.Second)
		defer cleanupCancel()
		if err := store.Delete(cleanupCtx, key); err != nil {
			t.Logf("cleanup: %v", err)
		}
	})

	if err := store.Put(ctx, key, bytes.NewReader(payload), "application/octet-stream"); err != nil {
		t.Fatalf("Put(%d bytes) error = %v, want nil", len(payload), err)
	}

	reader, contentType, err := store.Get(ctx, key)
	if err != nil {
		t.Fatalf("Get() error = %v, want nil", err)
	}
	defer reader.Close()

	if contentType != "application/octet-stream" {
		t.Errorf("Get() content type = %q, want %q", contentType, "application/octet-stream")
	}

	stored, err := io.ReadAll(reader)
	if err != nil {
		t.Fatalf("read back the object: %v", err)
	}
	if len(stored) != len(payload) {
		t.Fatalf("stored %d bytes, want %d", len(stored), len(payload))
	}
	if !bytes.Equal(stored, payload) {
		t.Error("the stored bytes differ from the uploaded bytes")
	}
}

// TestS3StorageMissingBucketSurfacesErrors pins that a misconfigured bucket is an
// error from Exists rather than a silent "not found".
func TestS3StorageMissingBucketSurfacesErrors(t *testing.T) {
	endpoint := strings.TrimSpace(os.Getenv("KNOT_S3_ENDPOINT"))
	if endpoint == "" {
		t.Skip("KNOT_S3_ENDPOINT is not set; skipping object-storage integration test")
	}

	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()

	store, err := NewS3Storage(
		ctx,
		endpoint,
		envOr("KNOT_S3_REGION", "us-east-1"),
		envOr("KNOT_S3_ACCESS_KEY", "knot"),
		envOr("KNOT_S3_SECRET_KEY", "knot_local_only_change_me"),
		"knot-bucket-that-does-not-exist",
	)
	if err != nil {
		t.Fatalf("NewS3Storage() error = %v, want nil", err)
	}

	if _, err := store.Exists(ctx, "anything"); err == nil {
		t.Error("Exists() against a missing bucket error = nil, want an error")
	}

	if body, _, err := store.Get(ctx, "anything"); err == nil {
		_ = body.Close()
		t.Error("Get() against a missing bucket error = nil, want an error")
	} else if errors.Is(err, ErrObjectNotFound) {
		t.Errorf("Get() against a missing bucket error = %v, want a bucket error rather than ErrObjectNotFound", err)
	}
}
