-- +goose Up
CREATE TABLE IF NOT EXISTS content_bank_items (
    id TEXT PRIMARY KEY,
    bank_name TEXT NOT NULL,
    bank_version INTEGER NOT NULL,
    game_kind TEXT NOT NULL CHECK (game_kind IN ('model_says', 'trivia_open', 'trivia_choice')),
    locale TEXT NOT NULL,
    category TEXT NOT NULL,
    question_text TEXT NOT NULL,
    payload_jsonb JSONB NOT NULL,
    review_status TEXT NOT NULL CHECK (review_status IN ('reviewed', 'unreviewed')),
    enabled BOOLEAN NOT NULL DEFAULT TRUE,
    source_sha256 TEXT NOT NULL,
    imported_at TIMESTAMPTZ NOT NULL DEFAULT NOW()
);

CREATE INDEX IF NOT EXISTS content_bank_items_selection_idx
    ON content_bank_items (game_kind, locale, enabled);

-- +goose Down
DROP TABLE IF EXISTS content_bank_items;
