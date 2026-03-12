package handler

import (
	"encoding/json"
	"errors"
	"net/http"
	"strings"
	"time"

	"github.com/go-playground/validator/v10"

	"presentarium/internal/errs"
	"presentarium/internal/service"
)

const refreshTokenCookie = "refresh_token"

type authHandler struct {
	authSvc         service.AuthService
	validate        *validator.Validate
	refreshTokenTTL time.Duration
	appBaseURL      string
}

func newAuthHandler(authSvc service.AuthService, refreshTokenTTLDays int, appBaseURL string) *authHandler {
	return &authHandler{
		authSvc:         authSvc,
		validate:        validator.New(),
		refreshTokenTTL: time.Duration(refreshTokenTTLDays) * 24 * time.Hour,
		appBaseURL:      appBaseURL,
	}
}

// registerRequest is the JSON body for POST /api/auth/register.
type registerRequest struct {
	Email    string `json:"email"    validate:"required,email"`
	Password string `json:"password" validate:"required,min=8"`
	Name     string `json:"name"     validate:"required,max=100"`
}

// loginRequest is the JSON body for POST /api/auth/login.
type loginRequest struct {
	Email    string `json:"email"    validate:"required,email"`
	Password string `json:"password" validate:"required"`
}

// tokenResponse is returned after register/login/refresh.
type tokenResponse struct {
	AccessToken string `json:"access_token"`
}

func (h *authHandler) handleRegister(w http.ResponseWriter, r *http.Request) {
	var req registerRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		writeError(w, http.StatusBadRequest, "invalid request body")
		return
	}

	req.Email = strings.TrimSpace(strings.ToLower(req.Email))
	req.Name = strings.TrimSpace(req.Name)

	if err := h.validate.Struct(req); err != nil {
		writeValidationError(w, err)
		return
	}

	pair, _, err := h.authSvc.Register(r.Context(), req.Email, req.Password, req.Name)
	if err != nil {
		if errors.Is(err, errs.ErrConflict) {
			writeError(w, http.StatusConflict, "email already registered")
			return
		}
		writeError(w, http.StatusInternalServerError, "registration failed")
		return
	}

	h.setRefreshCookie(w, pair.RefreshToken)
	writeJSON(w, http.StatusCreated, tokenResponse{AccessToken: pair.AccessToken})
}

func (h *authHandler) handleLogin(w http.ResponseWriter, r *http.Request) {
	var req loginRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		writeError(w, http.StatusBadRequest, "invalid request body")
		return
	}

	req.Email = strings.TrimSpace(strings.ToLower(req.Email))

	if err := h.validate.Struct(req); err != nil {
		writeValidationError(w, err)
		return
	}

	pair, _, err := h.authSvc.Login(r.Context(), req.Email, req.Password)
	if err != nil {
		if errors.Is(err, errs.ErrUnauthorized) {
			writeError(w, http.StatusUnauthorized, "invalid credentials")
			return
		}
		writeError(w, http.StatusInternalServerError, "login failed")
		return
	}

	h.setRefreshCookie(w, pair.RefreshToken)
	writeJSON(w, http.StatusOK, tokenResponse{AccessToken: pair.AccessToken})
}

func (h *authHandler) handleRefresh(w http.ResponseWriter, r *http.Request) {
	cookie, err := r.Cookie(refreshTokenCookie)
	if err != nil {
		writeError(w, http.StatusUnauthorized, "missing refresh token")
		return
	}

	pair, err := h.authSvc.Refresh(r.Context(), cookie.Value)
	if err != nil {
		if errors.Is(err, errs.ErrUnauthorized) {
			h.clearRefreshCookie(w)
			writeError(w, http.StatusUnauthorized, "invalid or expired refresh token")
			return
		}
		writeError(w, http.StatusInternalServerError, "refresh failed")
		return
	}

	h.setRefreshCookie(w, pair.RefreshToken)
	writeJSON(w, http.StatusOK, tokenResponse{AccessToken: pair.AccessToken})
}

func (h *authHandler) handleLogout(w http.ResponseWriter, r *http.Request) {
	cookie, err := r.Cookie(refreshTokenCookie)
	if err == nil {
		_ = h.authSvc.Logout(r.Context(), cookie.Value)
	}
	h.clearRefreshCookie(w)
	w.WriteHeader(http.StatusOK)
}

// forgotPasswordRequest is the JSON body for POST /api/auth/forgot-password.
type forgotPasswordRequest struct {
	Email string `json:"email" validate:"required,email"`
}

func (h *authHandler) handleForgotPassword(w http.ResponseWriter, r *http.Request) {
	var req forgotPasswordRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		writeError(w, http.StatusBadRequest, "invalid request body")
		return
	}
	req.Email = strings.TrimSpace(strings.ToLower(req.Email))
	if err := h.validate.Struct(req); err != nil {
		// Always return 200 — don't reveal whether the email exists.
		writeJSON(w, http.StatusOK, map[string]string{"message": "if the email exists, a reset link has been sent"})
		return
	}

	// Ignore error — always 200 to avoid account enumeration.
	_ = h.authSvc.ForgotPassword(r.Context(), req.Email, h.appBaseURL)
	writeJSON(w, http.StatusOK, map[string]string{"message": "if the email exists, a reset link has been sent"})
}

// resetPasswordRequest is the JSON body for POST /api/auth/reset-password.
type resetPasswordRequest struct {
	Token       string `json:"token"        validate:"required"`
	NewPassword string `json:"new_password" validate:"required,min=8"`
}

func (h *authHandler) handleResetPassword(w http.ResponseWriter, r *http.Request) {
	var req resetPasswordRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		writeError(w, http.StatusBadRequest, "invalid request body")
		return
	}
	if err := h.validate.Struct(req); err != nil {
		writeValidationError(w, err)
		return
	}

	if err := h.authSvc.ResetPassword(r.Context(), req.Token, req.NewPassword); err != nil {
		if errors.Is(err, errs.ErrNotFound) || errors.Is(err, errs.ErrValidation) {
			writeError(w, http.StatusBadRequest, "invalid or expired reset token")
			return
		}
		writeError(w, http.StatusInternalServerError, "password reset failed")
		return
	}

	writeJSON(w, http.StatusOK, map[string]string{"message": "password updated successfully"})
}

func (h *authHandler) setRefreshCookie(w http.ResponseWriter, token string) {
	http.SetCookie(w, &http.Cookie{
		Name:     refreshTokenCookie,
		Value:    token,
		Path:     "/api/auth",
		MaxAge:   int(h.refreshTokenTTL.Seconds()),
		HttpOnly: true,
		SameSite: http.SameSiteStrictMode,
		// Secure: true in production (requires HTTPS)
	})
}

func (h *authHandler) clearRefreshCookie(w http.ResponseWriter) {
	http.SetCookie(w, &http.Cookie{
		Name:     refreshTokenCookie,
		Value:    "",
		Path:     "/api/auth",
		MaxAge:   -1,
		HttpOnly: true,
		SameSite: http.SameSiteStrictMode,
	})
}

// writeJSON encodes v as JSON with the given status code.
func writeJSON(w http.ResponseWriter, status int, v interface{}) {
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(status)
	_ = json.NewEncoder(w).Encode(v)
}

// writeError writes a JSON error response.
func writeError(w http.ResponseWriter, status int, message string) {
	writeJSON(w, status, map[string]string{"error": message})
}

// writeValidationError formats validator errors and writes a 400 response.
func writeValidationError(w http.ResponseWriter, err error) {
	var ve validator.ValidationErrors
	if errors.As(err, &ve) {
		fields := make(map[string]string, len(ve))
		for _, fe := range ve {
			fields[strings.ToLower(fe.Field())] = fe.Tag()
		}
		writeJSON(w, http.StatusBadRequest, map[string]interface{}{
			"error":  "validation failed",
			"fields": fields,
		})
		return
	}
	writeError(w, http.StatusBadRequest, "invalid request")
}
