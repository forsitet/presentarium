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
	questionRepo    repository.QuestionRepository
	sessionRepo     repository.SessionRepository
	pollRepo        repository.PollRepository
	answerRepo      repository.AnswerRepository
	brainstormRepo  repository.BrainstormRepository
	presentationSvc PresentationService
	hub             *ws.Hub
}

// NewConductService creates a new ConductService.
// presentationSvc may be nil — in that case presentation WS messages produce
// an error response but the rest of the service behaves normally. This keeps
// the dependency optional for tests that don't exercise the presentation flow.
func NewConductService(
	questionRepo repository.QuestionRepository,
	sessionRepo repository.SessionRepository,
	pollRepo repository.PollRepository,
	answerRepo repository.AnswerRepository,
	brainstormRepo repository.BrainstormRepository,
	presentationSvc PresentationService,
	hub *ws.Hub,
) ConductService {
	return &conductService{
		questionRepo:    questionRepo,
		sessionRepo:     sessionRepo,
		pollRepo:        pollRepo,
		answerRepo:      answerRepo,
		brainstormRepo:  brainstormRepo,
		presentationSvc: presentationSvc,
		hub:             hub,
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
	case ws.MsgTypeSubmitIdea:
		s.handleSubmitIdea(c, room, env)
	case ws.MsgTypeSubmitVote:
		s.handleSubmitVote(c, room, env)
	case ws.MsgTypeBrainstormHideIdea:
		s.handleBrainstormHideIdea(c, room, env)
	case ws.MsgTypeBrainstormChangePhase:
		s.handleBrainstormChangePhase(c, room, env)
	case ws.MsgTypeOpenPresentation:
		s.handleOpenPresentation(c, room, env)
	case ws.MsgTypeChangeSlide:
		s.handleChangeSlide(c, room, env)
	case ws.MsgTypeClosePresentation:
		s.handleClosePresentation(c, room)
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
	if q.Type == "brainstorm" {
		room.InitBrainstorm()
	}

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

	count, avgMs := room.IncrementAnswerCount(responseTimeMs)
	total := room.ParticipantCount()

	if msg, err2 := ws.NewEnvelope(ws.MsgTypeAnswerAccepted, ws.AnswerAcceptedData{
		QuestionID: data.QuestionID,
		Score:      earnedScore,
		IsCorrect:  isCorrect,
	}); err2 == nil {
		room.SendToClient(c, msg)
	}

	if msg, err2 := ws.NewEnvelope(ws.MsgTypeAnswerCount, ws.AnswerCountData{
		Answered:      count,
		Total:         total,
		ParticipantID: *participantID,
		AvgResponseMs: avgMs,
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

	// Prevent duplicate answers (except word_cloud which allows multiple submissions).
	if current.Type != "word_cloud" {
		_, err := s.answerRepo.GetByParticipantAndQuestion(ctx, *participantID, data.QuestionID)
		if err == nil {
			sendError(room, c, "already answered this question")
			return
		}
		if !errors.Is(err, errs.ErrNotFound) {
			sendError(room, c, "internal error")
			return
		}
	}

	responseTimeMs := int(time.Since(time.Unix(current.StartedAt, 0)).Milliseconds())

	// For word_cloud and open_text questions: apply badwords filter before storing.
	storedText := data.Text
	if current.Type == "word_cloud" || current.Type == "open_text" {
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

	count, avgMs := room.IncrementAnswerCount(responseTimeMs)
	total := room.ParticipantCount()

	if msg, err2 := ws.NewEnvelope(ws.MsgTypeAnswerAccepted, ws.AnswerAcceptedData{
		QuestionID: data.QuestionID,
		Score:      0,
	}); err2 == nil {
		room.SendToClient(c, msg)
	}

	if msg, err2 := ws.NewEnvelope(ws.MsgTypeAnswerCount, ws.AnswerCountData{
		Answered:      count,
		Total:         total,
		ParticipantID: *participantID,
		AvgResponseMs: avgMs,
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

// handleSubmitIdea processes a participant's idea submission in brainstorm mode.
func (s *conductService) handleSubmitIdea(c *ws.Client, room *ws.Room, env ws.Envelope) {
	if c.Role() != ws.RoleParticipant {
		sendError(room, c, "only participants can submit ideas")
		return
	}
	participantID := c.ParticipantID()
	if participantID == nil {
		sendError(room, c, "participant not registered")
		return
	}

	current := room.CurrentQuestion()
	if current == nil || current.Type != "brainstorm" {
		sendError(room, c, "no active brainstorm question")
		return
	}
	if room.BrainstormPhase() != "collecting" {
		sendError(room, c, "idea collection phase is over")
		return
	}

	var data ws.SubmitIdeaData
	if err := json.Unmarshal(env.Data, &data); err != nil {
		sendError(room, c, "invalid submit_idea payload")
		return
	}
	if len([]rune(data.Text)) > 300 {
		sendError(room, c, "idea text exceeds 300 characters")
		return
	}

	// Check per-participant idea limit (5 ideas max).
	if room.BrainstormIdeaCount(*participantID) >= 5 {
		sendError(room, c, "Достигнут лимит идей")
		return
	}

	// Apply badwords filter.
	filtered, _ := badwords.Filter(data.Text)

	ctx := context.Background()
	session, err := s.sessionRepo.GetByCode(ctx, room.Code())
	if err != nil {
		sendError(room, c, "session not found")
		return
	}

	idea := &model.BrainstormIdea{
		ID:            uuid.New(),
		SessionID:     session.ID,
		QuestionID:    current.ID,
		ParticipantID: *participantID,
		Text:          filtered,
		IsHidden:      false,
		VotesCount:    0,
		CreatedAt:     time.Now(),
	}

	if err := s.brainstormRepo.CreateIdea(ctx, idea); err != nil {
		sendError(room, c, "failed to save idea")
		return
	}

	count := room.IncrementBrainstormIdeaCount(*participantID)
	slog.Debug("brainstorm idea submitted", "participant_id", *participantID, "idea_id", idea.ID, "count", count)

	// Broadcast new idea to organizer and notify the submitting participant.
	ideaData := ws.BrainstormIdeaData{
		ID:            idea.ID,
		Text:          idea.Text,
		ParticipantID: idea.ParticipantID,
	}
	if msg, err2 := ws.NewEnvelope(ws.MsgTypeBrainstormIdeaAdded, ideaData); err2 == nil {
		room.SendToOrganizer(msg)
		room.SendToClient(c, msg)
	}
}

// handleSubmitVote processes a participant's vote for a brainstorm idea.
func (s *conductService) handleSubmitVote(c *ws.Client, room *ws.Room, env ws.Envelope) {
	if c.Role() != ws.RoleParticipant {
		sendError(room, c, "only participants can vote")
		return
	}
	participantID := c.ParticipantID()
	if participantID == nil {
		sendError(room, c, "participant not registered")
		return
	}

	current := room.CurrentQuestion()
	if current == nil || current.Type != "brainstorm" {
		sendError(room, c, "no active brainstorm question")
		return
	}
	if room.BrainstormPhase() != "voting" {
		sendError(room, c, "not in voting phase")
		return
	}

	var data ws.SubmitVoteData
	if err := json.Unmarshal(env.Data, &data); err != nil {
		sendError(room, c, "invalid submit_vote payload")
		return
	}

	ctx := context.Background()

	// Fetch the idea to validate ownership and existence.
	idea, err := s.brainstormRepo.GetIdea(ctx, data.IdeaID)
	if err != nil {
		sendError(room, c, "idea not found")
		return
	}
	if idea.IsHidden {
		sendError(room, c, "cannot vote for hidden idea")
		return
	}
	// Cannot vote for own idea.
	if idea.ParticipantID == *participantID {
		sendError(room, c, "cannot vote for your own idea")
		return
	}

	// Check vote limit (3 votes per participant per question).
	if room.BrainstormVoteCount(*participantID) >= 3 {
		sendError(room, c, "vote limit reached (max 3)")
		return
	}

	vote := &model.BrainstormVote{
		ID:            uuid.New(),
		IdeaID:        data.IdeaID,
		ParticipantID: *participantID,
		CreatedAt:     time.Now(),
	}

	if err := s.brainstormRepo.CreateVote(ctx, vote); err != nil {
		if errors.Is(err, errs.ErrConflict) {
			sendError(room, c, "already voted for this idea")
			return
		}
		sendError(room, c, "failed to save vote")
		return
	}

	room.IncrementBrainstormVoteCount(*participantID)

	// Fetch updated votes count for the idea.
	updatedIdea, err2 := s.brainstormRepo.GetIdea(ctx, data.IdeaID)
	votesCount := 0
	if err2 == nil {
		votesCount = updatedIdea.VotesCount
	}

	slog.Debug("brainstorm vote submitted", "participant_id", *participantID, "idea_id", data.IdeaID, "session_id", room.SessionID())

	// Broadcast updated vote count to all clients.
	voteData := ws.BrainstormVoteData{
		IdeaID:     data.IdeaID,
		VotesCount: votesCount,
	}
	if msg, err3 := ws.NewEnvelope(ws.MsgTypeBrainstormVoteUpdated, voteData); err3 == nil {
		room.Broadcast(msg)
	}
}

// handleBrainstormHideIdea toggles visibility of a brainstorm idea (organizer only).
func (s *conductService) handleBrainstormHideIdea(c *ws.Client, room *ws.Room, env ws.Envelope) {
	if c.Role() != ws.RoleOrganizer {
		sendError(room, c, "only the organizer can hide ideas")
		return
	}

	var data ws.BrainstormHideIdeaData
	if err := json.Unmarshal(env.Data, &data); err != nil {
		sendError(room, c, "invalid brainstorm_hide_idea payload")
		return
	}

	ctx := context.Background()
	if err := s.brainstormRepo.SetIdeaHidden(ctx, data.IdeaID, data.IsHidden); err != nil {
		sendError(room, c, "failed to update idea visibility")
		return
	}

	// Notify all clients: participants will remove/add the idea from their list.
	if msg, err := ws.NewEnvelope(ws.MsgTypeAnswerHidden, ws.BrainstormHideIdeaData{
		IdeaID:   data.IdeaID,
		IsHidden: data.IsHidden,
	}); err == nil {
		room.Broadcast(msg)
	}
}

// handleBrainstormChangePhase advances the brainstorm phase (organizer only).
func (s *conductService) handleBrainstormChangePhase(c *ws.Client, room *ws.Room, env ws.Envelope) {
	if c.Role() != ws.RoleOrganizer {
		sendError(room, c, "only the organizer can change brainstorm phase")
		return
	}

	current := room.CurrentQuestion()
	if current == nil || current.Type != "brainstorm" {
		sendError(room, c, "no active brainstorm question")
		return
	}

	var data ws.BrainstormChangePhaseData
	if err := json.Unmarshal(env.Data, &data); err != nil {
		sendError(room, c, "invalid brainstorm_change_phase payload")
		return
	}

	// Validate allowed phases.
	switch data.Phase {
	case "collecting", "voting", "results":
	default:
		sendError(room, c, "invalid phase: must be collecting, voting, or results")
		return
	}

	room.SetBrainstormPhase(data.Phase)
	slog.Info("brainstorm phase changed", "room_code", room.Code(), "phase", data.Phase)

	// If transitioning to results, fetch ranked ideas and broadcast.
	if data.Phase == "results" {
		ctx := context.Background()
		session, err := s.sessionRepo.GetByCode(ctx, room.Code())
		if err == nil {
			ideas, err2 := s.brainstormRepo.ListIdeasRanked(ctx, session.ID, current.ID)
			if err2 == nil {
				// Build a ranked list payload to broadcast along with phase change.
				type rankedIdea struct {
					ID         uuid.UUID `json:"id"`
					Text       string    `json:"text"`
					VotesCount int       `json:"votes_count"`
				}
				ranked := make([]rankedIdea, len(ideas))
				for i, idea := range ideas {
					ranked[i] = rankedIdea{ID: idea.ID, Text: idea.Text, VotesCount: idea.VotesCount}
				}

				type phaseWithResults struct {
					Phase  string       `json:"phase"`
					Ideas  []rankedIdea `json:"ideas,omitempty"`
				}
				payload := phaseWithResults{Phase: data.Phase, Ideas: ranked}
				if msg, err3 := ws.NewEnvelope(ws.MsgTypeBrainstormPhaseChanged, payload); err3 == nil {
					room.Broadcast(msg)
					return
				}
			}
		}
	}

	phaseData := ws.BrainstormPhaseData{Phase: data.Phase}
	if msg, err := ws.NewEnvelope(ws.MsgTypeBrainstormPhaseChanged, phaseData); err == nil {
		room.Broadcast(msg)
	}
}

// handleOpenPresentation loads a presentation, verifies organizer ownership +
// "ready" status, records the active state on the session row and in the Room,
// then broadcasts the full snapshot (with resolved slide URLs) to everyone.
func (s *conductService) handleOpenPresentation(c *ws.Client, room *ws.Room, env ws.Envelope) {
	if s.presentationSvc == nil {
		sendError(room, c, "presentations not available on this server")
		return
	}
	userID := c.UserID()
	if userID == nil {
		sendError(room, c, "organizer identity unknown")
		return
	}

	var data ws.OpenPresentationData
	if err := json.Unmarshal(env.Data, &data); err != nil {
		sendError(room, c, "invalid open_presentation payload")
		return
	}

	ctx := context.Background()
	detail, err := s.presentationSvc.Get(ctx, *userID, data.PresentationID)
	if err != nil {
		if errors.Is(err, errs.ErrForbidden) {
			sendError(room, c, "not your presentation")
			return
		}
		if errors.Is(err, errs.ErrNotFound) {
			sendError(room, c, "presentation not found")
			return
		}
		sendError(room, c, "failed to load presentation")
		return
	}
	if detail.Status != "ready" {
		sendError(room, c, "presentation is not ready")
		return
	}
	if len(detail.Slides) == 0 {
		sendError(room, c, "presentation has no slides")
		return
	}

	// Normalise the starting slide position to the [1..N] range.
	startPos := data.SlidePosition
	if startPos < 1 {
		startPos = 1
	}
	if startPos > len(detail.Slides) {
		startPos = len(detail.Slides)
	}

	// Build the Room-level snapshot — resolved URLs included so participants
	// (who have no HTTP/JWT access to /api/presentations) get everything over WS.
	slideInfos := make([]ws.SlideInfo, 0, len(detail.Slides))
	for _, sl := range detail.Slides {
		slideInfos = append(slideInfos, ws.SlideInfo{
			ID:       sl.ID,
			Position: sl.Position,
			ImageURL: sl.ImageURL,
			Width:    sl.Width,
			Height:   sl.Height,
		})
	}
	active := &ws.ActivePresentation{
		ID:              detail.ID,
		Title:           detail.Title,
		SlideCount:      len(slideInfos),
		CurrentPosition: startPos,
		Slides:          slideInfos,
	}
	room.SetActivePresentation(active)

	// Persist so the state survives server restarts.
	presID := detail.ID
	if err := s.sessionRepo.UpdateActivePresentation(ctx, room.SessionID(), &presID, &startPos); err != nil {
		slog.Warn("persist active_presentation", "session_id", room.SessionID(), "error", err)
	}

	payload := ws.PresentationOpenedData{
		PresentationID:       active.ID,
		Title:                active.Title,
		SlideCount:           active.SlideCount,
		CurrentSlidePosition: active.CurrentPosition,
		Slides:               active.Slides,
	}
	if msg, err := ws.NewEnvelope(ws.MsgTypePresentationOpened, payload); err == nil {
		room.Broadcast(msg)
	}
	slog.Info("presentation opened",
		"room_code", room.Code(), "presentation_id", active.ID, "slides", active.SlideCount)
}

// handleChangeSlide validates the requested slide position, updates session +
// Room state, and broadcasts slide_changed.
func (s *conductService) handleChangeSlide(c *ws.Client, room *ws.Room, env ws.Envelope) {
	var data ws.ChangeSlideData
	if err := json.Unmarshal(env.Data, &data); err != nil {
		sendError(room, c, "invalid change_slide payload")
		return
	}

	active := room.ActivePresentation()
	if active == nil {
		sendError(room, c, "no active presentation")
		return
	}
	if !room.SetSlidePosition(data.SlidePosition) {
		sendError(room, c, fmt.Sprintf("slide position out of range (1..%d)", active.SlideCount))
		return
	}

	// Persist on a best-effort basis. A write failure should not block the
	// live broadcast — the in-memory state is authoritative while the room
	// is alive; the DB is only used to restore state on restart.
	pos := data.SlidePosition
	presID := active.ID
	if err := s.sessionRepo.UpdateActivePresentation(context.Background(), room.SessionID(), &presID, &pos); err != nil {
		slog.Warn("persist slide position", "session_id", room.SessionID(), "error", err)
	}

	if msg, err := ws.NewEnvelope(ws.MsgTypeSlideChanged, ws.SlideChangedData{
		SlidePosition: data.SlidePosition,
	}); err == nil {
		room.Broadcast(msg)
	}
}

// handleClosePresentation clears the active presentation from both the session
// row and the Room, then broadcasts presentation_closed.
func (s *conductService) handleClosePresentation(c *ws.Client, room *ws.Room) {
	if room.ActivePresentation() == nil {
		sendError(room, c, "no active presentation")
		return
	}

	room.ClearActivePresentation()

	if err := s.sessionRepo.UpdateActivePresentation(context.Background(), room.SessionID(), nil, nil); err != nil {
		slog.Warn("clear active_presentation", "session_id", room.SessionID(), "error", err)
	}

	if msg, err := ws.NewEnvelope(ws.MsgTypePresentationClosed, nil); err == nil {
		room.Broadcast(msg)
	}
	slog.Info("presentation closed", "room_code", room.Code())
}

// sendError sends an error envelope to the given client via the room.
func sendError(room *ws.Room, c *ws.Client, message string) {
	if msg, err := ws.NewEnvelope(ws.MsgTypeError, ws.ErrorData{Message: message}); err == nil {
		room.SendToClient(c, msg)
	}
}
