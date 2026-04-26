package service

import (
	"context"
	"fmt"
	"time"

	"github.com/google/uuid"

	"presentarium/internal/errs"
	"presentarium/internal/model"
	"presentarium/internal/repository"
)

// PollService defines business logic for polls.
type PollService interface {
	Create(ctx context.Context, userID uuid.UUID, req CreatePollRequest) (*model.Poll, error)
	Get(ctx context.Context, userID, pollID uuid.UUID) (*model.Poll, error)
	List(ctx context.Context, userID uuid.UUID) ([]*model.Poll, error)
	Update(ctx context.Context, userID, pollID uuid.UUID, req UpdatePollRequest) (*model.Poll, error)
	Delete(ctx context.Context, userID, pollID uuid.UUID) error
	Copy(ctx context.Context, userID, pollID uuid.UUID) (*model.Poll, error)
}

// CreatePollRequest holds fields for creating a poll.
type CreatePollRequest struct {
	Title                  string `json:"title"                    validate:"required,max=200"`
	Description            string `json:"description"              validate:"max=1000"`
	ScoringRule            string `json:"scoring_rule"             validate:"required,oneof=none correct_answer speed_bonus"`
	QuestionOrder          string `json:"question_order"           validate:"required,oneof=sequential random"`
	ShowAnswerDistribution bool   `json:"show_answer_distribution"`
}

// UpdatePollRequest holds fields for updating a poll.
type UpdatePollRequest struct {
	Title                  string `json:"title"                    validate:"required,max=200"`
	Description            string `json:"description"              validate:"max=1000"`
	ScoringRule            string `json:"scoring_rule"             validate:"required,oneof=none correct_answer speed_bonus"`
	QuestionOrder          string `json:"question_order"           validate:"required,oneof=sequential random"`
	ShowAnswerDistribution bool   `json:"show_answer_distribution"`
}

type pollService struct {
	pollRepo repository.PollRepository
}

// NewPollService creates a new PollService.
func NewPollService(pollRepo repository.PollRepository) PollService {
	return &pollService{pollRepo: pollRepo}
}

func (s *pollService) Create(ctx context.Context, userID uuid.UUID, req CreatePollRequest) (*model.Poll, error) {
	now := time.Now().UTC()
	poll := &model.Poll{
		ID:                     uuid.New(),
		UserID:                 userID,
		Title:                  req.Title,
		Description:            req.Description,
		ScoringRule:            req.ScoringRule,
		QuestionOrder:          req.QuestionOrder,
		ShowAnswerDistribution: req.ShowAnswerDistribution,
		CreatedAt:              now,
		UpdatedAt:              now,
	}
	if err := s.pollRepo.Create(ctx, poll); err != nil {
		return nil, err
	}
	return poll, nil
}

func (s *pollService) Get(ctx context.Context, userID, pollID uuid.UUID) (*model.Poll, error) {
	poll, err := s.pollRepo.GetByID(ctx, pollID)
	if err != nil {
		return nil, err
	}
	if poll.UserID != userID {
		return nil, errs.ErrForbidden
	}
	return poll, nil
}

func (s *pollService) List(ctx context.Context, userID uuid.UUID) ([]*model.Poll, error) {
	return s.pollRepo.ListByUser(ctx, userID)
}

func (s *pollService) Update(ctx context.Context, userID, pollID uuid.UUID, req UpdatePollRequest) (*model.Poll, error) {
	poll, err := s.pollRepo.GetByID(ctx, pollID)
	if err != nil {
		return nil, err
	}
	if poll.UserID != userID {
		return nil, errs.ErrForbidden
	}

	poll.Title = req.Title
	poll.Description = req.Description
	poll.ScoringRule = req.ScoringRule
	poll.QuestionOrder = req.QuestionOrder
	poll.ShowAnswerDistribution = req.ShowAnswerDistribution
	poll.UpdatedAt = time.Now().UTC()

	if err := s.pollRepo.Update(ctx, poll); err != nil {
		return nil, err
	}
	return poll, nil
}

func (s *pollService) Delete(ctx context.Context, userID, pollID uuid.UUID) error {
	poll, err := s.pollRepo.GetByID(ctx, pollID)
	if err != nil {
		return err
	}
	if poll.UserID != userID {
		return errs.ErrForbidden
	}
	return s.pollRepo.Delete(ctx, pollID)
}

func (s *pollService) Copy(ctx context.Context, userID, pollID uuid.UUID) (*model.Poll, error) {
	original, err := s.pollRepo.GetByID(ctx, pollID)
	if err != nil {
		return nil, err
	}
	if original.UserID != userID {
		return nil, errs.ErrForbidden
	}

	now := time.Now().UTC()
	copied := &model.Poll{
		ID:                     uuid.New(),
		UserID:                 userID,
		Title:                  fmt.Sprintf("%s (Копия)", original.Title),
		Description:            original.Description,
		ScoringRule:            original.ScoringRule,
		QuestionOrder:          original.QuestionOrder,
		ShowAnswerDistribution: original.ShowAnswerDistribution,
		CreatedAt:              now,
		UpdatedAt:              now,
	}
	if err := s.pollRepo.Create(ctx, copied); err != nil {
		return nil, err
	}
	return copied, nil
}
