package repository

import (
	"context"
	"database/sql"
	"errors"
	"time"

	"github.com/google/uuid"
	"github.com/jmoiron/sqlx"

	"presentarium/internal/errs"
	"presentarium/internal/model"
)

// SessionSummaryRow holds aggregated session data for the history list.
type SessionSummaryRow struct {
	ID               uuid.UUID  `db:"id"`
	PollID           uuid.UUID  `db:"poll_id"`
	PollTitle        string     `db:"poll_title"`
	RoomCode         string     `db:"room_code"`
	Status           string     `db:"status"`
	StartedAt        *time.Time `db:"started_at"`
	FinishedAt       *time.Time `db:"finished_at"`
	CreatedAt        time.Time  `db:"created_at"`
	ParticipantCount int        `db:"participant_count"`
	AverageScore     float64    `db:"average_score"`
}

// SessionWithPollRow holds session data joined with poll ownership info.
type SessionWithPollRow struct {
	ID         uuid.UUID  `db:"id"`
	PollID     uuid.UUID  `db:"poll_id"`
	PollTitle  string     `db:"poll_title"`
	PollUserID uuid.UUID  `db:"poll_user_id"`
	RoomCode   string     `db:"room_code"`
	Status     string     `db:"status"`
	StartedAt  *time.Time `db:"started_at"`
	FinishedAt *time.Time `db:"finished_at"`
	CreatedAt  time.Time  `db:"created_at"`
}

// SessionRepository defines data access for sessions.
type SessionRepository interface {
	Create(ctx context.Context, session *model.Session) error
	GetByCode(ctx context.Context, code string) (*model.Session, error)
	GetActiveByPoll(ctx context.Context, pollID uuid.UUID) (*model.Session, error)
	UpdateStatus(ctx context.Context, sessionID uuid.UUID, status string, startedAt, finishedAt *string) error
	// ListByUser returns summary rows for all sessions belonging to the user's polls, newest first.
	ListByUser(ctx context.Context, userID uuid.UUID) ([]SessionSummaryRow, error)
	// GetByIDWithPoll returns a session joined with its poll's title and user_id.
	GetByIDWithPoll(ctx context.Context, sessionID uuid.UUID) (*SessionWithPollRow, error)
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

func (r *postgresSessionRepo) ListByUser(ctx context.Context, userID uuid.UUID) ([]SessionSummaryRow, error) {
	var rows []SessionSummaryRow
	query := `
		SELECT s.id, s.poll_id, p.title AS poll_title, s.room_code, s.status,
		       s.started_at, s.finished_at, s.created_at,
		       COUNT(DISTINCT pt.id)::int AS participant_count,
		       COALESCE(AVG(pt.total_score::float), 0.0) AS average_score
		FROM sessions s
		JOIN polls p ON p.id = s.poll_id
		LEFT JOIN participants pt ON pt.session_id = s.id
		WHERE p.user_id = $1
		GROUP BY s.id, s.poll_id, p.title, s.room_code, s.status,
		         s.started_at, s.finished_at, s.created_at
		ORDER BY s.created_at DESC`
	if err := r.db.SelectContext(ctx, &rows, query, userID); err != nil {
		return nil, err
	}
	return rows, nil
}

func (r *postgresSessionRepo) GetByIDWithPoll(ctx context.Context, sessionID uuid.UUID) (*SessionWithPollRow, error) {
	var row SessionWithPollRow
	query := `
		SELECT s.id, s.poll_id, s.room_code, s.status,
		       s.started_at, s.finished_at, s.created_at,
		       p.title AS poll_title, p.user_id AS poll_user_id
		FROM sessions s
		JOIN polls p ON p.id = s.poll_id
		WHERE s.id = $1`
	err := r.db.GetContext(ctx, &row, query, sessionID)
	if err != nil {
		if errors.Is(err, sql.ErrNoRows) {
			return nil, errs.ErrNotFound
		}
		return nil, err
	}
	return &row, nil
}
