package repository

import (
	"context"
	"database/sql"
	"errors"

	"github.com/google/uuid"
	"github.com/jmoiron/sqlx"

	"presentarium/internal/errs"
	"presentarium/internal/model"
)

// PresentationRepository defines data access for presentations and their
// slides.
type PresentationRepository interface {
	Create(ctx context.Context, p *model.Presentation) error
	GetByID(ctx context.Context, id uuid.UUID) (*model.Presentation, error)
	ListByUser(ctx context.Context, userID uuid.UUID) ([]*model.Presentation, error)
	Delete(ctx context.Context, id uuid.UUID) error

	// MarkReady finalises a successful conversion.
	MarkReady(ctx context.Context, id uuid.UUID, slideCount int) error
	// MarkFailed records an unrecoverable conversion error.
	MarkFailed(ctx context.Context, id uuid.UUID, errMsg string) error

	// ListSlides returns all slides for a presentation ordered by position.
	ListSlides(ctx context.Context, presentationID uuid.UUID) ([]*model.PresentationSlide, error)
	// ReplaceSlides atomically removes all existing slides for a presentation
	// and inserts the given ones. Used at the end of a conversion.
	ReplaceSlides(ctx context.Context, presentationID uuid.UUID, slides []*model.PresentationSlide) error
}

type postgresPresentationRepo struct {
	db *sqlx.DB
}

// NewPostgresPresentationRepo creates a PostgreSQL-backed PresentationRepository.
func NewPostgresPresentationRepo(db *sqlx.DB) PresentationRepository {
	return &postgresPresentationRepo{db: db}
}

func (r *postgresPresentationRepo) Create(ctx context.Context, p *model.Presentation) error {
	query := `INSERT INTO presentations
	              (id, user_id, title, original_filename, source_key, slide_count,
	               status, error_message, created_at, updated_at)
	          VALUES
	              (:id, :user_id, :title, :original_filename, :source_key, :slide_count,
	               :status, :error_message, :created_at, :updated_at)`
	_, err := r.db.NamedExecContext(ctx, query, p)
	return err
}

func (r *postgresPresentationRepo) GetByID(ctx context.Context, id uuid.UUID) (*model.Presentation, error) {
	var p model.Presentation
	err := r.db.GetContext(ctx, &p, "SELECT * FROM presentations WHERE id = $1", id)
	if err != nil {
		if errors.Is(err, sql.ErrNoRows) {
			return nil, errs.ErrNotFound
		}
		return nil, err
	}
	return &p, nil
}

func (r *postgresPresentationRepo) ListByUser(ctx context.Context, userID uuid.UUID) ([]*model.Presentation, error) {
	var out []*model.Presentation
	err := r.db.SelectContext(ctx, &out,
		"SELECT * FROM presentations WHERE user_id = $1 ORDER BY created_at DESC", userID)
	if err != nil {
		return nil, err
	}
	return out, nil
}

func (r *postgresPresentationRepo) Delete(ctx context.Context, id uuid.UUID) error {
	res, err := r.db.ExecContext(ctx, "DELETE FROM presentations WHERE id = $1", id)
	if err != nil {
		return err
	}
	n, err := res.RowsAffected()
	if err != nil {
		return err
	}
	if n == 0 {
		return errs.ErrNotFound
	}
	return nil
}

func (r *postgresPresentationRepo) MarkReady(ctx context.Context, id uuid.UUID, slideCount int) error {
	_, err := r.db.ExecContext(ctx,
		`UPDATE presentations
		     SET status = 'ready', slide_count = $2, error_message = '', updated_at = NOW()
		     WHERE id = $1`,
		id, slideCount)
	return err
}

func (r *postgresPresentationRepo) MarkFailed(ctx context.Context, id uuid.UUID, errMsg string) error {
	// Truncate to the column size; error strings from exec pipelines can be huge.
	if len(errMsg) > 4000 {
		errMsg = errMsg[:4000]
	}
	_, err := r.db.ExecContext(ctx,
		`UPDATE presentations
		     SET status = 'failed', error_message = $2, updated_at = NOW()
		     WHERE id = $1`,
		id, errMsg)
	return err
}

func (r *postgresPresentationRepo) ListSlides(ctx context.Context, presentationID uuid.UUID) ([]*model.PresentationSlide, error) {
	var out []*model.PresentationSlide
	err := r.db.SelectContext(ctx, &out,
		`SELECT * FROM presentation_slides
		     WHERE presentation_id = $1
		     ORDER BY position`,
		presentationID)
	if err != nil {
		return nil, err
	}
	return out, nil
}

func (r *postgresPresentationRepo) ReplaceSlides(ctx context.Context, presentationID uuid.UUID, slides []*model.PresentationSlide) error {
	tx, err := r.db.BeginTxx(ctx, nil)
	if err != nil {
		return err
	}
	defer tx.Rollback() //nolint:errcheck

	if _, err := tx.ExecContext(ctx,
		"DELETE FROM presentation_slides WHERE presentation_id = $1", presentationID); err != nil {
		return err
	}

	if len(slides) > 0 {
		query := `INSERT INTO presentation_slides
		              (id, presentation_id, position, image_key, thumb_key,
		               width, height, notes, created_at)
		          VALUES
		              (:id, :presentation_id, :position, :image_key, :thumb_key,
		               :width, :height, :notes, :created_at)`
		if _, err := tx.NamedExecContext(ctx, query, slides); err != nil {
			return err
		}
	}

	return tx.Commit()
}
