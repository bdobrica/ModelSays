-- +goose Up
UPDATE rooms
SET settings_jsonb = jsonb_set(settings_jsonb, '{gameKind}', '"model_says"'::jsonb, true)
WHERE NOT (settings_jsonb ? 'gameKind');

ALTER TABLE games
    ADD COLUMN game_kind TEXT NOT NULL DEFAULT 'model_says';

ALTER TABLE games
    ADD CONSTRAINT games_game_kind_check
    CHECK (game_kind IN ('model_says', 'trivia_open', 'trivia_choice'));

-- +goose Down
ALTER TABLE games DROP CONSTRAINT games_game_kind_check;
ALTER TABLE games DROP COLUMN game_kind;

UPDATE rooms
SET settings_jsonb = settings_jsonb - 'gameKind';
