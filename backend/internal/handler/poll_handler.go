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
	"presentarium/internal/model"
	"presentarium/internal/service"
)

type pollHandler struct {
	pollSvc  service.PollService
	validate *validator.Validate
}

func newPollHandler(pollSvc service.PollService) *pollHandler {
	return &pollHandler{
		pollSvc:  pollSvc,
		validate: validator.New(),
	}
}

func (h *pollHandler) handleList(w http.ResponseWriter, r *http.Request) {
	userID := r.Context().Value(appmw.UserIDKey).(uuid.UUID)
	polls, err := h.pollSvc.List(r.Context(), userID)
	if err != nil {
		writeError(w, http.StatusInternalServerError, "failed to list polls")
		return
	}
	if polls == nil {
		polls = []*model.Poll{}
	}
	writeJSON(w, http.StatusOK, polls)
}

func (h *pollHandler) handleCreate(w http.ResponseWriter, r *http.Request) {
	userID := r.Context().Value(appmw.UserIDKey).(uuid.UUID)

	var req service.CreatePollRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		writeError(w, http.StatusBadRequest, "invalid request body")
		return
	}
	if err := h.validate.Struct(req); err != nil {
		writeValidationError(w, err)
		return
	}

	poll, err := h.pollSvc.Create(r.Context(), userID, req)
	if err != nil {
		writeError(w, http.StatusInternalServerError, "failed to create poll")
		return
	}
	writeJSON(w, http.StatusCreated, poll)
}

func (h *pollHandler) handleGet(w http.ResponseWriter, r *http.Request) {
	userID := r.Context().Value(appmw.UserIDKey).(uuid.UUID)
	pollID, err := uuid.Parse(chi.URLParam(r, "id"))
	if err != nil {
		writeError(w, http.StatusBadRequest, "invalid poll id")
		return
	}

	poll, err := h.pollSvc.Get(r.Context(), userID, pollID)
	if err != nil {
		if errors.Is(err, errs.ErrNotFound) {
			writeError(w, http.StatusNotFound, "poll not found")
			return
		}
		if errors.Is(err, errs.ErrForbidden) {
			writeError(w, http.StatusForbidden, "forbidden")
			return
		}
		writeError(w, http.StatusInternalServerError, "failed to get poll")
		return
	}
	writeJSON(w, http.StatusOK, poll)
}

func (h *pollHandler) handleUpdate(w http.ResponseWriter, r *http.Request) {
	userID := r.Context().Value(appmw.UserIDKey).(uuid.UUID)
	pollID, err := uuid.Parse(chi.URLParam(r, "id"))
	if err != nil {
		writeError(w, http.StatusBadRequest, "invalid poll id")
		return
	}

	var req service.UpdatePollRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		writeError(w, http.StatusBadRequest, "invalid request body")
		return
	}
	if err := h.validate.Struct(req); err != nil {
		writeValidationError(w, err)
		return
	}

	poll, err := h.pollSvc.Update(r.Context(), userID, pollID, req)
	if err != nil {
		if errors.Is(err, errs.ErrNotFound) {
			writeError(w, http.StatusNotFound, "poll not found")
			return
		}
		if errors.Is(err, errs.ErrForbidden) {
			writeError(w, http.StatusForbidden, "forbidden")
			return
		}
		writeError(w, http.StatusInternalServerError, "failed to update poll")
		return
	}
	writeJSON(w, http.StatusOK, poll)
}

func (h *pollHandler) handleDelete(w http.ResponseWriter, r *http.Request) {
	userID := r.Context().Value(appmw.UserIDKey).(uuid.UUID)
	pollID, err := uuid.Parse(chi.URLParam(r, "id"))
	if err != nil {
		writeError(w, http.StatusBadRequest, "invalid poll id")
		return
	}

	if err := h.pollSvc.Delete(r.Context(), userID, pollID); err != nil {
		if errors.Is(err, errs.ErrNotFound) {
			writeError(w, http.StatusNotFound, "poll not found")
			return
		}
		if errors.Is(err, errs.ErrForbidden) {
			writeError(w, http.StatusForbidden, "forbidden")
			return
		}
		writeError(w, http.StatusInternalServerError, "failed to delete poll")
		return
	}
	w.WriteHeader(http.StatusNoContent)
}

func (h *pollHandler) handleCopy(w http.ResponseWriter, r *http.Request) {
	userID := r.Context().Value(appmw.UserIDKey).(uuid.UUID)
	pollID, err := uuid.Parse(chi.URLParam(r, "id"))
	if err != nil {
		writeError(w, http.StatusBadRequest, "invalid poll id")
		return
	}

	poll, err := h.pollSvc.Copy(r.Context(), userID, pollID)
	if err != nil {
		if errors.Is(err, errs.ErrNotFound) {
			writeError(w, http.StatusNotFound, "poll not found")
			return
		}
		if errors.Is(err, errs.ErrForbidden) {
			writeError(w, http.StatusForbidden, "forbidden")
			return
		}
		writeError(w, http.StatusInternalServerError, "failed to copy poll")
		return
	}
	writeJSON(w, http.StatusCreated, poll)
}
