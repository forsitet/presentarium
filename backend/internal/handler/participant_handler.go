package handler

import (
	"errors"
	"net/http"

	"github.com/go-chi/chi/v5"

	"presentarium/internal/errs"
	"presentarium/internal/service"
)

type participantHandler struct {
	participantSvc service.ParticipantService
}

func newParticipantHandler(participantSvc service.ParticipantService) *participantHandler {
	return &participantHandler{participantSvc: participantSvc}
}

// handleList handles GET /api/rooms/:code/participants
func (h *participantHandler) handleList(w http.ResponseWriter, r *http.Request) {
	code := chi.URLParam(r, "code")

	participants, err := h.participantSvc.ListParticipants(r.Context(), code)
	if err != nil {
		if errors.Is(err, errs.ErrNotFound) {
			writeError(w, http.StatusNotFound, "room not found")
			return
		}
		writeError(w, http.StatusInternalServerError, "failed to list participants")
		return
	}

	type participantResp struct {
		ID        string `json:"id"`
		Name      string `json:"name"`
		JoinedAt  string `json:"joined_at"`
		TotalScore int   `json:"total_score"`
	}

	result := make([]participantResp, 0, len(participants))
	for _, p := range participants {
		result = append(result, participantResp{
			ID:         p.ID.String(),
			Name:       p.Name,
			JoinedAt:   p.JoinedAt.Format("2006-01-02T15:04:05Z07:00"),
			TotalScore: p.TotalScore,
		})
	}

	writeJSON(w, http.StatusOK, result)
}
