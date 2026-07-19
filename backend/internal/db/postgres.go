package db

import (
	"context"
	"crypto/rand"
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

func (repository *PostgresRoomRepository) RevealDueRounds(ctx context.Context, dueAt, occurredAt time.Time, limit int) (int, error) {
	if limit < 1 || limit > 100 {
		limit = 25
	}
	tx, err := repository.pool.BeginTx(ctx, pgx.TxOptions{})
	if err != nil {
		return 0, fmt.Errorf("begin due round transition tx: %w", err)
	}
	defer tx.Rollback(ctx)

	rows, err := tx.Query(ctx, `
		SELECT rooms.code, games.id, rounds.id
		FROM rounds
		INNER JOIN games ON games.id = rounds.game_id
		INNER JOIN rooms ON rooms.code = games.room_code
		WHERE rounds.status = $1 AND games.status = $2
		  AND rounds.answer_phase_ends_at <= $3
		ORDER BY rounds.answer_phase_ends_at, rounds.id
		FOR UPDATE OF rounds SKIP LOCKED
		LIMIT $4
	`, models.RoundStatusAnswering, models.GameStatusInProgress, dueAt, limit)
	if err != nil {
		return 0, fmt.Errorf("claim due rounds: %w", err)
	}
	type dueRound struct{ code, gameID, roundID string }
	var due []dueRound
	for rows.Next() {
		var item dueRound
		if err := rows.Scan(&item.code, &item.gameID, &item.roundID); err != nil {
			rows.Close()
			return 0, fmt.Errorf("scan due round: %w", err)
		}
		due = append(due, item)
	}
	if err := rows.Err(); err != nil {
		rows.Close()
		return 0, fmt.Errorf("iterate due rounds: %w", err)
	}
	rows.Close()

	for _, item := range due {
		tag, err := tx.Exec(ctx, `UPDATE rounds SET status = $2, reveal_started_at = $3 WHERE id = $1 AND status = $4`,
			item.roundID, models.RoundStatusRevealed, occurredAt, models.RoundStatusAnswering)
		if err != nil {
			return 0, fmt.Errorf("automatically reveal round: %w", err)
		}
		if tag.RowsAffected() != 1 {
			continue
		}
		transition := models.RoundTransition{
			ID: newEventID(), RoomCode: item.code, GameID: item.gameID, RoundID: item.roundID,
			Action: "reveal", Actor: models.RoundTransitionActorScheduler,
			Reason: "answer_deadline_elapsed", CreatedAt: occurredAt,
		}
		if err := insertRoundTransition(ctx, tx, transition); err != nil {
			return 0, err
		}
		if _, err := appendRoomEventTx(ctx, tx, item.code, models.RoomEventRoundRevealed, occurredAt); err != nil {
			return 0, err
		}
	}
	if err := tx.Commit(ctx); err != nil {
		return 0, fmt.Errorf("commit due round transitions: %w", err)
	}
	return len(due), nil
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

func (repository *PostgresRoomRepository) Ready(ctx context.Context) error {
	checkCtx, cancel := context.WithTimeout(ctx, 2*time.Second)
	defer cancel()
	if err := repository.pool.Ping(checkCtx); err != nil {
		return fmt.Errorf("database ping: %w", err)
	}
	var version int64
	var dirty bool
	if err := repository.pool.QueryRow(checkCtx, `SELECT version_id, is_applied = false FROM goose_db_version ORDER BY id DESC LIMIT 1`).Scan(&version, &dirty); err != nil {
		return fmt.Errorf("migration compatibility: %w", err)
	}
	if dirty || version != 11 {
		return fmt.Errorf("migration compatibility: database version %d dirty=%t, want 11 clean", version, dirty)
	}
	return nil
}

func (repository *PostgresRoomRepository) PoolStats() (acquired, idle, max int32) {
	stats := repository.pool.Stat()
	return stats.AcquiredConns(), stats.IdleConns(), stats.MaxConns()
}

func (repository *PostgresRoomRepository) ActiveRoomCount(ctx context.Context) (int, error) {
	var count int
	err := repository.pool.QueryRow(ctx, `SELECT COUNT(*) FROM rooms WHERE status IN ($1, $2)`, models.RoomStatusLobby, models.RoomStatusInGame).Scan(&count)
	return count, err
}

func (repository *PostgresRoomRepository) AppendRoomEvent(ctx context.Context, code string, eventType models.RoomEventType, occurredAt time.Time) (models.RoomEvent, error) {
	tx, err := repository.pool.BeginTx(ctx, pgx.TxOptions{})
	if err != nil {
		return models.RoomEvent{}, fmt.Errorf("begin append room event tx: %w", err)
	}
	defer tx.Rollback(ctx)
	var revision int64
	if err := tx.QueryRow(ctx, `UPDATE rooms SET revision = revision + 1 WHERE code = $1 RETURNING revision`, code).Scan(&revision); err != nil {
		if errors.Is(err, pgx.ErrNoRows) {
			return models.RoomEvent{}, game.ErrRoomNotFound
		}
		return models.RoomEvent{}, fmt.Errorf("increment room revision: %w", err)
	}
	event := models.RoomEvent{
		Version: models.RoomEventVersion, ID: newEventID(), RoomCode: code, Type: eventType,
		RoomRevision: revision, OccurredAt: occurredAt,
	}
	if _, err := tx.Exec(ctx, `INSERT INTO room_events (id, room_code, event_type, room_revision, occurred_at) VALUES ($1,$2,$3,$4,$5)`,
		event.ID, event.RoomCode, event.Type, event.RoomRevision, event.OccurredAt); err != nil {
		return models.RoomEvent{}, fmt.Errorf("insert room event: %w", err)
	}
	if _, err := tx.Exec(ctx, `DELETE FROM room_events WHERE room_code = $1 AND room_revision <= $2`, code, revision-1000); err != nil {
		return models.RoomEvent{}, fmt.Errorf("prune room events: %w", err)
	}
	if err := tx.Commit(ctx); err != nil {
		return models.RoomEvent{}, fmt.Errorf("commit room event: %w", err)
	}
	return event, nil
}

func (repository *PostgresRoomRepository) ListRoomEvents(ctx context.Context, code string, afterRevision int64, limit int) ([]models.RoomEvent, error) {
	if limit < 1 || limit > 100 {
		limit = 100
	}
	rows, err := repository.pool.Query(ctx, `
		SELECT id, room_code, event_type, room_revision, occurred_at
		FROM room_events WHERE room_code = $1 AND room_revision > $2
		ORDER BY room_revision LIMIT $3
	`, code, afterRevision, limit)
	if err != nil {
		return nil, fmt.Errorf("list room events: %w", err)
	}
	defer rows.Close()
	events := make([]models.RoomEvent, 0)
	for rows.Next() {
		event := models.RoomEvent{Version: models.RoomEventVersion}
		if err := rows.Scan(&event.ID, &event.RoomCode, &event.Type, &event.RoomRevision, &event.OccurredAt); err != nil {
			return nil, fmt.Errorf("scan room event: %w", err)
		}
		events = append(events, event)
	}
	return events, rows.Err()
}

func newEventID() string {
	var random [16]byte
	if _, err := rand.Read(random[:]); err != nil {
		return fmt.Sprintf("evt_%d", time.Now().UnixNano())
	}
	return fmt.Sprintf("evt_%x", random)
}

func (repository *PostgresRoomRepository) ListProviderAudits(ctx context.Context, code string) ([]models.ProviderCallAudit, error) {
	rows, err := repository.pool.Query(ctx, `
		SELECT id, room_code, game_id, COALESCE(round_id, ''), purpose, provider, model_name,
		       prompt_version, provider_request_id, outcome, latency_ms, input_tokens,
		       output_tokens, estimated_cost_usd, attempt, call_path, error_category,
		       raw_response, retention_class, started_at, completed_at
		FROM provider_call_audits WHERE room_code = $1 ORDER BY started_at, id
	`, code)
	if err != nil {
		return nil, fmt.Errorf("list provider audits: %w", err)
	}
	defer rows.Close()
	audits := make([]models.ProviderCallAudit, 0)
	for rows.Next() {
		var audit models.ProviderCallAudit
		if err := rows.Scan(&audit.ID, &audit.RoomCode, &audit.GameID, &audit.RoundID, &audit.Purpose,
			&audit.Provider, &audit.Model, &audit.PromptVersion, &audit.RequestID, &audit.Outcome,
			&audit.LatencyMillis, &audit.InputTokens, &audit.OutputTokens, &audit.EstimatedCostUSD,
			&audit.Attempt, &audit.Path, &audit.ErrorCategory, &audit.RawResponse,
			&audit.RetentionClass, &audit.StartedAt, &audit.CompletedAt); err != nil {
			return nil, fmt.Errorf("scan provider audit: %w", err)
		}
		audits = append(audits, audit)
	}
	return audits, rows.Err()
}

type auditExecer interface {
	Exec(context.Context, string, ...any) (pgconn.CommandTag, error)
}

func insertProviderAudits(ctx context.Context, executor auditExecer, audits []models.ProviderCallAudit) error {
	for _, audit := range audits {
		_, err := executor.Exec(ctx, `
			INSERT INTO provider_call_audits (
				id, room_code, game_id, round_id, purpose, provider, model_name, prompt_version,
				provider_request_id, outcome, latency_ms, input_tokens, output_tokens,
				estimated_cost_usd, attempt, call_path, error_category, raw_response,
				retention_class, started_at, completed_at
			) VALUES ($1,$2,$3,NULLIF($4,''),$5,$6,$7,$8,$9,$10,$11,$12,$13,$14,$15,$16,$17,$18,$19,$20,$21)
		`, audit.ID, audit.RoomCode, audit.GameID, audit.RoundID, audit.Purpose, audit.Provider,
			audit.Model, audit.PromptVersion, audit.RequestID, audit.Outcome, audit.LatencyMillis,
			audit.InputTokens, audit.OutputTokens, audit.EstimatedCostUSD, audit.Attempt, audit.Path,
			audit.ErrorCategory, audit.RawResponse, audit.RetentionClass, audit.StartedAt, audit.CompletedAt)
		if err != nil {
			return fmt.Errorf("insert provider audit: %w", err)
		}
	}
	return nil
}

func (repository *PostgresRoomRepository) StoreJudgeEvaluation(ctx context.Context, suggestion models.JudgeSuggestion, audits []models.ProviderCallAudit) error {
	tx, err := repository.pool.BeginTx(ctx, pgx.TxOptions{})
	if err != nil {
		return fmt.Errorf("begin store judge evaluation tx: %w", err)
	}
	defer tx.Rollback(ctx)
	if err := insertProviderAudits(ctx, tx, audits); err != nil {
		return err
	}
	_, err = tx.Exec(ctx, `
		INSERT INTO judge_suggestions (
			id, room_code, game_id, round_id, guess_id, suggested_prediction_answer_id,
			confidence, confidence_band, rationale_category, model_name, prompt_version,
			outcome, created_at, reviewed_at, review_decision
		) VALUES ($1,$2,$3,$4,$5,$6,$7,$8,$9,$10,$11,$12,$13,$14,$15)
	`, suggestion.ID, suggestion.RoomCode, suggestion.GameID, suggestion.RoundID, suggestion.GuessID,
		suggestion.SuggestedPredictionAnswerID, suggestion.Confidence, suggestion.ConfidenceBand,
		suggestion.RationaleCategory, suggestion.Model, suggestion.PromptVersion, suggestion.Outcome,
		suggestion.CreatedAt, suggestion.ReviewedAt, suggestion.ReviewDecision)
	if err != nil {
		return fmt.Errorf("insert judge suggestion: %w", err)
	}
	if err := tx.Commit(ctx); err != nil {
		return fmt.Errorf("commit judge evaluation: %w", err)
	}
	return nil
}

func (repository *PostgresRoomRepository) ListJudgeSuggestions(ctx context.Context, code string, roundID string) ([]models.JudgeSuggestion, error) {
	rows, err := repository.pool.Query(ctx, `
		SELECT id, room_code, game_id, round_id, guess_id, suggested_prediction_answer_id,
		       confidence, confidence_band, rationale_category, model_name, prompt_version,
		       outcome, created_at, reviewed_at, review_decision
		FROM judge_suggestions
		WHERE room_code = $1 AND round_id = $2
		ORDER BY created_at, id
	`, code, roundID)
	if err != nil {
		return nil, fmt.Errorf("list judge suggestions: %w", err)
	}
	defer rows.Close()
	result := make([]models.JudgeSuggestion, 0)
	for rows.Next() {
		var suggestion models.JudgeSuggestion
		if err := rows.Scan(
			&suggestion.ID, &suggestion.RoomCode, &suggestion.GameID, &suggestion.RoundID,
			&suggestion.GuessID, &suggestion.SuggestedPredictionAnswerID, &suggestion.Confidence,
			&suggestion.ConfidenceBand, &suggestion.RationaleCategory, &suggestion.Model,
			&suggestion.PromptVersion, &suggestion.Outcome, &suggestion.CreatedAt,
			&suggestion.ReviewedAt, &suggestion.ReviewDecision,
		); err != nil {
			return nil, fmt.Errorf("scan judge suggestion: %w", err)
		}
		result = append(result, suggestion)
	}
	return result, rows.Err()
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

func (repository *PostgresRoomRepository) GetRoomByReplayID(ctx context.Context, replayID string) (models.Room, error) {
	var code string
	err := repository.pool.QueryRow(ctx, `SELECT room_code FROM games WHERE replay_id = $1`, replayID).Scan(&code)
	if errors.Is(err, pgx.ErrNoRows) {
		return models.Room{}, game.ErrReplayNotFound
	}
	if err != nil {
		return models.Room{}, fmt.Errorf("query replay room: %w", err)
	}
	return repository.loadRoom(ctx, repository.pool, code)
}

func (repository *PostgresRoomRepository) ListGameRounds(ctx context.Context, gameID string) ([]models.Round, error) {
	rows, err := repository.pool.Query(ctx, `
		SELECT r.id, r.round_index, r.status, r.board_id, r.answer_phase_started_at,
		       r.answer_phase_ends_at, r.reveal_started_at, r.created_at,
		       q.id, q.text, q.locale, q.category, q.created_at
		FROM rounds r
		INNER JOIN questions q ON q.id = r.question_id
		WHERE r.game_id = $1
		ORDER BY r.round_index
	`, gameID)
	if err != nil {
		return nil, fmt.Errorf("query game rounds: %w", err)
	}
	defer rows.Close()
	result := make([]models.Round, 0)
	for rows.Next() {
		var round models.Round
		var boardID string
		if err := rows.Scan(&round.ID, &round.RoundIndex, &round.Status, &boardID,
			&round.AnswerPhaseStartedAt, &round.AnswerPhaseEndsAt, &round.RevealStartedAt, &round.CreatedAt,
			&round.Question.ID, &round.Question.Text, &round.Question.Locale, &round.Question.Category, &round.Question.CreatedAt); err != nil {
			return nil, fmt.Errorf("scan game round: %w", err)
		}
		round.Board, err = repository.loadBoard(ctx, repository.pool, boardID)
		if err != nil {
			return nil, err
		}
		round.BoardHash = valueOrBoardHash(round.Board)
		round.Guesses, err = repository.loadGuesses(ctx, repository.pool, round.ID)
		if err != nil {
			return nil, err
		}
		result = append(result, round)
	}
	return result, rows.Err()
}

func (repository *PostgresRoomRepository) AddPlayer(ctx context.Context, code string, player models.Player) (models.Room, error) {
	tx, err := repository.pool.BeginTx(ctx, pgx.TxOptions{})
	if err != nil {
		return models.Room{}, fmt.Errorf("begin add player tx: %w", err)
	}
	defer tx.Rollback(ctx)

	var roomCode string
	var roomStatus models.RoomStatus
	if err := tx.QueryRow(ctx, `SELECT code, status FROM rooms WHERE code = $1 FOR UPDATE`, code).Scan(&roomCode, &roomStatus); err != nil {
		if errors.Is(err, pgx.ErrNoRows) {
			return models.Room{}, game.ErrRoomNotFound
		}

		return models.Room{}, fmt.Errorf("lock room: %w", err)
	}
	if roomStatus != models.RoomStatusLobby {
		return models.Room{}, game.ErrRoomJoinClosed
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

func (repository *PostgresRoomRepository) CreateTeam(ctx context.Context, code string, team models.Team) (models.Room, error) {
	tx, err := repository.pool.BeginTx(ctx, pgx.TxOptions{})
	if err != nil {
		return models.Room{}, fmt.Errorf("begin create team tx: %w", err)
	}
	defer tx.Rollback(ctx)
	var status models.RoomStatus
	if err := tx.QueryRow(ctx, `SELECT status FROM rooms WHERE code=$1 FOR UPDATE`, code).Scan(&status); err != nil {
		if errors.Is(err, pgx.ErrNoRows) {
			return models.Room{}, game.ErrRoomNotFound
		}
		return models.Room{}, fmt.Errorf("lock room for team creation: %w", err)
	}
	if status != models.RoomStatusLobby {
		return models.Room{}, game.ErrGameAlreadyStarted
	}
	var count int
	if err := tx.QueryRow(ctx, `SELECT COUNT(*) FROM teams WHERE room_code=$1`, code).Scan(&count); err != nil {
		return models.Room{}, err
	}
	if count >= 4 {
		return models.Room{}, game.ErrTeamConfigurationInvalid
	}
	_, err = tx.Exec(ctx, `INSERT INTO teams (id,room_code,name,name_normalized,created_at) VALUES ($1,$2,$3,$4,$5)`,
		team.ID, code, team.Name, strings.ToLower(strings.TrimSpace(team.Name)), time.Now().UTC())
	if err != nil {
		if isUniqueViolation(err) {
			return models.Room{}, game.ErrTeamConfigurationInvalid
		}
		return models.Room{}, fmt.Errorf("insert team: %w", err)
	}
	room, err := repository.loadRoom(ctx, tx, code)
	if err != nil {
		return models.Room{}, err
	}
	if err := tx.Commit(ctx); err != nil {
		return models.Room{}, fmt.Errorf("commit create team: %w", err)
	}
	return room, nil
}

func (repository *PostgresRoomRepository) AssignPlayerTeam(ctx context.Context, code, playerID, teamID string) (models.Room, error) {
	tx, err := repository.pool.BeginTx(ctx, pgx.TxOptions{})
	if err != nil {
		return models.Room{}, fmt.Errorf("begin assign team tx: %w", err)
	}
	defer tx.Rollback(ctx)
	var status models.RoomStatus
	if err := tx.QueryRow(ctx, `SELECT status FROM rooms WHERE code=$1 FOR UPDATE`, code).Scan(&status); err != nil {
		if errors.Is(err, pgx.ErrNoRows) {
			return models.Room{}, game.ErrRoomNotFound
		}
		return models.Room{}, err
	}
	if status != models.RoomStatusLobby {
		return models.Room{}, game.ErrGameAlreadyStarted
	}
	tag, err := tx.Exec(ctx, `UPDATE players SET team_id=$3 WHERE room_code=$1 AND id=$2 AND EXISTS (SELECT 1 FROM teams WHERE id=$3 AND room_code=$1)`, code, playerID, teamID)
	if err != nil {
		return models.Room{}, fmt.Errorf("assign player team: %w", err)
	}
	if tag.RowsAffected() == 0 {
		return models.Room{}, game.ErrTeamNotFound
	}
	room, err := repository.loadRoom(ctx, tx, code)
	if err != nil {
		return models.Room{}, err
	}
	if err := tx.Commit(ctx); err != nil {
		return models.Room{}, fmt.Errorf("commit assign team: %w", err)
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
		INSERT INTO games (id, room_code, replay_id, status, mode, total_rounds, current_round_index, created_at, started_at)
		VALUES ($1, $2, $3, $4, $5, $6, $7, $8, $9)
	`, gameState.ID, code, gameState.ReplayID, gameState.Status, gameState.Mode, gameState.TotalRounds, gameState.CurrentRoundIndex, gameState.CreatedAt, gameState.StartedAt)
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
	if err := insertProviderAudits(ctx, tx, gameState.CurrentRound.ProviderAudits); err != nil {
		return models.Room{}, err
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

func (repository *PostgresRoomRepository) SubmitGuess(ctx context.Context, code string, roundID string, submission game.GuessSubmission, clock game.Clock) (models.Room, error) {
	tx, err := repository.pool.BeginTx(ctx, pgx.TxOptions{})
	if err != nil {
		return models.Room{}, fmt.Errorf("begin submit guess tx: %w", err)
	}
	defer tx.Rollback(ctx)

	var lockedRoundID string
	var roundStatus models.RoundStatus
	var boardID string
	var gameID string
	var answerPhaseEndsAt time.Time
	err = tx.QueryRow(ctx, `
		-- lock round for guess submission
		SELECT r.id, r.status, r.board_id, g.id, r.answer_phase_ends_at
		FROM rounds r
		INNER JOIN games g ON g.id = r.game_id
		WHERE g.room_code = $1 AND r.id = $2
		FOR UPDATE
	`, code, roundID).Scan(&lockedRoundID, &roundStatus, &boardID, &gameID, &answerPhaseEndsAt)
	if err != nil {
		if errors.Is(err, pgx.ErrNoRows) {
			return models.Room{}, game.ErrRoundNotFound
		}

		return models.Room{}, fmt.Errorf("lock round for guess submission: %w", err)
	}

	if roundStatus != models.RoundStatusAnswering {
		return models.Room{}, game.ErrRoundNotAcceptingGuesses
	}
	if !clock.Now().Before(answerPhaseEndsAt) {
		return models.Room{}, game.ErrAnswerPhaseExpired
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

func (repository *PostgresRoomRepository) RevealRound(ctx context.Context, code string, roundID string, transition models.RoundTransition) (models.Room, error) {
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
	`, code, roundID, models.RoundStatusRevealed, transition.CreatedAt, models.RoundStatusAnswering)
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
	if err := insertRoundTransition(ctx, tx, transition); err != nil {
		return models.Room{}, err
	}
	if _, err := appendRoomEventTx(ctx, tx, code, models.RoomEventRoundRevealed, transition.CreatedAt); err != nil {
		return models.Room{}, err
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

func insertRoundTransition(ctx context.Context, tx pgx.Tx, transition models.RoundTransition) error {
	_, err := tx.Exec(ctx, `
		INSERT INTO round_transitions (id, room_code, game_id, round_id, action, actor, reason, created_at)
		VALUES ($1,$2,$3,$4,$5,$6,$7,$8)
	`, transition.ID, transition.RoomCode, transition.GameID, transition.RoundID, transition.Action,
		transition.Actor, transition.Reason, transition.CreatedAt)
	if err != nil {
		return fmt.Errorf("insert round transition: %w", err)
	}
	return nil
}

func appendRoomEventTx(ctx context.Context, tx pgx.Tx, code string, eventType models.RoomEventType, occurredAt time.Time) (models.RoomEvent, error) {
	var revision int64
	if err := tx.QueryRow(ctx, `UPDATE rooms SET revision = revision + 1, updated_at = $2 WHERE code = $1 RETURNING revision`, code, occurredAt).Scan(&revision); err != nil {
		return models.RoomEvent{}, fmt.Errorf("increment room revision: %w", err)
	}
	event := models.RoomEvent{Version: models.RoomEventVersion, ID: newEventID(), RoomCode: code,
		Type: eventType, RoomRevision: revision, OccurredAt: occurredAt}
	if _, err := tx.Exec(ctx, `INSERT INTO room_events (id, room_code, event_type, room_revision, occurred_at) VALUES ($1,$2,$3,$4,$5)`,
		event.ID, event.RoomCode, event.Type, event.RoomRevision, event.OccurredAt); err != nil {
		return models.RoomEvent{}, fmt.Errorf("insert room event: %w", err)
	}
	if _, err := tx.Exec(ctx, `DELETE FROM room_events WHERE room_code = $1 AND room_revision <= $2`, code, revision-1000); err != nil {
		return models.RoomEvent{}, fmt.Errorf("prune room events: %w", err)
	}
	return event, nil
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
		if err := insertProviderAudits(ctx, tx, nextRound.ProviderAudits); err != nil {
			return models.Room{}, err
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
	if override.JudgeSuggestionID != "" {
		commandTag, err := tx.Exec(ctx, `
			UPDATE judge_suggestions
			SET reviewed_at = $4, review_decision = $5
			WHERE id = $1 AND room_code = $2 AND guess_id = $3
		`, override.JudgeSuggestionID, code, override.GuessID, override.CreatedAt, override.ReviewDecision)
		if err != nil {
			return models.Room{}, fmt.Errorf("mark judge suggestion reviewed: %w", err)
		}
		if commandTag.RowsAffected() == 0 {
			return models.Room{}, game.ErrGuessNotFound
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
		SELECT code, name, status, settings_jsonb, created_at, updated_at, revision
		FROM rooms
		WHERE code = $1
	`, code).Scan(&room.Code, &room.Name, &room.Status, &settingsJSON, &room.CreatedAt, &room.UpdatedAt, &room.Revision)
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
		SELECT id, display_name, is_host, token, joined_at, COALESCE(team_id, '')
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
		if err := rows.Scan(&player.ID, &player.DisplayName, &player.IsHost, &player.Token, &player.JoinedAt, &player.TeamID); err != nil {
			return models.Room{}, fmt.Errorf("scan player: %w", err)
		}
		players = append(players, player)
	}

	if err := rows.Err(); err != nil {
		return models.Room{}, fmt.Errorf("iterate players: %w", err)
	}

	room.Players = players
	teamRows, err := querier.Query(ctx, `SELECT id,name FROM teams WHERE room_code=$1 ORDER BY created_at,id`, code)
	if err != nil {
		return models.Room{}, fmt.Errorf("query room teams: %w", err)
	}
	for teamRows.Next() {
		var team models.Team
		if err := teamRows.Scan(&team.ID, &team.Name); err != nil {
			teamRows.Close()
			return models.Room{}, err
		}
		room.Teams = append(room.Teams, team)
	}
	if err := teamRows.Err(); err != nil {
		teamRows.Close()
		return models.Room{}, err
	}
	teamRows.Close()
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
		room.CurrentGame.TeamScoreboard = deriveTeamScoreboard(room.Teams, room.Players, room.CurrentGame.Scoreboard)
	}

	return room, nil
}

func deriveTeamScoreboard(teams []models.Team, players []models.Player, scores []models.ScoreboardEntry) []models.TeamScoreboardEntry {
	playerTeams := make(map[string]string, len(players))
	for _, player := range players {
		playerTeams[player.ID] = player.TeamID
	}
	totals := make(map[string]int, len(teams))
	for _, score := range scores {
		totals[playerTeams[score.PlayerID]] += score.Score
	}
	result := make([]models.TeamScoreboardEntry, 0, len(teams))
	for _, team := range teams {
		result = append(result, models.TeamScoreboardEntry{TeamID: team.ID, Name: team.Name, Score: totals[team.ID]})
	}
	return result
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
			COALESCE(g.replay_id, ''),
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
		&gameState.ReplayID,
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
