package service

import (
	"bytes"
	"context"
	"errors"
	"fmt"
	"log/slog"
	"time"

	"github.com/google/uuid"

	"presentarium/internal/errs"
	"presentarium/internal/model"
	"presentarium/internal/repository"
	"presentarium/internal/storage"
	"presentarium/pkg/pptx"
)

// PresentationService orchestrates the .pptx upload + async conversion flow.
type PresentationService interface {
	// Create persists the uploaded source, returns the presentation row
	// (status="processing") and launches a background conversion worker.
	Create(ctx context.Context, userID uuid.UUID, req CreatePresentationRequest) (*model.Presentation, error)
	// Get returns the presentation with its slides + public image URLs.
	Get(ctx context.Context, userID, id uuid.UUID) (*PresentationDetail, error)
	// GetForWS is like Get but without ownership check (used by the WS layer
	// which authorises via session membership instead).
	GetForWS(ctx context.Context, id uuid.UUID) (*PresentationDetail, error)
	List(ctx context.Context, userID uuid.UUID) ([]*model.Presentation, error)
	Delete(ctx context.Context, userID, id uuid.UUID) error
}

// CreatePresentationRequest is the service-level input for uploading a .pptx.
// Source holds the full file bytes (the handler must enforce the size cap
// before passing it in).
type CreatePresentationRequest struct {
	Title            string
	OriginalFilename string
	Source           []byte
}

// PresentationDetail is the API response shape for a single presentation.
// It embeds the Presentation and adds the expanded slide list with ready-to-use
// image URLs (built via Storage.PublicURL so the CDN host can be swapped
// without a data migration).
type PresentationDetail struct {
	*model.Presentation
	Slides []SlideDTO `json:"slides"`
}

// SlideDTO is the per-slide projection sent to the frontend.
type SlideDTO struct {
	ID       uuid.UUID `json:"id"`
	Position int       `json:"position"`
	ImageURL string    `json:"image_url"`
	ThumbURL string    `json:"thumb_url,omitempty"`
	Width    int       `json:"width"`
	Height   int       `json:"height"`
}

type presentationService struct {
	repo      repository.PresentationRepository
	storage   storage.Storage
	converter pptx.Converter
	logger    *slog.Logger
}

// NewPresentationService creates a PresentationService. A non-nil converter
// is required; production uses pptx.NewCLIConverter, tests inject a fake.
func NewPresentationService(
	repo repository.PresentationRepository,
	store storage.Storage,
	converter pptx.Converter,
) PresentationService {
	return &presentationService{
		repo:      repo,
		storage:   store,
		converter: converter,
		logger:    slog.Default(),
	}
}

// Conversion timeout for the background worker. Covers even large decks; if
// exceeded the worker marks the presentation failed.
const conversionTimeout = 10 * time.Minute

// sourceKey returns the S3 key under which a presentation's raw .pptx is
// stored in the private bucket.
func sourceKey(id uuid.UUID) string {
	return fmt.Sprintf("presentations/sources/%s.pptx", id)
}

// slideKey returns the S3 key under which a rendered slide image is stored
// in the public bucket. The key embeds the presentation ID so deletions
// become a simple prefix sweep.
func slideKey(presentationID uuid.UUID, position int) string {
	return fmt.Sprintf("presentations/%s/slides/%03d.webp", presentationID, position)
}

func (s *presentationService) Create(ctx context.Context, userID uuid.UUID, req CreatePresentationRequest) (*model.Presentation, error) {
	if len(req.Source) == 0 {
		return nil, errs.ErrValidation
	}
	title := req.Title
	if title == "" {
		title = req.OriginalFilename
	}
	if title == "" {
		title = "Untitled"
	}
	if len(title) > 255 {
		title = title[:255]
	}

	now := time.Now().UTC()
	id := uuid.New()
	key := sourceKey(id)

	if err := s.storage.PutPrivate(ctx, key, bytes.NewReader(req.Source),
		"application/vnd.openxmlformats-officedocument.presentationml.presentation"); err != nil {
		return nil, fmt.Errorf("upload source: %w", err)
	}

	p := &model.Presentation{
		ID:               id,
		UserID:           userID,
		Title:            title,
		OriginalFilename: req.OriginalFilename,
		SourceKey:        key,
		SlideCount:       0,
		Status:           "processing",
		ErrorMessage:     "",
		CreatedAt:        now,
		UpdatedAt:        now,
	}
	if err := s.repo.Create(ctx, p); err != nil {
		// Best-effort cleanup of the orphaned source object.
		_ = s.storage.DeletePrivate(context.Background(), key)
		return nil, fmt.Errorf("insert presentation: %w", err)
	}

	// Kick off the conversion worker. Use a fresh background context so the
	// worker is NOT tied to the HTTP request lifetime.
	src := append([]byte(nil), req.Source...)
	go s.convertWorker(id, src)

	return p, nil
}

// convertWorker runs the full conversion pipeline and persists the result.
// Errors are logged and recorded on the presentation row as status=failed.
// Runs in its own goroutine so HTTP handlers return immediately.
func (s *presentationService) convertWorker(id uuid.UUID, source []byte) {
	ctx, cancel := context.WithTimeout(context.Background(), conversionTimeout)
	defer cancel()

	s.logger.Info("presentation conversion started", "id", id, "bytes", len(source))

	slides, err := s.converter.Convert(ctx, bytes.NewReader(source))
	if err != nil {
		s.logger.Error("presentation conversion failed", "id", id, "error", err)
		if markErr := s.repo.MarkFailed(ctx, id, err.Error()); markErr != nil {
			s.logger.Error("mark failed", "id", id, "error", markErr)
		}
		return
	}

	// Upload every rendered slide to the public bucket and build DB rows.
	slideRows := make([]*model.PresentationSlide, 0, len(slides))
	uploadedKeys := make([]string, 0, len(slides))
	now := time.Now().UTC()
	for _, sl := range slides {
		key := slideKey(id, sl.Position)
		if _, err := s.storage.Put(ctx, key, bytes.NewReader(sl.WebP), "image/webp"); err != nil {
			s.logger.Error("upload slide", "id", id, "position", sl.Position, "error", err)
			// Roll back previously uploaded slides before failing.
			for _, k := range uploadedKeys {
				_ = s.storage.Delete(context.Background(), k)
			}
			if markErr := s.repo.MarkFailed(ctx, id, fmt.Sprintf("upload slide %d: %v", sl.Position, err)); markErr != nil {
				s.logger.Error("mark failed", "id", id, "error", markErr)
			}
			return
		}
		uploadedKeys = append(uploadedKeys, key)
		slideRows = append(slideRows, &model.PresentationSlide{
			ID:             uuid.New(),
			PresentationID: id,
			Position:       sl.Position,
			ImageKey:       key,
			ThumbKey:       "",
			Width:          sl.Width,
			Height:         sl.Height,
			CreatedAt:      now,
		})
	}

	if err := s.repo.ReplaceSlides(ctx, id, slideRows); err != nil {
		s.logger.Error("replace slides", "id", id, "error", err)
		for _, k := range uploadedKeys {
			_ = s.storage.Delete(context.Background(), k)
		}
		if markErr := s.repo.MarkFailed(ctx, id, "persist slides: "+err.Error()); markErr != nil {
			s.logger.Error("mark failed", "id", id, "error", markErr)
		}
		return
	}

	if err := s.repo.MarkReady(ctx, id, len(slideRows)); err != nil {
		s.logger.Error("mark ready", "id", id, "error", err)
		return
	}
	s.logger.Info("presentation conversion ready", "id", id, "slides", len(slideRows))
}

func (s *presentationService) Get(ctx context.Context, userID, id uuid.UUID) (*PresentationDetail, error) {
	p, err := s.repo.GetByID(ctx, id)
	if err != nil {
		return nil, err
	}
	if p.UserID != userID {
		return nil, errs.ErrForbidden
	}
	return s.buildDetail(ctx, p)
}

func (s *presentationService) GetForWS(ctx context.Context, id uuid.UUID) (*PresentationDetail, error) {
	p, err := s.repo.GetByID(ctx, id)
	if err != nil {
		return nil, err
	}
	return s.buildDetail(ctx, p)
}

func (s *presentationService) buildDetail(ctx context.Context, p *model.Presentation) (*PresentationDetail, error) {
	slides, err := s.repo.ListSlides(ctx, p.ID)
	if err != nil {
		return nil, err
	}
	dtos := make([]SlideDTO, 0, len(slides))
	for _, sl := range slides {
		d := SlideDTO{
			ID:       sl.ID,
			Position: sl.Position,
			ImageURL: s.storage.PublicURL(sl.ImageKey),
			Width:    sl.Width,
			Height:   sl.Height,
		}
		if sl.ThumbKey != "" {
			d.ThumbURL = s.storage.PublicURL(sl.ThumbKey)
		}
		dtos = append(dtos, d)
	}
	return &PresentationDetail{Presentation: p, Slides: dtos}, nil
}

func (s *presentationService) List(ctx context.Context, userID uuid.UUID) ([]*model.Presentation, error) {
	return s.repo.ListByUser(ctx, userID)
}

func (s *presentationService) Delete(ctx context.Context, userID, id uuid.UUID) error {
	p, err := s.repo.GetByID(ctx, id)
	if err != nil {
		return err
	}
	if p.UserID != userID {
		return errs.ErrForbidden
	}

	// Collect slide keys BEFORE deleting the DB row (cascade removes them).
	slides, err := s.repo.ListSlides(ctx, id)
	if err != nil {
		return err
	}

	if err := s.repo.Delete(ctx, id); err != nil {
		return err
	}

	// Best-effort storage cleanup; DB is the source of truth so storage
	// orphans are non-fatal.
	if p.SourceKey != "" {
		if err := s.storage.DeletePrivate(ctx, p.SourceKey); err != nil && !errors.Is(err, storage.ErrNotFound) {
			s.logger.Warn("delete source", "id", id, "error", err)
		}
	}
	for _, sl := range slides {
		if err := s.storage.Delete(ctx, sl.ImageKey); err != nil && !errors.Is(err, storage.ErrNotFound) {
			s.logger.Warn("delete slide", "id", id, "key", sl.ImageKey, "error", err)
		}
	}
	return nil
}
