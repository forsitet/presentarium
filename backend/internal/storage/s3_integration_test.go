//go:build integration

package storage

import (
	"bytes"
	"context"
	"io"
	"net/http"
	"os"
	"testing"
	"time"

	"github.com/aws/aws-sdk-go-v2/aws"
	"github.com/aws/aws-sdk-go-v2/service/s3"
	"github.com/google/uuid"
)

// Integration tests against a running MinIO instance. Bring one up via the
// project docker-compose (`docker compose up -d minio`) and run:
//
//	go test -tags=integration ./internal/storage/...
//
// The tests skip cleanly if MinIO is not reachable, mirroring the convention
// in test/integration/suite_test.go.

const (
	defaultEndpoint = "http://localhost:9000"
	defaultAccess   = "presentarium"
	defaultSecret   = "presentarium-dev-secret"
)

// newTestStorage returns an S3Storage wired to MinIO with unique per-run
// buckets. Skips the test if MinIO is unreachable.
func newTestStorage(t *testing.T) *S3Storage {
	t.Helper()

	endpoint := getenv("TEST_S3_ENDPOINT", defaultEndpoint)
	access := getenv("TEST_S3_ACCESS_KEY", defaultAccess)
	secret := getenv("TEST_S3_SECRET_KEY", defaultSecret)

	// Quick reachability probe — skip if MinIO is not running.
	ctx, cancel := context.WithTimeout(context.Background(), 2*time.Second)
	defer cancel()
	req, _ := http.NewRequestWithContext(ctx, http.MethodGet, endpoint+"/minio/health/live", nil)
	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		t.Skipf("MinIO unreachable at %s: %v — skipping", endpoint, err)
	}
	resp.Body.Close()

	run := uuid.New().String()[:8]
	pub := "test-pub-" + run
	priv := "test-priv-" + run

	s, err := NewS3(context.Background(), S3Config{
		Endpoint:       endpoint,
		Region:         "us-east-1",
		AccessKeyID:    access,
		SecretKey:      secret,
		BucketPublic:   pub,
		BucketPrivate:  priv,
		ForcePathStyle: true,
	})
	if err != nil {
		t.Fatalf("NewS3: %v", err)
	}
	if err := s.EnsureBuckets(context.Background()); err != nil {
		t.Fatalf("EnsureBuckets: %v", err)
	}
	t.Cleanup(func() { emptyAndDelete(t, s, pub); emptyAndDelete(t, s, priv) })
	return s
}

// getObject fetches an object from a bucket directly via the S3 API, bypassing
// public-bucket-policy concerns. Used only in tests for round-trip verification.
func (s *S3Storage) getObject(ctx context.Context, bucket, key string) ([]byte, error) {
	out, err := s.client.GetObject(ctx, &s3.GetObjectInput{
		Bucket: aws.String(bucket),
		Key:    aws.String(key),
	})
	if err != nil {
		return nil, err
	}
	defer out.Body.Close()
	return io.ReadAll(out.Body)
}

func TestS3_PutGetDelete_Public(t *testing.T) {
	s := newTestStorage(t)
	ctx := context.Background()

	key := "objects/" + uuid.New().String() + ".txt"
	body := []byte("hello presentarium")

	url, err := s.Put(ctx, key, bytes.NewReader(body), "text/plain")
	if err != nil {
		t.Fatalf("Put: %v", err)
	}
	if url == "" {
		t.Fatal("expected non-empty public URL")
	}
	if url != s.PublicURL(key) {
		t.Errorf("Put URL %q != PublicURL %q", url, s.PublicURL(key))
	}

	got, err := s.getObject(ctx, s.cfg.BucketPublic, key)
	if err != nil {
		t.Fatalf("getObject: %v", err)
	}
	if !bytes.Equal(got, body) {
		t.Errorf("round-trip body mismatch: got %q, want %q", got, body)
	}

	if err := s.Delete(ctx, key); err != nil {
		t.Fatalf("Delete: %v", err)
	}
	// Deleting again must not error — S3 semantics.
	if err := s.Delete(ctx, key); err != nil {
		t.Fatalf("second Delete should be idempotent: %v", err)
	}
}

func TestS3_PutGetDelete_Private(t *testing.T) {
	s := newTestStorage(t)
	ctx := context.Background()

	key := "sources/" + uuid.New().String() + ".bin"
	body := []byte{0x50, 0x4B, 0x03, 0x04, 0x14, 0x00} // fake ZIP header

	if err := s.PutPrivate(ctx, key, bytes.NewReader(body), "application/zip"); err != nil {
		t.Fatalf("PutPrivate: %v", err)
	}

	// Presign + fetch via HTTP — end-to-end signing check.
	presignURL, err := s.PresignGet(ctx, key, 30*time.Second)
	if err != nil {
		t.Fatalf("PresignGet: %v", err)
	}
	resp, err := http.Get(presignURL)
	if err != nil {
		t.Fatalf("GET presigned: %v", err)
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		t.Fatalf("presigned GET: status %d", resp.StatusCode)
	}
	got, _ := io.ReadAll(resp.Body)
	if !bytes.Equal(got, body) {
		t.Errorf("presigned body mismatch: got %x, want %x", got, body)
	}

	if err := s.DeletePrivate(ctx, key); err != nil {
		t.Fatalf("DeletePrivate: %v", err)
	}
}

func TestS3_PresignGet_ExpiredReturnsError(t *testing.T) {
	s := newTestStorage(t)
	ctx := context.Background()

	key := "sources/" + uuid.New().String() + ".bin"
	if err := s.PutPrivate(ctx, key, bytes.NewReader([]byte("x")), "application/octet-stream"); err != nil {
		t.Fatalf("PutPrivate: %v", err)
	}
	defer s.DeletePrivate(ctx, key) //nolint:errcheck

	url, err := s.PresignGet(ctx, key, 1*time.Second)
	if err != nil {
		t.Fatalf("PresignGet: %v", err)
	}
	time.Sleep(2 * time.Second)
	resp, err := http.Get(url)
	if err != nil {
		t.Fatalf("GET: %v", err)
	}
	resp.Body.Close()
	if resp.StatusCode == http.StatusOK {
		t.Error("expected non-200 for expired presigned URL")
	}
}

// emptyAndDelete removes all objects and then the bucket. Silences all errors
// — this is best-effort test cleanup.
func emptyAndDelete(t *testing.T, s *S3Storage, bucket string) {
	t.Helper()
	ctx := context.Background()
	list, err := s.client.ListObjectsV2(ctx, &s3.ListObjectsV2Input{Bucket: aws.String(bucket)})
	if err == nil {
		for _, obj := range list.Contents {
			_, _ = s.client.DeleteObject(ctx, &s3.DeleteObjectInput{
				Bucket: aws.String(bucket),
				Key:    obj.Key,
			})
		}
	}
	_, _ = s.client.DeleteBucket(ctx, &s3.DeleteBucketInput{Bucket: aws.String(bucket)})
}

func getenv(k, fallback string) string {
	if v := os.Getenv(k); v != "" {
		return v
	}
	return fallback
}
