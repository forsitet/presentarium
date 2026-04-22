package storage

import (
	"bytes"
	"context"
	"fmt"
	"io"
	"sync"
	"time"
)

// MemStorage is an in-memory Storage for unit and integration tests. It
// implements the full Storage interface against a map[string][]byte per
// bucket. Not suitable for production.
type MemStorage struct {
	mu       sync.RWMutex
	public   map[string][]byte
	private  map[string][]byte
	baseURL  string
	bucket   string
}

// NewMemStorage returns an empty MemStorage. The baseURL is used to build
// PublicURL() outputs; pass "http://test.local" or similar in tests.
func NewMemStorage(baseURL string) *MemStorage {
	return &MemStorage{
		public:  map[string][]byte{},
		private: map[string][]byte{},
		baseURL: baseURL,
		bucket:  "mem-public",
	}
}

func (m *MemStorage) Put(_ context.Context, key string, body io.Reader, _ string) (string, error) {
	data, err := io.ReadAll(body)
	if err != nil {
		return "", err
	}
	m.mu.Lock()
	m.public[key] = data
	m.mu.Unlock()
	return m.PublicURL(key), nil
}

func (m *MemStorage) PutPrivate(_ context.Context, key string, body io.Reader, _ string) error {
	data, err := io.ReadAll(body)
	if err != nil {
		return err
	}
	m.mu.Lock()
	m.private[key] = data
	m.mu.Unlock()
	return nil
}

func (m *MemStorage) Delete(_ context.Context, key string) error {
	m.mu.Lock()
	delete(m.public, key)
	m.mu.Unlock()
	return nil
}

func (m *MemStorage) DeletePrivate(_ context.Context, key string) error {
	m.mu.Lock()
	delete(m.private, key)
	m.mu.Unlock()
	return nil
}

// PresignGet returns a fake URL with an embedded expiry timestamp. Tests that
// care about expiry behaviour should assert against the URL string rather
// than do an actual HTTP fetch.
func (m *MemStorage) PresignGet(_ context.Context, key string, ttl time.Duration) (string, error) {
	m.mu.RLock()
	_, ok := m.private[key]
	m.mu.RUnlock()
	if !ok {
		return "", ErrNotFound
	}
	exp := time.Now().Add(ttl).Unix()
	return fmt.Sprintf("%s/__presigned/%s?exp=%d", m.baseURL, key, exp), nil
}

func (m *MemStorage) PublicURL(key string) string {
	if m.baseURL == "" {
		return "/" + m.bucket + "/" + key
	}
	return m.baseURL + "/" + key
}

// ReadPublic returns a copy of the bytes stored for a public key. Test-only.
func (m *MemStorage) ReadPublic(key string) ([]byte, bool) {
	m.mu.RLock()
	defer m.mu.RUnlock()
	v, ok := m.public[key]
	if !ok {
		return nil, false
	}
	return bytes.Clone(v), true
}

// ReadPrivate returns a copy of the bytes stored for a private key. Test-only.
func (m *MemStorage) ReadPrivate(key string) ([]byte, bool) {
	m.mu.RLock()
	defer m.mu.RUnlock()
	v, ok := m.private[key]
	if !ok {
		return nil, false
	}
	return bytes.Clone(v), true
}

// Reset clears all stored objects. Useful between tests.
func (m *MemStorage) Reset() {
	m.mu.Lock()
	m.public = map[string][]byte{}
	m.private = map[string][]byte{}
	m.mu.Unlock()
}

// Compile-time check that MemStorage satisfies Storage.
var _ Storage = (*MemStorage)(nil)
