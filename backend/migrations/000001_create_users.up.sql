CREATE EXTENSION IF NOT EXISTS "uuid-ossp";

CREATE TABLE users (
    id            UUID        PRIMARY KEY DEFAULT uuid_generate_v4(),
    email         VARCHAR(255) NOT NULL,
    password_hash TEXT        NOT NULL,
    name          VARCHAR(100) NOT NULL,
    created_at    TIMESTAMPTZ NOT NULL DEFAULT NOW(),
    updated_at    TIMESTAMPTZ NOT NULL DEFAULT NOW(),

    CONSTRAINT users_email_unique UNIQUE (email)
);

CREATE INDEX idx_users_email ON users (email);

-- Refresh tokens for JWT rotation
CREATE TABLE refresh_tokens (
    id         UUID        PRIMARY KEY DEFAULT uuid_generate_v4(),
    user_id    UUID        NOT NULL REFERENCES users (id) ON DELETE CASCADE,
    token      TEXT        NOT NULL,
    expires_at TIMESTAMPTZ NOT NULL,
    created_at TIMESTAMPTZ NOT NULL DEFAULT NOW(),

    CONSTRAINT refresh_tokens_token_unique UNIQUE (token)
);

CREATE INDEX idx_refresh_tokens_user_id  ON refresh_tokens (user_id);
CREATE INDEX idx_refresh_tokens_token    ON refresh_tokens (token);
CREATE INDEX idx_refresh_tokens_expires  ON refresh_tokens (expires_at);
