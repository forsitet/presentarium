package storage

import (
	"context"
	"testing"
)

// TestPublicURL_WithBase verifies that an explicit PublicBaseURL is used
// verbatim and a trailing slash is stripped exactly once.
func TestPublicURL_WithBase(t *testing.T) {
	cases := []struct {
		name, configuredBase, key, want string
	}{
		{
			name:           "no_trailing_slash",
			configuredBase: "https://cdn.example.com/public",
			key:            "pres/abc/slide-1.webp",
			want:           "https://cdn.example.com/public/pres/abc/slide-1.webp",
		},
		{
			name:           "trailing_slash_is_trimmed",
			configuredBase: "https://cdn.example.com/public/",
			key:            "x.webp",
			want:           "https://cdn.example.com/public/x.webp",
		},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			s, err := NewS3(context.Background(), S3Config{
				BucketPublic:  "presentarium-public",
				BucketPrivate: "presentarium-private",
				PublicBaseURL: c.configuredBase,
			})
			if err != nil {
				t.Fatalf("NewS3: %v", err)
			}
			if got := s.PublicURL(c.key); got != c.want {
				t.Errorf("PublicURL(%q) = %q, want %q", c.key, got, c.want)
			}
		})
	}
}

// TestPublicURL_EndpointFallback — when PublicBaseURL is empty but Endpoint
// is set, PublicURL is built from {endpoint}/{bucket}.
func TestPublicURL_EndpointFallback(t *testing.T) {
	s, err := NewS3(context.Background(), S3Config{
		Endpoint:      "http://localhost:9000",
		BucketPublic:  "presentarium-public",
		BucketPrivate: "presentarium-private",
	})
	if err != nil {
		t.Fatalf("NewS3: %v", err)
	}
	got := s.PublicURL("img/y.png")
	want := "http://localhost:9000/presentarium-public/img/y.png"
	if got != want {
		t.Errorf("PublicURL = %q, want %q", got, want)
	}
}

// TestPublicURL_NoEndpointNoBase falls back to a bucket-relative path.
func TestPublicURL_NoEndpointNoBase(t *testing.T) {
	s, err := NewS3(context.Background(), S3Config{
		BucketPublic:  "bucket",
		BucketPrivate: "bucket-private",
	})
	if err != nil {
		t.Fatalf("NewS3: %v", err)
	}
	if got := s.PublicURL("a/b.webp"); got != "/bucket/a/b.webp" {
		t.Errorf("got %q", got)
	}
}

// TestNewS3_RequiresBuckets — constructor rejects missing bucket names.
func TestNewS3_RequiresBuckets(t *testing.T) {
	cases := []S3Config{
		{BucketPrivate: "p"},
		{BucketPublic: "p"},
		{},
	}
	for i, c := range cases {
		if _, err := NewS3(context.Background(), c); err == nil {
			t.Errorf("case %d: expected error for missing bucket name, got nil", i)
		}
	}
}
