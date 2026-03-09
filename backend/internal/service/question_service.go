package service

import (
	"context"
	"net/http"
	"time"

	"github.com/google/uuid"

	"presentarium/internal/errs"
	"presentarium/internal/model"
	"presentarium/internal/repository"
)

// choiceTypes are question types that require answer options.
var choiceTypes = map[string]bool{
	"single_choice":   true,
	"multiple_choice": true,
	"image_choice":    true,
}

// QuestionService defines business logic for questions.
type QuestionService interface {
	Create(ctx context.Context, userID, pollID uuid.UUID, req CreateQuestionRequest) (*model.Question, error)
	List(ctx context.Context, userID, pollID uuid.UUID) ([]*model.Question, error)
	Update(ctx context.Context, userID, pollID, questionID uuid.UUID, req UpdateQuestionRequest) (*model.Question, error)
	Delete(ctx context.Context, userID, pollID, questionID uuid.UUID) error
	Reorder(ctx context.Context, userID, pollID uuid.UUID, items []ReorderRequest) error
}

// CreateQuestionRequest holds fields for creating a question.
type CreateQuestionRequest struct {
	Type             string                 `json:"type"               validate:"required,oneof=single_choice multiple_choice open_text image_choice word_cloud brainstorm"`
	Text             string                 `json:"text"               validate:"required,max=500"`
	Options          []model.QuestionOption `json:"options"`
	// TimeLimitSeconds=0 means "use default (30)"; non-zero values must be 5-300.
	TimeLimitSeconds int `json:"time_limit_seconds" validate:"omitempty,min=5,max=300"`
	Points           int `json:"points"             validate:"min=0"`
}

// UpdateQuestionRequest holds fields for updating a question.
type UpdateQuestionRequest struct {
	Type             string                 `json:"type"               validate:"required,oneof=single_choice multiple_choice open_text image_choice word_cloud brainstorm"`
	Text             string                 `json:"text"               validate:"required,max=500"`
	Options          []model.QuestionOption `json:"options"`
	TimeLimitSeconds int                    `json:"time_limit_seconds" validate:"omitempty,min=5,max=300"`
	Points           int                    `json:"points"             validate:"min=0"`
	Position         int                    `json:"position"           validate:"min=0"`
}

// ReorderRequest holds a question ID and its desired position.
type ReorderRequest struct {
	ID       uuid.UUID `json:"id"       validate:"required"`
	Position int       `json:"position" validate:"min=0"`
}

type questionService struct {
	questionRepo repository.QuestionRepository
	pollRepo     repository.PollRepository
}

// NewQuestionService creates a new QuestionService.
func NewQuestionService(questionRepo repository.QuestionRepository, pollRepo repository.PollRepository) QuestionService {
	return &questionService{
		questionRepo: questionRepo,
		pollRepo:     pollRepo,
	}
}

// verifyPollOwner checks that the poll exists and belongs to userID.
func (s *questionService) verifyPollOwner(ctx context.Context, userID, pollID uuid.UUID) error {
	poll, err := s.pollRepo.GetByID(ctx, pollID)
	if err != nil {
		return err
	}
	if poll.UserID != userID {
		return errs.ErrForbidden
	}
	return nil
}

// validateOptions checks choice-type questions have ≥2 options and ≥1 correct.
func validateOptions(qType string, options []model.QuestionOption) error {
	if !choiceTypes[qType] {
		return nil
	}
	if len(options) < 2 {
		return &errs.AppError{
			Code:    http.StatusBadRequest,
			Message: "choice questions require at least 2 options",
			Err:     errs.ErrValidation,
		}
	}
	hasCorrect := false
	for _, o := range options {
		if o.IsCorrect {
			hasCorrect = true
			break
		}
	}
	if !hasCorrect {
		return &errs.AppError{
			Code:    http.StatusBadRequest,
			Message: "choice questions require at least 1 correct option",
			Err:     errs.ErrValidation,
		}
	}
	return nil
}

func (s *questionService) Create(ctx context.Context, userID, pollID uuid.UUID, req CreateQuestionRequest) (*model.Question, error) {
	if err := s.verifyPollOwner(ctx, userID, pollID); err != nil {
		return nil, err
	}
	if err := validateOptions(req.Type, req.Options); err != nil {
		return nil, err
	}

	// Default time limit if not provided.
	if req.TimeLimitSeconds == 0 {
		req.TimeLimitSeconds = 30
	}
	// Default points.
	if req.Points == 0 {
		req.Points = 100
	}

	// Auto-assign next position.
	maxPos, err := s.questionRepo.MaxPosition(ctx, pollID)
	if err != nil {
		return nil, err
	}

	now := time.Now().UTC()
	q := &model.Question{
		ID:               uuid.New(),
		PollID:           pollID,
		Type:             req.Type,
		Text:             req.Text,
		Options:          model.OptionList(req.Options),
		TimeLimitSeconds: req.TimeLimitSeconds,
		Points:           req.Points,
		Position:         maxPos + 1,
		CreatedAt:        now,
		UpdatedAt:        now,
	}
	if err := s.questionRepo.Create(ctx, q); err != nil {
		return nil, err
	}
	return q, nil
}

func (s *questionService) List(ctx context.Context, userID, pollID uuid.UUID) ([]*model.Question, error) {
	if err := s.verifyPollOwner(ctx, userID, pollID); err != nil {
		return nil, err
	}
	return s.questionRepo.ListByPoll(ctx, pollID)
}

func (s *questionService) Update(ctx context.Context, userID, pollID, questionID uuid.UUID, req UpdateQuestionRequest) (*model.Question, error) {
	if err := s.verifyPollOwner(ctx, userID, pollID); err != nil {
		return nil, err
	}
	if err := validateOptions(req.Type, req.Options); err != nil {
		return nil, err
	}

	q, err := s.questionRepo.GetByID(ctx, questionID)
	if err != nil {
		return nil, err
	}
	if q.PollID != pollID {
		return nil, errs.ErrNotFound
	}

	q.Type = req.Type
	q.Text = req.Text
	q.Options = model.OptionList(req.Options)
	q.TimeLimitSeconds = req.TimeLimitSeconds
	q.Points = req.Points
	q.Position = req.Position
	q.UpdatedAt = time.Now().UTC()

	if err := s.questionRepo.Update(ctx, q); err != nil {
		return nil, err
	}
	return q, nil
}

func (s *questionService) Delete(ctx context.Context, userID, pollID, questionID uuid.UUID) error {
	if err := s.verifyPollOwner(ctx, userID, pollID); err != nil {
		return err
	}

	q, err := s.questionRepo.GetByID(ctx, questionID)
	if err != nil {
		return err
	}
	if q.PollID != pollID {
		return errs.ErrNotFound
	}

	return s.questionRepo.Delete(ctx, questionID)
}

func (s *questionService) Reorder(ctx context.Context, userID, pollID uuid.UUID, items []ReorderRequest) error {
	if err := s.verifyPollOwner(ctx, userID, pollID); err != nil {
		return err
	}

	repoItems := make([]repository.ReorderItem, len(items))
	for i, item := range items {
		repoItems[i] = repository.ReorderItem{
			ID:       item.ID,
			Position: item.Position,
		}
	}
	return s.questionRepo.Reorder(ctx, pollID, repoItems)
}
