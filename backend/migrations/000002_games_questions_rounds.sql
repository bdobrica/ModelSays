-- +goose Up
CREATE TABLE IF NOT EXISTS questions (
    id TEXT PRIMARY KEY,
    text TEXT NOT NULL,
    locale TEXT NOT NULL,
    category TEXT NOT NULL,
    created_at TIMESTAMPTZ NOT NULL
);

CREATE TABLE IF NOT EXISTS games (
    id TEXT PRIMARY KEY,
    room_code TEXT NOT NULL UNIQUE REFERENCES rooms(code) ON DELETE CASCADE,
    status TEXT NOT NULL,
    mode TEXT NOT NULL,
    total_rounds INTEGER NOT NULL,
    current_round_index INTEGER NOT NULL,
    created_at TIMESTAMPTZ NOT NULL,
    started_at TIMESTAMPTZ NOT NULL
);

CREATE TABLE IF NOT EXISTS rounds (
    id TEXT PRIMARY KEY,
    game_id TEXT NOT NULL REFERENCES games(id) ON DELETE CASCADE,
    round_index INTEGER NOT NULL,
    question_id TEXT NOT NULL REFERENCES questions(id),
    status TEXT NOT NULL,
    answer_phase_started_at TIMESTAMPTZ NOT NULL,
    answer_phase_ends_at TIMESTAMPTZ NOT NULL,
    created_at TIMESTAMPTZ NOT NULL,
    UNIQUE (game_id, round_index)
);

CREATE INDEX IF NOT EXISTS rounds_game_id_idx ON rounds(game_id);

-- +goose Down
DROP TABLE IF EXISTS rounds;
DROP TABLE IF EXISTS games;
DROP TABLE IF EXISTS questions;
