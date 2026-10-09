package storage

import (
	"bytes"
	"context"
	"errors"
	"fmt"
	"io"
	"sync"
	"testing"

	"github.com/aws/aws-sdk-go-v2/service/s3/types"
)

// memoryStorage is an in-memory reference implementation of Storage.
//
// It exists so the interface's contract can be pinned by a unit test that needs
// no server. The very same contract assertions run against the real S3
// implementation in integration_test.go, so the two can never drift apart in
// what they promise callers.
type memoryStorage struct {
	mu      sync.Mutex
	objects map[string]memoryObject
}

type memoryObject struct {
	body        []byte
	contentType string
}

var _ Storage = (*memoryStorage)(nil)

func newMemoryStorage() *memoryStorage {
	return &memoryStorage{objects: map[string]memoryObject{}}
}

func (m *memoryStorage) Put(ctx context.Context, key string, body io.Reader, contentType string) error {
	if err := ctx.Err(); err != nil {
		return err
	}

	data, err := io.ReadAll(body)
	if err != nil {
		return fmt.Errorf("memoryStorage: read body: %w", err)
	}

	m.mu.Lock()
	defer m.mu.Unlock()
	m.objects[key] = memoryObject{body: data, contentType: contentType}
	return nil
}

func (m *memoryStorage) Get(ctx context.Context, key string) (io.ReadCloser, string, error) {
	if err := ctx.Err(); err != nil {
		return nil, "", err
	}

	m.mu.Lock()
	defer m.mu.Unlock()

	object, ok := m.objects[key]
	if !ok {
		return nil, "", ErrObjectNotFound
	}
	return io.NopCloser(bytes.NewReader(object.body)), object.contentType, nil
}

func (m *memoryStorage) Delete(ctx context.Context, key string) error {
	if err := ctx.Err(); err != nil {
		return err
	}

	m.mu.Lock()
	defer m.mu.Unlock()
	delete(m.objects, key)
	return nil
}

func (m *memoryStorage) GetRange(ctx context.Context, key string, start, end int64) (io.ReadCloser, error) {
	if err := ctx.Err(); err != nil {
		return nil, err
	}

	m.mu.Lock()
	defer m.mu.Unlock()

	object, ok := m.objects[key]
	if !ok {
		return nil, ErrObjectNotFound
	}
	if start < 0 || end < start || start >= int64(len(object.body)) {
		return nil, fmt.Errorf("memoryStorage: range %d-%d outside object of %d bytes", start, end, len(object.body))
	}
	if end >= int64(len(object.body)) {
		end = int64(len(object.body)) - 1
	}

	return io.NopCloser(bytes.NewReader(object.body[start : end+1])), nil
}

func (m *memoryStorage) Exists(ctx context.Context, key string) (bool, error) {
	if err := ctx.Err(); err != nil {
		return false, err
	}

	m.mu.Lock()
	defer m.mu.Unlock()
	_, ok := m.objects[key]
	return ok, nil
}

// runStorageContract asserts the behaviour every Storage implementation promises.
//
// It is shared by the in-memory test double and the real S3 integration test so
// that the two cannot disagree about the contract.
func runStorageContract(t *testing.T, store Storage, prefix string) {
	t.Helper()

	ctx := context.Background()
	key := prefix + "/contract.txt"
	payload := []byte("knot storage contract payload")
	const contentType = "text/plain"

	// Put then Get returns the same bytes, under the stored content type.
	if err := store.Put(ctx, key, bytes.NewReader(payload), contentType); err != nil {
		t.Fatalf("Put() error = %v, want nil", err)
	}

	reader, gotType, err := store.Get(ctx, key)
	if err != nil {
		t.Fatalf("Get() error = %v, want nil", err)
	}
	got, readErr := io.ReadAll(reader)
	closeErr := reader.Close()
	if readErr != nil {
		t.Fatalf("reading stored object: %v", readErr)
	}
	if closeErr != nil {
		t.Fatalf("closing stored object: %v", closeErr)
	}
	if !bytes.Equal(got, payload) {
		t.Errorf("stored body = %q, want %q", got, payload)
	}
	if gotType != contentType {
		t.Errorf("stored content type = %q, want %q", gotType, contentType)
	}

	// Exists is accurate for a key that is present and one that is absent.
	present, err := store.Exists(ctx, key)
	if err != nil {
		t.Fatalf("Exists(present) error = %v, want nil", err)
	}
	if !present {
		t.Error("Exists(present) = false, want true")
	}

	missingKey := prefix + "/absent.txt"
	present, err = store.Exists(ctx, missingKey)
	if err != nil {
		t.Fatalf("Exists(absent) error = %v, want nil", err)
	}
	if present {
		t.Error("Exists(absent) = true, want false")
	}

	// Get of a missing key is the sentinel, not a raw driver error.
	if _, _, err := store.Get(ctx, missingKey); !errors.Is(err, ErrObjectNotFound) {
		t.Errorf("Get(absent) error = %v, want ErrObjectNotFound", err)
	}

	// GetRange returns exactly the requested inclusive slice of the object.
	ranged, err := store.GetRange(ctx, key, 5, 11)
	if err != nil {
		t.Fatalf("GetRange() error = %v, want nil", err)
	}
	slice, readErr := io.ReadAll(ranged)
	if readErr != nil {
		t.Fatalf("reading ranged object: %v", readErr)
	}
	if closeErr := ranged.Close(); closeErr != nil {
		t.Fatalf("closing ranged object: %v", closeErr)
	}
	if want := payload[5:12]; !bytes.Equal(slice, want) {
		t.Errorf("GetRange(5, 11) = %q, want %q", slice, want)
	}

	// A ranged read of a missing key is also the sentinel.
	if _, err := store.GetRange(ctx, missingKey, 0, 1); !errors.Is(err, ErrObjectNotFound) {
		t.Errorf("GetRange(absent) error = %v, want ErrObjectNotFound", err)
	}

	// Delete removes the object, and deleting an absent key is not an error.
	if err := store.Delete(ctx, key); err != nil {
		t.Fatalf("Delete(present) error = %v, want nil", err)
	}
	present, err = store.Exists(ctx, key)
	if err != nil {
		t.Fatalf("Exists(after delete) error = %v, want nil", err)
	}
	if present {
		t.Error("Exists(after delete) = true, want false")
	}
	if err := store.Delete(ctx, key); err != nil {
		t.Errorf("Delete(absent) error = %v, want nil", err)
	}
}

func TestMemoryStorageSatisfiesContract(t *testing.T) {
	runStorageContract(t, newMemoryStorage(), "contract")
}

// TestNewS3StorageRejectsIncompleteConfiguration pins the constructor's
// fail-fast behaviour: a half-configured object store must be a startup error,
// not a runtime surprise on the first upload.
func TestNewS3StorageRejectsIncompleteConfiguration(t *testing.T) {
	const (
		endpoint  = "http://127.0.0.1:9000"
		region    = "us-east-1"
		accessKey = "knot"
		secretKey = "knot_local_only_change_me"
		bucket    = "knot-media"
	)

	tests := map[string]struct {
		endpoint  string
		region    string
		accessKey string
		secretKey string
		bucket    string
	}{
		"no endpoint":    {endpoint: "", region: region, accessKey: accessKey, secretKey: secretKey, bucket: bucket},
		"blank endpoint": {endpoint: "   ", region: region, accessKey: accessKey, secretKey: secretKey, bucket: bucket},
		"no region":      {endpoint: endpoint, region: "", accessKey: accessKey, secretKey: secretKey, bucket: bucket},
		"no access key":  {endpoint: endpoint, region: region, accessKey: "", secretKey: secretKey, bucket: bucket},
		"no secret key":  {endpoint: endpoint, region: region, accessKey: accessKey, secretKey: "", bucket: bucket},
		"no bucket":      {endpoint: endpoint, region: region, accessKey: accessKey, secretKey: secretKey, bucket: ""},
	}

	for name, tc := range tests {
		t.Run(name, func(t *testing.T) {
			store, err := NewS3Storage(context.Background(), tc.endpoint, tc.region, tc.accessKey, tc.secretKey, tc.bucket)
			if err == nil {
				t.Fatal("NewS3Storage() error = nil, want an error")
			}
			if store != nil {
				t.Errorf("NewS3Storage() store = %#v, want nil", store)
			}
		})
	}
}

// TestIsNotFoundClassifiesMissingObjects covers the typed half of the
// classification. The transport-error half (a bare 404 from HEAD) is exercised by
// the integration test, which is the only place a real 404 exists.
func TestIsNotFoundClassifiesMissingObjects(t *testing.T) {
	if !isNotFound(&types.NoSuchKey{}) {
		t.Error("isNotFound(NoSuchKey) = false, want true")
	}
	if isNotFound(errors.New("connection reset by peer")) {
		t.Error("isNotFound(generic error) = true, want false")
	}
	if isNotFound(nil) {
		t.Error("isNotFound(nil) = true, want false")
	}
}

// TestIsNoSuchBucketIsDistinctFromNoSuchObject pins the difference that decides
// whether a caller sees an empty result or an error. A missing bucket must never
// be folded into "the object is absent".
func TestIsNoSuchBucketIsDistinctFromNoSuchObject(t *testing.T) {
	if !isNoSuchBucket(&types.NoSuchBucket{}) {
		t.Error("isNoSuchBucket(NoSuchBucket) = false, want true")
	}
	if isNoSuchBucket(&types.NoSuchKey{}) {
		t.Error("isNoSuchBucket(NoSuchKey) = true, want false")
	}
	if isNoSuchBucket(errors.New("connection reset by peer")) {
		t.Error("isNoSuchBucket(generic error) = true, want false")
	}
	if isNoSuchBucket(nil) {
		t.Error("isNoSuchBucket(nil) = true, want false")
	}

	// A typed NoSuchBucket is not a NoSuchKey, so a caller that checks for a
	// missing key first cannot mistake a missing bucket for an absent object.
	if isNotFound(&types.NoSuchBucket{}) {
		t.Error("isNotFound(NoSuchBucket) = true, want false")
	}
}
