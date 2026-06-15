-- +goose Up
CREATE TABLE IF NOT EXISTS rooms (
    code TEXT PRIMARY KEY,
    name TEXT NOT NULL,
    status TEXT NOT NULL,
    settings_jsonb JSONB NOT NULL,
    created_at TIMESTAMPTZ NOT NULL,
    updated_at TIMESTAMPTZ NOT NULL
);

CREATE TABLE IF NOT EXISTS players (
    id TEXT PRIMARY KEY,
    room_code TEXT NOT NULL REFERENCES rooms(code) ON DELETE CASCADE,
    display_name TEXT NOT NULL,
    display_name_normalized TEXT NOT NULL,
    is_host BOOLEAN NOT NULL DEFAULT FALSE,
    token TEXT NOT NULL,
    joined_at TIMESTAMPTZ NOT NULL,
    created_at TIMESTAMPTZ NOT NULL,
    UNIQUE (room_code, display_name_normalized)
);

CREATE INDEX IF NOT EXISTS players_room_code_idx ON players(room_code);

-- +goose Down
DROP TABLE IF EXISTS players;
DROP TABLE IF EXISTS rooms;
