-- +goose Up
ALTER TABLE rounds ADD COLUMN IF NOT EXISTS trivia_content_jsonb JSONB NULL;
ALTER TABLE rounds ADD COLUMN IF NOT EXISTS trivia_content_hash TEXT NULL;
ALTER TABLE rounds ADD CONSTRAINT rounds_trivia_content_pair_check CHECK (
    (trivia_content_jsonb IS NULL AND trivia_content_hash IS NULL)
    OR (trivia_content_jsonb IS NOT NULL AND trivia_content_hash IS NOT NULL AND length(trivia_content_hash) = 64)
);

-- +goose Down
ALTER TABLE rounds DROP CONSTRAINT IF EXISTS rounds_trivia_content_pair_check;
ALTER TABLE rounds DROP COLUMN IF EXISTS trivia_content_hash;
ALTER TABLE rounds DROP COLUMN IF EXISTS trivia_content_jsonb;
