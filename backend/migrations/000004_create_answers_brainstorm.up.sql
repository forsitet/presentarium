CREATE TABLE answers (
    id               UUID        PRIMARY KEY DEFAULT uuid_generate_v4(),
    participant_id   UUID        NOT NULL REFERENCES participants (id) ON DELETE CASCADE,
    question_id      UUID        NOT NULL REFERENCES questions (id) ON DELETE CASCADE,
    session_id       UUID        NOT NULL REFERENCES sessions (id) ON DELETE CASCADE,
    answer           JSONB,
    is_correct       BOOLEAN,
    score            INTEGER     NOT NULL DEFAULT 0,
    response_time_ms INTEGER     NOT NULL DEFAULT 0,
    is_hidden        BOOLEAN     NOT NULL DEFAULT FALSE,
    answered_at      TIMESTAMPTZ NOT NULL DEFAULT NOW(),

    CONSTRAINT answers_participant_question_unique UNIQUE (participant_id, question_id)
);

CREATE INDEX idx_answers_session_id    ON answers (session_id);
CREATE INDEX idx_answers_question_id   ON answers (question_id);
CREATE INDEX idx_answers_participant_id ON answers (participant_id);

CREATE TABLE brainstorm_ideas (
    id             UUID         PRIMARY KEY DEFAULT uuid_generate_v4(),
    session_id     UUID         NOT NULL REFERENCES sessions (id) ON DELETE CASCADE,
    question_id    UUID         NOT NULL REFERENCES questions (id) ON DELETE CASCADE,
    participant_id UUID         NOT NULL REFERENCES participants (id) ON DELETE CASCADE,
    text           VARCHAR(300) NOT NULL,
    is_hidden      BOOLEAN      NOT NULL DEFAULT FALSE,
    votes_count    INTEGER      NOT NULL DEFAULT 0,
    created_at     TIMESTAMPTZ  NOT NULL DEFAULT NOW()
);

CREATE INDEX idx_brainstorm_ideas_session_question  ON brainstorm_ideas (session_id, question_id);
CREATE INDEX idx_brainstorm_ideas_participant        ON brainstorm_ideas (participant_id);
CREATE INDEX idx_brainstorm_ideas_votes             ON brainstorm_ideas (votes_count DESC, created_at ASC);

CREATE TABLE brainstorm_votes (
    id             UUID        PRIMARY KEY DEFAULT uuid_generate_v4(),
    idea_id        UUID        NOT NULL REFERENCES brainstorm_ideas (id) ON DELETE CASCADE,
    participant_id UUID        NOT NULL REFERENCES participants (id) ON DELETE CASCADE,
    created_at     TIMESTAMPTZ NOT NULL DEFAULT NOW(),

    CONSTRAINT brainstorm_votes_idea_participant_unique UNIQUE (idea_id, participant_id)
);

CREATE INDEX idx_brainstorm_votes_idea_id        ON brainstorm_votes (idea_id);
CREATE INDEX idx_brainstorm_votes_participant_id ON brainstorm_votes (participant_id);

-- Trigger to keep brainstorm_ideas.votes_count in sync
CREATE OR REPLACE FUNCTION update_idea_votes_count()
RETURNS TRIGGER AS $$
BEGIN
    IF TG_OP = 'INSERT' THEN
        UPDATE brainstorm_ideas SET votes_count = votes_count + 1 WHERE id = NEW.idea_id;
    ELSIF TG_OP = 'DELETE' THEN
        UPDATE brainstorm_ideas SET votes_count = GREATEST(0, votes_count - 1) WHERE id = OLD.idea_id;
    END IF;
    RETURN NULL;
END;
$$ LANGUAGE plpgsql;

CREATE TRIGGER trg_brainstorm_votes_count
    AFTER INSERT OR DELETE ON brainstorm_votes
    FOR EACH ROW EXECUTE FUNCTION update_idea_votes_count();
