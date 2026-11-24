-- +goose Up
-- Repair databases where the private build used the same migration numbers
-- for different features before the open-source model activity migrations.
ALTER TABLE model_proxy_audit_records
    ADD COLUMN IF NOT EXISTS agent_id TEXT NOT NULL DEFAULT '',
    ADD COLUMN IF NOT EXISTS session_id TEXT NOT NULL DEFAULT '',
    ADD COLUMN IF NOT EXISTS turn_id TEXT NOT NULL DEFAULT '',
    ADD COLUMN IF NOT EXISTS activity_content JSONB NULL,
    ADD COLUMN IF NOT EXISTS client_type TEXT NOT NULL DEFAULT 'unknown',
    ADD COLUMN IF NOT EXISTS session_source TEXT NOT NULL DEFAULT '',
    ADD COLUMN IF NOT EXISTS turn_source TEXT NOT NULL DEFAULT '';

CREATE INDEX IF NOT EXISTS model_proxy_activity_created_idx
    ON model_proxy_audit_records (created_at DESC, audit_id DESC)
    WHERE activity_content IS NOT NULL;

-- +goose Down
-- Keep activity columns and audit data when rolling back.
SELECT 1;
