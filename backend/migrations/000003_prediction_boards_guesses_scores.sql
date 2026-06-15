-- +goose Up
CREATE TABLE IF NOT EXISTS prediction_boards (
    id TEXT PRIMARY KEY,
    question_id TEXT NOT NULL REFERENCES questions(id),
    provider TEXT NOT NULL,
    model_name TEXT NOT NULL,
    prompt_version TEXT NOT NULL,
    board_hash TEXT NOT NULL,
    created_at TIMESTAMPTZ NOT NULL
);

CREATE TABLE IF NOT EXISTS prediction_answers (
    id TEXT PRIMARY KEY,
    board_id TEXT NOT NULL REFERENCES prediction_boards(id) ON DELETE CASCADE,
    canonical_answer TEXT NOT NULL,
    aliases_jsonb JSONB NOT NULL,
    rank INTEGER NOT NULL,
    score INTEGER NOT NULL,
    created_at TIMESTAMPTZ NOT NULL
);

ALTER TABLE rounds ADD COLUMN IF NOT EXISTS board_id TEXT REFERENCES prediction_boards(id);
ALTER TABLE rounds ADD COLUMN IF NOT EXISTS reveal_started_at TIMESTAMPTZ NULL;

CREATE TABLE IF NOT EXISTS guesses (
    id TEXT PRIMARY KEY,
    round_id TEXT NOT NULL REFERENCES rounds(id) ON DELETE CASCADE,
    player_id TEXT NOT NULL REFERENCES players(id) ON DELETE CASCADE,
    raw_answer TEXT NOT NULL,
    normalized_answer TEXT NOT NULL,
    matched_prediction_answer_id TEXT NULL REFERENCES prediction_answers(id),
    score_awarded INTEGER NOT NULL DEFAULT 0,
    created_at TIMESTAMPTZ NOT NULL,
    UNIQUE (round_id, player_id)
);

CREATE INDEX IF NOT EXISTS guesses_round_id_idx ON guesses(round_id);
CREATE INDEX IF NOT EXISTS guesses_player_id_idx ON guesses(player_id);

CREATE TABLE IF NOT EXISTS score_events (
    id TEXT PRIMARY KEY,
    game_id TEXT NOT NULL REFERENCES games(id) ON DELETE CASCADE,
    round_id TEXT NOT NULL REFERENCES rounds(id) ON DELETE CASCADE,
    player_id TEXT NOT NULL REFERENCES players(id) ON DELETE CASCADE,
    delta INTEGER NOT NULL,
    reason TEXT NOT NULL,
    created_at TIMESTAMPTZ NOT NULL
);

CREATE INDEX IF NOT EXISTS score_events_game_id_idx ON score_events(game_id);
CREATE INDEX IF NOT EXISTS score_events_round_id_idx ON score_events(round_id);
CREATE INDEX IF NOT EXISTS score_events_player_id_idx ON score_events(player_id);

-- +goose Down
DROP TABLE IF EXISTS score_events;
DROP TABLE IF EXISTS guesses;
ALTER TABLE rounds DROP COLUMN IF EXISTS reveal_started_at;
ALTER TABLE rounds DROP COLUMN IF EXISTS board_id;
DROP TABLE IF EXISTS prediction_answers;
DROP TABLE IF EXISTS prediction_boards;
