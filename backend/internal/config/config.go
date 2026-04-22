package config

import (
	"fmt"

	"github.com/caarlos0/env/v11"
)

type Config struct {
	// Server
	ServerPort string `env:"SERVER_PORT" envDefault:"8080"`
	ServerHost string `env:"SERVER_HOST" envDefault:"0.0.0.0"`

	// Database
	DBHost     string `env:"DB_HOST" envDefault:"localhost"`
	DBPort     int    `env:"DB_PORT" envDefault:"5432"`
	DBUser     string `env:"DB_USER" envDefault:"postgres"`
	DBPassword string `env:"DB_PASSWORD" envDefault:"postgres"`
	DBName     string `env:"DB_NAME" envDefault:"presentarium"`
	DBSSLMode  string `env:"DB_SSLMODE" envDefault:"disable"`

	// JWT
	JWTSecret          string `env:"JWT_SECRET" envDefault:"change-me-in-production"`
	JWTAccessTokenTTL  int    `env:"JWT_ACCESS_TOKEN_TTL_MIN" envDefault:"15"`
	JWTRefreshTokenTTL int    `env:"JWT_REFRESH_TOKEN_TTL_DAYS" envDefault:"7"`

	// CORS
	CORSAllowedOrigin string `env:"CORS_ALLOWED_ORIGIN" envDefault:"http://localhost:5173"`

	// App public base URL (used in emails)
	AppBaseURL string `env:"APP_BASE_URL" envDefault:"http://localhost:5173"`

	// Uploads
	UploadsDir string `env:"UPLOADS_DIR" envDefault:"./uploads"`

	// Object storage (S3-compatible: MinIO in dev/prod, also works against R2/AWS)
	S3Endpoint       string `env:"S3_ENDPOINT" envDefault:"http://localhost:9000"`
	S3Region         string `env:"S3_REGION" envDefault:"us-east-1"`
	S3AccessKeyID    string `env:"S3_ACCESS_KEY_ID" envDefault:""`
	S3SecretKey      string `env:"S3_SECRET_ACCESS_KEY" envDefault:""`
	S3BucketPublic   string `env:"S3_BUCKET_PUBLIC" envDefault:"presentarium-public"`
	S3BucketPrivate  string `env:"S3_BUCKET_PRIVATE" envDefault:"presentarium-private"`
	S3PublicBaseURL  string `env:"S3_PUBLIC_BASE_URL" envDefault:""`
	S3ForcePathStyle bool   `env:"S3_FORCE_PATH_STYLE" envDefault:"true"`

	// Migrations
	MigrationsPath string `env:"MIGRATIONS_PATH" envDefault:"migrations"`

	// SMTP (optional)
	SMTPHost     string `env:"SMTP_HOST"`
	SMTPPort     int    `env:"SMTP_PORT" envDefault:"587"`
	SMTPUser     string `env:"SMTP_USER"`
	SMTPPassword string `env:"SMTP_PASSWORD"`
	SMTPFrom     string `env:"SMTP_FROM"`
}

func Load() (*Config, error) {
	cfg := &Config{}
	if err := env.Parse(cfg); err != nil {
		return nil, err
	}
	return cfg, nil
}

func (c *Config) DSN() string {
	return fmt.Sprintf(
		"postgres://%s:%s@%s:%d/%s?sslmode=%s",
		c.DBUser, c.DBPassword, c.DBHost, c.DBPort, c.DBName, c.DBSSLMode,
	)
}
