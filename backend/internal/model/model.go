package model

import (
	"time"

	"github.com/google/uuid"
)

// User represents an organizer account.
type User struct {
	ID           uuid.UUID `db:"id"`
	Email        string    `db:"email"`
	PasswordHash string    `db:"password_hash"`
	Name         string    `db:"name"`
	CreatedAt    time.Time `db:"created_at"`
	UpdatedAt    time.Time `db:"updated_at"`
}

// Poll represents a quiz/poll created by a user.
type Poll struct {
	ID            uuid.UUID `db:"id"`
	UserID        uuid.UUID `db:"user_id"`
	Title         string    `db:"title"`
	Description   string    `db:"description"`
	ScoringRule   string    `db:"scoring_rule"`   // none | correct_answer | speed_bonus
	QuestionOrder string    `db:"question_order"` // sequential | random
	CreatedAt     time.Time `db:"created_at"`
	UpdatedAt     time.Time `db:"updated_at"`
}

// Question represents a question inside a poll.
type Question struct {
	ID               uuid.UUID   `db:"id"`
	PollID           uuid.UUID   `db:"poll_id"`
	Type             string      `db:"type"` // single_choice | multiple_choice | open_text | image_choice | word_cloud | brainstorm
	Text             string      `db:"text"`
	Options          interface{} `db:"options"` // JSONB
	TimeLimitSeconds int         `db:"time_limit_seconds"`
	Points           int         `db:"points"`
	Position         int         `db:"position"`
	CreatedAt        time.Time   `db:"created_at"`
	UpdatedAt        time.Time   `db:"updated_at"`
}

// Session represents a live quiz session (room).
type Session struct {
	ID            uuid.UUID   `db:"id"`
	PollID        uuid.UUID   `db:"poll_id"`
	RoomCode      string      `db:"room_code"`
	Status        string      `db:"status"` // waiting | active | showing_question | showing_results | finished
	QuestionOrder interface{} `db:"question_order"` // JSONB array of question IDs
	StartedAt     *time.Time  `db:"started_at"`
	FinishedAt    *time.Time  `db:"finished_at"`
	CreatedAt     time.Time   `db:"created_at"`
}

// Participant represents a quiz participant in a session.
type Participant struct {
	ID           uuid.UUID  `db:"id"`
	SessionID    uuid.UUID  `db:"session_id"`
	Name         string     `db:"name"`
	SessionToken uuid.UUID  `db:"session_token"`
	TotalScore   int        `db:"total_score"`
	JoinedAt     time.Time  `db:"joined_at"`
	LastSeenAt   *time.Time `db:"last_seen_at"`
}

// Answer represents a participant's answer to a question.
type Answer struct {
	ID             uuid.UUID   `db:"id"`
	ParticipantID  uuid.UUID   `db:"participant_id"`
	QuestionID     uuid.UUID   `db:"question_id"`
	SessionID      uuid.UUID   `db:"session_id"`
	Answer         interface{} `db:"answer"` // JSONB
	IsCorrect      *bool       `db:"is_correct"`
	Score          int         `db:"score"`
	ResponseTimeMs int         `db:"response_time_ms"`
	IsHidden       bool        `db:"is_hidden"`
	AnsweredAt     time.Time   `db:"answered_at"`
}

// BrainstormIdea represents an idea submitted in brainstorm mode.
type BrainstormIdea struct {
	ID            uuid.UUID `db:"id"`
	SessionID     uuid.UUID `db:"session_id"`
	QuestionID    uuid.UUID `db:"question_id"`
	ParticipantID uuid.UUID `db:"participant_id"`
	Text          string    `db:"text"`
	IsHidden      bool      `db:"is_hidden"`
	VotesCount    int       `db:"votes_count"`
	CreatedAt     time.Time `db:"created_at"`
}

// BrainstormVote represents a vote for a brainstorm idea.
type BrainstormVote struct {
	ID            uuid.UUID `db:"id"`
	IdeaID        uuid.UUID `db:"idea_id"`
	ParticipantID uuid.UUID `db:"participant_id"`
	CreatedAt     time.Time `db:"created_at"`
}

// RefreshToken stores refresh tokens for JWT rotation.
type RefreshToken struct {
	ID        uuid.UUID `db:"id"`
	UserID    uuid.UUID `db:"user_id"`
	Token     string    `db:"token"`
	ExpiresAt time.Time `db:"expires_at"`
	CreatedAt time.Time `db:"created_at"`
}
