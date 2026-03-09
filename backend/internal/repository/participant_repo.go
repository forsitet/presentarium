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

// ParticipantRepository defines data access for session participants.
type ParticipantRepository interface {
	Create(ctx context.Context, p *model.Participant) error
	GetBySessionToken(ctx context.Context, token uuid.UUID) (*model.Participant, error)
	ListBySession(ctx context.Context, sessionID uuid.UUID) ([]*model.Participant, error)
	UpdateLastSeen(ctx context.Context, participantID uuid.UUID, at time.Time) error
}

type postgresParticipantRepo struct {
	db *sqlx.DB
}

// NewPostgresParticipantRepo creates a new PostgreSQL-backed ParticipantRepository.
func NewPostgresParticipantRepo(db *sqlx.DB) ParticipantRepository {
	return &postgresParticipantRepo{db: db}
}

func (r *postgresParticipantRepo) Create(ctx context.Context, p *model.Participant) error {
	query := `INSERT INTO participants (id, session_id, name, session_token, total_score, joined_at)
	          VALUES ($1, $2, $3, $4, $5, $6)`
	_, err := r.db.ExecContext(ctx, query,
		p.ID, p.SessionID, p.Name, p.SessionToken, p.TotalScore, p.JoinedAt)
	return err
}

func (r *postgresParticipantRepo) GetBySessionToken(ctx context.Context, token uuid.UUID) (*model.Participant, error) {
	var p model.Participant
	err := r.db.GetContext(ctx, &p,
		`SELECT id, session_id, name, session_token, total_score, joined_at, last_seen_at
		 FROM participants WHERE session_token = $1`, token)
	if err != nil {
		if errors.Is(err, sql.ErrNoRows) {
			return nil, errs.ErrNotFound
		}
		return nil, err
	}
	return &p, nil
}

func (r *postgresParticipantRepo) ListBySession(ctx context.Context, sessionID uuid.UUID) ([]*model.Participant, error) {
	var rows []*model.Participant
	err := r.db.SelectContext(ctx, &rows,
		`SELECT id, session_id, name, session_token, total_score, joined_at, last_seen_at
		 FROM participants WHERE session_id = $1 ORDER BY joined_at ASC`, sessionID)
	if err != nil {
		return nil, err
	}
	return rows, nil
}

func (r *postgresParticipantRepo) UpdateLastSeen(ctx context.Context, participantID uuid.UUID, at time.Time) error {
	_, err := r.db.ExecContext(ctx,
		`UPDATE participants SET last_seen_at = $2 WHERE id = $1`, participantID, at)
	return err
}
