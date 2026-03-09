package repository

import (
	"context"
	"database/sql"
	"errors"
	"time"

	"github.com/google/uuid"
	"github.com/jmoiron/sqlx"
	"github.com/lib/pq"

	"presentarium/internal/errs"
	"presentarium/internal/model"
)

// UserRepository defines data access for users and refresh tokens.
type UserRepository interface {
	CreateUser(ctx context.Context, user *model.User) error
	GetUserByEmail(ctx context.Context, email string) (*model.User, error)
	GetUserByID(ctx context.Context, id uuid.UUID) (*model.User, error)
	CreateRefreshToken(ctx context.Context, rt *model.RefreshToken) error
	GetRefreshToken(ctx context.Context, token string) (*model.RefreshToken, error)
	DeleteRefreshToken(ctx context.Context, token string) error
	DeleteExpiredRefreshTokens(ctx context.Context, before time.Time) error
}

type postgresUserRepo struct {
	db *sqlx.DB
}

// NewPostgresUserRepo creates a new PostgreSQL-backed UserRepository.
func NewPostgresUserRepo(db *sqlx.DB) UserRepository {
	return &postgresUserRepo{db: db}
}

func (r *postgresUserRepo) CreateUser(ctx context.Context, user *model.User) error {
	query := `INSERT INTO users (id, email, password_hash, name, created_at, updated_at)
              VALUES (:id, :email, :password_hash, :name, :created_at, :updated_at)`
	_, err := r.db.NamedExecContext(ctx, query, user)
	if err != nil {
		var pqErr *pq.Error
		if errors.As(err, &pqErr) && pqErr.Code == "23505" {
			return errs.ErrConflict
		}
		return err
	}
	return nil
}

func (r *postgresUserRepo) GetUserByEmail(ctx context.Context, email string) (*model.User, error) {
	var user model.User
	err := r.db.GetContext(ctx, &user, "SELECT * FROM users WHERE email = $1", email)
	if err != nil {
		if errors.Is(err, sql.ErrNoRows) {
			return nil, errs.ErrNotFound
		}
		return nil, err
	}
	return &user, nil
}

func (r *postgresUserRepo) GetUserByID(ctx context.Context, id uuid.UUID) (*model.User, error) {
	var user model.User
	err := r.db.GetContext(ctx, &user, "SELECT * FROM users WHERE id = $1", id)
	if err != nil {
		if errors.Is(err, sql.ErrNoRows) {
			return nil, errs.ErrNotFound
		}
		return nil, err
	}
	return &user, nil
}

func (r *postgresUserRepo) CreateRefreshToken(ctx context.Context, rt *model.RefreshToken) error {
	query := `INSERT INTO refresh_tokens (id, user_id, token, expires_at, created_at)
              VALUES (:id, :user_id, :token, :expires_at, :created_at)`
	_, err := r.db.NamedExecContext(ctx, query, rt)
	return err
}

func (r *postgresUserRepo) GetRefreshToken(ctx context.Context, token string) (*model.RefreshToken, error) {
	var rt model.RefreshToken
	err := r.db.GetContext(ctx, &rt, "SELECT * FROM refresh_tokens WHERE token = $1", token)
	if err != nil {
		if errors.Is(err, sql.ErrNoRows) {
			return nil, errs.ErrNotFound
		}
		return nil, err
	}
	return &rt, nil
}

func (r *postgresUserRepo) DeleteRefreshToken(ctx context.Context, token string) error {
	_, err := r.db.ExecContext(ctx, "DELETE FROM refresh_tokens WHERE token = $1", token)
	return err
}

func (r *postgresUserRepo) DeleteExpiredRefreshTokens(ctx context.Context, before time.Time) error {
	_, err := r.db.ExecContext(ctx, "DELETE FROM refresh_tokens WHERE expires_at < $1", before)
	return err
}
