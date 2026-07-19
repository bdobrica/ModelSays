-- +goose Up
CREATE UNIQUE INDEX guesses_one_scoring_claim_per_answer_idx
    ON guesses (round_id, matched_prediction_answer_id)
    WHERE matched_prediction_answer_id IS NOT NULL AND score_awarded > 0;

-- +goose Down
DROP INDEX IF EXISTS guesses_one_scoring_claim_per_answer_idx;
