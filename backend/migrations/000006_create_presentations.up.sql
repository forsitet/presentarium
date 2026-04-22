-- Presentations are PowerPoint files uploaded by organizers and converted
-- into per-slide WebP images for live display during a session.
--
-- Source .pptx lives in the private object-storage bucket (retrieved via
-- presigned URL); slide images and thumbnails live in the public bucket.

CREATE TABLE presentations (
    id                UUID         PRIMARY KEY DEFAULT uuid_generate_v4(),
    user_id           UUID         NOT NULL REFERENCES users (id) ON DELETE CASCADE,
    title             VARCHAR(255) NOT NULL,
    original_filename VARCHAR(255) NOT NULL DEFAULT '',
    source_key        VARCHAR(512) NOT NULL DEFAULT '',
    slide_count       INTEGER      NOT NULL DEFAULT 0 CHECK (slide_count >= 0),
    status            VARCHAR(20)  NOT NULL DEFAULT 'processing'
                           CHECK (status IN ('processing', 'ready', 'failed')),
    error_message     TEXT         NOT NULL DEFAULT '',
    created_at        TIMESTAMPTZ  NOT NULL DEFAULT NOW(),
    updated_at        TIMESTAMPTZ  NOT NULL DEFAULT NOW()
);

CREATE INDEX idx_presentations_user_id    ON presentations (user_id);
CREATE INDEX idx_presentations_created_at ON presentations (created_at DESC);

-- One row per converted slide. image_key/thumb_key are S3 object keys; the
-- full URL is built at read time from Storage.PublicURL() so the CDN/host
-- can be swapped without a data migration.
CREATE TABLE presentation_slides (
    id              UUID         PRIMARY KEY DEFAULT uuid_generate_v4(),
    presentation_id UUID         NOT NULL REFERENCES presentations (id) ON DELETE CASCADE,
    position        INTEGER      NOT NULL CHECK (position > 0),
    image_key       VARCHAR(512) NOT NULL,
    thumb_key       VARCHAR(512) NOT NULL DEFAULT '',
    width           INTEGER      NOT NULL DEFAULT 0 CHECK (width >= 0),
    height          INTEGER      NOT NULL DEFAULT 0 CHECK (height >= 0),
    notes           TEXT         NOT NULL DEFAULT '',
    created_at      TIMESTAMPTZ  NOT NULL DEFAULT NOW(),

    CONSTRAINT presentation_slides_unique_position UNIQUE (presentation_id, position)
);

CREATE INDEX idx_presentation_slides_presentation_id ON presentation_slides (presentation_id);

-- Attach the currently-open presentation (if any) to a live session so state
-- survives server restarts. ON DELETE SET NULL: deleting a presentation does
-- not destroy historical session records.
ALTER TABLE sessions
    ADD COLUMN active_presentation_id UUID
        REFERENCES presentations (id) ON DELETE SET NULL,
    ADD COLUMN current_slide_position INTEGER
        CHECK (current_slide_position IS NULL OR current_slide_position > 0);

CREATE INDEX idx_sessions_active_presentation_id
    ON sessions (active_presentation_id)
    WHERE active_presentation_id IS NOT NULL;
