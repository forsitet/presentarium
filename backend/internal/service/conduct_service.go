package service

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"log/slog"
	"strings"
	"time"

	"github.com/google/uuid"

	"presentarium/internal/errs"
	"presentarium/internal/model"
	"presentarium/internal/repository"
	"presentarium/internal/ws"
	"presentarium/pkg/badwords"
	"presentarium/pkg/normalize"
	"presentarium/pkg/scoring"
)

// ConductService orchestrates live quiz sessions: showing questions, managing
// the server-side timer, and broadcasting results after each question.
type ConductService interface {
	// HandleMessage is wired as the WebSocket message handler.
	HandleMessage(c *ws.Client, room *ws.Room, env ws.Envelope)
	// EndQuestion ends the current question early for the given room.
	// Called from the HTTP handler (PATCH /api/rooms/:code/state {action:"end_question"}).
	EndQuestion(ctx context.Context, userID uuid.UUID, roomCode string) error
	// EndSession ends the session, broadcasts session_end with the final leaderboard,
	// and updates the session status to "finished" in the database.
	EndSession(ctx context.Context, userID uuid.UUID, roomCode string) error
}

type conductService struct {
	questionRepo repository.QuestionRepository
	sessionRepo  repository.SessionRepository
	pollRepo     repository.PollRepository
	answerRepo   repository.AnswerRepository
	hub          *ws.Hub
}

// NewConductService creates a new ConductService.
func NewConductService(
	questionRepo repository.QuestionRepository,
	sessionRepo repository.SessionRepository,
	pollRepo repository.PollRepository,
	answerRepo repository.AnswerRepository,
	hub *ws.Hub,
) ConductService {
	return &conductService{
		questionRepo: questionRepo,
		sessionRepo:  sessionRepo,
		pollRepo:     pollRepo,
		answerRepo:   answerRepo,
		hub:          hub,
	}
}

// HandleMessage dispatches incoming WebSocket messages from the organizer or participants.
func (s *conductService) HandleMessage(c *ws.Client, room *ws.Room, env ws.Envelope) {
	switch env.Type {
	case ws.MsgTypeShowQuestion:
		s.handleShowQuestion(c, room, env)
	case ws.MsgTypeEndQuestion:
		s.handleEndQuestion(c, room)
	case ws.MsgTypeSubmitAnswer:
		s.handleSubmitAnswer(c, room, env)
	case ws.MsgTypeSubmitText:
		s.handleSubmitText(c, room, env)
	}
}

// EndQuestion implements the HTTP-triggered early question termination.
func (s *conductService) EndQuestion(ctx context.Context, userID uuid.UUID, roomCode string) error {
	session, err := s.sessionRepo.GetByCode(ctx, roomCode)
	if err != nil {
		return err
	}

	poll, err := s.pollRepo.GetByID(ctx, session.PollID)
	if err != nil {
		return err
	}
	if poll.UserID != userID {
		return errs.ErrForbidden
	}

	room := s.hub.GetRoom(roomCode)
	if room == nil {
		return errs.ErrNotFound
	}

	current := room.CurrentQuestion()
	if current == nil {
		return errs.ErrValidation
	}

	// Stop the timer and finish the question.
	room.StopCurrentTimer()
	s.finishQuestion(room, current.ID, session.ID)
	return nil
}

// EndSession ends the session: verifies ownership, updates DB status to "finished",
// broadcasts session_end with the final leaderboard, and removes the room from the Hub.
func (s *conductService) EndSession(ctx context.Context, userID uuid.UUID, roomCode string) error {
	session, err := s.sessionRepo.GetByCode(ctx, roomCode)
	if err != nil {
		return err
	}

	poll, err := s.pollRepo.GetByID(ctx, session.PollID)
	if err != nil {
		return err
	}
	if poll.UserID != userID {
		return errs.ErrForbidden
	}

	room := s.hub.GetRoom(roomCode)

	// Stop any running question timer before ending.
	if room != nil {
		room.StopCurrentTimer()
	}

	// Persist the finished status.
	now := time.Now().UTC().Format(time.RFC3339Nano)
	if err := s.sessionRepo.UpdateStatus(ctx, session.ID, "finished", nil, &now); err != nil {
		return err
	}

	if room == nil {
		return nil
	}

	room.SetState(ws.StateFinished)

	// Build final leaderboard (top 10).
	lbRows, _ := s.answerRepo.GetLeaderboard(ctx, session.ID)
	rankings := make([]ws.LeaderboardEntry, 0, len(lbRows))
	rankMap := make(map[uuid.UUID]int, len(lbRows))
	scoreMap := make(map[uuid.UUID]int, len(lbRows))
	for i, row := range lbRows {
		rank := i + 1
		rankMap[row.ParticipantID] = rank
		scoreMap[row.ParticipantID] = row.TotalScore
		rankings = append(rankings, ws.LeaderboardEntry{
			Rank:  rank,
			ID:    row.ParticipantID,
			Name:  row.Name,
			Score: row.TotalScore,
		})
	}

	// Broadcast session_end to the organizer (no personal data).
	if msg, err := ws.NewEnvelope(ws.MsgTypeSessionEnd, ws.SessionEndData{Rankings: rankings}); err == nil {
		room.SendToOrganizer(msg)
	}

	// Send personalised session_end to each participant.
	room.ForEachParticipant(func(c *ws.Client) {
		pid := c.ParticipantID()
		data := ws.SessionEndData{Rankings: rankings}
		if pid != nil {
			data.MyRank = rankMap[*pid]
			data.MyScore = scoreMap[*pid]
		}
		if msg, err := ws.NewEnvelope(ws.MsgTypeSessionEnd, data); err == nil {
			room.SendToClient(c, msg)
		}
	})

	slog.Info("session ended", "room_code", roomCode, "session_id", session.ID)
	return nil
}

// handleShowQuestion processes the organizer's show_question WS message.
func (s *conductService) handleShowQuestion(c *ws.Client, room *ws.Room, env ws.Envelope) {
	var data ws.ShowQuestionData
	if err := json.Unmarshal(env.Data, &data); err != nil {
		sendError(room, c, "invalid show_question payload")
		return
	}

	ctx := context.Background()

	// Fetch the question from the database.
	q, err := s.questionRepo.GetByID(ctx, data.QuestionID)
	if err != nil {
		sendError(room, c, "question not found")
		return
	}

	// Verify the question belongs to this session's poll.
	session, err := s.sessionRepo.GetByCode(ctx, room.Code())
	if err != nil {
		sendError(room, c, "session not found")
		return
	}
	if q.PollID != session.PollID {
		sendError(room, c, "question does not belong to this poll")
		return
	}

	// Load poll to get the scoring rule.
	poll, err := s.pollRepo.GetByID(ctx, session.PollID)
	if err != nil {
		sendError(room, c, "poll not found")
		return
	}

	// Count total questions in the poll for the position display.
	allQuestions, err := s.questionRepo.ListByPoll(ctx, session.PollID)
	if err != nil {
		allQuestions = nil
	}
	total := len(allQuestions)

	// Stop any existing timer (in case organizer skips a question).
	room.StopCurrentTimer()

	// Record the active question in the room with scoring metadata.
	activeQ := &ws.ActiveQuestion{
		ID:           q.ID,
		Type:         q.Type,
		TimeLimitSec: q.TimeLimitSeconds,
		StartedAt:    time.Now().Unix(),
		ScoringRule:  poll.ScoringRule,
		Points:       q.Points,
	}
	room.SetCurrentQuestion(activeQ)
	room.SetState(ws.StateShowingQuestion)
	room.ResetAnswerCount()

	// Build options payload hiding is_correct from participants.
	optionsForParticipants, _ := buildParticipantOptions(q.Options)

	// Broadcast question_start to all clients.
	startData := ws.QuestionStartData{
		QuestionID:   q.ID,
		Type:         q.Type,
		Text:         q.Text,
		Options:      optionsForParticipants,
		TimeLimitSec: q.TimeLimitSeconds,
		Points:       q.Points,
		Position:     q.Position,
		Total:        total,
	}
	if msg, err := ws.NewEnvelope(ws.MsgTypeQuestionStart, startData); err == nil {
		room.Broadcast(msg)
	}

	// Start the server-side countdown timer.
	s.startTimer(room, q.ID, session.ID, q.TimeLimitSeconds)
}

// handleEndQuestion processes the organizer's end_question WS message (early termination).
func (s *conductService) handleEndQuestion(c *ws.Client, room *ws.Room) {
	current := room.CurrentQuestion()
	if current == nil {
		sendError(room, c, "no active question")
		return
	}

	ctx := context.Background()
	session, err := s.sessionRepo.GetByCode(ctx, room.Code())
	if err != nil {
		sendError(room, c, "session not found")
		return
	}

	room.StopCurrentTimer()
	s.finishQuestion(room, current.ID, session.ID)
}

// startTimer launches a goroutine that ticks every second and auto-finishes
// the question when the time limit is reached.
func (s *conductService) startTimer(room *ws.Room, questionID, sessionID uuid.UUID, limitSec int) {
	stopCh := make(chan struct{}, 1)
	room.SetStopTimer(stopCh)

	go func() {
		remaining := limitSec
		for {
			// Broadcast the current remaining seconds.
			if msg, err := ws.NewEnvelope(ws.MsgTypeTimerTick, ws.TimerTickData{Remaining: remaining}); err == nil {
				room.Broadcast(msg)
			}

			if remaining == 0 {
				// Timer expired — finish the question automatically.
				s.finishQuestion(room, questionID, sessionID)
				return
			}
			remaining--

			select {
			case <-stopCh:
				return // Stopped early by organizer or EndQuestion API call.
			case <-time.After(time.Second):
			}
		}
	}()
}

// finishQuestion atomically ends the current question, broadcasts question_end,
// computes answer distribution (results) and leaderboard, then broadcasts both.
func (s *conductService) finishQuestion(room *ws.Room, questionID, sessionID uuid.UUID) {
	// TryFinishQuestion atomically transitions the room state.
	// Returns false if already finished (prevents double broadcast).
	if !room.TryFinishQuestion(questionID) {
		return
	}

	ctx := context.Background()

	// Fetch question to reveal correct options in question_end.
	q, err := s.questionRepo.GetByID(ctx, questionID)
	var optionsRevealed json.RawMessage
	if err == nil {
		optionsRevealed, _ = json.Marshal(q.Options)
	}

	// Broadcast question_end with correct options revealed.
	endData := ws.QuestionEndData{
		QuestionID: questionID,
		Options:    optionsRevealed,
	}
	if msg, err := ws.NewEnvelope(ws.MsgTypeQuestionEnd, endData); err == nil {
		room.Broadcast(msg)
	}

	// Compute answer distribution for results.
	answers, _ := s.answerRepo.ListByQuestion(ctx, questionID, sessionID)
	answerCounts := make(map[string]int)
	for _, a := range answers {
		key := fmt.Sprintf("%v", a.Answer)
		answerCounts[key]++
	}

	resultsData := ws.ResultsData{
		QuestionID:   questionID,
		AnswerCounts: answerCounts,
	}
	if msg, err := ws.NewEnvelope(ws.MsgTypeResults, resultsData); err == nil {
		room.Broadcast(msg)
	}

	// Compute leaderboard — top 5 after the question.
	lbRows, _ := s.answerRepo.GetLeaderboard(ctx, sessionID)
	const top = 5
	top5 := make([]ws.LeaderboardEntry, 0, top)
	rankMap := make(map[uuid.UUID]int, len(lbRows))
	scoreMap := make(map[uuid.UUID]int, len(lbRows))
	for i, row := range lbRows {
		rank := i + 1
		rankMap[row.ParticipantID] = rank
		scoreMap[row.ParticipantID] = row.TotalScore
		if i < top {
			top5 = append(top5, ws.LeaderboardEntry{
				Rank:  rank,
				ID:    row.ParticipantID,
				Name:  row.Name,
				Score: row.TotalScore,
			})
		}
	}

	// Send the leaderboard to the organizer (no personal data).
	if msg, err := ws.NewEnvelope(ws.MsgTypeLeaderboard, ws.LeaderboardData{Rankings: top5}); err == nil {
		room.SendToOrganizer(msg)
	}

	// Send a personalised leaderboard to each participant.
	room.ForEachParticipant(func(c *ws.Client) {
		pid := c.ParticipantID()
		if pid == nil {
			return
		}
		data := ws.LeaderboardData{
			Rankings: top5,
			MyRank:   rankMap[*pid],
			MyScore:  scoreMap[*pid],
		}
		if msg, err := ws.NewEnvelope(ws.MsgTypeLeaderboard, data); err == nil {
			room.SendToClient(c, msg)
		}
	})

	slog.Info("question finished", "question_id", questionID, "session_id", sessionID, "answers", len(answers))
}

// participantOption is the option payload sent to participants (no is_correct field).
type participantOption struct {
	Text     string `json:"text"`
	ImageURL string `json:"image_url,omitempty"`
}

// buildParticipantOptions builds option JSON without the is_correct field.
// is_correct is revealed only after question_end.
func buildParticipantOptions(opts model.OptionList) (json.RawMessage, error) {
	masked := make([]participantOption, len(opts))
	for i, o := range opts {
		masked[i] = participantOption{Text: o.Text, ImageURL: o.ImageURL}
	}
	return json.Marshal(masked)
}

// handleSubmitAnswer processes a participant's choice answer (single/multiple/image_choice).
func (s *conductService) handleSubmitAnswer(c *ws.Client, room *ws.Room, env ws.Envelope) {
	if c.Role() != ws.RoleParticipant {
		sendError(room, c, "only participants can submit answers")
		return
	}
	participantID := c.ParticipantID()
	if participantID == nil {
		sendError(room, c, "participant not registered")
		return
	}

	current := room.CurrentQuestion()
	if current == nil {
		sendError(room, c, "question has ended or not started")
		return
	}

	var data ws.SubmitAnswerData
	if err := json.Unmarshal(env.Data, &data); err != nil {
		sendError(room, c, "invalid submit_answer payload")
		return
	}
	if data.QuestionID != current.ID {
		sendError(room, c, "question mismatch")
		return
	}

	ctx := context.Background()

	// Prevent duplicate answers.
	_, err := s.answerRepo.GetByParticipantAndQuestion(ctx, *participantID, data.QuestionID)
	if err == nil {
		sendError(room, c, "already answered this question")
		return
	}
	if !errors.Is(err, errs.ErrNotFound) {
		sendError(room, c, "internal error")
		return
	}

	// Fetch question to determine correctness.
	q, err := s.questionRepo.GetByID(ctx, data.QuestionID)
	if err != nil {
		sendError(room, c, "question not found")
		return
	}

	isCorrect := computeIsCorrect(q, data.Answer)
	responseTimeMs := int(time.Since(time.Unix(current.StartedAt, 0)).Milliseconds())
	earnedScore := scoring.CalculateScore(current.Points, current.TimeLimitSec, responseTimeMs, current.ScoringRule, isCorrect)

	answer := &model.Answer{
		ID:             uuid.New(),
		ParticipantID:  *participantID,
		QuestionID:     data.QuestionID,
		SessionID:      room.SessionID(),
		Answer:         json.RawMessage(data.Answer),
		IsCorrect:      isCorrect,
		Score:          earnedScore,
		ResponseTimeMs: responseTimeMs,
		IsHidden:       false,
		AnsweredAt:     time.Now(),
	}

	if err := s.answerRepo.Create(ctx, answer); err != nil {
		sendError(room, c, "failed to save answer")
		return
	}

	if earnedScore > 0 {
		_ = s.answerRepo.UpdateParticipantScore(ctx, *participantID, earnedScore)
	}

	count := room.IncrementAnswerCount()
	total := room.ParticipantCount()

	if msg, err2 := ws.NewEnvelope(ws.MsgTypeAnswerAccepted, ws.AnswerAcceptedData{
		QuestionID: data.QuestionID,
		Score:      earnedScore,
		IsCorrect:  isCorrect,
	}); err2 == nil {
		room.SendToClient(c, msg)
	}

	if msg, err2 := ws.NewEnvelope(ws.MsgTypeAnswerCount, ws.AnswerCountData{
		Answered: count,
		Total:    total,
	}); err2 == nil {
		room.SendToOrganizer(msg)
	}

	slog.Debug("answer submitted", "participant_id", *participantID, "question_id", data.QuestionID, "score", earnedScore, "answered", count, "total", total)
}

// handleSubmitText processes a participant's text answer (open_text, word_cloud).
func (s *conductService) handleSubmitText(c *ws.Client, room *ws.Room, env ws.Envelope) {
	if c.Role() != ws.RoleParticipant {
		sendError(room, c, "only participants can submit answers")
		return
	}
	participantID := c.ParticipantID()
	if participantID == nil {
		sendError(room, c, "participant not registered")
		return
	}

	current := room.CurrentQuestion()
	if current == nil {
		sendError(room, c, "question has ended or not started")
		return
	}

	var data ws.SubmitTextData
	if err := json.Unmarshal(env.Data, &data); err != nil {
		sendError(room, c, "invalid submit_text payload")
		return
	}
	if data.QuestionID != current.ID {
		sendError(room, c, "question mismatch")
		return
	}

	ctx := context.Background()

	// Prevent duplicate answers.
	_, err := s.answerRepo.GetByParticipantAndQuestion(ctx, *participantID, data.QuestionID)
	if err == nil {
		sendError(room, c, "already answered this question")
		return
	}
	if !errors.Is(err, errs.ErrNotFound) {
		sendError(room, c, "internal error")
		return
	}

	responseTimeMs := int(time.Since(time.Unix(current.StartedAt, 0)).Milliseconds())

	// For word_cloud questions: normalize, filter, and store the processed text.
	storedText := data.Text
	if current.Type == "word_cloud" {
		filtered, _ := badwords.Filter(data.Text)
		storedText = filtered
	}

	textJSON, _ := json.Marshal(storedText)

	answer := &model.Answer{
		ID:             uuid.New(),
		ParticipantID:  *participantID,
		QuestionID:     data.QuestionID,
		SessionID:      room.SessionID(),
		Answer:         json.RawMessage(textJSON),
		IsCorrect:      nil, // text answers have no correct/incorrect concept
		Score:          0,
		ResponseTimeMs: responseTimeMs,
		IsHidden:       false,
		AnsweredAt:     time.Now(),
	}

	if err := s.answerRepo.Create(ctx, answer); err != nil {
		sendError(room, c, "failed to save answer")
		return
	}

	count := room.IncrementAnswerCount()
	total := room.ParticipantCount()

	if msg, err2 := ws.NewEnvelope(ws.MsgTypeAnswerAccepted, ws.AnswerAcceptedData{
		QuestionID: data.QuestionID,
		Score:      0,
	}); err2 == nil {
		room.SendToClient(c, msg)
	}

	if msg, err2 := ws.NewEnvelope(ws.MsgTypeAnswerCount, ws.AnswerCountData{
		Answered: count,
		Total:    total,
	}); err2 == nil {
		room.SendToOrganizer(msg)
	}

	// For word_cloud questions: update in-memory frequency and broadcast to organizer.
	if current.Type == "word_cloud" {
		normalized := normalize.Text(storedText)
		words := strings.Fields(normalized)
		room.AddWordCloudWords(words)

		topWords := room.WordCloudTopWords(100)
		if msg, err2 := ws.NewEnvelope(ws.MsgTypeWordcloudUpdate, ws.WordcloudUpdateData{Words: topWords}); err2 == nil {
			room.SendToOrganizer(msg)
		}
	}

	slog.Debug("text answer submitted", "participant_id", *participantID, "question_id", data.QuestionID, "answered", count, "total", total)
}

// computeIsCorrect evaluates whether the given raw answer is correct for the question.
// Returns nil for question types that have no correct/incorrect concept.
func computeIsCorrect(q *model.Question, rawAnswer json.RawMessage) *bool {
	trueVal := true
	falseVal := false

	switch q.Type {
	case "single_choice", "image_choice":
		var idx int
		if err := json.Unmarshal(rawAnswer, &idx); err != nil {
			return &falseVal
		}
		if idx < 0 || idx >= len(q.Options) {
			return &falseVal
		}
		if q.Options[idx].IsCorrect {
			return &trueVal
		}
		return &falseVal

	case "multiple_choice":
		var indices []int
		if err := json.Unmarshal(rawAnswer, &indices); err != nil {
			return &falseVal
		}
		selected := make(map[int]bool, len(indices))
		for _, i := range indices {
			selected[i] = true
		}
		for i, opt := range q.Options {
			if opt.IsCorrect && !selected[i] {
				return &falseVal
			}
			if !opt.IsCorrect && selected[i] {
				return &falseVal
			}
		}
		return &trueVal

	default:
		// open_text, word_cloud, brainstorm — no correct/incorrect
		return nil
	}
}

// sendError sends an error envelope to the given client via the room.
func sendError(room *ws.Room, c *ws.Client, message string) {
	if msg, err := ws.NewEnvelope(ws.MsgTypeError, ws.ErrorData{Message: message}); err == nil {
		room.SendToClient(c, msg)
	}
}
