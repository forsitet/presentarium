package handler

import (
	"encoding/json"
	"errors"
	"net/http"

	"github.com/go-chi/chi/v5"
	"github.com/google/uuid"

	"presentarium/internal/errs"
	appmw "presentarium/internal/middleware"
	"presentarium/internal/service"
)

type moderationHandler struct {
	moderationSvc service.ModerationService
}

func newModerationHandler(svc service.ModerationService) *moderationHandler {
	return &moderationHandler{moderationSvc: svc}
}

type moderationRequest struct {
	Action string `json:"action"` // "hide" or "show"
}

// handleHideAnswer processes PATCH /api/sessions/:sessionId/answers/:answerId
func (h *moderationHandler) handleHideAnswer(w http.ResponseWriter, r *http.Request) {
	userID := r.Context().Value(appmw.UserIDKey).(uuid.UUID)

	sessionID, err := uuid.Parse(chi.URLParam(r, "sessionId"))
	if err != nil {
		writeError(w, http.StatusBadRequest, "invalid session id")
		return
	}
	answerID, err := uuid.Parse(chi.URLParam(r, "answerId"))
	if err != nil {
		writeError(w, http.StatusBadRequest, "invalid answer id")
		return
	}

	var req moderationRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		writeError(w, http.StatusBadRequest, "invalid request body")
		return
	}

	var isHidden bool
	switch req.Action {
	case "hide":
		isHidden = true
	case "show":
		isHidden = false
	default:
		writeError(w, http.StatusBadRequest, "action must be 'hide' or 'show'")
		return
	}

	if err := h.moderationSvc.HideAnswer(r.Context(), userID, sessionID, answerID, isHidden); err != nil {
		h.writeModErr(w, err)
		return
	}

	w.WriteHeader(http.StatusNoContent)
}

// handleHideIdea processes PATCH /api/sessions/:sessionId/ideas/:ideaId
func (h *moderationHandler) handleHideIdea(w http.ResponseWriter, r *http.Request) {
	userID := r.Context().Value(appmw.UserIDKey).(uuid.UUID)

	sessionID, err := uuid.Parse(chi.URLParam(r, "sessionId"))
	if err != nil {
		writeError(w, http.StatusBadRequest, "invalid session id")
		return
	}
	ideaID, err := uuid.Parse(chi.URLParam(r, "ideaId"))
	if err != nil {
		writeError(w, http.StatusBadRequest, "invalid idea id")
		return
	}

	var req moderationRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		writeError(w, http.StatusBadRequest, "invalid request body")
		return
	}

	var isHidden bool
	switch req.Action {
	case "hide":
		isHidden = true
	case "show":
		isHidden = false
	default:
		writeError(w, http.StatusBadRequest, "action must be 'hide' or 'show'")
		return
	}

	if err := h.moderationSvc.HideIdea(r.Context(), userID, sessionID, ideaID, isHidden); err != nil {
		h.writeModErr(w, err)
		return
	}

	w.WriteHeader(http.StatusNoContent)
}

func (h *moderationHandler) writeModErr(w http.ResponseWriter, err error) {
	switch {
	case errors.Is(err, errs.ErrNotFound):
		writeError(w, http.StatusNotFound, "not found")
	case errors.Is(err, errs.ErrForbidden):
		writeError(w, http.StatusForbidden, "forbidden")
	default:
		writeError(w, http.StatusInternalServerError, "internal error")
	}
}
