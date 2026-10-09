package storage

import (
	"context"
	"errors"
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
