package handler

import (
	"bytes"
	"fmt"
	"io"
	"net/http"
	"strings"

	"github.com/google/uuid"

	"presentarium/internal/storage"
)

const maxUploadSize = 5 << 20 // 5 MB

// allowedMIMETypes maps detected MIME type to file extension.
var allowedMIMETypes = map[string]string{
	"image/jpeg": "jpg",
	"image/png":  "png",
	"image/webp": "webp",
}

type uploadHandler struct {
	store storage.Storage
}

func newUploadHandler(store storage.Storage) *uploadHandler {
	return &uploadHandler{store: store}
}

// handleImage handles POST /api/upload/image. Uploads are written to the
// public object-storage bucket under the "images/" key prefix and the
// returned image_url is the direct (CDN-fronted in prod) URL.
func (h *uploadHandler) handleImage(w http.ResponseWriter, r *http.Request) {
	// Limit request body to prevent OOM; extra 1 KB for form overhead.
	r.Body = http.MaxBytesReader(w, r.Body, maxUploadSize+1024)

	if err := r.ParseMultipartForm(maxUploadSize); err != nil {
		if strings.Contains(err.Error(), "http: request body too large") {
			writeError(w, http.StatusRequestEntityTooLarge, "file too large (max 5 MB)")
			return
		}
		writeError(w, http.StatusBadRequest, "failed to parse form")
		return
	}

	file, _, err := r.FormFile("image")
	if err != nil {
		writeError(w, http.StatusBadRequest, "missing 'image' field in form")
		return
	}
	defer file.Close()

	// Read up to 5MB + 1 byte to detect oversized uploads.
	data, err := io.ReadAll(io.LimitReader(file, maxUploadSize+1))
	if err != nil {
		writeError(w, http.StatusInternalServerError, "failed to read file")
		return
	}
	if int64(len(data)) > maxUploadSize {
		writeError(w, http.StatusRequestEntityTooLarge, "file too large (max 5 MB)")
		return
	}

	// Detect MIME type from file magic bytes (do NOT trust Content-Type header).
	mimeType := detectMIME(data)
	ext, allowed := allowedMIMETypes[mimeType]
	if !allowed {
		writeError(w, http.StatusUnsupportedMediaType, "unsupported file type; allowed: jpeg, png, webp")
		return
	}

	key := fmt.Sprintf("images/%s.%s", uuid.New().String(), ext)
	url, err := h.store.Put(r.Context(), key, bytes.NewReader(data), mimeType)
	if err != nil {
		writeError(w, http.StatusInternalServerError, "failed to save file")
		return
	}

	writeJSON(w, http.StatusOK, map[string]string{"image_url": url})
}

// detectMIME inspects the first bytes of data and returns the MIME type.
func detectMIME(data []byte) string {
	if len(data) < 4 {
		return ""
	}
	// JPEG: FF D8 FF
	if bytes.HasPrefix(data, []byte{0xFF, 0xD8, 0xFF}) {
		return "image/jpeg"
	}
	// PNG: 89 50 4E 47 0D 0A 1A 0A
	if bytes.HasPrefix(data, []byte{0x89, 0x50, 0x4E, 0x47, 0x0D, 0x0A, 0x1A, 0x0A}) {
		return "image/png"
	}
	// WebP: RIFF????WEBP
	if len(data) >= 12 &&
		bytes.HasPrefix(data, []byte("RIFF")) &&
		bytes.Equal(data[8:12], []byte("WEBP")) {
		return "image/webp"
	}
	return ""
}
