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

// QuestionRepository defines data access for questions.
type QuestionRepository interface {
	Create(ctx context.Context, q *model.Question) error
	GetByID(ctx context.Context, id uuid.UUID) (*model.Question, error)
	ListByPoll(ctx context.Context, pollID uuid.UUID) ([]*model.Question, error)
	Update(ctx context.Context, q *model.Question) error
	Delete(ctx context.Context, id uuid.UUID) error
	// Reorder updates position for a batch of questions within a poll.
	// items maps question ID → new position.
	Reorder(ctx context.Context, pollID uuid.UUID, items []ReorderItem) error
	// MaxPosition returns the highest position value in a poll (or -1 if none).
	MaxPosition(ctx context.Context, pollID uuid.UUID) (int, error)
}

// ReorderItem holds a question ID and its new position.
type ReorderItem struct {
	ID       uuid.UUID
	Position int
}

type postgresQuestionRepo struct {
	db *sqlx.DB
}

// NewPostgresQuestionRepo creates a new PostgreSQL-backed QuestionRepository.
func NewPostgresQuestionRepo(db *sqlx.DB) QuestionRepository {
	return &postgresQuestionRepo{db: db}
}

func (r *postgresQuestionRepo) Create(ctx context.Context, q *model.Question) error {
	query := `INSERT INTO questions
		(id, poll_id, type, text, options, time_limit_seconds, points, position, created_at, updated_at)
		VALUES (:id, :poll_id, :type, :text, :options, :time_limit_seconds, :points, :position, :created_at, :updated_at)`
	_, err := r.db.NamedExecContext(ctx, query, q)
	return err
}

func (r *postgresQuestionRepo) GetByID(ctx context.Context, id uuid.UUID) (*model.Question, error) {
	var q model.Question
	err := r.db.GetContext(ctx, &q, "SELECT * FROM questions WHERE id = $1", id)
	if err != nil {
		if errors.Is(err, sql.ErrNoRows) {
			return nil, errs.ErrNotFound
		}
		return nil, err
	}
	return &q, nil
}

func (r *postgresQuestionRepo) ListByPoll(ctx context.Context, pollID uuid.UUID) ([]*model.Question, error) {
	var questions []*model.Question
	err := r.db.SelectContext(ctx, &questions,
		"SELECT * FROM questions WHERE poll_id = $1 ORDER BY position ASC, created_at ASC", pollID)
	if err != nil {
		return nil, err
	}
	return questions, nil
}

func (r *postgresQuestionRepo) Update(ctx context.Context, q *model.Question) error {
	query := `UPDATE questions SET
		type=:type, text=:text, options=:options,
		time_limit_seconds=:time_limit_seconds, points=:points, position=:position, updated_at=:updated_at
		WHERE id=:id`
	res, err := r.db.NamedExecContext(ctx, query, q)
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

func (r *postgresQuestionRepo) Delete(ctx context.Context, id uuid.UUID) error {
	res, err := r.db.ExecContext(ctx, "DELETE FROM questions WHERE id = $1", id)
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

func (r *postgresQuestionRepo) Reorder(ctx context.Context, pollID uuid.UUID, items []ReorderItem) error {
	tx, err := r.db.BeginTxx(ctx, nil)
	if err != nil {
		return err
	}
	defer tx.Rollback() //nolint:errcheck

	for _, item := range items {
		res, err := tx.ExecContext(ctx,
			"UPDATE questions SET position=$1, updated_at=NOW() WHERE id=$2 AND poll_id=$3",
			item.Position, item.ID, pollID)
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
	}
	return tx.Commit()
}

func (r *postgresQuestionRepo) MaxPosition(ctx context.Context, pollID uuid.UUID) (int, error) {
	var max sql.NullInt64
	err := r.db.GetContext(ctx, &max,
		"SELECT MAX(position) FROM questions WHERE poll_id = $1", pollID)
	if err != nil {
		return -1, err
	}
	if !max.Valid {
		return -1, nil
	}
	return int(max.Int64), nil
}
