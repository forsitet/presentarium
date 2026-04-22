package storage

import (
	"bytes"
	"context"
	"errors"
	"strings"
	"testing"
	"time"
)

func TestMemStorage_PutDelete_Public(t *testing.T) {
	m := NewMemStorage("http://test.local/public")

	url, err := m.Put(context.Background(), "images/a.png", strings.NewReader("data"), "image/png")
	if err != nil {
		t.Fatalf("Put: %v", err)
	}
	if url != "http://test.local/public/images/a.png" {
		t.Errorf("unexpected URL: %q", url)
	}
	got, ok := m.ReadPublic("images/a.png")
	if !ok || !bytes.Equal(got, []byte("data")) {
		t.Errorf("ReadPublic mismatch: %q ok=%v", got, ok)
	}

	if err := m.Delete(context.Background(), "images/a.png"); err != nil {
		t.Fatalf("Delete: %v", err)
	}
	if _, ok := m.ReadPublic("images/a.png"); ok {
		t.Error("object should be gone after Delete")
	}
	// Idempotent
	if err := m.Delete(context.Background(), "images/a.png"); err != nil {
		t.Fatalf("second Delete: %v", err)
	}
}

func TestMemStorage_PresignGet_MissingReturnsErrNotFound(t *testing.T) {
	m := NewMemStorage("http://test.local")
	_, err := m.PresignGet(context.Background(), "does/not/exist", time.Minute)
	if !errors.Is(err, ErrNotFound) {
		t.Errorf("expected ErrNotFound, got %v", err)
	}
}

func TestMemStorage_PresignGet_PutPrivateRoundTrip(t *testing.T) {
	m := NewMemStorage("http://test.local")
	err := m.PutPrivate(context.Background(), "k.bin", strings.NewReader("hi"), "application/octet-stream")
	if err != nil {
		t.Fatalf("PutPrivate: %v", err)
	}
	url, err := m.PresignGet(context.Background(), "k.bin", 30*time.Second)
	if err != nil {
		t.Fatalf("PresignGet: %v", err)
	}
	if !strings.HasPrefix(url, "http://test.local/__presigned/k.bin?exp=") {
		t.Errorf("unexpected presign URL: %q", url)
	}
}
