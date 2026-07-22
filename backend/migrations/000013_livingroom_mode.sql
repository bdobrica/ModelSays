-- +goose Up
ALTER TABLE players
    ADD COLUMN IF NOT EXISTS role TEXT NOT NULL DEFAULT 'participant',
    ADD CONSTRAINT players_role_check CHECK (role IN ('participant', 'host_display'));

ALTER TABLE rounds
    ADD COLUMN IF NOT EXISTS reveal_phase_ends_at TIMESTAMPTZ;

ALTER TABLE round_transitions DROP CONSTRAINT IF EXISTS round_transitions_action_check;
ALTER TABLE round_transitions DROP CONSTRAINT IF EXISTS round_transitions_actor_reason_check;
ALTER TABLE round_transitions
    ADD COLUMN IF NOT EXISTS room_revision BIGINT,
    ADD CONSTRAINT round_transitions_action_check CHECK (action IN ('reveal', 'advance_turn', 'start_round', 'complete_game')),
    ADD CONSTRAINT round_transitions_actor_reason_check CHECK (
        (actor = 'host' AND reason = 'host_reveal') OR
        (actor = 'scheduler' AND reason IN (
            'answer_deadline_elapsed', 'turn_deadline_elapsed', 'all_participants_submitted',
            'reveal_pause_elapsed', 'final_reveal_pause_elapsed'
        )) OR
        (actor = 'player' AND reason IN ('answer_submitted', 'player_passed'))
    );

CREATE UNIQUE INDEX IF NOT EXISTS round_transitions_start_once_idx
    ON round_transitions(round_id) WHERE action = 'start_round';
CREATE UNIQUE INDEX IF NOT EXISTS round_transitions_complete_once_idx
    ON round_transitions(game_id) WHERE action = 'complete_game';
CREATE INDEX IF NOT EXISTS rounds_due_reveal_pause_idx
    ON rounds(reveal_phase_ends_at)
    WHERE status = 'revealed' AND reveal_phase_ends_at IS NOT NULL;

-- +goose Down
DROP INDEX IF EXISTS rounds_due_reveal_pause_idx;
DROP INDEX IF EXISTS round_transitions_complete_once_idx;
DROP INDEX IF EXISTS round_transitions_start_once_idx;
ALTER TABLE round_transitions DROP CONSTRAINT IF EXISTS round_transitions_actor_reason_check;
ALTER TABLE round_transitions DROP CONSTRAINT IF EXISTS round_transitions_action_check;
ALTER TABLE round_transitions
    ADD CONSTRAINT round_transitions_action_check CHECK (action IN ('reveal', 'advance_turn')),
    ADD CONSTRAINT round_transitions_actor_reason_check CHECK (
        (actor = 'host' AND reason = 'host_reveal') OR
        (actor = 'scheduler' AND reason IN ('answer_deadline_elapsed', 'turn_deadline_elapsed')) OR
        (actor = 'player' AND reason IN ('answer_submitted', 'player_passed'))
    );
ALTER TABLE rounds DROP COLUMN IF EXISTS reveal_phase_ends_at;
ALTER TABLE round_transitions DROP COLUMN IF EXISTS room_revision;
ALTER TABLE players DROP CONSTRAINT IF EXISTS players_role_check;
ALTER TABLE players DROP COLUMN IF EXISTS role;
