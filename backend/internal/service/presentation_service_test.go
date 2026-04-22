package service

import (
	"context"
	"errors"
	"sync"
	"testing"
	"time"

	"github.com/google/uuid"

	"presentarium/internal/errs"
	"presentarium/internal/model"
	"presentarium/internal/storage"
	"presentarium/pkg/pptx"
	"presentarium/pkg/pptx/pptxtest"
)

// ─── In-memory repository stub ────────────────────────────────────────────────

// memPresentationRepo is a trivial in-memory PresentationRepository for unit
// tests. It mirrors the parts of the Postgres repo the service actually
// exercises; each test creates a fresh instance.
type memPresentationRepo struct {
	mu     sync.Mutex
	items  map[uuid.UUID]*model.Presentation
	slides map[uuid.UUID][]*model.PresentationSlide
}

func newMemPresentationRepo() *memPresentationRepo {
	return &memPresentationRepo{
		items:  map[uuid.UUID]*model.Presentation{},
		slides: map[uuid.UUID][]*model.PresentationSlide{},
	}
}

func (r *memPresentationRepo) Create(_ context.Context, p *model.Presentation) error {
	r.mu.Lock()
	defer r.mu.Unlock()
	cp := *p
	r.items[p.ID] = &cp
	return nil
}

func (r *memPresentationRepo) GetByID(_ context.Context, id uuid.UUID) (*model.Presentation, error) {
	r.mu.Lock()
	defer r.mu.Unlock()
	p, ok := r.items[id]
	if !ok {
		return nil, errs.ErrNotFound
	}
	cp := *p
	return &cp, nil
}

func (r *memPresentationRepo) ListByUser(_ context.Context, userID uuid.UUID) ([]*model.Presentation, error) {
	r.mu.Lock()
	defer r.mu.Unlock()
	var out []*model.Presentation
	for _, p := range r.items {
		if p.UserID == userID {
			cp := *p
			out = append(out, &cp)
		}
	}
	return out, nil
}

func (r *memPresentationRepo) Delete(_ context.Context, id uuid.UUID) error {
	r.mu.Lock()
	defer r.mu.Unlock()
	if _, ok := r.items[id]; !ok {
		return errs.ErrNotFound
	}
	delete(r.items, id)
	delete(r.slides, id)
	return nil
}

func (r *memPresentationRepo) MarkReady(_ context.Context, id uuid.UUID, slideCount int) error {
	r.mu.Lock()
	defer r.mu.Unlock()
	p, ok := r.items[id]
	if !ok {
		return errs.ErrNotFound
	}
	p.Status = "ready"
	p.SlideCount = slideCount
	p.ErrorMessage = ""
	return nil
}

func (r *memPresentationRepo) MarkFailed(_ context.Context, id uuid.UUID, errMsg string) error {
	r.mu.Lock()
	defer r.mu.Unlock()
	p, ok := r.items[id]
	if !ok {
		return errs.ErrNotFound
	}
	p.Status = "failed"
	p.ErrorMessage = errMsg
	return nil
}

func (r *memPresentationRepo) ListSlides(_ context.Context, presentationID uuid.UUID) ([]*model.PresentationSlide, error) {
	r.mu.Lock()
	defer r.mu.Unlock()
	out := make([]*model.PresentationSlide, len(r.slides[presentationID]))
	for i, s := range r.slides[presentationID] {
		cp := *s
		out[i] = &cp
	}
	return out, nil
}

func (r *memPresentationRepo) ReplaceSlides(_ context.Context, presentationID uuid.UUID, slides []*model.PresentationSlide) error {
	r.mu.Lock()
	defer r.mu.Unlock()
	copyOf := make([]*model.PresentationSlide, len(slides))
	for i, s := range slides {
		cp := *s
		copyOf[i] = &cp
	}
	r.slides[presentationID] = copyOf
	return nil
}

// ─── Tests ────────────────────────────────────────────────────────────────────

// waitFor polls the presentation status until it becomes final (ready/failed)
// or the deadline elapses. Keeps tests fast and deterministic.
func waitFor(t *testing.T, repo *memPresentationRepo, id uuid.UUID, wantStatus string, timeout time.Duration) *model.Presentation {
	t.Helper()
	deadline := time.Now().Add(timeout)
	for time.Now().Before(deadline) {
		p, err := repo.GetByID(context.Background(), id)
		if err == nil && p.Status == wantStatus {
			return p
		}
		time.Sleep(5 * time.Millisecond)
	}
	t.Fatalf("timed out waiting for status=%s", wantStatus)
	return nil
}

func TestPresentationService_CreateAndConvert_Success(t *testing.T) {
	repo := newMemPresentationRepo()
	store := storage.NewMemStorage("http://test.local/public")
	svc := NewPresentationService(repo, store, &pptxtest.FakeConverter{SlideCount: 4, Width: 800, Height: 600})

	userID := uuid.New()
	// A pretend .pptx: the fake converter doesn't care about contents.
	src := []byte("PK\x03\x04fake-pptx-bytes")

	p, err := svc.Create(context.Background(), userID, CreatePresentationRequest{
		Title:            "Demo Deck",
		OriginalFilename: "demo.pptx",
		Source:           src,
	})
	if err != nil {
		t.Fatalf("Create: %v", err)
	}
	if p.Status != "processing" {
		t.Fatalf("status=%q want processing", p.Status)
	}
	if _, ok := store.ReadPrivate(p.SourceKey); !ok {
		t.Fatalf("source not uploaded to private bucket under %q", p.SourceKey)
	}

	ready := waitFor(t, repo, p.ID, "ready", 2*time.Second)
	if ready.SlideCount != 4 {
		t.Errorf("SlideCount=%d want 4", ready.SlideCount)
	}

	detail, err := svc.Get(context.Background(), userID, p.ID)
	if err != nil {
		t.Fatalf("Get: %v", err)
	}
	if len(detail.Slides) != 4 {
		t.Fatalf("len(Slides)=%d want 4", len(detail.Slides))
	}
	for i, s := range detail.Slides {
		if s.Position != i+1 {
			t.Errorf("slide %d Position=%d", i, s.Position)
		}
		if s.ImageURL == "" {
			t.Errorf("slide %d missing ImageURL", i)
		}
		if s.Width != 800 || s.Height != 600 {
			t.Errorf("slide %d dims %dx%d", i, s.Width, s.Height)
		}
	}
}

func TestPresentationService_CreateAndConvert_Failure(t *testing.T) {
	repo := newMemPresentationRepo()
	store := storage.NewMemStorage("http://test.local/public")
	svc := NewPresentationService(repo, store, &pptxtest.FakeConverter{Err: errors.New("boom")})

	userID := uuid.New()
	p, err := svc.Create(context.Background(), userID, CreatePresentationRequest{
		OriginalFilename: "broken.pptx",
		Source:           []byte("PK"),
	})
	if err != nil {
		t.Fatalf("Create: %v", err)
	}

	failed := waitFor(t, repo, p.ID, "failed", 2*time.Second)
	if failed.ErrorMessage == "" {
		t.Error("ErrorMessage not set on failure")
	}
}

func TestPresentationService_Delete_CleansStorage(t *testing.T) {
	repo := newMemPresentationRepo()
	store := storage.NewMemStorage("http://test.local/public")
	svc := NewPresentationService(repo, store, &pptxtest.FakeConverter{SlideCount: 2})

	userID := uuid.New()
	p, err := svc.Create(context.Background(), userID, CreatePresentationRequest{
		OriginalFilename: "x.pptx",
		Source:           []byte("PK\x03\x04..."),
	})
	if err != nil {
		t.Fatalf("Create: %v", err)
	}
	waitFor(t, repo, p.ID, "ready", 2*time.Second)

	// Gather the slide keys so we can assert they disappear.
	detail, err := svc.Get(context.Background(), userID, p.ID)
	if err != nil {
		t.Fatalf("Get: %v", err)
	}
	var publicKeys []string
	for _, sl := range detail.Slides {
		// Strip base URL to get the key back.
		key := sl.ImageURL[len("http://test.local/public/"):]
		publicKeys = append(publicKeys, key)
	}

	if err := svc.Delete(context.Background(), userID, p.ID); err != nil {
		t.Fatalf("Delete: %v", err)
	}

	if _, ok := store.ReadPrivate(p.SourceKey); ok {
		t.Error("source still present after Delete")
	}
	for _, k := range publicKeys {
		if _, ok := store.ReadPublic(k); ok {
			t.Errorf("slide key %s still present after Delete", k)
		}
	}
	if _, err := repo.GetByID(context.Background(), p.ID); err == nil {
		t.Error("repo row still present after Delete")
	}
}

func TestPresentationService_Get_ForbidsOtherUsers(t *testing.T) {
	repo := newMemPresentationRepo()
	store := storage.NewMemStorage("http://test.local/public")
	svc := NewPresentationService(repo, store, &pptxtest.FakeConverter{})

	owner := uuid.New()
	intruder := uuid.New()
	p, err := svc.Create(context.Background(), owner, CreatePresentationRequest{
		OriginalFilename: "x.pptx", Source: []byte("PK\x03\x04"),
	})
	if err != nil {
		t.Fatalf("Create: %v", err)
	}
	waitFor(t, repo, p.ID, "ready", 2*time.Second)

	if _, err := svc.Get(context.Background(), intruder, p.ID); !errors.Is(err, errs.ErrForbidden) {
		t.Fatalf("Get by intruder: want ErrForbidden, got %v", err)
	}
}

// Compile-time check that our test stub satisfies the repository interface.
// Keeps the test in lockstep with the interface if new methods are added.
var _ interface {
	Create(context.Context, *model.Presentation) error
} = (*memPresentationRepo)(nil)

// Ensure the pptxtest.FakeConverter still satisfies pptx.Converter.
var _ pptx.Converter = (*pptxtest.FakeConverter)(nil)
