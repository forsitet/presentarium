DROP INDEX IF EXISTS idx_sessions_active_presentation_id;

ALTER TABLE sessions
    DROP COLUMN IF EXISTS current_slide_position,
    DROP COLUMN IF EXISTS active_presentation_id;

DROP TABLE IF EXISTS presentation_slides;
DROP TABLE IF EXISTS presentations;
