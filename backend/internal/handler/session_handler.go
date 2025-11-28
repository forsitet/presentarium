package handler

import (
	"errors"
	"net/http"

	"github.com/go-chi/chi/v5"
	"github.com/google/uuid"

	"presentarium/internal/errs"
	appmw "presentarium/internal/middleware"
	"presentarium/internal/service"
)

type sessionHandler struct {
	historySvc service.HistoryService
}

func newSessionHandler(historySvc service.HistoryService) *sessionHandler {
	return &sessionHandler{historySvc: historySvc}
}

// handleList returns the list of sessions for the authenticated organizer.
func (h *sessionHandler) handleList(w http.ResponseWriter, r *http.Request) {
	userID := r.Context().Value(appmw.UserIDKey).(uuid.UUID)
	sessions, err := h.historySvc.ListSessions(r.Context(), userID)
	if err != nil {
		writeError(w, http.StatusInternalServerError, "failed to list sessions")
		return
	}
	writeJSON(w, http.StatusOK, sessions)
}

// handleGet returns detailed info for a single finished session.
func (h *sessionHandler) handleGet(w http.ResponseWriter, r *http.Request) {
	userID := r.Context().Value(appmw.UserIDKey).(uuid.UUID)
	sessionID, err := uuid.Parse(chi.URLParam(r, "id"))
	if err != nil {
		writeError(w, http.StatusBadRequest, "invalid session id")
		return
	}

	detail, err := h.historySvc.GetSession(r.Context(), userID, sessionID)
	if err != nil {
		if errors.Is(err, errs.ErrNotFound) {
			writeError(w, http.StatusNotFound, "session not found")
			return
		}
		if errors.Is(err, errs.ErrForbidden) {
			writeError(w, http.StatusForbidden, "forbidden")
			return
		}
		writeError(w, http.StatusInternalServerError, "failed to get session")
		return
	}
	writeJSON(w, http.StatusOK, detail)
}

// handleListParticipants returns participants with their final scores.
func (h *sessionHandler) handleListParticipants(w http.ResponseWriter, r *http.Request) {
	userID := r.Context().Value(appmw.UserIDKey).(uuid.UUID)
	sessionID, err := uuid.Parse(chi.URLParam(r, "id"))
	if err != nil {
		writeError(w, http.StatusBadRequest, "invalid session id")
		return
	}

	participants, err := h.historySvc.ListParticipants(r.Context(), userID, sessionID)
	if err != nil {
		if errors.Is(err, errs.ErrNotFound) {
			writeError(w, http.StatusNotFound, "session not found")
			return
		}
		if errors.Is(err, errs.ErrForbidden) {
			writeError(w, http.StatusForbidden, "forbidden")
			return
		}
		writeError(w, http.StatusInternalServerError, "failed to list participants")
		return
	}
	writeJSON(w, http.StatusOK, participants)
}

// handleListAnswers returns all answers in a session (for export / analysis).
func (h *sessionHandler) handleListAnswers(w http.ResponseWriter, r *http.Request) {
	userID := r.Context().Value(appmw.UserIDKey).(uuid.UUID)
	sessionID, err := uuid.Parse(chi.URLParam(r, "id"))
	if err != nil {
		writeError(w, http.StatusBadRequest, "invalid session id")
		return
	}

	answers, err := h.historySvc.ListAnswers(r.Context(), userID, sessionID)
	if err != nil {
		if errors.Is(err, errs.ErrNotFound) {
			writeError(w, http.StatusNotFound, "session not found")
			return
		}
		if errors.Is(err, errs.ErrForbidden) {
			writeError(w, http.StatusForbidden, "forbidden")
			return
		}
		writeError(w, http.StatusInternalServerError, "failed to list answers")
		return
	}
	writeJSON(w, http.StatusOK, answers)
}
