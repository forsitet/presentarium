package handler

import (
	"encoding/csv"
	"encoding/json"
	"errors"
	"fmt"
	"net/http"
	"strconv"
	"time"

	"github.com/go-chi/chi/v5"
	"github.com/google/uuid"

	"presentarium/internal/errs"
	appmw "presentarium/internal/middleware"
	"presentarium/internal/service"
	"presentarium/pkg/pdf"
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

// handleExportCSV generates and streams a UTF-8 CSV file of all (non-hidden) answers.
func (h *sessionHandler) handleExportCSV(w http.ResponseWriter, r *http.Request) {
	userID := r.Context().Value(appmw.UserIDKey).(uuid.UUID)
	sessionID, err := uuid.Parse(chi.URLParam(r, "id"))
	if err != nil {
		writeError(w, http.StatusBadRequest, "invalid session id")
		return
	}

	answers, finishedAt, err := h.historySvc.ExportCSV(r.Context(), userID, sessionID)
	if err != nil {
		if errors.Is(err, errs.ErrNotFound) {
			writeError(w, http.StatusNotFound, "session not found")
			return
		}
		if errors.Is(err, errs.ErrForbidden) {
			writeError(w, http.StatusForbidden, "forbidden")
			return
		}
		writeError(w, http.StatusInternalServerError, "failed to export csv")
		return
	}

	// Build filename: session_{id}_{date}.csv
	dateStr := time.Now().Format("2006-01-02")
	if finishedAt != nil {
		dateStr = finishedAt.Format("2006-01-02")
	}
	filename := fmt.Sprintf("session_%s_%s.csv", sessionID.String(), dateStr)

	w.Header().Set("Content-Type", "text/csv; charset=utf-8")
	w.Header().Set("Content-Disposition", fmt.Sprintf(`attachment; filename="%s"`, filename))
	w.WriteHeader(http.StatusOK)

	// Write UTF-8 BOM for Excel compatibility.
	_, _ = w.Write([]byte{0xEF, 0xBB, 0xBF})

	cw := csv.NewWriter(w)
	_ = cw.Write([]string{"participant_name", "question_text", "answer", "is_correct", "score", "response_time_ms"})

	for _, a := range answers {
		isCorrectStr := ""
		if a.IsCorrect != nil {
			if *a.IsCorrect {
				isCorrectStr = "true"
			} else {
				isCorrectStr = "false"
			}
		}

		answerStr := answerToString(a.Answer)

		_ = cw.Write([]string{
			a.ParticipantName,
			a.QuestionText,
			answerStr,
			isCorrectStr,
			strconv.Itoa(a.Score),
			strconv.Itoa(a.ResponseTimeMs),
		})
	}

	cw.Flush()
}

// handleGetByParticipantToken returns session summary for a participant by their session_token query param.
// This is a public endpoint — no JWT required.
func (h *sessionHandler) handleGetByParticipantToken(w http.ResponseWriter, r *http.Request) {
	tokenStr := r.URL.Query().Get("session_token")
	if tokenStr == "" {
		writeError(w, http.StatusBadRequest, "session_token query param required")
		return
	}
	token, err := uuid.Parse(tokenStr)
	if err != nil {
		writeError(w, http.StatusBadRequest, "invalid session_token")
		return
	}
	summary, err := h.historySvc.GetParticipantHistory(r.Context(), token)
	if err != nil {
		if errors.Is(err, errs.ErrNotFound) {
			writeError(w, http.StatusNotFound, "session not found")
			return
		}
		writeError(w, http.StatusInternalServerError, "failed to get session")
		return
	}
	writeJSON(w, http.StatusOK, summary)
}

// pdfExportBody is the optional request body for POST /api/sessions/:id/export/pdf.
type pdfExportBody struct {
	Charts []pdfChartEntry `json:"charts"`
}

type pdfChartEntry struct {
	QuestionIndex int    `json:"question_index"` // 0-based index into questions list
	Image         string `json:"image"`          // base64-encoded PNG or JPEG
}

// handleExportPDF generates and streams a PDF report for a finished session.
// POST /api/sessions/:id/export/pdf
// Optional body: { "charts": [{"question_index": 0, "image": "<base64>"}] }
func (h *sessionHandler) handleExportPDF(w http.ResponseWriter, r *http.Request) {
	userID := r.Context().Value(appmw.UserIDKey).(uuid.UUID)
	sessionID, err := uuid.Parse(chi.URLParam(r, "id"))
	if err != nil {
		writeError(w, http.StatusBadRequest, "invalid session id")
		return
	}

	// Parse optional body
	var body pdfExportBody
	if r.ContentLength > 0 {
		_ = json.NewDecoder(r.Body).Decode(&body)
	}

	data, err := h.historySvc.ExportPDFData(r.Context(), userID, sessionID)
	if err != nil {
		if errors.Is(err, errs.ErrNotFound) {
			writeError(w, http.StatusNotFound, "session not found")
			return
		}
		if errors.Is(err, errs.ErrForbidden) {
			writeError(w, http.StatusForbidden, "forbidden")
			return
		}
		writeError(w, http.StatusInternalServerError, "failed to load session data")
		return
	}

	// Build chart image map
	chartImages := make(map[int]string, len(body.Charts))
	for _, c := range body.Charts {
		if c.Image != "" {
			chartImages[c.QuestionIndex] = c.Image
		}
	}

	// Convert service types to pdf.SessionReport
	questions := make([]pdf.QuestionReport, 0, len(data.Detail.Questions))
	for _, q := range data.Detail.Questions {
		questions = append(questions, pdf.QuestionReport{
			Text:         q.Text,
			Type:         q.Type,
			TotalAnswers: q.TotalAnswers,
			Distribution: q.Distribution,
		})
	}

	leaderboard := make([]pdf.LeaderboardEntry, 0, len(data.Detail.Leaderboard))
	for i, row := range data.Detail.Leaderboard {
		leaderboard = append(leaderboard, pdf.LeaderboardEntry{
			Rank:  i + 1,
			Name:  row.Name,
			Score: row.TotalScore,
		})
	}

	report := pdf.SessionReport{
		PollTitle:        data.Detail.PollTitle,
		StartedAt:        data.Detail.StartedAt,
		FinishedAt:       data.Detail.FinishedAt,
		ParticipantCount: data.Detail.ParticipantCount,
		AverageScore:     data.Detail.AverageScore,
		Questions:        questions,
		Leaderboard:      leaderboard,
		ChartImages:      chartImages,
	}

	// Build filename
	dateStr := time.Now().Format("2006-01-02")
	if data.FinishedAt != nil {
		dateStr = data.FinishedAt.Format("2006-01-02")
	}
	filename := fmt.Sprintf("session_%s_%s.pdf", sessionID.String(), dateStr)

	w.Header().Set("Content-Type", "application/pdf")
	w.Header().Set("Content-Disposition", fmt.Sprintf(`attachment; filename="%s"`, filename))
	w.WriteHeader(http.StatusOK)

	if err := pdf.GenerateReport(w, report); err != nil {
		// Headers already sent; nothing we can do
		return
	}
}

// answerToString converts a JSON answer value to a human-readable string.
// Quoted JSON strings are unquoted; other JSON values are left as-is.
func answerToString(raw json.RawMessage) string {
	if len(raw) == 0 {
		return ""
	}
	var s string
	if err := json.Unmarshal(raw, &s); err == nil {
		return s
	}
	return string(raw)
}
