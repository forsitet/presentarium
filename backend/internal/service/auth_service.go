package service

import (
	"context"
	"errors"
	"fmt"
	"time"

	"github.com/golang-jwt/jwt/v5"
	"github.com/google/uuid"
	"golang.org/x/crypto/bcrypt"

	"presentarium/internal/errs"
	"presentarium/internal/model"
	"presentarium/internal/repository"
	"presentarium/pkg/email"
)

// TokenPair holds an access token and a refresh token string.
type TokenPair struct {
	AccessToken  string
	RefreshToken string
}

// AuthService defines the business logic for authentication.
type AuthService interface {
	Register(ctx context.Context, email, password, name string) (*TokenPair, *model.User, error)
	Login(ctx context.Context, email, password string) (*TokenPair, *model.User, error)
	Refresh(ctx context.Context, refreshToken string) (*TokenPair, error)
	Logout(ctx context.Context, refreshToken string) error
	ForgotPassword(ctx context.Context, userEmail, appBaseURL string) error
	ResetPassword(ctx context.Context, token, newPassword string) error
}

type authClaims struct {
	jwt.RegisteredClaims
	UserID string `json:"uid"`
}

type authService struct {
	userRepo        repository.UserRepository
	jwtSecret       []byte
	accessTokenTTL  time.Duration
	refreshTokenTTL time.Duration
	emailSender     *email.Sender
}

// NewAuthService creates a new AuthService.
func NewAuthService(
	userRepo repository.UserRepository,
	jwtSecret string,
	accessTokenTTLMin int,
	refreshTokenTTLDays int,
	emailSender *email.Sender,
) AuthService {
	return &authService{
		userRepo:        userRepo,
		jwtSecret:       []byte(jwtSecret),
		accessTokenTTL:  time.Duration(accessTokenTTLMin) * time.Minute,
		refreshTokenTTL: time.Duration(refreshTokenTTLDays) * 24 * time.Hour,
		emailSender:     emailSender,
	}
}

func (s *authService) Register(ctx context.Context, email, password, name string) (*TokenPair, *model.User, error) {
	hash, err := bcrypt.GenerateFromPassword([]byte(password), 12)
	if err != nil {
		return nil, nil, err
	}

	now := time.Now().UTC()
	user := &model.User{
		ID:           uuid.New(),
		Email:        email,
		PasswordHash: string(hash),
		Name:         name,
		CreatedAt:    now,
		UpdatedAt:    now,
	}

	if err := s.userRepo.CreateUser(ctx, user); err != nil {
		return nil, nil, err
	}

	pair, err := s.issueTokenPair(ctx, user.ID)
	if err != nil {
		return nil, nil, err
	}

	return pair, user, nil
}

func (s *authService) Login(ctx context.Context, email, password string) (*TokenPair, *model.User, error) {
	user, err := s.userRepo.GetUserByEmail(ctx, email)
	if err != nil {
		if errors.Is(err, errs.ErrNotFound) {
			return nil, nil, errs.ErrUnauthorized
		}
		return nil, nil, err
	}

	if err := bcrypt.CompareHashAndPassword([]byte(user.PasswordHash), []byte(password)); err != nil {
		return nil, nil, errs.ErrUnauthorized
	}

	pair, err := s.issueTokenPair(ctx, user.ID)
	if err != nil {
		return nil, nil, err
	}

	return pair, user, nil
}

func (s *authService) Refresh(ctx context.Context, refreshToken string) (*TokenPair, error) {
	rt, err := s.userRepo.GetRefreshToken(ctx, refreshToken)
	if err != nil {
		if errors.Is(err, errs.ErrNotFound) {
			return nil, errs.ErrUnauthorized
		}
		return nil, err
	}

	if time.Now().UTC().After(rt.ExpiresAt) {
		_ = s.userRepo.DeleteRefreshToken(ctx, refreshToken)
		return nil, errs.ErrUnauthorized
	}

	// Rotate: delete old token, issue new pair
	if err := s.userRepo.DeleteRefreshToken(ctx, refreshToken); err != nil {
		return nil, err
	}

	pair, err := s.issueTokenPair(ctx, rt.UserID)
	if err != nil {
		return nil, err
	}

	return pair, nil
}

func (s *authService) Logout(ctx context.Context, refreshToken string) error {
	if refreshToken == "" {
		return nil
	}
	return s.userRepo.DeleteRefreshToken(ctx, refreshToken)
}

// ForgotPassword generates a reset token and sends an email. Always returns nil to avoid
// leaking whether an account exists for the given email address.
func (s *authService) ForgotPassword(ctx context.Context, userEmail, appBaseURL string) error {
	user, err := s.userRepo.GetUserByEmail(ctx, userEmail)
	if err != nil {
		// Don't reveal whether the email exists — silently succeed.
		return nil
	}

	now := time.Now().UTC()
	prt := &model.PasswordResetToken{
		ID:        uuid.New(),
		UserID:    user.ID,
		Token:     uuid.NewString(),
		ExpiresAt: now.Add(time.Hour),
		CreatedAt: now,
	}
	if err := s.userRepo.CreatePasswordResetToken(ctx, prt); err != nil {
		return err
	}

	resetLink := fmt.Sprintf("%s/reset-password?token=%s", appBaseURL, prt.Token)
	body := "Вы запросили сброс пароля для вашего аккаунта.\n\n" +
		"Перейдите по ссылке для сброса пароля (ссылка действительна 1 час):\n\n" +
		resetLink + "\n\n" +
		"Если вы не запрашивали сброс пароля, просто проигнорируйте это письмо."

	// Send email (ignore error — SMTP may not be configured in dev).
	_ = s.emailSender.Send(userEmail, "Сброс пароля — Presentarium", body)
	return nil
}

// ResetPassword validates the reset token and updates the user's password.
func (s *authService) ResetPassword(ctx context.Context, token, newPassword string) error {
	prt, err := s.userRepo.GetPasswordResetToken(ctx, token)
	if err != nil {
		return errs.ErrNotFound
	}

	if prt.UsedAt != nil {
		return errs.ErrValidation
	}
	if time.Now().UTC().After(prt.ExpiresAt) {
		return errs.ErrValidation
	}

	hash, err := bcrypt.GenerateFromPassword([]byte(newPassword), 12)
	if err != nil {
		return err
	}

	now := time.Now().UTC()
	if err := s.userRepo.MarkPasswordResetTokenUsed(ctx, token, now); err != nil {
		if errors.Is(err, errs.ErrNotFound) {
			return errs.ErrValidation // race condition — already used
		}
		return err
	}

	return s.userRepo.UpdateUserPassword(ctx, prt.UserID, string(hash))
}

// issueTokenPair generates a new JWT access token and persists a new refresh token.
func (s *authService) issueTokenPair(ctx context.Context, userID uuid.UUID) (*TokenPair, error) {
	now := time.Now().UTC()

	// Access token
	claims := authClaims{
		RegisteredClaims: jwt.RegisteredClaims{
			Subject:   userID.String(),
			IssuedAt:  jwt.NewNumericDate(now),
			ExpiresAt: jwt.NewNumericDate(now.Add(s.accessTokenTTL)),
		},
		UserID: userID.String(),
	}
	token := jwt.NewWithClaims(jwt.SigningMethodHS256, claims)
	accessToken, err := token.SignedString(s.jwtSecret)
	if err != nil {
		return nil, err
	}

	// Refresh token — opaque UUID stored in DB
	rawRefresh := uuid.NewString()
	rt := &model.RefreshToken{
		ID:        uuid.New(),
		UserID:    userID,
		Token:     rawRefresh,
		ExpiresAt: now.Add(s.refreshTokenTTL),
		CreatedAt: now,
	}
	if err := s.userRepo.CreateRefreshToken(ctx, rt); err != nil {
		return nil, err
	}

	return &TokenPair{
		AccessToken:  accessToken,
		RefreshToken: rawRefresh,
	}, nil
}

// ValidateAccessToken parses and validates a JWT access token, returning the user ID.
func ValidateAccessToken(tokenStr string, jwtSecret string) (uuid.UUID, error) {
	token, err := jwt.ParseWithClaims(tokenStr, &authClaims{}, func(t *jwt.Token) (interface{}, error) {
		if _, ok := t.Method.(*jwt.SigningMethodHMAC); !ok {
			return nil, errs.ErrUnauthorized
		}
		return []byte(jwtSecret), nil
	})
	if err != nil || !token.Valid {
		return uuid.Nil, errs.ErrUnauthorized
	}

	claims, ok := token.Claims.(*authClaims)
	if !ok {
		return uuid.Nil, errs.ErrUnauthorized
	}

	id, err := uuid.Parse(claims.UserID)
	if err != nil {
		return uuid.Nil, errs.ErrUnauthorized
	}
	return id, nil
}
