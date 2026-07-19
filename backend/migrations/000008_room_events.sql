-- +goose Up
ALTER TABLE rooms ADD COLUMN IF NOT EXISTS revision BIGINT NOT NULL DEFAULT 0;

CREATE TABLE IF NOT EXISTS room_events (
    id TEXT PRIMARY KEY,
    room_code TEXT NOT NULL REFERENCES rooms(code) ON DELETE CASCADE,
    event_type TEXT NOT NULL,
    room_revision BIGINT NOT NULL,
    occurred_at TIMESTAMPTZ NOT NULL,
    UNIQUE (room_code, room_revision)
);

CREATE INDEX IF NOT EXISTS room_events_room_revision_idx
    ON room_events(room_code, room_revision);

-- +goose Down
DROP TABLE IF EXISTS room_events;
ALTER TABLE rooms DROP COLUMN IF EXISTS revision;
