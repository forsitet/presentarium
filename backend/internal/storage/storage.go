// Package storage provides an abstraction over S3-compatible object storage
// (MinIO, Cloudflare R2, AWS S3, Yandex Object Storage). The backend writes
// uploads here instead of the local disk so files are reachable from any
// replica of the API and can be served directly by an edge/CDN.
package storage

import (
	"context"
	"errors"
	"io"
	"time"
)

// ErrNotFound is returned when the requested object does not exist.
var ErrNotFound = errors.New("storage: object not found")

// Storage is an abstraction over an S3-compatible object store with two
// logical buckets: a public one for directly fetchable assets (served to
// participants via CDN) and a private one that requires a presigned URL
// (source .pptx files, exports, etc.).
//
// Implementations must be safe for concurrent use.
type Storage interface {
	// Put uploads a public object under key. Returns the public URL that
	// browsers/CDN can fetch directly.
	Put(ctx context.Context, key string, body io.Reader, contentType string) (string, error)

	// PutPrivate uploads a private object. Retrieve via PresignGet.
	PutPrivate(ctx context.Context, key string, body io.Reader, contentType string) error

	// Delete removes a public object. It is not an error if the object is
	// already missing.
	Delete(ctx context.Context, key string) error

	// DeletePrivate removes a private object. It is not an error if the
	// object is already missing.
	DeletePrivate(ctx context.Context, key string) error

	// PresignGet returns a short-lived signed URL for a private object.
	PresignGet(ctx context.Context, key string, ttl time.Duration) (string, error)

	// PublicURL builds the public URL for a public key without making a
	// request. Useful at row-insert time.
	PublicURL(key string) string
}
