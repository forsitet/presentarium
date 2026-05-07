package service_test

import (
	"testing"
	"time"

	"github.com/golang-jwt/jwt/v5"
	"github.com/google/uuid"

	"presentarium/internal/errs"
	"presentarium/internal/service"
)

const testSecret = "test-secret"

// signedToken builds a JWT mirroring the format the service issues internally.
func signedToken(t *testing.T, secret string, method jwt.SigningMethod, key interface{}, userID string, expires time.Time) string {
	t.Helper()
	type claims struct {
		jwt.RegisteredClaims
		UserID string `json:"uid"`
	}
	c := claims{
		RegisteredClaims: jwt.RegisteredClaims{
			Subject:   userID,
			IssuedAt:  jwt.NewNumericDate(time.Now()),
			ExpiresAt: jwt.NewNumericDate(expires),
		},
		UserID: userID,
	}
	tok := jwt.NewWithClaims(method, c)
	s, err := tok.SignedString(key)
	if err != nil {
		t.Fatalf("sign: %v", err)
	}
	return s
}

func TestValidateAccessToken_Valid(t *testing.T) {
	uid := uuid.New()
	tok := signedToken(t, testSecret, jwt.SigningMethodHS256, []byte(testSecret), uid.String(), time.Now().Add(time.Hour))
	got, err := service.ValidateAccessToken(tok, testSecret)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if got != uid {
		t.Errorf("user id = %s, want %s", got, uid)
	}
}

func TestValidateAccessToken_Garbage(t *testing.T) {
	_, err := service.ValidateAccessToken("not-a-jwt", testSecret)
	if err == nil || !errs.IsUnauthorized(err) {
		t.Errorf("expected ErrUnauthorized for garbage, got %v", err)
	}
}

func TestValidateAccessToken_WrongSecret(t *testing.T) {
	uid := uuid.New()
	tok := signedToken(t, testSecret, jwt.SigningMethodHS256, []byte(testSecret), uid.String(), time.Now().Add(time.Hour))
	_, err := service.ValidateAccessToken(tok, "different-secret")
	if err == nil || !errs.IsUnauthorized(err) {
		t.Errorf("expected ErrUnauthorized on signature mismatch, got %v", err)
	}
}

func TestValidateAccessToken_Expired(t *testing.T) {
	uid := uuid.New()
	tok := signedToken(t, testSecret, jwt.SigningMethodHS256, []byte(testSecret), uid.String(), time.Now().Add(-time.Hour))
	_, err := service.ValidateAccessToken(tok, testSecret)
	if err == nil || !errs.IsUnauthorized(err) {
		t.Errorf("expected ErrUnauthorized on expired token, got %v", err)
	}
}

func TestValidateAccessToken_BadUserID(t *testing.T) {
	tok := signedToken(t, testSecret, jwt.SigningMethodHS256, []byte(testSecret), "not-a-uuid", time.Now().Add(time.Hour))
	_, err := service.ValidateAccessToken(tok, testSecret)
	if err == nil || !errs.IsUnauthorized(err) {
		t.Errorf("expected ErrUnauthorized when uid claim is not a UUID, got %v", err)
	}
}

func TestValidateAccessToken_EmptyString(t *testing.T) {
	_, err := service.ValidateAccessToken("", testSecret)
	if err == nil {
		t.Error("expected error for empty token")
	}
}
