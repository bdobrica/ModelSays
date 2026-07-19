-- +goose Up
CREATE TABLE IF NOT EXISTS teams (
    id TEXT PRIMARY KEY,
    room_code TEXT NOT NULL REFERENCES rooms(code) ON DELETE CASCADE,
    name TEXT NOT NULL,
    name_normalized TEXT NOT NULL,
    created_at TIMESTAMPTZ NOT NULL,
    UNIQUE (room_code, name_normalized)
);
CREATE INDEX IF NOT EXISTS teams_room_code_idx ON teams(room_code);
ALTER TABLE players ADD COLUMN IF NOT EXISTS team_id TEXT NULL REFERENCES teams(id);

-- +goose Down
ALTER TABLE players DROP COLUMN IF EXISTS team_id;
DROP TABLE IF EXISTS teams;
