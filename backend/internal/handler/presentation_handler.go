package handler

import (
	"bytes"
	"errors"
	"io"
	"net/http"
	"path/filepath"
	"strings"

	"github.com/go-chi/chi/v5"
	"github.com/google/uuid"

	"presentarium/internal/errs"
	appmw "presentarium/internal/middleware"
	"presentarium/internal/model"
	"presentarium/internal/service"
)

// maxPresentationUploadSize caps the .pptx file size. 50 MB comfortably
// covers typical decks with embedded media without exposing the server to
// obviously malicious uploads.
const maxPresentationUploadSize = 50 << 20 // 50 MB

// pptxMIMEType is the canonical Content-Type for modern PowerPoint files.
const pptxMIMEType = "application/vnd.openxmlformats-officedocument.presentationml.presentation"

// pptxZIPMagic matches the ZIP-archive header that every .pptx file starts
// with (pptx is a ZIP container). We check this in addition to the filename
// extension so a renamed .exe cannot sneak through.
var pptxZIPMagic = []byte{0x50, 0x4B, 0x03, 0x04}

type presentationHandler struct {
	svc service.PresentationService
}

func newPresentationHandler(svc service.PresentationService) *presentationHandler {
	return &presentationHandler{svc: svc}
}

// handleCreate handles POST /api/presentations. Accepts multipart/form-data
// with a "file" field (required, .pptx) and an optional "title" field.
// Returns 202 Accepted — the actual conversion happens in the background.
func (h *presentationHandler) handleCreate(w http.ResponseWriter, r *http.Request) {
	userID := r.Context().Value(appmw.UserIDKey).(uuid.UUID)

	// +1 KB for form overhead
	r.Body = http.MaxBytesReader(w, r.Body, maxPresentationUploadSize+1024)
	if err := r.ParseMultipartForm(maxPresentationUploadSize); err != nil {
		if strings.Contains(err.Error(), "http: request body too large") {
			writeError(w, http.StatusRequestEntityTooLarge, "file too large (max 50 MB)")
			return
		}
		writeError(w, http.StatusBadRequest, "failed to parse form")
		return
	}

	file, fileHeader, err := r.FormFile("file")
	if err != nil {
		writeError(w, http.StatusBadRequest, "missing 'file' field in form")
		return
	}
	defer file.Close()

	// Enforce extension + magic bytes BEFORE reading the whole file.
	filename := fileHeader.Filename
	if strings.ToLower(filepath.Ext(filename)) != ".pptx" {
		writeError(w, http.StatusUnsupportedMediaType, "only .pptx files are supported")
		return
	}

	data, err := io.ReadAll(io.LimitReader(file, maxPresentationUploadSize+1))
	if err != nil {
		writeError(w, http.StatusInternalServerError, "failed to read file")
		return
	}
	if int64(len(data)) > maxPresentationUploadSize {
		writeError(w, http.StatusRequestEntityTooLarge, "file too large (max 50 MB)")
		return
	}
	if !bytes.HasPrefix(data, pptxZIPMagic) {
		writeError(w, http.StatusUnsupportedMediaType, "file is not a valid .pptx")
		return
	}

	req := service.CreatePresentationRequest{
		Title:            strings.TrimSpace(r.FormValue("title")),
		OriginalFilename: filename,
		Source:           data,
	}
	p, err := h.svc.Create(r.Context(), userID, req)
	if err != nil {
		if errors.Is(err, errs.ErrValidation) {
			writeError(w, http.StatusBadRequest, "invalid file")
			return
		}
		writeError(w, http.StatusInternalServerError, "failed to create presentation")
		return
	}
	writeJSON(w, http.StatusAccepted, p)
}

// handleList handles GET /api/presentations.
func (h *presentationHandler) handleList(w http.ResponseWriter, r *http.Request) {
	userID := r.Context().Value(appmw.UserIDKey).(uuid.UUID)
	items, err := h.svc.List(r.Context(), userID)
	if err != nil {
		writeError(w, http.StatusInternalServerError, "failed to list presentations")
		return
	}
	if items == nil {
		items = []*model.Presentation{}
	}
	writeJSON(w, http.StatusOK, items)
}

// handleGet handles GET /api/presentations/{id}. Returns the presentation
// together with an ordered list of slide URLs (empty while processing).
func (h *presentationHandler) handleGet(w http.ResponseWriter, r *http.Request) {
	userID := r.Context().Value(appmw.UserIDKey).(uuid.UUID)
	id, err := uuid.Parse(chi.URLParam(r, "id"))
	if err != nil {
		writeError(w, http.StatusBadRequest, "invalid presentation id")
		return
	}
	detail, err := h.svc.Get(r.Context(), userID, id)
	if err != nil {
		if errors.Is(err, errs.ErrNotFound) {
			writeError(w, http.StatusNotFound, "presentation not found")
			return
		}
		if errors.Is(err, errs.ErrForbidden) {
			writeError(w, http.StatusForbidden, "forbidden")
			return
		}
		writeError(w, http.StatusInternalServerError, "failed to get presentation")
		return
	}
	writeJSON(w, http.StatusOK, detail)
}

// handleDelete handles DELETE /api/presentations/{id}.
func (h *presentationHandler) handleDelete(w http.ResponseWriter, r *http.Request) {
	userID := r.Context().Value(appmw.UserIDKey).(uuid.UUID)
	id, err := uuid.Parse(chi.URLParam(r, "id"))
	if err != nil {
		writeError(w, http.StatusBadRequest, "invalid presentation id")
		return
	}
	if err := h.svc.Delete(r.Context(), userID, id); err != nil {
		if errors.Is(err, errs.ErrNotFound) {
			writeError(w, http.StatusNotFound, "presentation not found")
			return
		}
		if errors.Is(err, errs.ErrForbidden) {
			writeError(w, http.StatusForbidden, "forbidden")
			return
		}
		writeError(w, http.StatusInternalServerError, "failed to delete presentation")
		return
	}
	w.WriteHeader(http.StatusNoContent)
}
