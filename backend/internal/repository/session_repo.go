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

// SessionRepository defines data access for sessions.
type SessionRepository interface {
	Create(ctx context.Context, session *model.Session) error
	GetByCode(ctx context.Context, code string) (*model.Session, error)
	GetActiveByPoll(ctx context.Context, pollID uuid.UUID) (*model.Session, error)
	UpdateStatus(ctx context.Context, sessionID uuid.UUID, status string, startedAt, finishedAt *string) error
}

type postgresSessionRepo struct {
	db *sqlx.DB
}

// NewPostgresSessionRepo creates a new PostgreSQL-backed SessionRepository.
func NewPostgresSessionRepo(db *sqlx.DB) SessionRepository {
	return &postgresSessionRepo{db: db}
}

func (r *postgresSessionRepo) Create(ctx context.Context, session *model.Session) error {
	query := `INSERT INTO sessions (id, poll_id, room_code, status, created_at)
	          VALUES ($1, $2, $3, $4, $5)`
	_, err := r.db.ExecContext(ctx, query,
		session.ID, session.PollID, session.RoomCode, session.Status, session.CreatedAt)
	return err
}

func (r *postgresSessionRepo) GetByCode(ctx context.Context, code string) (*model.Session, error) {
	var s model.Session
	err := r.db.GetContext(ctx, &s,
		`SELECT id, poll_id, room_code, status, question_order, started_at, finished_at, created_at
		 FROM sessions WHERE room_code = $1`, code)
	if err != nil {
		if errors.Is(err, sql.ErrNoRows) {
			return nil, errs.ErrNotFound
		}
		return nil, err
	}
	return &s, nil
}

func (r *postgresSessionRepo) GetActiveByPoll(ctx context.Context, pollID uuid.UUID) (*model.Session, error) {
	var s model.Session
	err := r.db.GetContext(ctx, &s,
		`SELECT id, poll_id, room_code, status, question_order, started_at, finished_at, created_at
		 FROM sessions WHERE poll_id = $1 AND status <> 'finished'`, pollID)
	if err != nil {
		if errors.Is(err, sql.ErrNoRows) {
			return nil, errs.ErrNotFound
		}
		return nil, err
	}
	return &s, nil
}

func (r *postgresSessionRepo) UpdateStatus(ctx context.Context, sessionID uuid.UUID, status string, startedAt, finishedAt *string) error {
	query := `UPDATE sessions SET status = $2, started_at = COALESCE($3::timestamptz, started_at),
	          finished_at = COALESCE($4::timestamptz, finished_at) WHERE id = $1`
	res, err := r.db.ExecContext(ctx, query, sessionID, status, startedAt, finishedAt)
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
