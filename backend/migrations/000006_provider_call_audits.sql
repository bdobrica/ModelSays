-- +goose Up
CREATE TABLE provider_call_audits (
    id TEXT PRIMARY KEY,
    room_code TEXT NOT NULL REFERENCES rooms(code) ON DELETE CASCADE,
    game_id TEXT NOT NULL REFERENCES games(id) ON DELETE CASCADE,
    round_id TEXT NULL REFERENCES rounds(id) ON DELETE CASCADE,
    purpose TEXT NOT NULL,
    provider TEXT NOT NULL,
    model_name TEXT NOT NULL,
    prompt_version TEXT NOT NULL,
    provider_request_id TEXT NOT NULL DEFAULT '',
    outcome TEXT NOT NULL,
    latency_ms BIGINT NOT NULL,
    input_tokens INTEGER NOT NULL DEFAULT 0,
    output_tokens INTEGER NOT NULL DEFAULT 0,
    estimated_cost_usd NUMERIC(12,8) NOT NULL DEFAULT 0,
    attempt INTEGER NOT NULL,
    call_path TEXT NOT NULL,
    error_category TEXT NOT NULL DEFAULT '',
    raw_response TEXT NOT NULL DEFAULT '',
    retention_class TEXT NOT NULL,
    started_at TIMESTAMPTZ NOT NULL,
    completed_at TIMESTAMPTZ NOT NULL
);

CREATE INDEX provider_call_audits_room_game_idx
    ON provider_call_audits (room_code, game_id, started_at);

-- +goose Down
DROP TABLE IF EXISTS provider_call_audits;
