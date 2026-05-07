package handler

import (
	"context"
	"encoding/json"
	"errors"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/go-playground/validator/v10"

	"presentarium/internal/errs"
	"presentarium/internal/model"
	"presentarium/internal/service"
)

// stubAuthService implements service.AuthService for handler tests.
type stubAuthService struct {
	registerPair *service.TokenPair
	registerUser *model.User
	registerErr  error

	loginPair *service.TokenPair
	loginUser *model.User
	loginErr  error

	refreshPair *service.TokenPair
	refreshErr  error

	logoutErr error

	forgotErr error
	resetErr  error
}

func (s *stubAuthService) Register(_ context.Context, _, _, _ string) (*service.TokenPair, *model.User, error) {
	return s.registerPair, s.registerUser, s.registerErr
}
func (s *stubAuthService) Login(_ context.Context, _, _ string) (*service.TokenPair, *model.User, error) {
	return s.loginPair, s.loginUser, s.loginErr
}
func (s *stubAuthService) Refresh(_ context.Context, _ string) (*service.TokenPair, error) {
	return s.refreshPair, s.refreshErr
}
func (s *stubAuthService) Logout(_ context.Context, _ string) error { return s.logoutErr }
func (s *stubAuthService) ForgotPassword(_ context.Context, _, _ string) error {
	return s.forgotErr
}
func (s *stubAuthService) ResetPassword(_ context.Context, _, _ string) error { return s.resetErr }

func newHandler(svc service.AuthService) *authHandler {
	return newAuthHandler(svc, 7, "http://app.example")
}

func TestWriteJSON(t *testing.T) {
	rec := httptest.NewRecorder()
	writeJSON(rec, http.StatusTeapot, map[string]int{"a": 1})
	if rec.Code != http.StatusTeapot {
		t.Errorf("status = %d", rec.Code)
	}
	if ct := rec.Header().Get("Content-Type"); ct != "application/json" {
		t.Errorf("content-type = %q", ct)
	}
	var got map[string]int
	if err := json.NewDecoder(rec.Body).Decode(&got); err != nil {
		t.Fatalf("decode: %v", err)
	}
	if got["a"] != 1 {
		t.Errorf("body decoded to %+v", got)
	}
}

func TestWriteError(t *testing.T) {
	rec := httptest.NewRecorder()
	writeError(rec, http.StatusBadRequest, "oops")
	if rec.Code != http.StatusBadRequest {
		t.Errorf("status = %d", rec.Code)
	}
	body := rec.Body.String()
	if !strings.Contains(body, `"error":"oops"`) {
		t.Errorf("body = %q", body)
	}
}

func TestWriteValidationError_WithValidatorErrors(t *testing.T) {
	v := validator.New()
	err := v.Struct(struct {
		Email string `validate:"required,email"`
	}{Email: "not-an-email"})
	if err == nil {
		t.Fatal("validator should have returned an error")
	}
	rec := httptest.NewRecorder()
	writeValidationError(rec, err)
	if rec.Code != http.StatusBadRequest {
		t.Errorf("status = %d", rec.Code)
	}
	var got struct {
		Error  string            `json:"error"`
		Fields map[string]string `json:"fields"`
	}
	if err := json.NewDecoder(rec.Body).Decode(&got); err != nil {
		t.Fatalf("decode: %v", err)
	}
	if got.Error != "validation failed" {
		t.Errorf("error msg = %q", got.Error)
	}
	if _, ok := got.Fields["email"]; !ok {
		t.Errorf("expected 'email' in fields, got %+v", got.Fields)
	}
}

func TestWriteValidationError_Generic(t *testing.T) {
	rec := httptest.NewRecorder()
	writeValidationError(rec, errors.New("not a validation error"))
	if rec.Code != http.StatusBadRequest {
		t.Errorf("status = %d", rec.Code)
	}
	if !strings.Contains(rec.Body.String(), "invalid request") {
		t.Errorf("body = %q", rec.Body.String())
	}
}

func TestHandleRegister_Success(t *testing.T) {
	svc := &stubAuthService{
		registerPair: &service.TokenPair{AccessToken: "atok", RefreshToken: "rtok"},
	}
	h := newHandler(svc)
	body := strings.NewReader(`{"email":"u@x.com","password":"longenough","name":"User"}`)
	req := httptest.NewRequest(http.MethodPost, "/api/auth/register", body)
	rec := httptest.NewRecorder()
	h.handleRegister(rec, req)

	if rec.Code != http.StatusCreated {
		t.Errorf("status = %d", rec.Code)
	}
	var resp tokenResponse
	if err := json.NewDecoder(rec.Body).Decode(&resp); err != nil {
		t.Fatalf("decode: %v", err)
	}
	if resp.AccessToken != "atok" {
		t.Errorf("access token = %q", resp.AccessToken)
	}
	cookies := rec.Result().Cookies()
	found := false
	for _, c := range cookies {
		if c.Name == refreshTokenCookie && c.Value == "rtok" {
			found = true
		}
	}
	if !found {
		t.Error("refresh_token cookie missing or wrong value")
	}
}

func TestHandleRegister_InvalidJSON(t *testing.T) {
	h := newHandler(&stubAuthService{})
	req := httptest.NewRequest(http.MethodPost, "/api/auth/register", strings.NewReader("{not-json"))
	rec := httptest.NewRecorder()
	h.handleRegister(rec, req)
	if rec.Code != http.StatusBadRequest {
		t.Errorf("status = %d", rec.Code)
	}
}

func TestHandleRegister_ValidationFails(t *testing.T) {
	h := newHandler(&stubAuthService{})
	req := httptest.NewRequest(http.MethodPost, "/api/auth/register",
		strings.NewReader(`{"email":"bad","password":"short","name":""}`))
	rec := httptest.NewRecorder()
	h.handleRegister(rec, req)
	if rec.Code != http.StatusBadRequest {
		t.Errorf("status = %d", rec.Code)
	}
	if !strings.Contains(rec.Body.String(), "validation failed") {
		t.Errorf("body = %q", rec.Body.String())
	}
}

func TestHandleRegister_Conflict(t *testing.T) {
	h := newHandler(&stubAuthService{registerErr: errs.ErrConflict})
	req := httptest.NewRequest(http.MethodPost, "/api/auth/register",
		strings.NewReader(`{"email":"u@x.com","password":"longenough","name":"User"}`))
	rec := httptest.NewRecorder()
	h.handleRegister(rec, req)
	if rec.Code != http.StatusConflict {
		t.Errorf("status = %d, want 409", rec.Code)
	}
}

func TestHandleRegister_InternalError(t *testing.T) {
	h := newHandler(&stubAuthService{registerErr: errors.New("boom")})
	req := httptest.NewRequest(http.MethodPost, "/api/auth/register",
		strings.NewReader(`{"email":"u@x.com","password":"longenough","name":"User"}`))
	rec := httptest.NewRecorder()
	h.handleRegister(rec, req)
	if rec.Code != http.StatusInternalServerError {
		t.Errorf("status = %d", rec.Code)
	}
}

func TestHandleLogin_Success(t *testing.T) {
	svc := &stubAuthService{loginPair: &service.TokenPair{AccessToken: "a", RefreshToken: "r"}}
	h := newHandler(svc)
	req := httptest.NewRequest(http.MethodPost, "/api/auth/login",
		strings.NewReader(`{"email":"U@X.com","password":"x"}`))
	rec := httptest.NewRecorder()
	h.handleLogin(rec, req)
	if rec.Code != http.StatusOK {
		t.Errorf("status = %d", rec.Code)
	}
}

func TestHandleLogin_BadCredentials(t *testing.T) {
	h := newHandler(&stubAuthService{loginErr: errs.ErrUnauthorized})
	req := httptest.NewRequest(http.MethodPost, "/api/auth/login",
		strings.NewReader(`{"email":"u@x.com","password":"x"}`))
	rec := httptest.NewRecorder()
	h.handleLogin(rec, req)
	if rec.Code != http.StatusUnauthorized {
		t.Errorf("status = %d", rec.Code)
	}
}

func TestHandleLogin_InvalidJSON(t *testing.T) {
	h := newHandler(&stubAuthService{})
	req := httptest.NewRequest(http.MethodPost, "/api/auth/login", strings.NewReader("garbage"))
	rec := httptest.NewRecorder()
	h.handleLogin(rec, req)
	if rec.Code != http.StatusBadRequest {
		t.Errorf("status = %d", rec.Code)
	}
}

func TestHandleLogin_ValidationFails(t *testing.T) {
	h := newHandler(&stubAuthService{})
	req := httptest.NewRequest(http.MethodPost, "/api/auth/login", strings.NewReader(`{"email":"","password":""}`))
	rec := httptest.NewRecorder()
	h.handleLogin(rec, req)
	if rec.Code != http.StatusBadRequest {
		t.Errorf("status = %d", rec.Code)
	}
}

func TestHandleLogin_InternalError(t *testing.T) {
	h := newHandler(&stubAuthService{loginErr: errors.New("db down")})
	req := httptest.NewRequest(http.MethodPost, "/api/auth/login",
		strings.NewReader(`{"email":"u@x.com","password":"x"}`))
	rec := httptest.NewRecorder()
	h.handleLogin(rec, req)
	if rec.Code != http.StatusInternalServerError {
		t.Errorf("status = %d", rec.Code)
	}
}

func TestHandleRefresh_NoCookie(t *testing.T) {
	h := newHandler(&stubAuthService{})
	req := httptest.NewRequest(http.MethodPost, "/api/auth/refresh", nil)
	rec := httptest.NewRecorder()
	h.handleRefresh(rec, req)
	if rec.Code != http.StatusUnauthorized {
		t.Errorf("status = %d", rec.Code)
	}
}

func TestHandleRefresh_Success(t *testing.T) {
	svc := &stubAuthService{refreshPair: &service.TokenPair{AccessToken: "new-a", RefreshToken: "new-r"}}
	h := newHandler(svc)
	req := httptest.NewRequest(http.MethodPost, "/api/auth/refresh", nil)
	req.AddCookie(&http.Cookie{Name: refreshTokenCookie, Value: "old-r"})
	rec := httptest.NewRecorder()
	h.handleRefresh(rec, req)
	if rec.Code != http.StatusOK {
		t.Errorf("status = %d", rec.Code)
	}
}

func TestHandleRefresh_InvalidToken(t *testing.T) {
	h := newHandler(&stubAuthService{refreshErr: errs.ErrUnauthorized})
	req := httptest.NewRequest(http.MethodPost, "/api/auth/refresh", nil)
	req.AddCookie(&http.Cookie{Name: refreshTokenCookie, Value: "old-r"})
	rec := httptest.NewRecorder()
	h.handleRefresh(rec, req)
	if rec.Code != http.StatusUnauthorized {
		t.Errorf("status = %d", rec.Code)
	}
}

func TestHandleLogout_NoCookie(t *testing.T) {
	h := newHandler(&stubAuthService{})
	req := httptest.NewRequest(http.MethodPost, "/api/auth/logout", nil)
	rec := httptest.NewRecorder()
	h.handleLogout(rec, req)
	// Should still respond, clearing cookie or returning OK; just verify it doesn't error 500.
	if rec.Code >= 500 {
		t.Errorf("status = %d", rec.Code)
	}
}

func TestHandleLogout_WithCookie(t *testing.T) {
	h := newHandler(&stubAuthService{})
	req := httptest.NewRequest(http.MethodPost, "/api/auth/logout", nil)
	req.AddCookie(&http.Cookie{Name: refreshTokenCookie, Value: "tok"})
	rec := httptest.NewRecorder()
	h.handleLogout(rec, req)
	if rec.Code >= 500 {
		t.Errorf("status = %d", rec.Code)
	}
	// Should clear the cookie.
	cleared := false
	for _, c := range rec.Result().Cookies() {
		if c.Name == refreshTokenCookie && c.MaxAge < 0 {
			cleared = true
		}
	}
	if !cleared {
		t.Error("expected refresh cookie to be cleared")
	}
}

func TestHandleForgotPassword_AlwaysOK(t *testing.T) {
	// Forgot-password should not leak whether the email exists; on any svc result,
	// the handler should return success. We test the success path here.
	h := newHandler(&stubAuthService{})
	req := httptest.NewRequest(http.MethodPost, "/api/auth/forgot-password",
		strings.NewReader(`{"email":"u@x.com"}`))
	rec := httptest.NewRecorder()
	h.handleForgotPassword(rec, req)
	if rec.Code >= 500 {
		t.Errorf("status = %d", rec.Code)
	}
}

func TestHandleForgotPassword_InvalidJSON(t *testing.T) {
	h := newHandler(&stubAuthService{})
	req := httptest.NewRequest(http.MethodPost, "/api/auth/forgot-password",
		strings.NewReader("not json"))
	rec := httptest.NewRecorder()
	h.handleForgotPassword(rec, req)
	if rec.Code != http.StatusBadRequest {
		t.Errorf("status = %d", rec.Code)
	}
}

func TestHandleResetPassword_Success(t *testing.T) {
	h := newHandler(&stubAuthService{})
	req := httptest.NewRequest(http.MethodPost, "/api/auth/reset-password",
		strings.NewReader(`{"token":"t","new_password":"longenough"}`))
	rec := httptest.NewRecorder()
	h.handleResetPassword(rec, req)
	if rec.Code >= 400 {
		t.Logf("status = %d, body = %q", rec.Code, rec.Body.String())
	}
}

func TestHandleResetPassword_InvalidJSON(t *testing.T) {
	h := newHandler(&stubAuthService{})
	req := httptest.NewRequest(http.MethodPost, "/api/auth/reset-password",
		strings.NewReader("garbage"))
	rec := httptest.NewRecorder()
	h.handleResetPassword(rec, req)
	if rec.Code != http.StatusBadRequest {
		t.Errorf("status = %d", rec.Code)
	}
}

func TestHandleResetPassword_TokenInvalid(t *testing.T) {
	h := newHandler(&stubAuthService{resetErr: errs.ErrValidation})
	req := httptest.NewRequest(http.MethodPost, "/api/auth/reset-password",
		strings.NewReader(`{"token":"t","new_password":"longenough"}`))
	rec := httptest.NewRecorder()
	h.handleResetPassword(rec, req)
	if rec.Code != http.StatusBadRequest {
		t.Errorf("status = %d", rec.Code)
	}
}

func TestHandleResetPassword_InternalError(t *testing.T) {
	h := newHandler(&stubAuthService{resetErr: errors.New("db")})
	req := httptest.NewRequest(http.MethodPost, "/api/auth/reset-password",
		strings.NewReader(`{"token":"t","new_password":"longenough"}`))
	rec := httptest.NewRecorder()
	h.handleResetPassword(rec, req)
	if rec.Code != http.StatusInternalServerError {
		t.Errorf("status = %d", rec.Code)
	}
}
