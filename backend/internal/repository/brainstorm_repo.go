package repository

import (
	"context"
	"database/sql"
	"errors"

	"github.com/google/uuid"
	"github.com/jmoiron/sqlx"
	"github.com/lib/pq"

	"presentarium/internal/errs"
	"presentarium/internal/model"
)

// BrainstormRepository defines data access for brainstorm ideas and votes.
type BrainstormRepository interface {
	// CreateIdea stores a new brainstorm idea.
	CreateIdea(ctx context.Context, idea *model.BrainstormIdea) error
	// GetIdea returns an idea by ID, or ErrNotFound.
	GetIdea(ctx context.Context, ideaID uuid.UUID) (*model.BrainstormIdea, error)
	// CountIdeasByParticipant counts how many ideas a participant has submitted for a question.
	CountIdeasByParticipant(ctx context.Context, sessionID, questionID, participantID uuid.UUID) (int, error)
	// SetIdeaHidden updates the is_hidden flag on an idea.
	SetIdeaHidden(ctx context.Context, ideaID uuid.UUID, isHidden bool) error
	// ListIdeasRanked returns non-hidden ideas ordered by votes_count DESC, created_at ASC.
	ListIdeasRanked(ctx context.Context, sessionID, questionID uuid.UUID) ([]model.BrainstormIdea, error)
	// ListAllIdeas returns all ideas (including hidden) for organizer view.
	ListAllIdeas(ctx context.Context, sessionID, questionID uuid.UUID) ([]model.BrainstormIdea, error)
	// CreateVote records a vote. Returns ErrConflict if the participant already voted for this idea.
	CreateVote(ctx context.Context, vote *model.BrainstormVote) error
	// CountVotesByParticipant counts how many ideas a participant has voted for in a question.
	CountVotesByParticipant(ctx context.Context, sessionID, questionID, participantID uuid.UUID) (int, error)
}

type postgresBrainstormRepo struct {
	db *sqlx.DB
}

// NewPostgresBrainstormRepo creates a new PostgreSQL-backed BrainstormRepository.
func NewPostgresBrainstormRepo(db *sqlx.DB) BrainstormRepository {
	return &postgresBrainstormRepo{db: db}
}

func (r *postgresBrainstormRepo) CreateIdea(ctx context.Context, idea *model.BrainstormIdea) error {
	_, err := r.db.ExecContext(ctx,
		`INSERT INTO brainstorm_ideas (id, session_id, question_id, participant_id, text, is_hidden, votes_count, created_at)
		 VALUES ($1, $2, $3, $4, $5, $6, $7, $8)`,
		idea.ID, idea.SessionID, idea.QuestionID, idea.ParticipantID,
		idea.Text, idea.IsHidden, idea.VotesCount, idea.CreatedAt,
	)
	return err
}

func (r *postgresBrainstormRepo) GetIdea(ctx context.Context, ideaID uuid.UUID) (*model.BrainstormIdea, error) {
	var idea model.BrainstormIdea
	err := r.db.GetContext(ctx, &idea,
		`SELECT id, session_id, question_id, participant_id, text, is_hidden, votes_count, created_at
		 FROM brainstorm_ideas WHERE id=$1`, ideaID)
	if err != nil {
		if errors.Is(err, sql.ErrNoRows) {
			return nil, errs.ErrNotFound
		}
		return nil, err
	}
	return &idea, nil
}

func (r *postgresBrainstormRepo) CountIdeasByParticipant(ctx context.Context, sessionID, questionID, participantID uuid.UUID) (int, error) {
	var count int
	err := r.db.GetContext(ctx, &count,
		`SELECT COUNT(*) FROM brainstorm_ideas
		 WHERE session_id=$1 AND question_id=$2 AND participant_id=$3`,
		sessionID, questionID, participantID)
	return count, err
}

func (r *postgresBrainstormRepo) SetIdeaHidden(ctx context.Context, ideaID uuid.UUID, isHidden bool) error {
	_, err := r.db.ExecContext(ctx,
		`UPDATE brainstorm_ideas SET is_hidden=$2 WHERE id=$1`,
		ideaID, isHidden)
	return err
}

func (r *postgresBrainstormRepo) ListIdeasRanked(ctx context.Context, sessionID, questionID uuid.UUID) ([]model.BrainstormIdea, error) {
	var ideas []model.BrainstormIdea
	err := r.db.SelectContext(ctx, &ideas,
		`SELECT id, session_id, question_id, participant_id, text, is_hidden, votes_count, created_at
		 FROM brainstorm_ideas
		 WHERE session_id=$1 AND question_id=$2 AND is_hidden=false
		 ORDER BY votes_count DESC, created_at ASC`,
		sessionID, questionID)
	return ideas, err
}

func (r *postgresBrainstormRepo) ListAllIdeas(ctx context.Context, sessionID, questionID uuid.UUID) ([]model.BrainstormIdea, error) {
	var ideas []model.BrainstormIdea
	err := r.db.SelectContext(ctx, &ideas,
		`SELECT id, session_id, question_id, participant_id, text, is_hidden, votes_count, created_at
		 FROM brainstorm_ideas
		 WHERE session_id=$1 AND question_id=$2
		 ORDER BY created_at ASC`,
		sessionID, questionID)
	return ideas, err
}

func (r *postgresBrainstormRepo) CreateVote(ctx context.Context, vote *model.BrainstormVote) error {
	_, err := r.db.ExecContext(ctx,
		`INSERT INTO brainstorm_votes (id, idea_id, participant_id, created_at)
		 VALUES ($1, $2, $3, $4)`,
		vote.ID, vote.IdeaID, vote.ParticipantID, vote.CreatedAt)
	if err != nil {
		var pqErr *pq.Error
		if errors.As(err, &pqErr) && pqErr.Code == "23505" {
			return errs.ErrConflict
		}
		return err
	}
	return nil
}

func (r *postgresBrainstormRepo) CountVotesByParticipant(ctx context.Context, sessionID, questionID, participantID uuid.UUID) (int, error) {
	var count int
	err := r.db.GetContext(ctx, &count,
		`SELECT COUNT(*) FROM brainstorm_votes bv
		 JOIN brainstorm_ideas bi ON bi.id = bv.idea_id
		 WHERE bi.session_id=$1 AND bi.question_id=$2 AND bv.participant_id=$3`,
		sessionID, questionID, participantID)
	return count, err
}


