package repository

import (
	"context"
	"database/sql"
	"encoding/json"
	"errors"

	"github.com/google/uuid"
	"github.com/jmoiron/sqlx"

	"presentarium/internal/errs"
	"presentarium/internal/model"
)

// LeaderboardRow holds aggregated score data for a single participant.
type LeaderboardRow struct {
	ParticipantID uuid.UUID `db:"id"`
	Name          string    `db:"name"`
	TotalScore    int       `db:"total_score"`
}

// AnswerRepository defines data access for participant answers.
type AnswerRepository interface {
	// Create stores a new answer. The Answer.Answer field must be JSON-serialisable.
	Create(ctx context.Context, answer *model.Answer) error
	// GetByParticipantAndQuestion returns the answer for (participant, question), or ErrNotFound.
	GetByParticipantAndQuestion(ctx context.Context, participantID, questionID uuid.UUID) (*model.Answer, error)
	// ListByQuestion returns all non-hidden answers for a question in a session.
	ListByQuestion(ctx context.Context, questionID, sessionID uuid.UUID) ([]model.Answer, error)
	// GetLeaderboard returns participants ordered by total_score DESC (top 10).
	GetLeaderboard(ctx context.Context, sessionID uuid.UUID) ([]LeaderboardRow, error)
	// UpdateParticipantScore increments the participant's total_score by delta.
	UpdateParticipantScore(ctx context.Context, participantID uuid.UUID, delta int) error
}

type postgresAnswerRepo struct {
	db *sqlx.DB
}

// NewPostgresAnswerRepo creates a new PostgreSQL-backed AnswerRepository.
func NewPostgresAnswerRepo(db *sqlx.DB) AnswerRepository {
	return &postgresAnswerRepo{db: db}
}

func (r *postgresAnswerRepo) Create(ctx context.Context, answer *model.Answer) error {
	// Serialise the answer payload to JSONB.
	answerJSON, err := json.Marshal(answer.Answer)
	if err != nil {
		return err
	}

	query := `INSERT INTO answers
		(id, participant_id, question_id, session_id, answer, is_correct, score, response_time_ms, is_hidden, answered_at)
		VALUES ($1, $2, $3, $4, $5, $6, $7, $8, $9, $10)`
	_, err = r.db.ExecContext(ctx, query,
		answer.ID,
		answer.ParticipantID,
		answer.QuestionID,
		answer.SessionID,
		answerJSON,
		answer.IsCorrect,
		answer.Score,
		answer.ResponseTimeMs,
		answer.IsHidden,
		answer.AnsweredAt,
	)
	return err
}

func (r *postgresAnswerRepo) GetByParticipantAndQuestion(ctx context.Context, participantID, questionID uuid.UUID) (*model.Answer, error) {
	var a model.Answer
	err := r.db.GetContext(ctx, &a,
		`SELECT id, participant_id, question_id, session_id, answer, is_correct, score, response_time_ms, is_hidden, answered_at
		 FROM answers WHERE participant_id=$1 AND question_id=$2`, participantID, questionID)
	if err != nil {
		if errors.Is(err, sql.ErrNoRows) {
			return nil, errs.ErrNotFound
		}
		return nil, err
	}
	return &a, nil
}

func (r *postgresAnswerRepo) ListByQuestion(ctx context.Context, questionID, sessionID uuid.UUID) ([]model.Answer, error) {
	var answers []model.Answer
	err := r.db.SelectContext(ctx, &answers,
		`SELECT id, participant_id, question_id, session_id, answer, is_correct, score, response_time_ms, is_hidden, answered_at
		 FROM answers WHERE question_id=$1 AND session_id=$2 AND is_hidden=false
		 ORDER BY answered_at ASC`,
		questionID, sessionID)
	if err != nil {
		return nil, err
	}
	return answers, nil
}

func (r *postgresAnswerRepo) GetLeaderboard(ctx context.Context, sessionID uuid.UUID) ([]LeaderboardRow, error) {
	var rows []LeaderboardRow
	err := r.db.SelectContext(ctx, &rows,
		`SELECT id, name, total_score
		 FROM participants WHERE session_id=$1
		 ORDER BY total_score DESC, joined_at ASC
		 LIMIT 10`,
		sessionID)
	if err != nil {
		return nil, err
	}
	return rows, nil
}

func (r *postgresAnswerRepo) UpdateParticipantScore(ctx context.Context, participantID uuid.UUID, delta int) error {
	_, err := r.db.ExecContext(ctx,
		`UPDATE participants SET total_score = total_score + $2 WHERE id = $1`,
		participantID, delta)
	return err
}
