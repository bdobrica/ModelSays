-- +goose Up
ALTER TABLE rounds
    ADD COLUMN IF NOT EXISTS turn_order_jsonb JSONB NOT NULL DEFAULT '[]'::jsonb,
    ADD COLUMN IF NOT EXISTS current_turn_index INTEGER,
    ADD COLUMN IF NOT EXISTS turn_ends_at TIMESTAMPTZ;

ALTER TABLE round_transitions DROP CONSTRAINT IF EXISTS round_transitions_action_check;
ALTER TABLE round_transitions DROP CONSTRAINT IF EXISTS round_transitions_actor_check;
ALTER TABLE round_transitions DROP CONSTRAINT IF EXISTS round_transitions_actor_reason_check;
ALTER TABLE round_transitions DROP CONSTRAINT IF EXISTS round_transitions_reason_check;
ALTER TABLE round_transitions DROP CONSTRAINT IF EXISTS round_transitions_check;
ALTER TABLE round_transitions DROP CONSTRAINT IF EXISTS round_transitions_round_id_action_key;
ALTER TABLE round_transitions
    ADD COLUMN IF NOT EXISTS turn_index INTEGER,
    ADD CONSTRAINT round_transitions_action_check CHECK (action IN ('reveal', 'advance_turn')),
    ADD CONSTRAINT round_transitions_actor_check CHECK (actor IN ('host', 'scheduler', 'player')),
    ADD CONSTRAINT round_transitions_actor_reason_check CHECK (
        (actor = 'host' AND reason = 'host_reveal') OR
        (actor = 'scheduler' AND reason IN ('answer_deadline_elapsed', 'turn_deadline_elapsed')) OR
        (actor = 'player' AND reason IN ('answer_submitted', 'player_passed'))
    );
CREATE UNIQUE INDEX IF NOT EXISTS round_transitions_reveal_once_idx
    ON round_transitions(round_id) WHERE action = 'reveal';
CREATE UNIQUE INDEX IF NOT EXISTS round_transitions_turn_once_idx
    ON round_transitions(round_id, turn_index) WHERE action = 'advance_turn';
CREATE INDEX IF NOT EXISTS rounds_due_turn_idx
    ON rounds(turn_ends_at)
    WHERE status = 'answering' AND current_turn_index IS NOT NULL;

-- +goose Down
DROP INDEX IF EXISTS rounds_due_turn_idx;
DROP INDEX IF EXISTS round_transitions_turn_once_idx;
DROP INDEX IF EXISTS round_transitions_reveal_once_idx;
ALTER TABLE round_transitions DROP CONSTRAINT IF EXISTS round_transitions_actor_reason_check;
ALTER TABLE round_transitions DROP CONSTRAINT IF EXISTS round_transitions_action_check;
ALTER TABLE round_transitions DROP CONSTRAINT IF EXISTS round_transitions_actor_check;
ALTER TABLE round_transitions DROP COLUMN IF EXISTS turn_index;
ALTER TABLE round_transitions
    ADD CONSTRAINT round_transitions_action_check CHECK (action = 'reveal'),
    ADD CONSTRAINT round_transitions_actor_check CHECK (actor IN ('host', 'scheduler')),
    ADD CONSTRAINT round_transitions_actor_reason_check CHECK (
        (actor = 'host' AND reason = 'host_reveal') OR
        (actor = 'scheduler' AND reason = 'answer_deadline_elapsed')
    ),
    ADD CONSTRAINT round_transitions_round_id_action_key UNIQUE (round_id, action);
ALTER TABLE rounds
    DROP COLUMN IF EXISTS turn_ends_at,
    DROP COLUMN IF EXISTS current_turn_index,
    DROP COLUMN IF EXISTS turn_order_jsonb;
