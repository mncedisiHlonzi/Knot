package storage

import (
	"context"
	"errors"
	"fmt"
	"io"
	"net/http"
	"strings"

	"github.com/aws/aws-sdk-go-v2/aws"
	awshttp "github.com/aws/aws-sdk-go-v2/aws/transport/http"
	awsconfig "github.com/aws/aws-sdk-go-v2/config"
	"github.com/aws/aws-sdk-go-v2/credentials"
	"github.com/aws/aws-sdk-go-v2/feature/s3/transfermanager"
	transfertypes "github.com/aws/aws-sdk-go-v2/feature/s3/transfermanager/types"
	"github.com/aws/aws-sdk-go-v2/service/s3"
	"github.com/aws/aws-sdk-go-v2/service/s3/types"
)

// How the multipart uploader is tuned (KNOT-037-fix2, KNOT-037-fix3).
//
// The numbers are measured, not copied from a default. A 20 MiB upload to R2 over
// the link this was tuned on took 65-211s as one PutObject, 98s with a 5 MiB/5
// configuration, and 42s with 8 MiB parts and three at a time. Fewer, larger parts
// mean fewer round trips and less per-request overhead, which is what a slow,
// high-latency link punishes; more concurrency than that only splits a saturated
// uplink into smaller pieces. 8 MiB is also comfortably above S3's 5 MiB minimum
// part size, so a small file still goes as a single PutObject.
const (
	// uploadPartSize is the size of one multipart part.
	uploadPartSize = 8 << 20
	// uploadConcurrency is how many parts are sent at once.
	uploadConcurrency = 3
)

// putUploader is the one method this package needs from the transfer manager.
//
// It exists as an interface so a unit test can prove that Put goes through the
// multipart uploader rather than calling PutObject itself. The SDK's client is a
// concrete type, so without this seam "did we use it?" cannot be observed without
// a network.
type putUploader interface {
	UploadObject(ctx context.Context, input *transfermanager.UploadObjectInput, opts ...func(*transfermanager.Options)) (*transfermanager.UploadObjectOutput, error)
}

// S3Storage is the S3-compatible implementation of Storage. It holds the client,
// the multipart uploader and the bucket name, and no other state, so it is safe
// for concurrent use.
type S3Storage struct {
	client   *s3.Client
	uploader putUploader
	bucket   string
}

// Compile-time proof that the implementation satisfies the contract.
var _ Storage = (*S3Storage)(nil)

// NewS3Storage builds a client for an S3-compatible endpoint.
//
// endpoint is the base URL of the service — http://127.0.0.1:9000 for the local
// MinIO. Credentials are static (access key and secret) because there is no
// instance role or SSO to inherit locally or in the MVP's deployment; the values
// come from configuration, never from the process's ambient AWS environment.
//
// Path-style addressing is forced: MinIO serves a bucket as the first path
// segment, and virtual-host addressing would require wildcard DNS for every
// bucket name.
func NewS3Storage(ctx context.Context, endpoint, region, accessKey, secretKey, bucket string) (*S3Storage, error) {
	if strings.TrimSpace(endpoint) == "" {
		return nil, fmt.Errorf("storage: endpoint is required")
	}
	if strings.TrimSpace(region) == "" {
		return nil, fmt.Errorf("storage: region is required")
	}
	if strings.TrimSpace(accessKey) == "" {
		return nil, fmt.Errorf("storage: access key is required")
	}
	if strings.TrimSpace(secretKey) == "" {
		return nil, fmt.Errorf("storage: secret key is required")
	}
	if strings.TrimSpace(bucket) == "" {
		return nil, fmt.Errorf("storage: bucket is required")
	}

	cfg, err := awsconfig.LoadDefaultConfig(
		ctx,
		awsconfig.WithRegion(region),
		awsconfig.WithCredentialsProvider(
			credentials.NewStaticCredentialsProvider(accessKey, secretKey, ""),
		),
	)
	if err != nil {
		return nil, fmt.Errorf("storage: load aws config: %w", err)
	}

	client := s3.NewFromConfig(cfg, func(options *s3.Options) {
		options.BaseEndpoint = aws.String(endpoint)
		options.UsePathStyle = true
		// The SDK will not validate a multipart object's composite checksum on
		// download — GetObject hard-codes IgnoreMultipartValidation — and it warns
		// once per such read. Uploads are checksummed below, so what the warning
		// reports is a read-path gap that cannot be closed from this layer; left on,
		// it would print for every large media request forever and train readers to
		// ignore warnings. The limitation is recorded in KNOT-ADR-091 instead
		// (KNOT-037-fix4).
		options.DisableLogOutputChecksumValidationSkipped = true
	})

	// The transfer client shares the S3 client, so it inherits its endpoint,
	// path-style addressing and — importantly — its retryer: a failed part is
	// retried with the SDK's standard exponential backoff and jitter, which means a
	// transient network fault costs one part rather than the whole upload
	// (KNOT-037-fix2).
	//
	// MultipartUploadThreshold is set explicitly, which manager had no equivalent
	// for: manager switched to multipart once the body exceeded PartSize, whereas
	// transfermanager defaults the threshold to 16 MiB independently. Leaving the
	// default would have silently sent an 8-16 MiB upload as one PutObject — the
	// exact behaviour this was tuned to avoid (KNOT-037-fix3).
	uploader := transfermanager.New(client, func(o *transfermanager.Options) {
		o.PartSizeBytes = uploadPartSize
		o.MultipartUploadThreshold = uploadPartSize
		o.Concurrency = uploadConcurrency
		// Every upload carries a CRC32 that the store verifies per part, so
		// integrity no longer rests on the transport alone. CRC32 rather than
		// SHA256: the goal is detecting corruption in transit, not resisting an
		// adversary who controls the bytes, and CRC32 is the cheapest algorithm R2
		// supports. The SDK would add a checksum by default anyway; naming it makes
		// the guarantee explicit instead of dependent on a default that could change
		// (KNOT-037-fix4).
		//
		// The value is transfermanager's own ChecksumAlgorithm type, not
		// service/s3/types: the two are distinct string types and cannot be
		// assigned across each other.
		o.ChecksumAlgorithm = transfertypes.ChecksumAlgorithmCrc32
	})

	return &S3Storage{client: client, uploader: uploader, bucket: bucket}, nil
}

// Put stores body under key with the given content type.
//
// It goes through the transfer manager rather than a bare PutObject. For a body
// that fits in one part the manager sends a single PutObject — the same request as
// before — and above that it splits the body into parts and sends them with bounded
// concurrency. The point is not throughput on a fast link, where one request is
// already efficient: it is that a large upload over a slow or unreliable one no
// longer has to survive as a single long-lived request, and a failure retries a
// part instead of restarting the file (KNOT-037-fix2).
func (s *S3Storage) Put(ctx context.Context, key string, body io.Reader, contentType string) error {
	_, err := s.uploader.UploadObject(ctx, &transfermanager.UploadObjectInput{
		Bucket:      aws.String(s.bucket),
		Key:         aws.String(key),
		Body:        body,
		ContentType: aws.String(contentType),
	})
	if err != nil {
		return fmt.Errorf("storage: put %q: %w", key, err)
	}
	return nil
}

// Get returns the object at key and the content type it was stored with. A
// missing key is reported as ErrObjectNotFound; a bucket that does not exist is
// reported as an error, because that is a deployment mistake rather than an empty
// result.
func (s *S3Storage) Get(ctx context.Context, key string) (io.ReadCloser, string, error) {
	out, err := s.client.GetObject(ctx, &s3.GetObjectInput{
		Bucket: aws.String(s.bucket),
		Key:    aws.String(key),
	})
	if err != nil {
		if isNotFound(err) {
			return nil, "", s.classifyMiss(ctx, err)
		}
		return nil, "", fmt.Errorf("storage: get %q: %w", key, err)
	}

	contentType := ""
	if out.ContentType != nil {
		contentType = *out.ContentType
	}

	return out.Body, contentType, nil
}

// GetRange returns the bytes [start, end] inclusive of the object at key.
//
// The S3 API supports ranges natively through the GetObject request's Range
// field, so only the requested bytes cross the wire and the cost of a seek is
// proportional to the range rather than to the object. The object's content type
// is not returned: a ranged read is used to continue a response whose
// content type was already decided from the stored row (KNOT-ADR-033).
func (s *S3Storage) GetRange(ctx context.Context, key string, start, end int64) (io.ReadCloser, error) {
	if start < 0 || end < start {
		return nil, fmt.Errorf("storage: invalid range %d-%d for %q", start, end, key)
	}

	out, err := s.client.GetObject(ctx, &s3.GetObjectInput{
		Bucket: aws.String(s.bucket),
		Key:    aws.String(key),
		Range:  aws.String(fmt.Sprintf("bytes=%d-%d", start, end)),
	})
	if err != nil {
		if isNotFound(err) {
			return nil, s.classifyMiss(ctx, err)
		}
		return nil, fmt.Errorf("storage: get range %q: %w", key, err)
	}

	return out.Body, nil
}

// Delete removes key. S3 deletes are idempotent, so a key that is already absent
// is not an error.
func (s *S3Storage) Delete(ctx context.Context, key string) error {
	_, err := s.client.DeleteObject(ctx, &s3.DeleteObjectInput{
		Bucket: aws.String(s.bucket),
		Key:    aws.String(key),
	})
	if err != nil {
		return fmt.Errorf("storage: delete %q: %w", key, err)
	}
	return nil
}

// Exists reports whether key is present, using a HEAD request so the object's
// body is never transferred.
func (s *S3Storage) Exists(ctx context.Context, key string) (bool, error) {
	_, err := s.client.HeadObject(ctx, &s3.HeadObjectInput{
		Bucket: aws.String(s.bucket),
		Key:    aws.String(key),
	})
	if err != nil {
		if isNotFound(err) {
			if missErr := s.classifyMiss(ctx, err); !errors.Is(missErr, ErrObjectNotFound) {
				return false, missErr
			}
			return false, nil
		}
		return false, fmt.Errorf("storage: head %q: %w", key, err)
	}
	return true, nil
}

// isNotFound reports whether err is S3's "no such object" answer.
//
// GET returns a typed NoSuchKey; HEAD returns a bare 404 that only the transport
// response error exposes. Both are recognised here so callers see one
// ErrObjectNotFound regardless of which method asked.
func isNotFound(err error) bool {
	var noSuchKey *types.NoSuchKey
	if errors.As(err, &noSuchKey) {
		return true
	}

	var responseErr *awshttp.ResponseError
	if errors.As(err, &responseErr) {
		return responseErr.HTTPStatusCode() == http.StatusNotFound
	}

	return false
}

// apiError is the slice of a service error that this package needs in order to
// read an S3 error code. It is declared here rather than imported from smithy-go
// so that a single string comparison does not promote an indirect dependency to a
// direct one.
type apiError interface {
	error
	ErrorCode() string
}

// noSuchBucketCode is the S3 error code for a bucket that does not exist.
const noSuchBucketCode = "NoSuchBucket"

// isNoSuchBucket reports whether err is the object store saying that the bucket
// itself is missing.
func isNoSuchBucket(err error) bool {
	var apiErr apiError
	return errors.As(err, &apiErr) && apiErr.ErrorCode() == noSuchBucketCode
}

// classifyMiss turns a 404 into the right answer for the caller: ErrObjectNotFound
// when the object is simply absent, and an error when the bucket is missing or
// unreachable.
//
// The distinction is worth the second request. A missing bucket is a deployment
// mistake, and folding it into "no such object" would turn a misconfigured server
// into a stream of empty results — every avatar would look absent and nothing would
// say why. HEAD is why the extra call is needed at all: S3 models no error for it,
// so a 404 from HeadObject carries no service error code and the bucket has to be
// asked about directly. That HeadBucket call requires `s3:ListBucket` on the
// configured credential.
func (s *S3Storage) classifyMiss(ctx context.Context, err error) error {
	if isNoSuchBucket(err) {
		return fmt.Errorf("storage: bucket %q does not exist: %w", s.bucket, err)
	}

	if _, bucketErr := s.client.HeadBucket(ctx, &s3.HeadBucketInput{Bucket: aws.String(s.bucket)}); bucketErr != nil {
		return fmt.Errorf("storage: bucket %q is not reachable: %w", s.bucket, bucketErr)
	}

	return ErrObjectNotFound
}
