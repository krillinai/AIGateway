-- +goose Up
ALTER TABLE model_proxy_audit_records
    ADD COLUMN IF NOT EXISTS agent_id TEXT NOT NULL DEFAULT '',
    ADD COLUMN IF NOT EXISTS session_id TEXT NOT NULL DEFAULT '',
    ADD COLUMN IF NOT EXISTS turn_id TEXT NOT NULL DEFAULT '',
    ADD COLUMN IF NOT EXISTS activity_content JSONB NULL;
CREATE INDEX IF NOT EXISTS model_proxy_activity_created_idx ON model_proxy_audit_records (created_at DESC, audit_id DESC)
    WHERE activity_content IS NOT NULL;

-- +goose Down
-- 回退管理台时保留采集字段及审计数据。
SELECT 1;
