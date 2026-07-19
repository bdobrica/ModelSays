-- +goose Up
CREATE TABLE judge_suggestions (
    id TEXT PRIMARY KEY,
    room_code TEXT NOT NULL REFERENCES rooms(code) ON DELETE CASCADE,
    game_id TEXT NOT NULL REFERENCES games(id) ON DELETE CASCADE,
    round_id TEXT NOT NULL REFERENCES rounds(id) ON DELETE CASCADE,
    guess_id TEXT NOT NULL UNIQUE REFERENCES guesses(id) ON DELETE CASCADE,
    suggested_prediction_answer_id TEXT NULL REFERENCES prediction_answers(id) ON DELETE SET NULL,
    confidence DOUBLE PRECISION NOT NULL CHECK (confidence >= 0 AND confidence <= 1),
    confidence_band TEXT NOT NULL,
    rationale_category TEXT NOT NULL,
    model_name TEXT NOT NULL,
    prompt_version TEXT NOT NULL,
    outcome TEXT NOT NULL,
    created_at TIMESTAMPTZ NOT NULL,
    reviewed_at TIMESTAMPTZ NULL,
    review_decision TEXT NOT NULL DEFAULT ''
);

CREATE INDEX judge_suggestions_room_round_idx
    ON judge_suggestions (room_code, round_id, created_at);

-- +goose Down
DROP TABLE IF EXISTS judge_suggestions;
