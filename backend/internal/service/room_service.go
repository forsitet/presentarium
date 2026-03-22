package service

import (
	"context"
	crand "crypto/rand"
	"fmt"
	"log/slog"
	"math/big"
	mrand "math/rand/v2"
	"strings"
	"time"

	"github.com/google/uuid"

	"presentarium/internal/errs"
	"presentarium/internal/model"
	"presentarium/internal/repository"
	"presentarium/internal/ws"
)

// RoomService defines business logic for session (room) management.
type RoomService interface {
	CreateRoom(ctx context.Context, userID, pollID uuid.UUID) (*CreateRoomResponse, error)
	GetRoom(ctx context.Context, code string) (*RoomInfoResponse, error)
	ChangeState(ctx context.Context, userID uuid.UUID, code, action string) error
}

// CreateRoomResponse is returned on successful room creation.
type CreateRoomResponse struct {
	RoomCode string `json:"room_code"`
	JoinURL  string `json:"join_url"`
	SessionID uuid.UUID `json:"session_id"`
}

// RoomInfoResponse is returned when querying room info.
type RoomInfoResponse struct {
	RoomCode     string    `json:"room_code"`
	PollID       uuid.UUID `json:"poll_id"`
	SessionID    uuid.UUID `json:"session_id"`
	Status       string    `json:"status"`
	Participants int       `json:"participants"`
}

type roomService struct {
	sessionRepo  repository.SessionRepository
	pollRepo     repository.PollRepository
	questionRepo repository.QuestionRepository
	hub          *ws.Hub
}

// NewRoomService creates a new RoomService.
func NewRoomService(
	sessionRepo repository.SessionRepository,
	pollRepo repository.PollRepository,
	questionRepo repository.QuestionRepository,
	hub *ws.Hub,
) RoomService {
	return &roomService{
		sessionRepo:  sessionRepo,
		pollRepo:     pollRepo,
		questionRepo: questionRepo,
		hub:          hub,
	}
}

func (s *roomService) CreateRoom(ctx context.Context, userID, pollID uuid.UUID) (*CreateRoomResponse, error) {
	// Verify poll ownership
	poll, err := s.pollRepo.GetByID(ctx, pollID)
	if err != nil {
		return nil, err
	}
	if poll.UserID != userID {
		return nil, errs.ErrForbidden
	}

	// Check for existing active room for this poll
	_, err = s.sessionRepo.GetActiveByPoll(ctx, pollID)
	if err == nil {
		// Active room exists
		return nil, errs.ErrConflict
	}
	if err != errs.ErrNotFound {
		return nil, err
	}

	// Generate unique 6-digit code with retry on collision
	var session *model.Session
	for attempt := 0; attempt < 10; attempt++ {
		code, genErr := generateRoomCode()
		if genErr != nil {
			return nil, genErr
		}

		candidate := &model.Session{
			ID:        uuid.New(),
			PollID:    pollID,
			RoomCode:  code,
			Status:    "waiting",
			CreatedAt: time.Now().UTC(),
		}

		if createErr := s.sessionRepo.Create(ctx, candidate); createErr != nil {
			if isUniqueViolation(createErr) {
				continue
			}
			return nil, createErr
		}
		session = candidate
		break
	}
	if session == nil {
		return nil, fmt.Errorf("failed to generate unique room code after retries")
	}

	// Initialize room in Hub
	s.hub.CreateRoom(session.RoomCode, session.ID)

	return &CreateRoomResponse{
		RoomCode:  session.RoomCode,
		JoinURL:   "/join/" + session.RoomCode,
		SessionID: session.ID,
	}, nil
}

func (s *roomService) GetRoom(ctx context.Context, code string) (*RoomInfoResponse, error) {
	session, err := s.sessionRepo.GetByCode(ctx, code)
	if err != nil {
		return nil, err
	}

	participants := 0
	if room := s.hub.GetRoom(code); room != nil {
		participants = room.ParticipantCount()
	}

	return &RoomInfoResponse{
		RoomCode:     session.RoomCode,
		PollID:       session.PollID,
		SessionID:    session.ID,
		Status:       session.Status,
		Participants: participants,
	}, nil
}

func (s *roomService) ChangeState(ctx context.Context, userID uuid.UUID, code, action string) error {
	session, err := s.sessionRepo.GetByCode(ctx, code)
	if err != nil {
		return err
	}

	if session.Status == "finished" {
		return errs.ErrConflict
	}

	// Verify poll ownership — only the organizer can change state
	poll, err := s.pollRepo.GetByID(ctx, session.PollID)
	if err != nil {
		return err
	}
	if poll.UserID != userID {
		return errs.ErrForbidden
	}

	// Determine new status and timestamps
	var newStatus string
	var startedAt, finishedAt *string

	now := time.Now().UTC().Format(time.RFC3339Nano)

	switch action {
	case "start":
		if session.Status != "waiting" {
			return errs.ErrValidation
		}
		newStatus = "active"
		startedAt = &now
	case "end":
		newStatus = "finished"
		finishedAt = &now
	default:
		return errs.ErrValidation
	}

	if err := s.sessionRepo.UpdateStatus(ctx, session.ID, newStatus, startedAt, finishedAt); err != nil {
		return err
	}

	// Update room state in Hub
	if room := s.hub.GetRoom(code); room != nil {
		switch newStatus {
		case "active":
			room.SetState(ws.StateActive)

			// Determine session question order (sequential or random) and persist it.
			s.applyQuestionOrder(ctx, session.ID, session.PollID, poll.QuestionOrder, room)

		case "finished":
			room.SetState(ws.StateFinished)
		}
	}

	return nil
}

// applyQuestionOrder fetches questions, optionally shuffles them, saves the order to
// the sessions table, stores it in the Room, and broadcasts room_started to the organizer.
func (s *roomService) applyQuestionOrder(
	ctx context.Context,
	sessionID, pollID uuid.UUID,
	pollQuestionOrder string,
	room *ws.Room,
) {
	questions, err := s.questionRepo.ListByPoll(ctx, pollID)
	if err != nil {
		slog.Warn("applyQuestionOrder: failed to list questions", "poll_id", pollID, "err", err)
		return
	}
	if len(questions) == 0 {
		return
	}

	ids := make([]uuid.UUID, len(questions))
	for i, q := range questions {
		ids[i] = q.ID
	}

	if pollQuestionOrder == "random" {
		shuffleUUIDs(ids)
	}

	// Persist order to DB.
	if err := s.sessionRepo.SaveQuestionOrder(ctx, sessionID, ids); err != nil {
		slog.Warn("applyQuestionOrder: failed to save question order", "session_id", sessionID, "err", err)
	}

	// Store in Room for in-process access.
	room.SetQuestionOrder(ids)

	// Notify the organizer so the frontend can step through questions in order.
	if msg, err := ws.NewEnvelope(ws.MsgTypeRoomStarted, ws.RoomStartedData{QuestionOrder: ids}); err == nil {
		room.SendToOrganizer(msg)
	}
}

// shuffleUUIDs performs a Fisher-Yates in-place shuffle.
func shuffleUUIDs(ids []uuid.UUID) {
	for i := len(ids) - 1; i > 0; i-- {
		j := mrand.IntN(i + 1)
		ids[i], ids[j] = ids[j], ids[i]
	}
}

// generateRoomCode returns a random 6-digit numeric string (100000–999999).
func generateRoomCode() (string, error) {
	n, err := crand.Int(crand.Reader, big.NewInt(900000))
	if err != nil {
		return "", err
	}
	return fmt.Sprintf("%06d", n.Int64()+100000), nil
}

// isUniqueViolation returns true if the error indicates a PostgreSQL unique constraint violation.
func isUniqueViolation(err error) bool {
	if err == nil {
		return false
	}
	msg := err.Error()
	return strings.Contains(msg, "23505") ||
		strings.Contains(msg, "unique constraint") ||
		strings.Contains(msg, "unique_violation")
}
