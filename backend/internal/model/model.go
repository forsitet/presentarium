package model

import (
	"database/sql/driver"
	"encoding/json"
	"fmt"
	"time"

	"github.com/google/uuid"
)

// QuestionOption represents a single answer option for a question.
type QuestionOption struct {
	Text      string `json:"text"`
	IsCorrect bool   `json:"is_correct"`
	ImageURL  string `json:"image_url,omitempty"`
}

// OptionList is a slice of QuestionOption that can be stored as JSONB in PostgreSQL.
type OptionList []QuestionOption

// Value implements driver.Valuer for SQL serialization.
func (o OptionList) Value() (driver.Value, error) {
	if o == nil {
		return nil, nil
	}
	b, err := json.Marshal(o)
	return string(b), err
}

// Scan implements sql.Scanner for SQL deserialization.
func (o *OptionList) Scan(src interface{}) error {
	if src == nil {
		*o = nil
		return nil
	}
	var b []byte
	switch v := src.(type) {
	case []byte:
		b = v
	case string:
		b = []byte(v)
	default:
		return fmt.Errorf("OptionList.Scan: unsupported type %T", src)
	}
	return json.Unmarshal(b, o)
}

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
	ID            uuid.UUID `db:"id"             json:"id"`
	UserID        uuid.UUID `db:"user_id"        json:"user_id"`
	Title         string    `db:"title"          json:"title"`
	Description   string    `db:"description"    json:"description"`
	ScoringRule   string    `db:"scoring_rule"   json:"scoring_rule"`
	QuestionOrder string    `db:"question_order" json:"question_order"`
	CreatedAt     time.Time `db:"created_at"     json:"created_at"`
	UpdatedAt     time.Time `db:"updated_at"     json:"updated_at"`
}

// Question represents a question inside a poll.
type Question struct {
	ID               uuid.UUID  `db:"id"                 json:"id"`
	PollID           uuid.UUID  `db:"poll_id"            json:"poll_id"`
	Type             string     `db:"type"               json:"type"`
	Text             string     `db:"text"               json:"text"`
	Options          OptionList `db:"options"             json:"options"`
	TimeLimitSeconds int        `db:"time_limit_seconds" json:"time_limit_seconds"`
	Points           int        `db:"points"             json:"points"`
	Position         int        `db:"position"           json:"position"`
	CreatedAt        time.Time  `db:"created_at"         json:"created_at"`
	UpdatedAt        time.Time  `db:"updated_at"         json:"updated_at"`
}

// Session represents a live quiz session (room).
type Session struct {
	ID                   uuid.UUID   `db:"id"                       json:"id"`
	PollID               uuid.UUID   `db:"poll_id"                  json:"poll_id"`
	RoomCode             string      `db:"room_code"                json:"room_code"`
	Status               string      `db:"status"                   json:"status"`
	QuestionOrder        interface{} `db:"question_order"           json:"question_order"`
	ActivePresentationID *uuid.UUID  `db:"active_presentation_id"   json:"active_presentation_id"`
	CurrentSlidePosition *int        `db:"current_slide_position"   json:"current_slide_position"`
	StartedAt            *time.Time  `db:"started_at"               json:"started_at"`
	FinishedAt           *time.Time  `db:"finished_at"              json:"finished_at"`
	CreatedAt            time.Time   `db:"created_at"               json:"created_at"`
}

// Presentation represents a PowerPoint uploaded by an organizer. Its slides
// are converted to WebP images and served during live sessions.
type Presentation struct {
	ID               uuid.UUID `db:"id"                 json:"id"`
	UserID           uuid.UUID `db:"user_id"            json:"user_id"`
	Title            string    `db:"title"              json:"title"`
	OriginalFilename string    `db:"original_filename"  json:"original_filename"`
	SourceKey        string    `db:"source_key"         json:"-"`
	SlideCount       int       `db:"slide_count"        json:"slide_count"`
	Status           string    `db:"status"             json:"status"`
	ErrorMessage     string    `db:"error_message"      json:"error_message,omitempty"`
	CreatedAt        time.Time `db:"created_at"         json:"created_at"`
	UpdatedAt        time.Time `db:"updated_at"         json:"updated_at"`
}

// PresentationSlide represents a single converted slide. ImageKey/ThumbKey are
// S3 object keys; the full URL is built by Storage.PublicURL() at read time.
type PresentationSlide struct {
	ID             uuid.UUID `db:"id"               json:"id"`
	PresentationID uuid.UUID `db:"presentation_id"  json:"presentation_id"`
	Position       int       `db:"position"         json:"position"`
	ImageKey       string    `db:"image_key"        json:"-"`
	ThumbKey       string    `db:"thumb_key"        json:"-"`
	Width          int       `db:"width"            json:"width"`
	Height         int       `db:"height"           json:"height"`
	Notes          string    `db:"notes"            json:"notes,omitempty"`
	CreatedAt      time.Time `db:"created_at"       json:"created_at"`
}

// Participant represents a quiz participant in a session.
type Participant struct {
	ID           uuid.UUID  `db:"id"            json:"id"`
	SessionID    uuid.UUID  `db:"session_id"    json:"session_id"`
	Name         string     `db:"name"          json:"name"`
	SessionToken uuid.UUID  `db:"session_token" json:"session_token"`
	TotalScore   int        `db:"total_score"   json:"total_score"`
	JoinedAt     time.Time  `db:"joined_at"     json:"joined_at"`
	LastSeenAt   *time.Time `db:"last_seen_at"  json:"last_seen_at"`
}

// Answer represents a participant's answer to a question.
type Answer struct {
	ID             uuid.UUID   `db:"id"               json:"id"`
	ParticipantID  uuid.UUID   `db:"participant_id"   json:"participant_id"`
	QuestionID     uuid.UUID   `db:"question_id"      json:"question_id"`
	SessionID      uuid.UUID   `db:"session_id"       json:"session_id"`
	Answer         interface{} `db:"answer"            json:"answer"`
	IsCorrect      *bool       `db:"is_correct"       json:"is_correct"`
	Score          int         `db:"score"             json:"score"`
	ResponseTimeMs int         `db:"response_time_ms" json:"response_time_ms"`
	IsHidden       bool        `db:"is_hidden"        json:"is_hidden"`
	AnsweredAt     time.Time   `db:"answered_at"      json:"answered_at"`
}

// BrainstormIdea represents an idea submitted in brainstorm mode.
type BrainstormIdea struct {
	ID            uuid.UUID `db:"id"             json:"id"`
	SessionID     uuid.UUID `db:"session_id"     json:"session_id"`
	QuestionID    uuid.UUID `db:"question_id"    json:"question_id"`
	ParticipantID uuid.UUID `db:"participant_id" json:"participant_id"`
	Text          string    `db:"text"           json:"text"`
	IsHidden      bool      `db:"is_hidden"      json:"is_hidden"`
	VotesCount    int       `db:"votes_count"    json:"votes_count"`
	CreatedAt     time.Time `db:"created_at"     json:"created_at"`
}

// BrainstormVote represents a vote for a brainstorm idea.
type BrainstormVote struct {
	ID            uuid.UUID `db:"id"             json:"id"`
	IdeaID        uuid.UUID `db:"idea_id"        json:"idea_id"`
	ParticipantID uuid.UUID `db:"participant_id" json:"participant_id"`
	CreatedAt     time.Time `db:"created_at"     json:"created_at"`
}

// RefreshToken stores refresh tokens for JWT rotation.
type RefreshToken struct {
	ID        uuid.UUID `db:"id"`
	UserID    uuid.UUID `db:"user_id"`
	Token     string    `db:"token"`
	ExpiresAt time.Time `db:"expires_at"`
	CreatedAt time.Time `db:"created_at"`
}

// PasswordResetToken stores one-time tokens for password recovery.
type PasswordResetToken struct {
	ID        uuid.UUID  `db:"id"`
	UserID    uuid.UUID  `db:"user_id"`
	Token     string     `db:"token"`
	ExpiresAt time.Time  `db:"expires_at"`
	UsedAt    *time.Time `db:"used_at"`
	CreatedAt time.Time  `db:"created_at"`
}
