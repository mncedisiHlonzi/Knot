package storage

// Package storage defines Knot's object-storage contract and an S3-compatible
// implementation of it.
//
// Media lives in an object store rather than in Postgres:
// a binary does not belong in a relational row, and an object store gives cheap
// storage, streaming reads, and a clear path to a CDN later. The contract is
// deliberately small — put, get, ranged get, delete, exists — because that is
// all the product needs; there is no bucket-management or listing API here.
// GetRange was added for video streaming (KNOT-ADR-033), where a client plays a
// file by requesting byte ranges rather than the whole object.
//
// The implementation speaks the S3 API, so the same code runs against MinIO
// locally (KNOT-ADR-028) and against a managed S3-compatible service in
// production. Clients are never handed a direct object-store URL: every upload
// and download is mediated by the backend (KNOT-ADR-029).

import (
	"context"
	"errors"
	"io"
)

// ErrObjectNotFound is returned by Get for a key that does not exist. Callers map
// it to 404; it is never returned to a client verbatim.
var ErrObjectNotFound = errors.New("storage: object not found")

// Storage is the object-storage contract.
//
// Implementations must accept a context on every call and must be safe for
// concurrent use.
type Storage interface {
	// Put stores body under key, tagged with contentType. Storing a key that
	// already exists replaces it.
	Put(ctx context.Context, key string, body io.Reader, contentType string) error

	// Get returns the object's body together with the content type it was stored
	// with. The caller must close the returned reader. A missing key is reported
	// as ErrObjectNotFound.
	Get(ctx context.Context, key string) (io.ReadCloser, string, error)

	// GetRange returns a reader for the byte range [start, end] inclusive of the
	// object at key. It exists so a video can be streamed in pieces without
	// transferring the whole object to answer one HTTP Range request
	// (KNOT-ADR-033). The caller must close the returned reader. A missing key is
	// reported as ErrObjectNotFound; a start or end outside [0, size-1] is a
	// caller error and implementations may return an error.
	//
	// An implementation whose underlying store has no native range support may
	// satisfy it by fetching the whole object and discarding the bytes outside
	// the range, but the S3 implementation uses the store's native range so the
	// cost is proportional to the bytes requested.
	GetRange(ctx context.Context, key string, start, end int64) (io.ReadCloser, error)

	// Delete removes key. Deleting a key that does not exist is not an error:
	// the caller wants the object gone, and it is.
	Delete(ctx context.Context, key string) error

	// Exists reports whether key is present.
	Exists(ctx context.Context, key string) (bool, error)
}
