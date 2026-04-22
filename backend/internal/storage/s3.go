package storage

import (
	"context"
	"errors"
	"fmt"
	"io"
	"strings"
	"time"

	"github.com/aws/aws-sdk-go-v2/aws"
	awsconfig "github.com/aws/aws-sdk-go-v2/config"
	"github.com/aws/aws-sdk-go-v2/credentials"
	"github.com/aws/aws-sdk-go-v2/service/s3"
	s3types "github.com/aws/aws-sdk-go-v2/service/s3/types"
)

// S3Config holds connection parameters for an S3-compatible backend.
// For MinIO: set Endpoint + ForcePathStyle=true.
// For AWS: leave Endpoint empty, ForcePathStyle=false.
// For Cloudflare R2: set Endpoint to the R2 endpoint URL, ForcePathStyle=true.
type S3Config struct {
	Endpoint       string // http://localhost:9000 for MinIO; empty for AWS
	Region         string // "us-east-1" works for MinIO
	AccessKeyID    string
	SecretKey      string
	BucketPublic   string
	BucketPrivate  string
	PublicBaseURL  string // e.g. https://cdn.example.com/public-bucket; falls back to {endpoint}/{bucket}
	ForcePathStyle bool   // true for MinIO/R2; false for native AWS S3
}

// S3Storage is an S3-compatible Storage implementation.
type S3Storage struct {
	cfg     S3Config
	client  *s3.Client
	presign *s3.PresignClient
	pubBase string
}

// NewS3 constructs an S3Storage. No network calls are made here; bucket
// existence is checked lazily (or via EnsureBuckets).
func NewS3(ctx context.Context, cfg S3Config) (*S3Storage, error) {
	if cfg.BucketPublic == "" || cfg.BucketPrivate == "" {
		return nil, fmt.Errorf("storage: bucket names must be set")
	}
	if cfg.Region == "" {
		cfg.Region = "us-east-1"
	}

	awsCfg, err := awsconfig.LoadDefaultConfig(ctx,
		awsconfig.WithRegion(cfg.Region),
		awsconfig.WithCredentialsProvider(credentials.NewStaticCredentialsProvider(
			cfg.AccessKeyID, cfg.SecretKey, "",
		)),
	)
	if err != nil {
		return nil, fmt.Errorf("storage: load aws config: %w", err)
	}

	client := s3.NewFromConfig(awsCfg, func(o *s3.Options) {
		if cfg.Endpoint != "" {
			o.BaseEndpoint = aws.String(cfg.Endpoint)
		}
		o.UsePathStyle = cfg.ForcePathStyle
	})

	pubBase := strings.TrimRight(cfg.PublicBaseURL, "/")
	if pubBase == "" && cfg.Endpoint != "" {
		pubBase = strings.TrimRight(cfg.Endpoint, "/") + "/" + cfg.BucketPublic
	}

	return &S3Storage{
		cfg:     cfg,
		client:  client,
		presign: s3.NewPresignClient(client),
		pubBase: pubBase,
	}, nil
}

// EnsureBuckets creates the configured buckets if they are missing. Safe to
// call at startup — idempotent. Prefer running this via a one-shot init
// container in prod and leaving it to startup only in dev.
func (s *S3Storage) EnsureBuckets(ctx context.Context) error {
	for _, b := range []string{s.cfg.BucketPublic, s.cfg.BucketPrivate} {
		if _, err := s.client.HeadBucket(ctx, &s3.HeadBucketInput{Bucket: aws.String(b)}); err == nil {
			continue
		}
		_, cerr := s.client.CreateBucket(ctx, &s3.CreateBucketInput{Bucket: aws.String(b)})
		if cerr == nil {
			continue
		}
		var already *s3types.BucketAlreadyOwnedByYou
		var exists *s3types.BucketAlreadyExists
		if !errors.As(cerr, &already) && !errors.As(cerr, &exists) {
			return fmt.Errorf("storage: create bucket %q: %w", b, cerr)
		}
	}
	return nil
}

// Put uploads a public object and returns its public URL.
func (s *S3Storage) Put(ctx context.Context, key string, body io.Reader, contentType string) (string, error) {
	if err := s.put(ctx, s.cfg.BucketPublic, key, body, contentType); err != nil {
		return "", err
	}
	return s.PublicURL(key), nil
}

// PutPrivate uploads a private object.
func (s *S3Storage) PutPrivate(ctx context.Context, key string, body io.Reader, contentType string) error {
	return s.put(ctx, s.cfg.BucketPrivate, key, body, contentType)
}

func (s *S3Storage) put(ctx context.Context, bucket, key string, body io.Reader, contentType string) error {
	if contentType == "" {
		contentType = "application/octet-stream"
	}
	if _, err := s.client.PutObject(ctx, &s3.PutObjectInput{
		Bucket:      aws.String(bucket),
		Key:         aws.String(key),
		Body:        body,
		ContentType: aws.String(contentType),
	}); err != nil {
		return fmt.Errorf("storage: put %s/%s: %w", bucket, key, err)
	}
	return nil
}

// Delete removes a public object.
func (s *S3Storage) Delete(ctx context.Context, key string) error {
	return s.delete(ctx, s.cfg.BucketPublic, key)
}

// DeletePrivate removes a private object.
func (s *S3Storage) DeletePrivate(ctx context.Context, key string) error {
	return s.delete(ctx, s.cfg.BucketPrivate, key)
}

func (s *S3Storage) delete(ctx context.Context, bucket, key string) error {
	if _, err := s.client.DeleteObject(ctx, &s3.DeleteObjectInput{
		Bucket: aws.String(bucket),
		Key:    aws.String(key),
	}); err != nil {
		return fmt.Errorf("storage: delete %s/%s: %w", bucket, key, err)
	}
	return nil
}

// PresignGet returns a presigned URL for downloading a private object.
func (s *S3Storage) PresignGet(ctx context.Context, key string, ttl time.Duration) (string, error) {
	req, err := s.presign.PresignGetObject(ctx,
		&s3.GetObjectInput{
			Bucket: aws.String(s.cfg.BucketPrivate),
			Key:    aws.String(key),
		},
		s3.WithPresignExpires(ttl),
	)
	if err != nil {
		return "", fmt.Errorf("storage: presign get %s: %w", key, err)
	}
	return req.URL, nil
}

// PublicURL builds the direct URL for a public object. Keys must consist of
// URL-safe characters (we only generate UUID-based keys, so this holds).
func (s *S3Storage) PublicURL(key string) string {
	if s.pubBase == "" {
		return "/" + s.cfg.BucketPublic + "/" + key
	}
	return s.pubBase + "/" + key
}
