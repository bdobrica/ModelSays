-- +goose Up
CREATE TABLE IF NOT EXISTS round_transitions (
    id TEXT PRIMARY KEY,
    room_code TEXT NOT NULL REFERENCES rooms(code) ON DELETE CASCADE,
    game_id TEXT NOT NULL REFERENCES games(id) ON DELETE CASCADE,
    round_id TEXT NOT NULL REFERENCES rounds(id) ON DELETE CASCADE,
    action TEXT NOT NULL CHECK (action = 'reveal'),
    actor TEXT NOT NULL CHECK (actor IN ('host', 'scheduler')),
    reason TEXT NOT NULL CHECK (
        (actor = 'host' AND reason = 'host_reveal') OR
        (actor = 'scheduler' AND reason = 'answer_deadline_elapsed')
    ),
    created_at TIMESTAMPTZ NOT NULL,
    UNIQUE (round_id, action)
);

CREATE INDEX IF NOT EXISTS rounds_due_answering_idx
    ON rounds(answer_phase_ends_at)
    WHERE status = 'answering';

-- +goose Down
DROP INDEX IF EXISTS rounds_due_answering_idx;
DROP TABLE IF EXISTS round_transitions;
