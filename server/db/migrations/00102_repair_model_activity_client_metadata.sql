-- +goose Up
-- Repair databases whose migration ledger advanced past 00092 without the
-- model activity client metadata columns required by the current API.
ALTER TABLE model_proxy_audit_records
    ADD COLUMN IF NOT EXISTS client_type TEXT NOT NULL DEFAULT 'unknown',
    ADD COLUMN IF NOT EXISTS session_source TEXT NOT NULL DEFAULT '',
    ADD COLUMN IF NOT EXISTS turn_source TEXT NOT NULL DEFAULT '';

-- +goose Down
-- Keep activity metadata when rolling back; the columns are additive repairs.
SELECT 1;
