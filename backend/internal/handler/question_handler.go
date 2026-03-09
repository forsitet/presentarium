package handler

import (
	"encoding/json"
	"errors"
	"net/http"

	"github.com/go-chi/chi/v5"
	"github.com/go-playground/validator/v10"
	"github.com/google/uuid"

	"presentarium/internal/errs"
	appmw "presentarium/internal/middleware"
	"presentarium/internal/service"
)

type questionHandler struct {
	questionSvc service.QuestionService
	validate    *validator.Validate
}

func newQuestionHandler(questionSvc service.QuestionService) *questionHandler {
	return &questionHandler{
		questionSvc: questionSvc,
		validate:    validator.New(),
	}
}

func (h *questionHandler) handleList(w http.ResponseWriter, r *http.Request) {
	userID := r.Context().Value(appmw.UserIDKey).(uuid.UUID)
	pollID, err := uuid.Parse(chi.URLParam(r, "pollId"))
	if err != nil {
		writeError(w, http.StatusBadRequest, "invalid poll id")
		return
	}

	questions, err := h.questionSvc.List(r.Context(), userID, pollID)
	if err != nil {
		if errors.Is(err, errs.ErrNotFound) {
			writeError(w, http.StatusNotFound, "poll not found")
			return
		}
		if errors.Is(err, errs.ErrForbidden) {
			writeError(w, http.StatusForbidden, "forbidden")
			return
		}
		writeError(w, http.StatusInternalServerError, "failed to list questions")
		return
	}
	writeJSON(w, http.StatusOK, questions)
}

func (h *questionHandler) handleCreate(w http.ResponseWriter, r *http.Request) {
	userID := r.Context().Value(appmw.UserIDKey).(uuid.UUID)
	pollID, err := uuid.Parse(chi.URLParam(r, "pollId"))
	if err != nil {
		writeError(w, http.StatusBadRequest, "invalid poll id")
		return
	}

	var req service.CreateQuestionRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		writeError(w, http.StatusBadRequest, "invalid request body")
		return
	}
	if err := h.validate.Struct(req); err != nil {
		writeValidationError(w, err)
		return
	}

	q, err := h.questionSvc.Create(r.Context(), userID, pollID, req)
	if err != nil {
		if errors.Is(err, errs.ErrNotFound) {
			writeError(w, http.StatusNotFound, "poll not found")
			return
		}
		if errors.Is(err, errs.ErrForbidden) {
			writeError(w, http.StatusForbidden, "forbidden")
			return
		}
		if errors.Is(err, errs.ErrValidation) {
			var appErr *errs.AppError
			if errors.As(err, &appErr) {
				writeError(w, http.StatusBadRequest, appErr.Message)
			} else {
				writeError(w, http.StatusBadRequest, "validation error")
			}
			return
		}
		writeError(w, http.StatusInternalServerError, "failed to create question")
		return
	}
	writeJSON(w, http.StatusCreated, q)
}

func (h *questionHandler) handleUpdate(w http.ResponseWriter, r *http.Request) {
	userID := r.Context().Value(appmw.UserIDKey).(uuid.UUID)
	pollID, err := uuid.Parse(chi.URLParam(r, "pollId"))
	if err != nil {
		writeError(w, http.StatusBadRequest, "invalid poll id")
		return
	}
	questionID, err := uuid.Parse(chi.URLParam(r, "id"))
	if err != nil {
		writeError(w, http.StatusBadRequest, "invalid question id")
		return
	}

	var req service.UpdateQuestionRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		writeError(w, http.StatusBadRequest, "invalid request body")
		return
	}
	if err := h.validate.Struct(req); err != nil {
		writeValidationError(w, err)
		return
	}

	q, err := h.questionSvc.Update(r.Context(), userID, pollID, questionID, req)
	if err != nil {
		if errors.Is(err, errs.ErrNotFound) {
			writeError(w, http.StatusNotFound, "question not found")
			return
		}
		if errors.Is(err, errs.ErrForbidden) {
			writeError(w, http.StatusForbidden, "forbidden")
			return
		}
		if errors.Is(err, errs.ErrValidation) {
			var appErr *errs.AppError
			if errors.As(err, &appErr) {
				writeError(w, http.StatusBadRequest, appErr.Message)
			} else {
				writeError(w, http.StatusBadRequest, "validation error")
			}
			return
		}
		writeError(w, http.StatusInternalServerError, "failed to update question")
		return
	}
	writeJSON(w, http.StatusOK, q)
}

func (h *questionHandler) handleDelete(w http.ResponseWriter, r *http.Request) {
	userID := r.Context().Value(appmw.UserIDKey).(uuid.UUID)
	pollID, err := uuid.Parse(chi.URLParam(r, "pollId"))
	if err != nil {
		writeError(w, http.StatusBadRequest, "invalid poll id")
		return
	}
	questionID, err := uuid.Parse(chi.URLParam(r, "id"))
	if err != nil {
		writeError(w, http.StatusBadRequest, "invalid question id")
		return
	}

	if err := h.questionSvc.Delete(r.Context(), userID, pollID, questionID); err != nil {
		if errors.Is(err, errs.ErrNotFound) {
			writeError(w, http.StatusNotFound, "question not found")
			return
		}
		if errors.Is(err, errs.ErrForbidden) {
			writeError(w, http.StatusForbidden, "forbidden")
			return
		}
		writeError(w, http.StatusInternalServerError, "failed to delete question")
		return
	}
	w.WriteHeader(http.StatusNoContent)
}

// reorderRequest wraps the array of reorder items.
type reorderRequest struct {
	Items []service.ReorderRequest `json:"items" validate:"required,min=1,dive"`
}

func (h *questionHandler) handleReorder(w http.ResponseWriter, r *http.Request) {
	userID := r.Context().Value(appmw.UserIDKey).(uuid.UUID)
	pollID, err := uuid.Parse(chi.URLParam(r, "pollId"))
	if err != nil {
		writeError(w, http.StatusBadRequest, "invalid poll id")
		return
	}

	var req reorderRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		writeError(w, http.StatusBadRequest, "invalid request body")
		return
	}
	if err := h.validate.Struct(req); err != nil {
		writeValidationError(w, err)
		return
	}

	if err := h.questionSvc.Reorder(r.Context(), userID, pollID, req.Items); err != nil {
		if errors.Is(err, errs.ErrNotFound) {
			writeError(w, http.StatusNotFound, "question not found")
			return
		}
		if errors.Is(err, errs.ErrForbidden) {
			writeError(w, http.StatusForbidden, "forbidden")
			return
		}
		writeError(w, http.StatusInternalServerError, "failed to reorder questions")
		return
	}
	w.WriteHeader(http.StatusOK)
}
