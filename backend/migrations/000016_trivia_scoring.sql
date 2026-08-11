-- +goose Up
ALTER TABLE guesses ADD COLUMN IF NOT EXISTS selected_option_id TEXT NULL;
ALTER TABLE guesses ADD COLUMN IF NOT EXISTS correct BOOLEAN NULL;
ALTER TABLE guesses ADD CONSTRAINT guesses_trivia_submission_check CHECK (
    selected_option_id IS NULL
    OR (correct IS NOT NULL AND raw_answer = '' AND length(selected_option_id) BETWEEN 1 AND 80)
);

-- +goose Down
ALTER TABLE guesses DROP CONSTRAINT IF EXISTS guesses_trivia_submission_check;
ALTER TABLE guesses DROP COLUMN IF EXISTS correct;
ALTER TABLE guesses DROP COLUMN IF EXISTS selected_option_id;
