package service

import (
	"context"

	"github.com/google/uuid"

	"presentarium/internal/errs"
	"presentarium/internal/repository"
	"presentarium/internal/ws"
)

// ModerationService allows the organizer to hide/show answers and brainstorm ideas.
type ModerationService interface {
	// HideAnswer toggles is_hidden on an answer and broadcasts answer_hidden to the room.
	// Returns ErrForbidden if the caller doesn't own the session.
	HideAnswer(ctx context.Context, userID, sessionID, answerID uuid.UUID, isHidden bool) error
	// HideIdea toggles is_hidden on a brainstorm idea and broadcasts answer_hidden to the room.
	// Returns ErrForbidden if the caller doesn't own the session.
	HideIdea(ctx context.Context, userID, sessionID, ideaID uuid.UUID, isHidden bool) error
}

type moderationService struct {
	sessionRepo    repository.SessionRepository
	answerRepo     repository.AnswerRepository
	brainstormRepo repository.BrainstormRepository
	hub            *ws.Hub
}

// NewModerationService creates a new ModerationService.
func NewModerationService(
	sessionRepo repository.SessionRepository,
	answerRepo repository.AnswerRepository,
	brainstormRepo repository.BrainstormRepository,
	hub *ws.Hub,
) ModerationService {
	return &moderationService{
		sessionRepo:    sessionRepo,
		answerRepo:     answerRepo,
		brainstormRepo: brainstormRepo,
		hub:            hub,
	}
}

func (s *moderationService) HideAnswer(ctx context.Context, userID, sessionID, answerID uuid.UUID, isHidden bool) error {
	row, err := s.sessionRepo.GetByIDWithPoll(ctx, sessionID)
	if err != nil {
		return err
	}
	if row.PollUserID != userID {
		return errs.ErrForbidden
	}

	// Verify the answer belongs to this session.
	answer, err := s.answerRepo.GetByID(ctx, answerID)
	if err != nil {
		return err
	}
	if answer.SessionID != sessionID {
		return errs.ErrForbidden
	}

	if err := s.answerRepo.SetHidden(ctx, answerID, isHidden); err != nil {
		return err
	}

	// Broadcast to the live room (if still active) so participants see the change immediately.
	room := s.hub.GetRoom(row.RoomCode)
	if room != nil {
		if msg, err := ws.NewEnvelope(ws.MsgTypeAnswerHidden, ws.HideAnswerData{
			AnswerID: answerID,
			IsHidden: isHidden,
		}); err == nil {
			room.Broadcast(msg)
		}
	}

	return nil
}

func (s *moderationService) HideIdea(ctx context.Context, userID, sessionID, ideaID uuid.UUID, isHidden bool) error {
	row, err := s.sessionRepo.GetByIDWithPoll(ctx, sessionID)
	if err != nil {
		return err
	}
	if row.PollUserID != userID {
		return errs.ErrForbidden
	}

	// Verify the idea belongs to this session.
	idea, err := s.brainstormRepo.GetIdea(ctx, ideaID)
	if err != nil {
		return err
	}
	if idea.SessionID != sessionID {
		return errs.ErrForbidden
	}

	if err := s.brainstormRepo.SetIdeaHidden(ctx, ideaID, isHidden); err != nil {
		return err
	}

	// Broadcast to the live room so participants see the change immediately.
	room := s.hub.GetRoom(row.RoomCode)
	if room != nil {
		if msg, err := ws.NewEnvelope(ws.MsgTypeAnswerHidden, ws.BrainstormHideIdeaData{
			IdeaID:   ideaID,
			IsHidden: isHidden,
		}); err == nil {
			room.Broadcast(msg)
		}
	}

	return nil
}
