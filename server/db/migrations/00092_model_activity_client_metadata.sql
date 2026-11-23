-- +goose Up
ALTER TABLE model_proxy_audit_records
    ADD COLUMN IF NOT EXISTS client_type TEXT NOT NULL DEFAULT 'unknown',
    ADD COLUMN IF NOT EXISTS session_source TEXT NOT NULL DEFAULT '',
    ADD COLUMN IF NOT EXISTS turn_source TEXT NOT NULL DEFAULT '';

-- +goose Down
-- 保留模型活动关联数据，回退时不删除审计字段。
SELECT 1;
