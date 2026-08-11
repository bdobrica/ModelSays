# Model Says — Backend TODO

Status note:

- Checked items are implemented in the current codebase.
- Some nearby items are only partially implemented and remain unchecked.
- The controlled simultaneous MVP lifecycle is complete and CI-gated. Unchecked items are known limitations or post-MVP roadmap work, not prerequisites for the current playable scope.
- Current scope changes:
  - local development still falls back to an in-process static model client when `OPENAI_API_KEY` is not set;
  - host-only authorization is currently implemented for start, reveal, next-round, and override actions, not every future host action;
  - the round state machine enforces the answer deadline and durably auto-reveals expired PostgreSQL rounds; next-round progression intentionally remains manual for host review;
  - the public room projection hides boards, guesses, outcomes, awarded points, and current-round score changes until reveal while preserving submission progress.
  - authoritative matching is deterministic Unicode-aware normalization plus exact canonical/alias equality; optional semantic judging is advisory, post-miss, host-only after reveal, and never scores automatically.
  - model content and judging use strict schemas, provider-independent validation, one retry, and safe deterministic fallbacks; provider calls have private room-scoped audits and shared cost controls.
  - room creation enforces narrow settings and bounded JSON; public endpoints now add bounded IP/room/player/action limits, explicit proxy trust, English whole-word moderation, and paid-provider circuit breakers.
  - same-browser refresh validates the stored token through the room session endpoint; heartbeat/presence and cross-device transfer remain deferred.
  - CI now gates formatting, vetting, backend/client tests, the production client build, and a PostgreSQL-backed full API lifecycle that uses only curated content.
  - a local PostgreSQL/HTTP baseline measures representative workloads; bounded operational telemetry, readiness, drain, and recovery tooling are implemented, while environment-specific load/query-plan evidence remains future hardening.

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
- [x] Create migration for teams and player assignments.
- [x] Create migration for games.
- [x] Add the backward-compatible `game_kind` foundation and keep it orthogonal to pacing modes.
- [x] Create migration for questions.
- [x] Create migration for prediction_boards.
- [x] Create migration for prediction_answers.
- [x] Create migration for rounds.
- [x] Persist validated, versioned trivia solutions and integrity hashes on rounds.
- [x] Add reviewed five-round Open/Choice Trivia banks and atomic `trivia-v1` provider generation with validated fallback.
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
- [x] Implement authenticated same-browser session recovery.
- [ ] Implement player heartbeat.
- [x] Implement basic room state response.
- [x] Implement host-only authorization checks for every supported MVP host mutation.

## Phase 3 — Real-Time Gateway

- [x] Select SSE for one-way room invalidations and document the decision.
- [x] Add an authenticated, origin-checked SSE endpoint.
- [x] Persist ordered room revisions and a bounded replay log.
- [x] Publish content-free join, start, submission-progress, reveal, score, round, and completion invalidations.
- [x] Add heartbeat, process connection limits, slow-consumer write deadlines, and graceful shutdown.
- [x] Consume events in the browser with reconnect and polling fallback (FUTURE-02B).
- [ ] Add player presence/heartbeat semantics separately from transport heartbeat.

## Phase 4 — Game Engine

- [x] Define game settings.
- [x] Implement simultaneous mode.
- [x] Implement sequential mode with persisted join-order turns, pass/timeout advancement, and separate projections.
- [x] Implement isolated living-room mode with a non-playing display, frozen participant quorum, persisted reveal pause, and durable automatic advancement/completion.
- [x] Implement game creation from a room.
- [x] Implement round creation.
- [x] Implement current round state machine.
- [x] Enforce the answer phase deadline when guesses are submitted.
- [x] Automatically reveal after the answer deadline with durable multi-worker claims; progression remains host-controlled.
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
- [x] Randomize the curated question order for each new game.
- [ ] Add prompt versioning.

## Phase 6 — Prediction Board Generation

- [x] Define board-generation prompt.
- [x] Require structured JSON output.
- [x] Validate generated question and board shape.
- [x] Normalize scores.
- [x] Normalize answer ranks.
- [x] Require canonical answers.
- [x] Require aliases.
- [x] Support explicitly enabled, bounded and redacted raw model response storage (disabled by default).
- [x] Store provider, model, and prompt version accurately.
- [x] Store token/cost audit metadata.
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
- [x] Add tests for advisory semantic matching, failures, secrecy, persistence, and host review.

## Phase 8 — Scoring

- [x] Implement score-per-answer from board.
- [x] Implement duplicate handling policy.
- [x] Implement per-player score.
- [x] Derive per-team score from player score events.
- [x] Implement miss behavior.
- [x] Implement score events.
- [ ] Add scoreboard endpoint.
- [ ] Add final scoreboard response.
- [x] Add tests for canonical, alias, miss, repeat-submission, duplicate, and override scoring cases.

## Phase 9 — Model Provider Abstraction

- [x] Create `ModelClient` interface.
- [x] Implement OpenAI client.
- [x] Add model policy/registry foundation.
- [x] Add allowed prediction models.
- [ ] Add allowed judge models.
- [x] Add per-room selected model settings.
- [x] Add provider timeout handling.
- [x] Add provider retry policy.
- [x] Add private provider cost/audit metadata.
- [ ] Add future extension point for other providers.

## Phase 10 — Admin and Host Controls

- [x] Host can start game.
- [x] Host can reveal round.
- [x] Host can advance round.
- [ ] Host can end game.
- [x] Host can override a match.
- [ ] Host can kick a player.
- [ ] Host can change room settings before game start.
- [x] Host can enable team mode at room creation.
- [x] Host can create and assign teams in the lobby.

## Phase 11 — Safety

- [ ] Add question safety filter.
- [ ] Add stricter team-building safety mode.
- [ ] Add banned topic list.
- [ ] Add model-output validation.
- [x] Add bounded English whole-word moderation for room/display names and answers; broader locale/context moderation remains future work.
- [x] Add bounded rate limiting by hashed IP/room/player/action and paid-provider circuits.
- [ ] Add request size limits.
- [x] Add CORS config.
- [x] Add privacy-safe abuse decision logging and metrics without high-cardinality IP, room, or token labels.

## Phase 12 — Tests

- [ ] Unit tests for room code generation.
- [x] Unit tests for game state transitions.
- [x] Unit tests for scoring.
- [ ] Unit tests for board hash generation.
- [x] Unit tests for duplicate handling.
- [x] Unit tests for host override.
- [x] Integration tests for the supported REST API lifecycle.
- [ ] Integration tests for WebSocket events.
- [x] Integration tests with a deterministic non-network model client.
- [x] Migration tests for atomic answer claims.
- [x] PostgreSQL concurrency and scoreboard reload tests.
- [x] Add repeatable 3-, 8-, and 12-player PostgreSQL gameplay baselines.

## Phase 13 — Developer Experience

- [x] Add `.env.example`.
- [x] Add Docker Compose for PostgreSQL.
- [x] Add Makefile.
- [x] Pin migration tooling and add reproducible bootstrap/verification targets.
- [ ] Add seed command.
- [ ] Add local fake LLM mode.
- [x] Add README examples.
- [ ] Add API examples.
- [x] Add CI workflow.
- [x] Add Go vet static analysis.

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
- [x] Same-browser refresh validates and restores the player session without breaking the game.
- [x] Add privacy-safe completed-game replay retrieval and host-controlled clean play-again lifecycle.
