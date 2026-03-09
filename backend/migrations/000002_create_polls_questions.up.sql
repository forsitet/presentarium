CREATE TABLE polls (
    id             UUID         PRIMARY KEY DEFAULT uuid_generate_v4(),
    user_id        UUID         NOT NULL REFERENCES users (id) ON DELETE CASCADE,
    title          VARCHAR(255) NOT NULL,
    description    TEXT         NOT NULL DEFAULT '',
    scoring_rule   VARCHAR(20)  NOT NULL DEFAULT 'none'
                       CHECK (scoring_rule IN ('none', 'correct_answer', 'speed_bonus')),
    question_order VARCHAR(20)  NOT NULL DEFAULT 'sequential'
                       CHECK (question_order IN ('sequential', 'random')),
    created_at     TIMESTAMPTZ  NOT NULL DEFAULT NOW(),
    updated_at     TIMESTAMPTZ  NOT NULL DEFAULT NOW()
);

CREATE INDEX idx_polls_user_id    ON polls (user_id);
CREATE INDEX idx_polls_created_at ON polls (created_at DESC);

CREATE TABLE questions (
    id                 UUID         PRIMARY KEY DEFAULT uuid_generate_v4(),
    poll_id            UUID         NOT NULL REFERENCES polls (id) ON DELETE CASCADE,
    type               VARCHAR(30)  NOT NULL
                           CHECK (type IN ('single_choice', 'multiple_choice', 'open_text',
                                           'image_choice', 'word_cloud', 'brainstorm')),
    text               VARCHAR(500) NOT NULL,
    options            JSONB,
    time_limit_seconds INTEGER      NOT NULL DEFAULT 30
                           CHECK (time_limit_seconds BETWEEN 5 AND 300),
    points             INTEGER      NOT NULL DEFAULT 100
                           CHECK (points >= 0),
    position           INTEGER      NOT NULL DEFAULT 0,
    created_at         TIMESTAMPTZ  NOT NULL DEFAULT NOW(),
    updated_at         TIMESTAMPTZ  NOT NULL DEFAULT NOW()
);

CREATE INDEX idx_questions_poll_id          ON questions (poll_id);
CREATE INDEX idx_questions_poll_id_position ON questions (poll_id, position);
