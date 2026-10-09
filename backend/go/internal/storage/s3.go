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
	"github.com/aws/aws-sdk-go-v2/service/s3"
	"github.com/aws/aws-sdk-go-v2/service/s3/types"
)

// S3Storage is the S3-compatible implementation of Storage. It holds an injected
// client and the bucket name, and no other state, so it is safe for concurrent
// use.
type S3Storage struct {
	client *s3.Client
	bucket string
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
	})

	return &S3Storage{client: client, bucket: bucket}, nil
}

// Put stores body under key with the given content type.
func (s *S3Storage) Put(ctx context.Context, key string, body io.Reader, contentType string) error {
	_, err := s.client.PutObject(ctx, &s3.PutObjectInput{
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
