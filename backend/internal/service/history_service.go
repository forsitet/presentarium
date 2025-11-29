package service

import (
	"context"
	"time"

	"github.com/google/uuid"

	"presentarium/internal/errs"
	"presentarium/internal/repository"
)

// SessionSummaryResponse is returned by ListSessions.
type SessionSummaryResponse struct {
	ID               uuid.UUID  `json:"id"`
	PollID           uuid.UUID  `json:"poll_id"`
	PollTitle        string     `json:"poll_title"`
	RoomCode         string     `json:"room_code"`
	Status           string     `json:"status"`
	ParticipantCount int        `json:"participant_count"`
	AverageScore     float64    `json:"average_score"`
	StartedAt        *time.Time `json:"started_at"`
	FinishedAt       *time.Time `json:"finished_at"`
	CreatedAt        time.Time  `json:"created_at"`
}

// QuestionStatResponse holds per-question stats for a finished session.
type QuestionStatResponse struct {
	ID           uuid.UUID      `json:"id"`
	Text         string         `json:"text"`
	Type         string         `json:"type"`
	Points       int            `json:"points"`
	TotalAnswers int            `json:"total_answers"`
	Distribution map[string]int `json:"answer_distribution,omitempty"`
}

// SessionDetailResponse is returned by GetSession.
type SessionDetailResponse struct {
	ID               uuid.UUID                    `json:"id"`
	PollID           uuid.UUID                    `json:"poll_id"`
	PollTitle        string                       `json:"poll_title"`
	RoomCode         string                       `json:"room_code"`
	Status           string                       `json:"status"`
	ParticipantCount int                          `json:"participant_count"`
	AverageScore     float64                      `json:"average_score"`
	StartedAt        *time.Time                   `json:"started_at"`
	FinishedAt       *time.Time                   `json:"finished_at"`
	CreatedAt        time.Time                    `json:"created_at"`
	Leaderboard      []repository.LeaderboardRow  `json:"leaderboard"`
	Questions        []QuestionStatResponse       `json:"questions"`
}

// ParticipantResultResponse is returned by ListParticipants.
type ParticipantResultResponse struct {
	ID         uuid.UUID  `json:"id"`
	Name       string     `json:"name"`
	TotalScore int        `json:"total_score"`
	JoinedAt   time.Time  `json:"joined_at"`
}

// ParticipantHistorySummary is returned for a participant's session result by token.
type ParticipantHistorySummary struct {
	SessionID         string     `json:"session_id"`
	PollTitle         string     `json:"poll_title"`
	StartedAt         *time.Time `json:"started_at"`
	FinishedAt        *time.Time `json:"finished_at"`
	TotalScore        int        `json:"total_score"`
	MyRank            int        `json:"my_rank"`
	TotalParticipants int        `json:"total_participants"`
}

// HistoryService exposes read-only session history for organizers.
type HistoryService interface {
	ListSessions(ctx context.Context, userID uuid.UUID) ([]SessionSummaryResponse, error)
	GetSession(ctx context.Context, userID, sessionID uuid.UUID) (*SessionDetailResponse, error)
	ListParticipants(ctx context.Context, userID, sessionID uuid.UUID) ([]ParticipantResultResponse, error)
	ListAnswers(ctx context.Context, userID, sessionID uuid.UUID) ([]repository.AnswerExportRow, error)
	ExportCSV(ctx context.Context, userID, sessionID uuid.UUID) ([]repository.AnswerExportRow, *time.Time, error)
	// GetParticipantHistory returns session summary for a participant identified by session_token.
	GetParticipantHistory(ctx context.Context, sessionToken uuid.UUID) (*ParticipantHistorySummary, error)
}

type historyService struct {
	sessionRepo     repository.SessionRepository
	answerRepo      repository.AnswerRepository
	participantRepo repository.ParticipantRepository
	questionRepo    repository.QuestionRepository
}

// NewHistoryService creates a new HistoryService.
func NewHistoryService(
	sessionRepo repository.SessionRepository,
	answerRepo repository.AnswerRepository,
	participantRepo repository.ParticipantRepository,
	questionRepo repository.QuestionRepository,
) HistoryService {
	return &historyService{
		sessionRepo:     sessionRepo,
		answerRepo:      answerRepo,
		participantRepo: participantRepo,
		questionRepo:    questionRepo,
	}
}

func (s *historyService) ListSessions(ctx context.Context, userID uuid.UUID) ([]SessionSummaryResponse, error) {
	rows, err := s.sessionRepo.ListByUser(ctx, userID)
	if err != nil {
		return nil, err
	}
	result := make([]SessionSummaryResponse, 0, len(rows))
	for _, r := range rows {
		result = append(result, SessionSummaryResponse{
			ID:               r.ID,
			PollID:           r.PollID,
			PollTitle:        r.PollTitle,
			RoomCode:         r.RoomCode,
			Status:           r.Status,
			ParticipantCount: r.ParticipantCount,
			AverageScore:     r.AverageScore,
			StartedAt:        r.StartedAt,
			FinishedAt:       r.FinishedAt,
			CreatedAt:        r.CreatedAt,
		})
	}
	return result, nil
}

func (s *historyService) GetSession(ctx context.Context, userID, sessionID uuid.UUID) (*SessionDetailResponse, error) {
	row, err := s.sessionRepo.GetByIDWithPoll(ctx, sessionID)
	if err != nil {
		return nil, err
	}
	if row.PollUserID != userID {
		return nil, errs.ErrForbidden
	}

	participants, err := s.participantRepo.ListBySession(ctx, sessionID)
	if err != nil {
		return nil, err
	}

	var totalScore int
	for _, p := range participants {
		totalScore += p.TotalScore
	}
	var avgScore float64
	if len(participants) > 0 {
		avgScore = float64(totalScore) / float64(len(participants))
	}

	leaderboard, err := s.answerRepo.GetLeaderboard(ctx, sessionID)
	if err != nil {
		return nil, err
	}

	questions, err := s.questionRepo.ListByPoll(ctx, row.PollID)
	if err != nil {
		return nil, err
	}

	questionStats := make([]QuestionStatResponse, 0, len(questions))
	for _, q := range questions {
		dist, err := s.answerRepo.DistributionByQuestion(ctx, q.ID, sessionID)
		if err != nil {
			return nil, err
		}
		totalAnswers := 0
		for _, cnt := range dist {
			totalAnswers += cnt
		}
		questionStats = append(questionStats, QuestionStatResponse{
			ID:           q.ID,
			Text:         q.Text,
			Type:         q.Type,
			Points:       q.Points,
			TotalAnswers: totalAnswers,
			Distribution: dist,
		})
	}

	return &SessionDetailResponse{
		ID:               row.ID,
		PollID:           row.PollID,
		PollTitle:        row.PollTitle,
		RoomCode:         row.RoomCode,
		Status:           row.Status,
		ParticipantCount: len(participants),
		AverageScore:     avgScore,
		StartedAt:        row.StartedAt,
		FinishedAt:       row.FinishedAt,
		CreatedAt:        row.CreatedAt,
		Leaderboard:      leaderboard,
		Questions:        questionStats,
	}, nil
}

func (s *historyService) ListParticipants(ctx context.Context, userID, sessionID uuid.UUID) ([]ParticipantResultResponse, error) {
	row, err := s.sessionRepo.GetByIDWithPoll(ctx, sessionID)
	if err != nil {
		return nil, err
	}
	if row.PollUserID != userID {
		return nil, errs.ErrForbidden
	}

	participants, err := s.participantRepo.ListBySession(ctx, sessionID)
	if err != nil {
		return nil, err
	}

	result := make([]ParticipantResultResponse, 0, len(participants))
	for _, p := range participants {
		result = append(result, ParticipantResultResponse{
			ID:         p.ID,
			Name:       p.Name,
			TotalScore: p.TotalScore,
			JoinedAt:   p.JoinedAt,
		})
	}
	return result, nil
}

func (s *historyService) ListAnswers(ctx context.Context, userID, sessionID uuid.UUID) ([]repository.AnswerExportRow, error) {
	row, err := s.sessionRepo.GetByIDWithPoll(ctx, sessionID)
	if err != nil {
		return nil, err
	}
	if row.PollUserID != userID {
		return nil, errs.ErrForbidden
	}
	return s.answerRepo.ListExportBySession(ctx, sessionID)
}

func (s *historyService) ExportCSV(ctx context.Context, userID, sessionID uuid.UUID) ([]repository.AnswerExportRow, *time.Time, error) {
	row, err := s.sessionRepo.GetByIDWithPoll(ctx, sessionID)
	if err != nil {
		return nil, nil, err
	}
	if row.PollUserID != userID {
		return nil, nil, errs.ErrForbidden
	}
	answers, err := s.answerRepo.ListExportBySession(ctx, sessionID)
	if err != nil {
		return nil, nil, err
	}
	return answers, row.FinishedAt, nil
}

func (s *historyService) GetParticipantHistory(ctx context.Context, sessionToken uuid.UUID) (*ParticipantHistorySummary, error) {
	row, err := s.sessionRepo.GetByParticipantToken(ctx, sessionToken)
	if err != nil {
		return nil, err
	}
	return &ParticipantHistorySummary{
		SessionID:         row.SessionID.String(),
		PollTitle:         row.PollTitle,
		StartedAt:         row.StartedAt,
		FinishedAt:        row.FinishedAt,
		TotalScore:        row.TotalScore,
		MyRank:            row.MyRank,
		TotalParticipants: row.TotalParticipants,
	}, nil
}
