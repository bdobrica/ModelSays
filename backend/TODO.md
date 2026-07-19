# Model Says — Backend TODO

Status note:

- Checked items are implemented in the current codebase.
- Some nearby items are only partially implemented and remain unchecked.
- Current scope changes:
  - local development still falls back to an in-process static model client when `OPENAI_API_KEY` is not set;
  - host-only authorization is currently implemented for start, reveal, next-round, and override actions, not every future host action;
  - the round state machine enforces the answer deadline during submission and covers reveal, next-round transition, and final completion, but expiry does not yet trigger automatic reveal or progression;
  - the public room projection hides boards, guesses, outcomes, awarded points, and current-round score changes until reveal while preserving submission progress.
  - MVP matching is deterministic Unicode-aware normalization plus exact canonical/alias equality; semantic judge-model matching is deferred.
  - model content uses strict schemas, provider-independent validation, one retry, and a five-round curated fallback; raw response and cost auditing remain deferred.
  - room creation now enforces the narrow MVP settings and name/code bounds, strict bounded JSON decoding, and lobby-only joins in both repositories; public-launch rate limiting remains deferred.

## Phase 0 — Project Setup

- [x] Create Go module.
- [x] Add basic project layout:
  - [x] `cmd/server`
  - [x] `internal/http`
  - [x] `internal/game`
  - [x] `internal/db`
  - [x] `internal/models`
  - [x] `internal/llm`
  - [x] `migrations`
- [x] Add PostgreSQL driver.
- [x] Choose query approach: sqlc, pgx, or database/sql.
- [x] Add migration tool.
- [x] Add config loader.
- [x] Add structured logging.
- [x] Add graceful shutdown.
- [x] Add health endpoint.

## Phase 1 — Database

- [x] Create migration for rooms.
- [x] Create migration for players.
- [ ] Create migration for teams.
- [x] Create migration for games.
- [x] Create migration for questions.
- [x] Create migration for prediction_boards.
- [x] Create migration for prediction_answers.
- [x] Create migration for rounds.
- [x] Create migration for guesses.
- [x] Create migration for score_events.
- [x] Add indexes for room code, game id, round id, player id.
- [x] Add unique constraint for room code.
- [x] Add unique constraint to prevent duplicate player submissions per round.
- [x] Add a database-enforced unique scoring claim per round and prediction answer.
- [ ] Add seed questions for local development.

## Phase 2 — Room and Lobby API

- [x] Implement `POST /api/rooms`.
- [x] Implement room-code generation.
- [x] Implement `GET /api/rooms/{code}`.
- [x] Implement `POST /api/rooms/{code}/join`.
- [x] Implement display-name validation.
- [x] Reject joins after game start under the room lock.
- [x] Enforce MVP settings and strict bounded JSON request decoding.
- [x] Implement host assignment.
- [x] Implement player reconnect token.
- [ ] Implement player heartbeat.
- [x] Implement basic room state response.
- [ ] Implement host-only authorization checks.

## Phase 3 — Real-Time Gateway

- [ ] Add WebSocket endpoint.
- [ ] Add connection manager.
- [ ] Track room subscriptions.
- [ ] Broadcast room updates.
- [ ] Broadcast player joined/left events.
- [ ] Broadcast game started events.
- [ ] Broadcast round started events.
- [ ] Broadcast guess submitted events without revealing answer content.
- [ ] Broadcast reveal events.
- [ ] Broadcast score updates.
- [ ] Add reconnect behavior.
- [ ] Add ping/pong or heartbeat handling.

## Phase 4 — Game Engine

- [x] Define game settings.
- [x] Implement simultaneous mode.
- [x] Implement game creation from a room.
- [x] Implement round creation.
- [x] Implement current round state machine.
- [x] Enforce the answer phase deadline when guesses are submitted.
- [ ] Automatically reveal or progress after the answer deadline; expiry currently closes submissions and waits for the host.
- [x] Implement reveal phase.
- [x] Implement next-round transition.
- [x] Implement game-ended transition.
- [x] Implement scoreboard calculation.
- [x] Persist score events.

## Phase 5 — Question Generation

- [x] Define question schema.
- [x] Add static development question source.
- [x] Add LLM-generated question source.
- [x] Add locale setting.
- [x] Add category setting.
- [ ] Add team-building safe mode.
- [x] Store generated questions.
- [x] Avoid duplicate questions in a five-round curated MVP game.
- [ ] Add prompt versioning.

## Phase 6 — Prediction Board Generation

- [x] Define board-generation prompt.
- [x] Require structured JSON output.
- [x] Validate generated question and board shape.
- [x] Normalize scores.
- [x] Normalize answer ranks.
- [x] Require canonical answers.
- [x] Require aliases.
- [ ] Store raw model response.
- [x] Store provider, model, and prompt version accurately.
- [ ] Store temperature and token/cost audit metadata.
- [x] Compute board hash.
- [x] Freeze board before accepting guesses.
- [x] Add bounded retry behavior for invalid model output.
- [x] Add fallback to static board if generation fails.

## Phase 7 — Guess Matching

- [x] Define and document deterministic MVP normalization and exact matching.
- [x] Reject canonical/alias phrases owned by multiple board answers after normalization.
- [ ] Define judge prompt.
- [ ] Require structured JSON output.
- [x] Match raw player answer to canonical board answers.
- [ ] Return match confidence.
- [x] Return no-match when appropriate.
- [ ] Mark low-confidence matches for host review.
- [x] Prevent duplicate scoring atomically for submissions and host overrides.
- [ ] Store judge model metadata.
- [x] Add host override endpoint.
- [x] Add tests for deterministic normalization and alias matching.
- [ ] Add tests for future fuzzy/semantic matching.

## Phase 8 — Scoring

- [x] Implement score-per-answer from board.
- [x] Implement duplicate handling policy.
- [x] Implement per-player score.
- [ ] Implement per-team score.
- [x] Implement miss behavior.
- [x] Implement score events.
- [ ] Add scoreboard endpoint.
- [ ] Add final scoreboard response.
- [x] Add tests for canonical, alias, miss, repeat-submission, duplicate, and override scoring cases.

## Phase 9 — Model Provider Abstraction

- [x] Create `ModelClient` interface.
- [x] Implement OpenAI client.
- [ ] Add model registry.
- [ ] Add allowed prediction models.
- [ ] Add allowed judge models.
- [x] Add per-room selected model settings.
- [x] Add provider timeout handling.
- [ ] Add provider retry policy.
- [ ] Add cost/logging metadata.
- [ ] Add future extension point for other providers.

## Phase 10 — Admin and Host Controls

- [x] Host can start game.
- [x] Host can reveal round.
- [x] Host can advance round.
- [ ] Host can end game.
- [x] Host can override a match.
- [ ] Host can kick a player.
- [ ] Host can change room settings before game start.
- [ ] Host can enable team mode.
- [ ] Host can assign teams.

## Phase 11 — Safety

- [ ] Add question safety filter.
- [ ] Add stricter team-building safety mode.
- [ ] Add banned topic list.
- [ ] Add model-output validation.
- [ ] Add profanity or harassment handling for player display names.
- [ ] Add rate limiting by IP/room/player.
- [ ] Add request size limits.
- [x] Add CORS config.
- [ ] Add basic abuse logging.

## Phase 12 — Tests

- [ ] Unit tests for room code generation.
- [x] Unit tests for game state transitions.
- [x] Unit tests for scoring.
- [ ] Unit tests for board hash generation.
- [x] Unit tests for duplicate handling.
- [x] Unit tests for host override.
- [ ] Integration tests for REST API.
- [ ] Integration tests for WebSocket events.
- [ ] Integration tests with mocked LLM client.
- [x] Migration tests for atomic answer claims.
- [x] PostgreSQL concurrency and scoreboard reload tests.

## Phase 13 — Developer Experience

- [x] Add `.env.example`.
- [x] Add Docker Compose for PostgreSQL.
- [x] Add Makefile.
- [x] Pin migration tooling and add reproducible bootstrap/verification targets.
- [ ] Add seed command.
- [ ] Add local fake LLM mode.
- [x] Add README examples.
- [ ] Add API examples.
- [ ] Add CI workflow.
- [ ] Add linting.

## Phase 14 — MVP Acceptance Criteria

- [x] Host creates a room.
- [x] Players join by room code.
- [x] Host starts a simultaneous game.
- [x] Backend creates frozen boards before answers.
- [x] Players submit answers.
- [x] Guesses are matched.
- [x] Scores are persisted.
- [x] Round reveal works.
- [x] Final scoreboard works.
- [ ] Browser refresh/reconnect does not break the game.
