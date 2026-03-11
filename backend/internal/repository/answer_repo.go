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
	ParticipantID uuid.UUID `db:"id"         json:"participant_id"`
	Name          string    `db:"name"        json:"name"`
	TotalScore    int       `db:"total_score" json:"total_score"`
}

// AnswerExportRow holds joined data for answer export (participant + question + answer).
type AnswerExportRow struct {
	ParticipantName string          `db:"participant_name" json:"participant_name"`
	QuestionText    string          `db:"question_text"    json:"question_text"`
	Answer          json.RawMessage `db:"answer"           json:"answer"`
	IsCorrect       *bool           `db:"is_correct"       json:"is_correct"`
	Score           int             `db:"score"            json:"score"`
	ResponseTimeMs  int             `db:"response_time_ms" json:"response_time_ms"`
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
	// DistributionByQuestion returns a map of answer value (JSON text) → count for non-hidden answers.
	DistributionByQuestion(ctx context.Context, questionID, sessionID uuid.UUID) (map[string]int, error)
	// ListExportBySession returns all answers in a session joined with participant and question data.
	ListExportBySession(ctx context.Context, sessionID uuid.UUID) ([]AnswerExportRow, error)
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
	// Tiebreaker: participants who answered faster (lower sum of response_time_ms) rank higher.
	// COALESCE with a large sentinel places non-answering participants last.
	err := r.db.SelectContext(ctx, &rows,
		`SELECT p.id, p.name, p.total_score
		 FROM participants p
		 LEFT JOIN answers a ON a.participant_id = p.id AND a.session_id = $1
		 WHERE p.session_id = $1
		 GROUP BY p.id, p.name, p.total_score
		 ORDER BY p.total_score DESC,
		          COALESCE(SUM(a.response_time_ms), 2147483647) ASC
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

type distributionRow struct {
	Key string `db:"key"`
	Cnt int    `db:"cnt"`
}

func (r *postgresAnswerRepo) DistributionByQuestion(ctx context.Context, questionID, sessionID uuid.UUID) (map[string]int, error) {
	var rows []distributionRow
	err := r.db.SelectContext(ctx, &rows,
		`SELECT answer::text AS key, COUNT(*)::int AS cnt
		 FROM answers
		 WHERE question_id = $1 AND session_id = $2 AND is_hidden = false
		 GROUP BY answer::text`,
		questionID, sessionID)
	if err != nil {
		return nil, err
	}
	dist := make(map[string]int, len(rows))
	for _, row := range rows {
		dist[row.Key] = row.Cnt
	}
	return dist, nil
}

func (r *postgresAnswerRepo) ListExportBySession(ctx context.Context, sessionID uuid.UUID) ([]AnswerExportRow, error) {
	var rows []AnswerExportRow
	err := r.db.SelectContext(ctx, &rows,
		`SELECT pt.name AS participant_name,
		        q.text  AS question_text,
		        a.answer,
		        a.is_correct,
		        a.score,
		        a.response_time_ms
		 FROM answers a
		 JOIN participants pt ON pt.id = a.participant_id
		 JOIN questions q    ON q.id  = a.question_id
		 WHERE a.session_id = $1 AND a.is_hidden = false
		 ORDER BY pt.name ASC, q.position ASC`,
		sessionID)
	if err != nil {
		return nil, err
	}
	return rows, nil
}
