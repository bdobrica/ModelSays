# Model Says — Backend

Model Says is a real-time party game where players try to guess what an AI model thinks the most common answers are.

This backend provides the game engine, room management, persistent generated answer boards, scoring, player sessions, and REST API used by the React client.

The key idea is not to simulate a real survey. The game is explicitly about guessing the model’s predicted cultural priors.

## Performance Baseline

From the repository root, `make baseline` runs deterministic 3-, 8-, and 12-player workloads through the real HTTP handler and PostgreSQL repository. It emits versioned JSON containing request/poll volume, response bytes, backend and mutation-visibility latency, database query counts/duration, and the built client size. The harness uses curated content, creates isolated schemas, and makes no provider calls. It is a regression baseline, not production telemetry or a saturation test; interpretation and current budgets are in [`docs/baselines/pb-00.md`](../docs/baselines/pb-00.md).

## Core Concept

For each round:

1. A question is selected; curated play randomly samples from questions not yet used in that game.
2. A chosen prediction model generates an answer board.
3. The board is stored before players submit answers.
4. Players submit answers before the server-enforced deadline.
5. The server matches normalized guesses exactly to canonical answers or configured aliases.
6. Scores are awarded.
7. The board is revealed.
8. The next round begins.

The generated board must be persistent so that all players compete against the same answers and scores.

## Product Name

**Model Says**

Suggested tagline:

> Guess what the AI thinks people would say.

## Backend Responsibilities

The backend owns:

- room creation and joining;
- player sessions;
- game lifecycle;
- question generation;
- prediction-board generation;
- answer matching;
- scoring;
- timers;
- persistence;
- polling-friendly room state;
- model/provider configuration;
- replay/debug metadata.

The frontend should not be trusted for game-state decisions.

## Supported MVP

The backend models game rules and pacing independently. `gameKind` accepts `model_says`, `trivia_open`, or `trivia_choice`; `mode` accepts simultaneous, teams, sequential, or living-room. Missing legacy kinds default to `model_says`. Trivia kinds currently establish only the persisted/API foundation, support simultaneous, teams, and living-room combinations, and reject sequential; Model Says remains the only playable ruleset until the later trivia steps. The backend otherwise supports English games with 1–5 rounds and 15–120 second timers, curated offline content or validated and audited OpenAI generation, PostgreSQL persistence, deterministic canonical/alias scoring, optional advisory semantic judging for misses, first-claim duplicate scoring, phase-aware response secrecy, durable deadline transitions, host early-reveal/review/override/advance controls, room-scoped same-browser session recovery, and layered public API/provider abuse controls. Provider operations are documented in [`docs/provider-operations.md`](../docs/provider-operations.md); deadline-worker behavior is documented in [`docs/deadline-transitions.md`](../docs/deadline-transitions.md); the ruleset matrix is in [ADR-007](../docs/adr/07-orthogonal_game_rulesets.md); the threat model, proxy trust, limiter policies, moderation, and `429` contract are in [`docs/security.md`](../docs/security.md).

The PostgreSQL lifecycle gate runs a host plus two players through two curated rounds and covers shared board identity, session recovery during answering and reveal, equivalent guesses, deadline rejection, override, completion, and score reload after the repository/service/server are rebuilt.

## Known Limitations

Clients use authenticated SSE invalidations with polling recovery. Automatic reveal and sequential timeout advancement require PostgreSQL, while next-round advancement remains host-controlled. Presence, cross-device session transfer, and non-English locales are not supported. Abuse limits are process-local, so public deployments must use one API replica. Privacy-safe JSON logs, authenticated bounded metrics, dependency-aware readiness, graceful drain, and opt-in recovery/retention tooling are implemented; environment-specific staging evidence remains an operator responsibility. See [`docs/operations.md`](../docs/operations.md).

Completed games receive a random 128-bit replay identifier. `GET /api/replays/{replayID}` returns rankings, ties, revealed question/board answers, guesses, answer matches, and per-round score deltas. It deliberately omits tokens, normalized guesses, aliases, provider/model/prompt metadata, audits, judge records, and raw provider content. `POST /api/rooms/{code}/play-again` is host-token protected and creates a distinct lobby with copied settings and a new host identity; no gameplay rows or credentials are reused.

## Suggested Stack

- Go
- PostgreSQL
- sqlc or pgx
- goose or tern for migrations
- OpenAI API initially
- Optional future support for other model providers
- WebSocket or Server-Sent Events for real-time updates
- REST for setup/admin/lobby actions

## Main Game Modes

### Simultaneous Mode

Best for parties, Zoom calls, and team-building.

Flow:

1. Everyone sees the question.
2. Everyone submits one answer before the timer ends.
3. Answers are matched to the hidden board.
4. The host reveals early or the durable worker reveals automatically after expiry.
5. Scores are awarded.

This should be the MVP mode.

The `answerPhaseEndsAt` timestamp is authoritative. A guess received at or after that instant is rejected with HTTP `409` and `{"error":"answer phase has expired"}`. The repository checks the deadline again while holding the round lock, so a request that waits past the cutoff cannot be persisted. Expiry triggers an idempotent PostgreSQL reveal transaction; it never advances the round automatically.

### Sequential Mode

A more classic turn-based mode.

Flow:

1. Lobby join order is frozen for each round.
2. Players answer one at a time.
3. Prior raw claims are visible; hidden match and score outcomes are not.
4. Submit, pass, or timeout advances durably; the final turn reveals.
5. First claim and scoring use the same auditable rules as simultaneous mode.

Sequential is a separate mode and cannot be combined with teams.

### Team Mode

Useful for Zoom/team-building.

Flow:

1. Players are grouped into teams.
2. Scores accumulate by team.
3. Players may answer individually, but points count toward the team.

Team creation and assignment are host-only and lobby-only. A game requires 2–4 non-empty teams and no unassigned players. Assignments are immutable after start. Players submit individually, first-committed answer claims remain global across the round, and team totals are derived from member `score_events` rather than stored as a mutable second score.

### Living-room Mode

The creator is persisted as `host_display`, can start the game, cannot guess, and is excluded from the frozen participant quorum, claims, score events, rankings, and replay rankings. At least two joined `participant` players are required. Living-room cannot be combined with teams or sequential turns.

All rounds are generated and stored at start; later rounds remain `pending`, so transition workers never perform provider calls while holding locks. The last committed participant guess can reveal early. Otherwise the answer deadline reveals normally. Both persist `reveal_phase_ends_at`; when it becomes due, the worker atomically records a unique `start_round` or `complete_game` transition.

## Data Model Sketch

### rooms

Stores a lobby/game room.

Fields:

- id
- code
- name
- host_player_id
- status
- settings_jsonb
- created_at
- updated_at

### players

Stores players connected to a room.

Fields:

- id
- room_id
- display_name
- avatar_url
- is_host
- team_id nullable
- connection_status
- created_at
- updated_at

### teams

Optional team grouping.

Fields:

- id
- room_id
- name
- created_at

### games

Stores a started game session.

Fields:

- id
- room_id
- status
- mode
- total_rounds
- current_round_index
- created_at
- started_at
- ended_at

### questions

Stores generated or curated questions.

Fields:

- id
- text
- locale
- category
- created_by_model
- prompt_version
- safety_status
- created_at

### prediction_boards

Stores a frozen model-generated answer board.

Fields:

- id
- question_id
- provider
- model_name
- prompt_version
- temperature
- seed
- request_id
- board_hash
- raw_response_jsonb
- created_at

### prediction_answers

Stores canonical board answers.

Fields:

- id
- board_id
- canonical_answer
- aliases_jsonb
- rank
- score
- confidence
- rationale_hidden
- created_at

### rounds

Stores one game round.

Fields:

- id
- game_id
- round_index
- question_id
- board_id
- status
- answer_phase_started_at
- answer_phase_ends_at
- reveal_started_at
- created_at

### guesses

Stores player submissions.

Fields:

- id
- round_id
- player_id
- raw_answer
- normalized_answer
- matched_prediction_answer_id nullable
- score_awarded
- created_at

The database permits only one positive-scoring guess for each prediction answer in a round. A matched guess with `score_awarded = 0` represents a duplicate claim.

### score_events

Stores auditable scoring changes.

Fields:

- id
- game_id
- round_id
- player_id nullable
- team_id nullable
- delta
- reason
- metadata_jsonb
- created_at

## Board Hash

Before accepting answers, the backend should compute and store a hash of the frozen board.

Example input:

- question text;
- canonical answers;
- aliases;
- ranks;
- scores;
- model name;
- prompt version.

This prevents suspicion that the board changed after players answered.

The UI can optionally show a short hash after the round:

> Board generated before answers: `a13f91c2`

## LLM Roles

Use separate logical roles.

### Question Generator

Creates answerable, party-safe questions.

Example system intent:

> Generate broad, fun prompts that produce varied common answers. Avoid private, hateful, sexual, medical, legal, or highly sensitive topics.

### Prediction Model

Creates the answer board that players are trying to guess.

The selected model is part of the game identity.

### Judge Model

Matches player guesses to board entries.

This should generally be a strong, stable model. It does not need to be the same model that generated the board.

## API Shape

### REST Endpoints

Suggested initial endpoints:

- `POST /api/rooms`
- `GET /api/rooms/{code}`
- `POST /api/rooms/{code}/join`
- `POST /api/rooms/{code}/session`
- `POST /api/rooms/{code}/start`
- `POST /api/rooms/{code}/players/{playerID}/heartbeat`
- `POST /api/rooms/{code}/rounds/{roundID}/guesses`
- `POST /api/rooms/{code}/rounds/{roundID}/reveal`
- `GET /api/rooms/{code}/state`

Guess submission returns `409 Conflict` with `{"error":"answer phase has expired"}` when server time is equal to or later than the round deadline.

Room responses use one phase-aware public projection across create, join, session recovery, start, submit, fetch, reveal, advance, and override operations. During `answering`, the response includes the question, board hash, deadline, player list, per-player `submissionMade` status, and scores through the last revealed round. It omits the board, every guess field, match and duplicate results, awarded points, and current-round score changes. During `revealed`, the response includes the frozen board, guesses and their outcomes, and the updated scoreboard. Player tokens appear only in the top-level `player` returned when that player creates, joins, or successfully recovers a session; tokens never appear in `room.players`.

`POST /api/rooms/{code}/session` accepts `{"playerToken":"..."}`. It validates that the token belongs to a player in that room and returns the authoritative public room plus that player identity. Invalid or cross-room tokens return HTTP 403. This supports controlled same-browser refresh; transferable sessions and presence heartbeats remain deferred.

Host-only endpoints:

- `POST /api/rooms/{code}/settings`
- `POST /api/rooms/{code}/next-round`
- `POST /api/rooms/{code}/override-match`
- `GET /api/rooms/{code}/rounds/{roundId}/judge-suggestions` (host header, revealed rounds only)
- `GET /api/rooms/{code}/provider-audits` (host header)
- `POST /api/rooms/{code}/end`

### Server-Sent room events

`GET /api/rooms/{code}/events` accepts the room-scoped credential only in `X-Player-Token` and publishes ordered, versioned `room_invalidation` envelopes. Events carry a room revision and public type but no game content; clients must refetch the authoritative room projection. `Last-Event-ID` resumes after a revision, while duplicate/gap handling remains mandatory. The durable PostgreSQL log retains 1,000 events per room and survives backend reconstruction.

SSE was selected over WebSockets because mutations remain REST requests and transport is one-way. Heartbeats, origin validation, connection/write limits, and graceful shutdown are enforced. See [`../docs/events.md`](../docs/events.md) for the full contract, event types, configuration, secrecy, and fallback behavior.

## Configuration

Environment variables:

```bash
DATABASE_URL=postgres://...
OPENAI_API_KEY=...
HTTP_ADDR=:8080
APP_ENV=development
CORS_ALLOWED_ORIGINS=http://localhost:5173
DEFAULT_PREDICTION_MODEL=gpt-5.6-luna
DEFAULT_QUESTION_MODEL=gpt-5.6-luna
ALLOWED_PREDICTION_MODELS=gpt-5.6-luna
ALLOWED_QUESTION_MODELS=gpt-5.6-luna
MODEL_TIMEOUT_SECONDS=10
MODEL_MAX_ATTEMPTS=2
MODEL_MAX_CALLS_PER_GAME=20
MODEL_MAX_COST_USD_PER_GAME=0.10
MODEL_CAPTURE_RAW_RESPONSES=false
MODEL_RAW_RESPONSE_MAX_BYTES=4096
EVENT_POLL_INTERVAL_MS=250
EVENT_HEARTBEAT_SECONDS=15
EVENT_MAX_CONNECTIONS=100
LIVINGROOM_REVEAL_PAUSE_SECONDS=8
EVENT_WRITE_TIMEOUT_SECONDS=5
TRUSTED_PROXY_CIDRS=
RATE_LIMIT_MAX_KEYS=10000
RATE_LIMIT_CREATE_REQUESTS=10
RATE_LIMIT_CREATE_WINDOW_SECONDS=60
PROVIDER_LIMIT_GLOBAL_REQUESTS=120
PROVIDER_LIMIT_GLOBAL_WINDOW_SECONDS=3600
```

All limiter prefixes and defaults are listed in [`docs/security.md`](../docs/security.md). Each prefix has `_REQUESTS` and `_WINDOW_SECONDS`. Health/readiness, CORS preflight, and the internal transition worker are deliberately exempt.

## Local Development

Use Go 1.25 or newer and Docker Engine 24 or newer with Docker Compose v2. From the repository root, download locked dependencies and start the full stack:

```bash
cp .env.example .env
make bootstrap
make start
```

PostgreSQL 16 is supplied by `compose.yml`. Migrations use Goose `v3.27.2` through `go run`, so Goose does not need to be installed globally.

For backend-only development:

```bash
make postgres-up
make migrate-up
make backend
```

Run backend tests or the complete repository verification:

```bash
make test-backend
make vet-backend
make verify
```

`make verify` is the local equivalent of the CI gates. It checks formatting and vetting, runs all backend and client tests, and builds the production client.

Run the backend directly from its directory when PostgreSQL and migrations are already ready:

```bash
cd backend
go run ./cmd/server
```

## OpenAI Integration

The backend should wrap model providers behind an interface.

Example:

```go
type ModelClient interface {
    GenerateQuestions(ctx context.Context, req GenerateQuestionsRequest) (*GenerateQuestionsResponse, error)
    GenerateBoard(ctx context.Context, req GenerateBoardRequest) (*GenerateBoardResponse, error)
}
```

Do not scatter provider-specific logic throughout handlers.

The MVP requests strict JSON-schema responses from OpenAI, then applies the same provider-independent validation used for curated content. A playable board requires one bounded question with ID/locale/category metadata and exactly five ordered answers with unique normalized canonicals, ranks 1–5, strictly descending scores from 1–100, bounded canonical/alias text, bounded aliases, and non-empty provider/model/prompt metadata. Canonical and alias ownership must also be unambiguous after normalization.

## Supported MVP API input

Room settings persist `gameKind` independently from `mode`. The supported matrix is recorded in ADR-007; omitted kinds default to `model_says`, and trivia with sequential mode returns a stable unsupported-settings error. The browser intentionally sends only `model_says` until trivia is playable.

Trivia round solutions use the version-1 JSONB contract recorded in ADR-008. It freezes the canonical answer, ordered explicit aliases, base score, optional explanation/source, and—only for Choice Trivia—four ordered opaque option IDs/labels plus one correct option ID. Validation bounds every field and rejects ambiguous normalized aliases/labels, duplicate option IDs, incorrect option cardinality, and a correct ID outside the option set. SHA-256 covers the complete payload; reconstruction verifies both the embedded value and the separately persisted hash. During answering, REST responses expose only version/kind/base score and Choice Trivia's ordered option IDs/labels. Canonical answers, aliases, correct IDs, explanation, source, and hashes are absent until reveal; SSE remains content-free and completed replays receive the revealed projection.

Room creation accepts individual simultaneous, team, or sequential mode, 1–5 rounds, a 15–120 second answer timer, locale `en`, and the prediction model configured by `DEFAULT_PREDICTION_MODEL` (default `gpt-5.6-luna`). Omitted settings retain the defaults of five rounds and 45 seconds. Room names must contain 3–48 Unicode characters and display names 2–24; control characters are rejected. Route room codes must be six characters from the invite-code alphabet.

Joining creates a new player only while the room is in the lobby. Once start wins the room lock, later joins return HTTP `409` with `game has started; new players cannot join`. Player-token mutations resolve the token only within the requested room and operate only on that room's current requested round.

All JSON mutation bodies are capped at 16 KiB. Decoding rejects unknown object fields, malformed JSON, empty bodies, and additional trailing JSON values. Validation failures return HTTP `400`.

Generation gets two attempts. If either attempt fails or returns invalid content, the server falls back to the curated bank entry for that round. The English bank contains five unique validated rounds, matching the maximum MVP round count. Provider failures do not block a static game; if the model and curated paths both fail, start/advance returns HTTP 503 with a retryable content-unavailable message. Persisted board metadata records the actual provider, requested model, and prompt version rather than trusting model-authored metadata. Raw responses, token use, and cost auditing remain post-MVP work.

## Safety and Content Rules

Generated questions should avoid:

- hate or harassment;
- sexual content;
- medical/legal/financial advice;
- personally identifying questions;
- workplace-sensitive prompts in team-building mode;
- topics that create awkward HR situations.

Team-building mode should have stricter filtering.

## Matching and Duplicate Rules

The MVP matcher preserves Unicode letters and numbers, lowercases them, removes punctuation, and collapses whitespace. It then requires exact equality with a canonical answer or configured alias normalized by the same rule. Accents remain significant, and the matcher performs no stemming, fuzzy comparison, translation, or semantic inference.

Examples:

- `CRÈME, BRÛLÉE!` matches `crème brûlée`;
- `boss` matches `manager` only when `boss` is an alias for that answer;
- `money` does not match `salary` unless the board explicitly includes it as an alias.

Board generation fails validation when one normalized canonical answer or alias belongs to multiple board answers. This prevents answer ordering from silently deciding an ambiguous match.

An optional semantic judge runs only after a guess is authoritatively stored as a deterministic miss. Its strict `judge-v1` response includes:

- one frozen-board answer ID or null;
- confidence from 0 to 1, classified as low (`<0.60`), medium (`0.60–0.849…`), or high (`>=0.85`);
- one bounded rationale category.

Suggestions and their provider audits are stored separately from guesses and score events. They are absent from every room projection and available only to the authenticated host after reveal through `GET /api/rooms/{code}/rounds/{roundId}/judge-suggestions`. Even a high-confidence suggestion is advisory: the host must accept it, select another frozen answer, or retain the miss through the existing override endpoint. An override cannot create a second scoring claim for an answer. Timeout, invalid output, exhausted budget, and missing-provider paths leave the deterministic miss unchanged.

Submission scoring is authoritative inside the repository transaction. The transaction locks the round, resolves the match against its frozen board, and checks existing positive-scoring claims before inserting the guess and score event together. Only the earliest transaction to commit a claim receives the answer score; later equivalent guesses remain visible as duplicates and score zero. Host overrides use the same locked claim rule and append an auditable score event when they change a score.

### PostgreSQL integration tests

Set `TEST_DATABASE_URL` to a PostgreSQL database where the test user may create schemas, then run:

```bash
cd backend
TEST_DATABASE_URL='postgres://postgres:postgres@localhost:5432/postgres?sslmode=disable' go test -race ./...
```

Each integration test creates a unique schema, applies all migrations, and removes the schema afterward. The suite includes concurrency and migration checks plus a two-round HTTP lifecycle with a host and two players covering create, joins, start, shared board identity, hidden answering state, session recovery, equivalent-answer duplicate scoring, deadline rejection, reveal, override, next round, completion, and reload through a reconstructed repository and service. It always uses curated content and never needs an OpenAI key. Without `TEST_DATABASE_URL`, PostgreSQL integration tests are skipped while unit tests still run.

## Testing Priorities

Test these first:

- room code uniqueness;
- joining/leaving rooms;
- game start rules;
- board generation persistence;
- board hash stability;
- answer matching;
- duplicate-answer handling;
- scoring;
- reconnection;
- host override;
- timer expiry.

## MVP Definition

The backend MVP is complete when:

- a host can create a room;
- players can join with a room code;
- the host can start a simultaneous-mode game;
- the backend can generate or load questions;
- each round has a frozen prediction board;
- players can submit answers;
- guesses are matched to the board;
- scores are computed and persisted;
- clients receive polling-friendly authoritative state;
- the final scoreboard is available.
