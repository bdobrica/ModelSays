package db

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"strings"
	"time"

	"github.com/bogdandobrica/modelsays/backend/internal/game"
	"github.com/bogdandobrica/modelsays/backend/internal/models"
	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgconn"
	"github.com/jackc/pgx/v5/pgxpool"
)

type queryRower interface {
	Query(context.Context, string, ...any) (pgx.Rows, error)
	QueryRow(context.Context, string, ...any) pgx.Row
}

type PostgresRoomRepository struct {
	pool *pgxpool.Pool
}

func OpenPostgresPool(ctx context.Context, databaseURL string) (*pgxpool.Pool, error) {
	pool, err := pgxpool.New(ctx, databaseURL)
	if err != nil {
		return nil, fmt.Errorf("create postgres pool: %w", err)
	}

	pingCtx, cancel := context.WithTimeout(ctx, 5*time.Second)
	defer cancel()
	if err := pool.Ping(pingCtx); err != nil {
		pool.Close()
		return nil, fmt.Errorf("ping postgres: %w", err)
	}

	return pool, nil
}

func NewPostgresRoomRepository(pool *pgxpool.Pool) *PostgresRoomRepository {
	return &PostgresRoomRepository{pool: pool}
}

func (repository *PostgresRoomRepository) CreateRoom(ctx context.Context, room models.Room) error {
	settingsJSON, err := json.Marshal(room.Settings)
	if err != nil {
		return fmt.Errorf("marshal room settings: %w", err)
	}

	tx, err := repository.pool.BeginTx(ctx, pgx.TxOptions{})
	if err != nil {
		return fmt.Errorf("begin create room tx: %w", err)
	}
	defer tx.Rollback(ctx)

	_, err = tx.Exec(ctx, `
		INSERT INTO rooms (code, name, status, settings_jsonb, created_at, updated_at)
		VALUES ($1, $2, $3, $4, $5, $6)
	`, room.Code, room.Name, room.Status, settingsJSON, room.CreatedAt, room.UpdatedAt)
	if err != nil {
		if isUniqueViolation(err) {
			return game.ErrRoomCodeConflict
		}

		return fmt.Errorf("insert room: %w", err)
	}

	host := room.Players[0]
	_, err = tx.Exec(ctx, `
		INSERT INTO players (id, room_code, display_name, display_name_normalized, is_host, token, joined_at, created_at)
		VALUES ($1, $2, $3, $4, $5, $6, $7, $8)
	`, host.ID, room.Code, host.DisplayName, normalizeDisplayName(host.DisplayName), host.IsHost, host.Token, host.JoinedAt, room.CreatedAt)
	if err != nil {
		return fmt.Errorf("insert host player: %w", err)
	}

	if err := tx.Commit(ctx); err != nil {
		return fmt.Errorf("commit create room tx: %w", err)
	}

	return nil
}

func (repository *PostgresRoomRepository) GetRoom(ctx context.Context, code string) (models.Room, error) {
	return repository.loadRoom(ctx, repository.pool, code)
}

func (repository *PostgresRoomRepository) AddPlayer(ctx context.Context, code string, player models.Player) (models.Room, error) {
	tx, err := repository.pool.BeginTx(ctx, pgx.TxOptions{})
	if err != nil {
		return models.Room{}, fmt.Errorf("begin add player tx: %w", err)
	}
	defer tx.Rollback(ctx)

	var roomCode string
	if err := tx.QueryRow(ctx, `SELECT code FROM rooms WHERE code = $1 FOR UPDATE`, code).Scan(&roomCode); err != nil {
		if errors.Is(err, pgx.ErrNoRows) {
			return models.Room{}, game.ErrRoomNotFound
		}

		return models.Room{}, fmt.Errorf("lock room: %w", err)
	}

	_, err = tx.Exec(ctx, `
		INSERT INTO players (id, room_code, display_name, display_name_normalized, is_host, token, joined_at, created_at)
		VALUES ($1, $2, $3, $4, $5, $6, $7, $8)
	`, player.ID, roomCode, player.DisplayName, normalizeDisplayName(player.DisplayName), player.IsHost, player.Token, player.JoinedAt, player.JoinedAt)
	if err != nil {
		if isUniqueViolation(err) {
			return models.Room{}, game.ErrDuplicatePlayer
		}

		return models.Room{}, fmt.Errorf("insert player: %w", err)
	}

	_, err = tx.Exec(ctx, `UPDATE rooms SET updated_at = $2 WHERE code = $1`, roomCode, time.Now().UTC())
	if err != nil {
		return models.Room{}, fmt.Errorf("touch room update time: %w", err)
	}

	room, err := repository.loadRoom(ctx, tx, roomCode)
	if err != nil {
		return models.Room{}, err
	}

	if err := tx.Commit(ctx); err != nil {
		return models.Room{}, fmt.Errorf("commit add player tx: %w", err)
	}

	return room, nil
}

func (repository *PostgresRoomRepository) StartGame(ctx context.Context, code string, gameState models.Game) (models.Room, error) {
	tx, err := repository.pool.BeginTx(ctx, pgx.TxOptions{})
	if err != nil {
		return models.Room{}, fmt.Errorf("begin start game tx: %w", err)
	}
	defer tx.Rollback(ctx)

	var roomStatus models.RoomStatus
	if err := tx.QueryRow(ctx, `SELECT status FROM rooms WHERE code = $1 FOR UPDATE`, code).Scan(&roomStatus); err != nil {
		if errors.Is(err, pgx.ErrNoRows) {
			return models.Room{}, game.ErrRoomNotFound
		}

		return models.Room{}, fmt.Errorf("lock room for game start: %w", err)
	}

	if roomStatus != models.RoomStatusLobby {
		return models.Room{}, game.ErrGameAlreadyStarted
	}

	question := gameState.CurrentRound.Question
	board := gameState.CurrentRound.Board
	if board == nil {
		return models.Room{}, fmt.Errorf("start game missing prediction board")
	}
	_, err = tx.Exec(ctx, `
		INSERT INTO questions (id, text, locale, category, created_at)
		VALUES ($1, $2, $3, $4, $5)
		ON CONFLICT (id) DO NOTHING
	`, question.ID, question.Text, question.Locale, question.Category, question.CreatedAt)
	if err != nil {
		return models.Room{}, fmt.Errorf("insert question: %w", err)
	}

	_, err = tx.Exec(ctx, `
		INSERT INTO prediction_boards (id, question_id, provider, model_name, prompt_version, board_hash, created_at)
		VALUES ($1, $2, $3, $4, $5, $6, $7)
	`, board.ID, question.ID, board.Provider, board.ModelName, board.PromptVersion, board.BoardHash, board.CreatedAt)
	if err != nil {
		return models.Room{}, fmt.Errorf("insert prediction board: %w", err)
	}

	for _, answer := range board.Answers {
		aliasesJSON, err := json.Marshal(answer.Aliases)
		if err != nil {
			return models.Room{}, fmt.Errorf("marshal prediction answer aliases: %w", err)
		}

		_, err = tx.Exec(ctx, `
			INSERT INTO prediction_answers (id, board_id, canonical_answer, aliases_jsonb, rank, score, created_at)
			VALUES ($1, $2, $3, $4, $5, $6, $7)
		`, answer.ID, board.ID, answer.CanonicalAnswer, aliasesJSON, answer.Rank, answer.Score, answer.CreatedAt)
		if err != nil {
			return models.Room{}, fmt.Errorf("insert prediction answer: %w", err)
		}
	}

	_, err = tx.Exec(ctx, `
		INSERT INTO games (id, room_code, status, mode, total_rounds, current_round_index, created_at, started_at)
		VALUES ($1, $2, $3, $4, $5, $6, $7, $8)
	`, gameState.ID, code, gameState.Status, gameState.Mode, gameState.TotalRounds, gameState.CurrentRoundIndex, gameState.CreatedAt, gameState.StartedAt)
	if err != nil {
		if isUniqueViolation(err) {
			return models.Room{}, game.ErrGameAlreadyStarted
		}

		return models.Room{}, fmt.Errorf("insert game: %w", err)
	}

	_, err = tx.Exec(ctx, `
		INSERT INTO rounds (id, game_id, round_index, question_id, board_id, status, answer_phase_started_at, answer_phase_ends_at, reveal_started_at, created_at)
		VALUES ($1, $2, $3, $4, $5, $6, $7, $8, $9, $10)
	`, gameState.CurrentRound.ID, gameState.ID, gameState.CurrentRound.RoundIndex, question.ID, board.ID, gameState.CurrentRound.Status, gameState.CurrentRound.AnswerPhaseStartedAt, gameState.CurrentRound.AnswerPhaseEndsAt, gameState.CurrentRound.RevealStartedAt, gameState.CurrentRound.CreatedAt)
	if err != nil {
		return models.Room{}, fmt.Errorf("insert round: %w", err)
	}

	_, err = tx.Exec(ctx, `UPDATE rooms SET status = $2, updated_at = $3 WHERE code = $1`, code, models.RoomStatusInGame, time.Now().UTC())
	if err != nil {
		return models.Room{}, fmt.Errorf("update room status for game start: %w", err)
	}

	room, err := repository.loadRoom(ctx, tx, code)
	if err != nil {
		return models.Room{}, err
	}

	if err := tx.Commit(ctx); err != nil {
		return models.Room{}, fmt.Errorf("commit start game tx: %w", err)
	}

	return room, nil
}

func (repository *PostgresRoomRepository) SubmitGuess(ctx context.Context, code string, roundID string, submission game.GuessSubmission) (models.Room, error) {
	tx, err := repository.pool.BeginTx(ctx, pgx.TxOptions{})
	if err != nil {
		return models.Room{}, fmt.Errorf("begin submit guess tx: %w", err)
	}
	defer tx.Rollback(ctx)

	var lockedRoundID string
	var roundStatus models.RoundStatus
	var boardID string
	var gameID string
	err = tx.QueryRow(ctx, `
		SELECT r.id, r.status, r.board_id, g.id
		FROM rounds r
		INNER JOIN games g ON g.id = r.game_id
		WHERE g.room_code = $1 AND r.id = $2
		FOR UPDATE
	`, code, roundID).Scan(&lockedRoundID, &roundStatus, &boardID, &gameID)
	if err != nil {
		if errors.Is(err, pgx.ErrNoRows) {
			return models.Room{}, game.ErrRoundNotFound
		}

		return models.Room{}, fmt.Errorf("lock round for guess submission: %w", err)
	}

	if roundStatus != models.RoundStatusAnswering {
		return models.Room{}, game.ErrRoundNotAcceptingGuesses
	}

	board, err := repository.loadBoard(ctx, tx, boardID)
	if err != nil {
		return models.Room{}, err
	}
	guesses, err := repository.loadGuesses(ctx, tx, lockedRoundID)
	if err != nil {
		return models.Room{}, err
	}
	guess := game.ResolveGuess(submission, board.Answers, guesses)

	_, err = tx.Exec(ctx, `
		INSERT INTO guesses (id, round_id, player_id, raw_answer, normalized_answer, matched_prediction_answer_id, score_awarded, created_at)
		VALUES ($1, $2, $3, $4, $5, $6, $7, $8)
	`, guess.ID, lockedRoundID, guess.PlayerID, guess.RawAnswer, guess.NormalizedAnswer, guess.MatchedPredictionAnswerID, guess.ScoreAwarded, guess.CreatedAt)
	if err != nil {
		if isUniqueViolation(err) {
			return models.Room{}, game.ErrGuessAlreadySubmitted
		}

		return models.Room{}, fmt.Errorf("insert guess: %w", err)
	}

	if guess.ScoreAwarded > 0 {
		_, err = tx.Exec(ctx, `
			INSERT INTO score_events (id, game_id, round_id, player_id, delta, reason, created_at)
			VALUES ($1, $2, $3, $4, $5, $6, $7)
		`, submission.ScoreEventID, gameID, lockedRoundID, submission.PlayerID, guess.ScoreAwarded, "guess_matched_prediction_answer", submission.CreatedAt)
		if err != nil {
			return models.Room{}, fmt.Errorf("insert score event: %w", err)
		}
	}

	room, err := repository.loadRoom(ctx, tx, code)
	if err != nil {
		return models.Room{}, err
	}

	if err := tx.Commit(ctx); err != nil {
		return models.Room{}, fmt.Errorf("commit submit guess tx: %w", err)
	}

	return room, nil
}

func (repository *PostgresRoomRepository) RevealRound(ctx context.Context, code string, roundID string, revealStartedAt time.Time) (models.Room, error) {
	tx, err := repository.pool.BeginTx(ctx, pgx.TxOptions{})
	if err != nil {
		return models.Room{}, fmt.Errorf("begin reveal round tx: %w", err)
	}
	defer tx.Rollback(ctx)

	commandTag, err := tx.Exec(ctx, `
		UPDATE rounds r
		SET status = $3, reveal_started_at = $4
		FROM games g
		WHERE r.game_id = g.id AND g.room_code = $1 AND r.id = $2 AND r.status = $5
	`, code, roundID, models.RoundStatusRevealed, revealStartedAt, models.RoundStatusAnswering)
	if err != nil {
		return models.Room{}, fmt.Errorf("update round reveal state: %w", err)
	}
	if commandTag.RowsAffected() == 0 {
		var existingStatus models.RoundStatus
		if err := tx.QueryRow(ctx, `
			SELECT r.status
			FROM rounds r
			INNER JOIN games g ON g.id = r.game_id
			WHERE g.room_code = $1 AND r.id = $2
		`, code, roundID).Scan(&existingStatus); err != nil {
			if errors.Is(err, pgx.ErrNoRows) {
				return models.Room{}, game.ErrRoundNotFound
			}

			return models.Room{}, fmt.Errorf("query round status before reveal: %w", err)
		}
		if existingStatus == models.RoundStatusRevealed {
			return models.Room{}, game.ErrRoundAlreadyRevealed
		}
		return models.Room{}, game.ErrRoundNotAcceptingGuesses
	}

	room, err := repository.loadRoom(ctx, tx, code)
	if err != nil {
		return models.Room{}, err
	}

	if err := tx.Commit(ctx); err != nil {
		return models.Room{}, fmt.Errorf("commit reveal round tx: %w", err)
	}

	return room, nil
}

func (repository *PostgresRoomRepository) AdvanceGame(ctx context.Context, code string, gameState models.Game, nextRound *models.Round) (models.Room, error) {
	tx, err := repository.pool.BeginTx(ctx, pgx.TxOptions{})
	if err != nil {
		return models.Room{}, fmt.Errorf("begin advance game tx: %w", err)
	}
	defer tx.Rollback(ctx)

	if nextRound != nil && nextRound.Board != nil {
		question := nextRound.Question
		board := nextRound.Board
		_, err = tx.Exec(ctx, `
			INSERT INTO questions (id, text, locale, category, created_at)
			VALUES ($1, $2, $3, $4, $5)
			ON CONFLICT (id) DO NOTHING
		`, question.ID, question.Text, question.Locale, question.Category, question.CreatedAt)
		if err != nil {
			return models.Room{}, fmt.Errorf("insert next-round question: %w", err)
		}

		_, err = tx.Exec(ctx, `
			INSERT INTO prediction_boards (id, question_id, provider, model_name, prompt_version, board_hash, created_at)
			VALUES ($1, $2, $3, $4, $5, $6, $7)
		`, board.ID, question.ID, board.Provider, board.ModelName, board.PromptVersion, board.BoardHash, board.CreatedAt)
		if err != nil {
			return models.Room{}, fmt.Errorf("insert next-round prediction board: %w", err)
		}

		for _, answer := range board.Answers {
			aliasesJSON, err := json.Marshal(answer.Aliases)
			if err != nil {
				return models.Room{}, fmt.Errorf("marshal next-round aliases: %w", err)
			}
			_, err = tx.Exec(ctx, `
				INSERT INTO prediction_answers (id, board_id, canonical_answer, aliases_jsonb, rank, score, created_at)
				VALUES ($1, $2, $3, $4, $5, $6, $7)
			`, answer.ID, board.ID, answer.CanonicalAnswer, aliasesJSON, answer.Rank, answer.Score, answer.CreatedAt)
			if err != nil {
				return models.Room{}, fmt.Errorf("insert next-round prediction answer: %w", err)
			}
		}

		_, err = tx.Exec(ctx, `
			INSERT INTO rounds (id, game_id, round_index, question_id, board_id, status, answer_phase_started_at, answer_phase_ends_at, reveal_started_at, created_at)
			VALUES ($1, $2, $3, $4, $5, $6, $7, $8, $9, $10)
		`, nextRound.ID, gameState.ID, nextRound.RoundIndex, question.ID, board.ID, nextRound.Status, nextRound.AnswerPhaseStartedAt, nextRound.AnswerPhaseEndsAt, nextRound.RevealStartedAt, nextRound.CreatedAt)
		if err != nil {
			return models.Room{}, fmt.Errorf("insert next round: %w", err)
		}
	}

	_, err = tx.Exec(ctx, `
		UPDATE games
		SET status = $2, current_round_index = $3, ended_at = $4
		WHERE room_code = $1
	`, code, gameState.Status, gameState.CurrentRoundIndex, gameState.EndedAt)
	if err != nil {
		return models.Room{}, fmt.Errorf("update game after advance: %w", err)
	}

	_, err = tx.Exec(ctx, `UPDATE rooms SET updated_at = $2 WHERE code = $1`, code, time.Now().UTC())
	if err != nil {
		return models.Room{}, fmt.Errorf("touch room during game advance: %w", err)
	}

	room, err := repository.loadRoom(ctx, tx, code)
	if err != nil {
		return models.Room{}, err
	}

	if err := tx.Commit(ctx); err != nil {
		return models.Room{}, fmt.Errorf("commit advance game tx: %w", err)
	}

	return room, nil
}

func (repository *PostgresRoomRepository) OverrideGuess(ctx context.Context, code string, roundID string, override game.GuessOverride) (models.Room, error) {
	tx, err := repository.pool.BeginTx(ctx, pgx.TxOptions{})
	if err != nil {
		return models.Room{}, fmt.Errorf("begin override guess tx: %w", err)
	}
	defer tx.Rollback(ctx)

	var boardID string
	var gameID string
	var roundStatus models.RoundStatus
	err = tx.QueryRow(ctx, `
		SELECT r.board_id, game.id, r.status
		FROM rounds r
		INNER JOIN games game ON game.id = r.game_id
		WHERE r.id = $1 AND game.room_code = $2
		FOR UPDATE
	`, roundID, code).Scan(&boardID, &gameID, &roundStatus)
	if err != nil {
		if errors.Is(err, pgx.ErrNoRows) {
			return models.Room{}, game.ErrRoundNotFound
		}
		return models.Room{}, fmt.Errorf("lock round for guess override: %w", err)
	}
	if roundStatus != models.RoundStatusRevealed {
		return models.Room{}, game.ErrRoundNotRevealed
	}

	board, err := repository.loadBoard(ctx, tx, boardID)
	if err != nil {
		return models.Room{}, err
	}
	guesses, err := repository.loadGuesses(ctx, tx, roundID)
	if err != nil {
		return models.Room{}, err
	}
	guess, delta, err := game.ResolveOverride(override, board, guesses)
	if err != nil {
		return models.Room{}, err
	}

	commandTag, err := tx.Exec(ctx, `
		UPDATE guesses g
		SET matched_prediction_answer_id = $4, score_awarded = $5
		FROM rounds r
		INNER JOIN games game ON game.id = r.game_id
		WHERE g.id = $1 AND g.round_id = r.id AND r.id = $2 AND game.room_code = $3
	`, guess.ID, roundID, code, guess.MatchedPredictionAnswerID, guess.ScoreAwarded)
	if err != nil {
		return models.Room{}, fmt.Errorf("update overridden guess: %w", err)
	}
	if commandTag.RowsAffected() == 0 {
		return models.Room{}, game.ErrGuessNotFound
	}

	if delta != 0 {
		_, err = tx.Exec(ctx, `
			INSERT INTO score_events (id, game_id, round_id, player_id, delta, reason, created_at)
			VALUES ($1, $2, $3, $4, $5, $6, $7)
		`, override.ScoreEventID, gameID, roundID, guess.PlayerID, delta, "host_override_match", override.CreatedAt)
		if err != nil {
			return models.Room{}, fmt.Errorf("insert override score event: %w", err)
		}
	}

	room, err := repository.loadRoom(ctx, tx, code)
	if err != nil {
		return models.Room{}, err
	}

	if err := tx.Commit(ctx); err != nil {
		return models.Room{}, fmt.Errorf("commit override guess tx: %w", err)
	}

	return room, nil
}

func (repository *PostgresRoomRepository) loadRoom(ctx context.Context, querier queryRower, code string) (models.Room, error) {
	var room models.Room
	var settingsJSON []byte

	err := querier.QueryRow(ctx, `
		SELECT code, name, status, settings_jsonb, created_at, updated_at
		FROM rooms
		WHERE code = $1
	`, code).Scan(&room.Code, &room.Name, &room.Status, &settingsJSON, &room.CreatedAt, &room.UpdatedAt)
	if err != nil {
		if errors.Is(err, pgx.ErrNoRows) {
			return models.Room{}, game.ErrRoomNotFound
		}

		return models.Room{}, fmt.Errorf("query room: %w", err)
	}

	if err := json.Unmarshal(settingsJSON, &room.Settings); err != nil {
		return models.Room{}, fmt.Errorf("decode room settings: %w", err)
	}

	rows, err := querier.Query(ctx, `
		SELECT id, display_name, is_host, token, joined_at
		FROM players
		WHERE room_code = $1
		ORDER BY joined_at ASC, id ASC
	`, code)
	if err != nil {
		return models.Room{}, fmt.Errorf("query room players: %w", err)
	}
	defer rows.Close()

	players := make([]models.Player, 0)
	for rows.Next() {
		var player models.Player
		if err := rows.Scan(&player.ID, &player.DisplayName, &player.IsHost, &player.Token, &player.JoinedAt); err != nil {
			return models.Room{}, fmt.Errorf("scan player: %w", err)
		}
		players = append(players, player)
	}

	if err := rows.Err(); err != nil {
		return models.Room{}, fmt.Errorf("iterate players: %w", err)
	}

	room.Players = players
	room.CurrentGame, err = repository.loadCurrentGame(ctx, querier, code)
	if err != nil {
		return models.Room{}, err
	}
	if room.CurrentGame != nil {
		room.CurrentGame.Scoreboard, err = repository.loadScoreboard(ctx, querier, room.CurrentGame.ID, room.Players)
		if err != nil {
			return models.Room{}, err
		}
		if room.CurrentGame.CurrentRound != nil {
			markSubmittedPlayers(room.CurrentGame.Scoreboard, room.CurrentGame.CurrentRound.Guesses)
		}
	}

	return room, nil
}

func (repository *PostgresRoomRepository) loadCurrentGame(ctx context.Context, querier queryRower, code string) (*models.Game, error) {
	var (
		gameState         models.Game
		endedAt           *time.Time
		currentRoundID    *string
		roundIndex        *int
		roundStatus       *models.RoundStatus
		questionID        *string
		questionText      *string
		questionLocale    *string
		questionCategory  *string
		boardID           *string
		roundStartedAt    *time.Time
		roundEndsAt       *time.Time
		roundCreatedAt    *time.Time
		revealStartedAt   *time.Time
		questionCreatedAt *time.Time
	)

	err := querier.QueryRow(ctx, `
		SELECT
			g.id,
			g.status,
			g.mode,
			g.total_rounds,
			g.current_round_index,
			g.created_at,
			g.started_at,
			g.ended_at,
			r.id,
			r.round_index,
			r.status,
			r.board_id,
			r.answer_phase_started_at,
			r.answer_phase_ends_at,
			r.reveal_started_at,
			r.created_at,
			q.id,
			q.text,
			q.locale,
			q.category,
			q.created_at
		FROM games g
		LEFT JOIN rounds r ON r.game_id = g.id AND r.round_index = g.current_round_index
		LEFT JOIN questions q ON q.id = r.question_id
		WHERE g.room_code = $1
		ORDER BY g.started_at DESC
		LIMIT 1
	`, code).Scan(
		&gameState.ID,
		&gameState.Status,
		&gameState.Mode,
		&gameState.TotalRounds,
		&gameState.CurrentRoundIndex,
		&gameState.CreatedAt,
		&gameState.StartedAt,
		&endedAt,
		&currentRoundID,
		&roundIndex,
		&roundStatus,
		&boardID,
		&roundStartedAt,
		&roundEndsAt,
		&revealStartedAt,
		&roundCreatedAt,
		&questionID,
		&questionText,
		&questionLocale,
		&questionCategory,
		&questionCreatedAt,
	)
	if err != nil {
		if errors.Is(err, pgx.ErrNoRows) {
			return nil, nil
		}

		return nil, fmt.Errorf("query current game: %w", err)
	}
	gameState.EndedAt = endedAt

	if currentRoundID != nil && roundIndex != nil && roundStatus != nil && questionID != nil && questionText != nil && questionLocale != nil && questionCategory != nil && roundStartedAt != nil && roundEndsAt != nil && roundCreatedAt != nil && questionCreatedAt != nil {
		var board *models.PredictionBoard
		if boardID != nil {
			board, err = repository.loadBoard(ctx, querier, *boardID)
			if err != nil {
				return nil, err
			}
		}
		var guesses []models.Guess
		guesses, err = repository.loadGuesses(ctx, querier, *currentRoundID)
		if err != nil {
			return nil, err
		}

		gameState.CurrentRound = &models.Round{
			ID:         *currentRoundID,
			RoundIndex: *roundIndex,
			Status:     *roundStatus,
			Question: models.Question{
				ID:        *questionID,
				Text:      *questionText,
				Locale:    *questionLocale,
				Category:  *questionCategory,
				CreatedAt: *questionCreatedAt,
			},
			BoardHash:            valueOrBoardHash(board),
			Board:                board,
			Guesses:              guesses,
			AnswerPhaseStartedAt: *roundStartedAt,
			AnswerPhaseEndsAt:    *roundEndsAt,
			RevealStartedAt:      revealStartedAt,
			CreatedAt:            *roundCreatedAt,
		}
	}

	return &gameState, nil
}

func (repository *PostgresRoomRepository) loadBoard(ctx context.Context, querier queryRower, boardID string) (*models.PredictionBoard, error) {
	var board models.PredictionBoard
	err := querier.QueryRow(ctx, `
		SELECT id, provider, model_name, prompt_version, board_hash, created_at
		FROM prediction_boards
		WHERE id = $1
	`, boardID).Scan(&board.ID, &board.Provider, &board.ModelName, &board.PromptVersion, &board.BoardHash, &board.CreatedAt)
	if err != nil {
		if errors.Is(err, pgx.ErrNoRows) {
			return nil, nil
		}

		return nil, fmt.Errorf("query prediction board: %w", err)
	}

	rows, err := querier.Query(ctx, `
		SELECT id, canonical_answer, aliases_jsonb, rank, score, created_at
		FROM prediction_answers
		WHERE board_id = $1
		ORDER BY rank ASC
	`, boardID)
	if err != nil {
		return nil, fmt.Errorf("query prediction answers: %w", err)
	}
	defer rows.Close()

	answers := make([]models.PredictionAnswer, 0)
	for rows.Next() {
		var answer models.PredictionAnswer
		var aliasesJSON []byte
		if err := rows.Scan(&answer.ID, &answer.CanonicalAnswer, &aliasesJSON, &answer.Rank, &answer.Score, &answer.CreatedAt); err != nil {
			return nil, fmt.Errorf("scan prediction answer: %w", err)
		}
		if err := json.Unmarshal(aliasesJSON, &answer.Aliases); err != nil {
			return nil, fmt.Errorf("decode prediction answer aliases: %w", err)
		}
		answers = append(answers, answer)
	}
	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("iterate prediction answers: %w", err)
	}

	board.Answers = answers
	return &board, nil
}

func (repository *PostgresRoomRepository) loadGuesses(ctx context.Context, querier queryRower, roundID string) ([]models.Guess, error) {
	rows, err := querier.Query(ctx, `
		SELECT g.id, g.player_id, p.display_name, g.raw_answer, g.normalized_answer, g.matched_prediction_answer_id, g.score_awarded, g.created_at
		FROM guesses g
		INNER JOIN players p ON p.id = g.player_id
		WHERE g.round_id = $1
		ORDER BY g.created_at ASC, g.id ASC
	`, roundID)
	if err != nil {
		return nil, fmt.Errorf("query guesses: %w", err)
	}
	defer rows.Close()

	guesses := make([]models.Guess, 0)
	for rows.Next() {
		var guess models.Guess
		if err := rows.Scan(&guess.ID, &guess.PlayerID, &guess.PlayerDisplayName, &guess.RawAnswer, &guess.NormalizedAnswer, &guess.MatchedPredictionAnswerID, &guess.ScoreAwarded, &guess.CreatedAt); err != nil {
			return nil, fmt.Errorf("scan guess: %w", err)
		}
		if guess.MatchedPredictionAnswerID != nil && guess.ScoreAwarded == 0 {
			guess.Duplicate = true
		}
		guesses = append(guesses, guess)
	}
	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("iterate guesses: %w", err)
	}

	return guesses, nil
}

func (repository *PostgresRoomRepository) loadScoreboard(ctx context.Context, querier queryRower, gameID string, players []models.Player) ([]models.ScoreboardEntry, error) {
	rows, err := querier.Query(ctx, `
		SELECT player_id, COALESCE(SUM(delta), 0) AS score
		FROM score_events
		WHERE game_id = $1
		GROUP BY player_id
	`, gameID)
	if err != nil {
		return nil, fmt.Errorf("query scoreboard: %w", err)
	}
	defer rows.Close()

	scores := make(map[string]int, len(players))
	for rows.Next() {
		var playerID string
		var score int
		if err := rows.Scan(&playerID, &score); err != nil {
			return nil, fmt.Errorf("scan scoreboard row: %w", err)
		}
		scores[playerID] = score
	}
	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("iterate scoreboard rows: %w", err)
	}

	entries := make([]models.ScoreboardEntry, 0, len(players))
	for _, player := range players {
		entries = append(entries, models.ScoreboardEntry{
			PlayerID:       player.ID,
			DisplayName:    player.DisplayName,
			Score:          scores[player.ID],
			IsHost:         player.IsHost,
			SubmissionMade: false,
		})
	}

	return entries, nil
}

func valueOrBoardHash(board *models.PredictionBoard) string {
	if board == nil {
		return ""
	}

	return board.BoardHash
}

func markSubmittedPlayers(scoreboard []models.ScoreboardEntry, guesses []models.Guess) {
	submitted := make(map[string]struct{}, len(guesses))
	for _, guess := range guesses {
		submitted[guess.PlayerID] = struct{}{}
	}

	for index := range scoreboard {
		_, ok := submitted[scoreboard[index].PlayerID]
		scoreboard[index].SubmissionMade = ok
	}
}

func isUniqueViolation(err error) bool {
	var pgErr *pgconn.PgError
	return errors.As(err, &pgErr) && pgErr.Code == "23505"
}

func normalizeDisplayName(displayName string) string {
	return strings.ToLower(strings.TrimSpace(displayName))
}
