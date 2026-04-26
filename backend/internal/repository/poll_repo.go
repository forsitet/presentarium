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

// PollRepository defines data access for polls.
type PollRepository interface {
	Create(ctx context.Context, poll *model.Poll) error
	GetByID(ctx context.Context, id uuid.UUID) (*model.Poll, error)
	ListByUser(ctx context.Context, userID uuid.UUID) ([]*model.Poll, error)
	Update(ctx context.Context, poll *model.Poll) error
	Delete(ctx context.Context, id uuid.UUID) error
}

type postgresPollRepo struct {
	db *sqlx.DB
}

// NewPostgresPollRepo creates a new PostgreSQL-backed PollRepository.
func NewPostgresPollRepo(db *sqlx.DB) PollRepository {
	return &postgresPollRepo{db: db}
}

func (r *postgresPollRepo) Create(ctx context.Context, poll *model.Poll) error {
	query := `INSERT INTO polls (id, user_id, title, description, scoring_rule, question_order, show_answer_distribution, created_at, updated_at)
              VALUES (:id, :user_id, :title, :description, :scoring_rule, :question_order, :show_answer_distribution, :created_at, :updated_at)`
	_, err := r.db.NamedExecContext(ctx, query, poll)
	return err
}

func (r *postgresPollRepo) GetByID(ctx context.Context, id uuid.UUID) (*model.Poll, error) {
	var poll model.Poll
	err := r.db.GetContext(ctx, &poll, "SELECT * FROM polls WHERE id = $1", id)
	if err != nil {
		if errors.Is(err, sql.ErrNoRows) {
			return nil, errs.ErrNotFound
		}
		return nil, err
	}
	return &poll, nil
}

func (r *postgresPollRepo) ListByUser(ctx context.Context, userID uuid.UUID) ([]*model.Poll, error) {
	var polls []*model.Poll
	err := r.db.SelectContext(ctx, &polls,
		"SELECT * FROM polls WHERE user_id = $1 ORDER BY created_at DESC", userID)
	if err != nil {
		return nil, err
	}
	return polls, nil
}

func (r *postgresPollRepo) Update(ctx context.Context, poll *model.Poll) error {
	query := `UPDATE polls SET title=:title, description=:description,
              scoring_rule=:scoring_rule, question_order=:question_order,
              show_answer_distribution=:show_answer_distribution, updated_at=:updated_at
              WHERE id=:id`
	res, err := r.db.NamedExecContext(ctx, query, poll)
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

func (r *postgresPollRepo) Delete(ctx context.Context, id uuid.UUID) error {
	res, err := r.db.ExecContext(ctx, "DELETE FROM polls WHERE id = $1", id)
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
