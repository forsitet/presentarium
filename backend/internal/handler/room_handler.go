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

type roomHandler struct {
	roomSvc    service.RoomService
	conductSvc service.ConductService
}

func newRoomHandler(roomSvc service.RoomService, conductSvc service.ConductService) *roomHandler {
	return &roomHandler{roomSvc: roomSvc, conductSvc: conductSvc}
}

// handleCreate handles POST /api/rooms
func (h *roomHandler) handleCreate(w http.ResponseWriter, r *http.Request) {
	userID := r.Context().Value(appmw.UserIDKey).(uuid.UUID)

	var req struct {
		PollID uuid.UUID `json:"poll_id"`
	}
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		writeError(w, http.StatusBadRequest, "invalid request body")
		return
	}
	if req.PollID == uuid.Nil {
		writeError(w, http.StatusBadRequest, "poll_id is required")
		return
	}

	resp, err := h.roomSvc.CreateRoom(r.Context(), userID, req.PollID)
	if err != nil {
		if errors.Is(err, errs.ErrNotFound) {
			writeError(w, http.StatusNotFound, "poll not found")
			return
		}
		if errors.Is(err, errs.ErrForbidden) {
			writeError(w, http.StatusForbidden, "forbidden")
			return
		}
		if errors.Is(err, errs.ErrConflict) {
			writeError(w, http.StatusConflict, "active room already exists for this poll")
			return
		}
		writeError(w, http.StatusInternalServerError, "failed to create room")
		return
	}
	writeJSON(w, http.StatusCreated, resp)
}

// handleGet handles GET /api/rooms/:code
func (h *roomHandler) handleGet(w http.ResponseWriter, r *http.Request) {
	code := chi.URLParam(r, "code")

	resp, err := h.roomSvc.GetRoom(r.Context(), code)
	if err != nil {
		if errors.Is(err, errs.ErrNotFound) {
			writeError(w, http.StatusNotFound, "room not found")
			return
		}
		writeError(w, http.StatusInternalServerError, "failed to get room")
		return
	}
	writeJSON(w, http.StatusOK, resp)
}

// handleChangeState handles PATCH /api/rooms/:code/state
// Supports actions: start, end, end_question.
func (h *roomHandler) handleChangeState(w http.ResponseWriter, r *http.Request) {
	userID := r.Context().Value(appmw.UserIDKey).(uuid.UUID)
	code := chi.URLParam(r, "code")

	var req struct {
		Action string `json:"action"`
	}
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		writeError(w, http.StatusBadRequest, "invalid request body")
		return
	}

	switch req.Action {
	case "end_question":
		// Handled by conduct service (TASK-017).
		if h.conductSvc == nil {
			writeError(w, http.StatusServiceUnavailable, "conduct service not available")
			return
		}
		if err := h.conductSvc.EndQuestion(r.Context(), userID, code); err != nil {
			if errors.Is(err, errs.ErrNotFound) {
				writeError(w, http.StatusNotFound, "room or active question not found")
				return
			}
			if errors.Is(err, errs.ErrForbidden) {
				writeError(w, http.StatusForbidden, "forbidden")
				return
			}
			if errors.Is(err, errs.ErrValidation) {
				writeError(w, http.StatusBadRequest, "no active question to end")
				return
			}
			writeError(w, http.StatusInternalServerError, "failed to end question")
			return
		}
		writeJSON(w, http.StatusOK, map[string]string{"status": "ok"})
		return

	case "end":
		// Session end: conduct service broadcasts session_end and updates DB status.
		if h.conductSvc != nil {
			if err := h.conductSvc.EndSession(r.Context(), userID, code); err != nil {
				if errors.Is(err, errs.ErrNotFound) {
					writeError(w, http.StatusNotFound, "room not found")
					return
				}
				if errors.Is(err, errs.ErrForbidden) {
					writeError(w, http.StatusForbidden, "forbidden")
					return
				}
				writeError(w, http.StatusInternalServerError, "failed to end session")
				return
			}
			writeJSON(w, http.StatusOK, map[string]string{"status": "ok"})
			return
		}
		// Fall through to room service if conduct service is unavailable.
	}

	if err := h.roomSvc.ChangeState(r.Context(), userID, code, req.Action); err != nil {
		if errors.Is(err, errs.ErrNotFound) {
			writeError(w, http.StatusNotFound, "room not found")
			return
		}
		if errors.Is(err, errs.ErrForbidden) {
			writeError(w, http.StatusForbidden, "forbidden")
			return
		}
		if errors.Is(err, errs.ErrConflict) {
			writeError(w, http.StatusConflict, "room is already finished")
			return
		}
		if errors.Is(err, errs.ErrValidation) {
			writeError(w, http.StatusBadRequest, "invalid action for current room state")
			return
		}
		writeError(w, http.StatusInternalServerError, "failed to change room state")
		return
	}
	writeJSON(w, http.StatusOK, map[string]string{"status": "ok"})
}
