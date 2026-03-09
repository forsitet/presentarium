CREATE TABLE sessions (
    id             UUID        PRIMARY KEY DEFAULT uuid_generate_v4(),
    poll_id        UUID        NOT NULL REFERENCES polls (id) ON DELETE RESTRICT,
    room_code      VARCHAR(6)  NOT NULL,
    status         VARCHAR(30) NOT NULL DEFAULT 'waiting'
                       CHECK (status IN ('waiting', 'active', 'showing_question',
                                         'showing_results', 'finished')),
    question_order JSONB,
    started_at     TIMESTAMPTZ,
    finished_at    TIMESTAMPTZ,
    created_at     TIMESTAMPTZ NOT NULL DEFAULT NOW(),

    CONSTRAINT sessions_room_code_unique UNIQUE (room_code)
);

CREATE INDEX idx_sessions_poll_id   ON sessions (poll_id);
CREATE INDEX idx_sessions_room_code ON sessions (room_code);
CREATE INDEX idx_sessions_status    ON sessions (status);

-- Active-room uniqueness: only one non-finished room per poll
CREATE UNIQUE INDEX idx_sessions_poll_active
    ON sessions (poll_id)
    WHERE status <> 'finished';

CREATE TABLE participants (
    id            UUID        PRIMARY KEY DEFAULT uuid_generate_v4(),
    session_id    UUID        NOT NULL REFERENCES sessions (id) ON DELETE CASCADE,
    name          VARCHAR(100) NOT NULL,
    session_token UUID        NOT NULL DEFAULT uuid_generate_v4(),
    total_score   INTEGER     NOT NULL DEFAULT 0,
    joined_at     TIMESTAMPTZ NOT NULL DEFAULT NOW(),
    last_seen_at  TIMESTAMPTZ,

    CONSTRAINT participants_session_token_unique UNIQUE (session_token)
);

CREATE INDEX idx_participants_session_id    ON participants (session_id);
CREATE INDEX idx_participants_session_token ON participants (session_token);
