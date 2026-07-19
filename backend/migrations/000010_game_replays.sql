-- +goose Up
ALTER TABLE games ADD COLUMN replay_id TEXT;
CREATE UNIQUE INDEX games_replay_id_idx ON games(replay_id) WHERE replay_id IS NOT NULL;

-- +goose Down
DROP INDEX IF EXISTS games_replay_id_idx;
ALTER TABLE games DROP COLUMN replay_id;
