package ws

import (
	"encoding/json"

	"github.com/google/uuid"
)

// Message type constants — outgoing (server → client).
const (
	MsgTypeConnected              = "connected"
	MsgTypeParticipantJoined      = "participant_joined"
	MsgTypeParticipantLeft        = "participant_left"
	MsgTypeRoomStarted            = "room_started"
	MsgTypeQuestionStart          = "question_start"
	MsgTypeTimerTick              = "timer_tick"
	MsgTypeQuestionEnd            = "question_end"
	MsgTypeResults                = "results"
	MsgTypeLeaderboard            = "leaderboard"
	MsgTypeAnswerAccepted         = "answer_accepted"
	MsgTypeAnswerCount            = "answer_count"
	MsgTypeWordcloudUpdate        = "wordcloud_update"
	MsgTypeBrainstormIdeaAdded    = "brainstorm_idea_added"
	MsgTypeBrainstormPhaseChanged = "brainstorm_phase_changed"
	MsgTypeBrainstormVoteUpdated  = "brainstorm_vote_updated"
	MsgTypeSessionEnd             = "session_end"
	MsgTypeAnswerHidden           = "answer_hidden"
	MsgTypePresentationOpened     = "presentation_opened"
	MsgTypeSlideChanged           = "slide_changed"
	MsgTypePresentationClosed     = "presentation_closed"
	MsgTypeError                  = "error"
)

// Message type constants — incoming (client → server).
const (
	MsgTypeSubmitAnswer          = "submit_answer"
	MsgTypeSubmitText            = "submit_text"
	MsgTypeSubmitVote            = "submit_vote"
	MsgTypeSubmitIdea            = "submit_idea"
	MsgTypeShowQuestion          = "show_question"
	MsgTypeEndQuestion           = "end_question"
	MsgTypeHideAnswer            = "hide_answer"
	MsgTypeBrainstormHideIdea    = "brainstorm_hide_idea"
	MsgTypeBrainstormChangePhase = "brainstorm_change_phase"
	MsgTypeOpenPresentation      = "open_presentation"
	MsgTypeChangeSlide           = "change_slide"
	MsgTypeClosePresentation     = "close_presentation"
)

// Envelope is the wire format for all WebSocket messages.
type Envelope struct {
	Type string          `json:"type"`
	Data json.RawMessage `json:"data,omitempty"`
}

// NewEnvelope creates a JSON-encoded envelope with the given type and data payload.
func NewEnvelope(msgType string, data interface{}) ([]byte, error) {
	var raw json.RawMessage
	if data != nil {
		b, err := json.Marshal(data)
		if err != nil {
			return nil, err
		}
		raw = b
	}
	return json.Marshal(Envelope{Type: msgType, Data: raw})
}

// --- Outgoing data payloads ---

// RoomStartedData is sent to the organizer when the session transitions to active.
// It carries the ordered list of question IDs the organizer should step through.
type RoomStartedData struct {
	QuestionOrder []uuid.UUID `json:"question_order"`
}

// ConnectedData is sent to a client upon successful connection.
type ConnectedData struct {
	SessionToken uuid.UUID `json:"session_token"`
	Role         string    `json:"role"` // organizer | participant
}

// ParticipantData carries basic participant info for join/leave events.
type ParticipantData struct {
	ID   uuid.UUID `json:"id"`
	Name string    `json:"name"`
}

// QuestionStartData is sent to all clients when a question begins.
type QuestionStartData struct {
	QuestionID   uuid.UUID       `json:"question_id"`
	Type         string          `json:"type"`
	Text         string          `json:"text"`
	Options      json.RawMessage `json:"options,omitempty"`
	TimeLimitSec int             `json:"time_limit_seconds"`
	Points       int             `json:"points"`
	Position     int             `json:"position"`
	Total        int             `json:"total"`
}

// TimerTickData carries the remaining seconds for the current question.
type TimerTickData struct {
	Remaining int `json:"remaining"`
}

// QuestionEndData is sent when a question's time expires or is ended early.
type QuestionEndData struct {
	QuestionID uuid.UUID       `json:"question_id"`
	Options    json.RawMessage `json:"options,omitempty"` // options with is_correct revealed
}

// LeaderboardEntry is a single ranked entry in the leaderboard.
type LeaderboardEntry struct {
	Rank  int       `json:"rank"`
	ID    uuid.UUID `json:"id"`
	Name  string    `json:"name"`
	Score int       `json:"score"`
}

// LeaderboardData carries the leaderboard sent after each question.
type LeaderboardData struct {
	Rankings []LeaderboardEntry `json:"rankings"`
	MyRank   int                `json:"my_rank,omitempty"`
	MyScore  int                `json:"my_score,omitempty"`
}

// ResultsData carries answer distribution data for the current question.
type ResultsData struct {
	QuestionID   uuid.UUID      `json:"question_id"`
	AnswerCounts map[string]int `json:"answer_counts"` // option index/text → count
}

// AnswerAcceptedData is sent to a participant after their answer is recorded.
type AnswerAcceptedData struct {
	QuestionID uuid.UUID `json:"question_id"`
	Score      int       `json:"score"`
	IsCorrect  *bool     `json:"is_correct,omitempty"`
}

// SessionEndData is broadcast to all clients when the session ends.
// MyRank and MyScore are populated only for participant recipients.
type SessionEndData struct {
	Rankings []LeaderboardEntry `json:"rankings"`
	MyRank   int                `json:"my_rank,omitempty"`
	MyScore  int                `json:"my_score,omitempty"`
}

// AnswerCountData is sent to the organizer when a new answer arrives.
type AnswerCountData struct {
	Answered      int       `json:"answered"`
	Total         int       `json:"total"`
	ParticipantID uuid.UUID `json:"participant_id"`
	AvgResponseMs int       `json:"avg_response_ms"`
}

// WordcloudWord is a single word with its frequency count.
type WordcloudWord struct {
	Text  string `json:"text"`
	Count int    `json:"count"`
}

// WordcloudUpdateData carries the current word frequencies for the organizer.
type WordcloudUpdateData struct {
	Words []WordcloudWord `json:"words"`
}

// BrainstormIdeaData represents an idea added in brainstorm mode.
type BrainstormIdeaData struct {
	ID            uuid.UUID `json:"id"`
	Text          string    `json:"text"`
	ParticipantID uuid.UUID `json:"participant_id"`
}

// BrainstormPhaseData carries the new phase name.
type BrainstormPhaseData struct {
	Phase string `json:"phase"` // collecting | voting | results
}

// BrainstormVoteData carries updated vote count for an idea.
type BrainstormVoteData struct {
	IdeaID     uuid.UUID `json:"idea_id"`
	VotesCount int       `json:"votes_count"`
}

// ErrorData carries an error message sent to the client.
type ErrorData struct {
	Message string `json:"message"`
}

// --- Incoming data payloads ---

// SubmitAnswerData is sent by a participant to submit a choice answer.
type SubmitAnswerData struct {
	QuestionID uuid.UUID       `json:"question_id"`
	Answer     json.RawMessage `json:"answer"` // index or array of indices
}

// SubmitTextData is sent by a participant to submit an open-text answer.
type SubmitTextData struct {
	QuestionID uuid.UUID `json:"question_id"`
	Text       string    `json:"text"`
}

// SubmitVoteData is sent by a participant to vote for a brainstorm idea.
type SubmitVoteData struct {
	IdeaID uuid.UUID `json:"idea_id"`
}

// SubmitIdeaData is sent by a participant to add a brainstorm idea.
type SubmitIdeaData struct {
	Text string `json:"text"`
}

// ShowQuestionData is sent by the organizer to display the next question.
type ShowQuestionData struct {
	QuestionID uuid.UUID `json:"question_id"`
}

// HideAnswerData is sent by the organizer to hide a specific answer.
type HideAnswerData struct {
	AnswerID uuid.UUID `json:"answer_id"`
	IsHidden bool      `json:"is_hidden"`
}

// BrainstormHideIdeaData is sent by the organizer to hide/show an idea.
type BrainstormHideIdeaData struct {
	IdeaID   uuid.UUID `json:"idea_id"`
	IsHidden bool      `json:"is_hidden"`
}

// BrainstormChangePhaseData is sent by the organizer to advance the brainstorm phase.
type BrainstormChangePhaseData struct {
	Phase string `json:"phase"`
}

// OpenPresentationData is sent by the organizer to display a presentation
// to all participants. SlidePosition is 1-indexed; omit or pass 1 to start
// from the first slide.
type OpenPresentationData struct {
	PresentationID uuid.UUID `json:"presentation_id"`
	SlidePosition  int       `json:"slide_position,omitempty"`
}

// ChangeSlideData is sent by the organizer to jump to a different slide in
// the currently-open presentation. SlidePosition is 1-indexed.
type ChangeSlideData struct {
	SlidePosition int `json:"slide_position"`
}

// --- Presentation outgoing payloads ---

// SlideInfo is a single slide's renderable metadata sent over WS.
type SlideInfo struct {
	ID       uuid.UUID `json:"id"`
	Position int       `json:"position"`
	ImageURL string    `json:"image_url"`
	Width    int       `json:"width"`
	Height   int       `json:"height"`
}

// PresentationOpenedData is broadcast when the organizer opens a presentation
// AND is replayed as a snapshot to any client that connects while a
// presentation is already active.
type PresentationOpenedData struct {
	PresentationID       uuid.UUID   `json:"presentation_id"`
	Title                string      `json:"title"`
	SlideCount           int         `json:"slide_count"`
	CurrentSlidePosition int         `json:"current_slide_position"`
	Slides               []SlideInfo `json:"slides"`
}

// SlideChangedData is broadcast when the organizer jumps to a different slide.
type SlideChangedData struct {
	SlidePosition int `json:"slide_position"`
}
