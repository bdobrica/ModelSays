# Model Says — Backend

Model Says is a real-time party game where players try to guess what an AI model thinks the most common answers are.

This backend provides the game engine, room management, persistent AI-generated answer boards, scoring, player sessions, and API/WebSocket gateway used by the React client.

The key idea is not to simulate a real survey. The game is explicitly about guessing the model’s predicted cultural priors.

## Core Concept

For each round:

1. A question is selected.
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
- WebSocket updates;
- model/provider configuration;
- replay/debug metadata.

The frontend should not be trusted for game-state decisions.

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
4. The host reveals the board, either early or after answering expires.
5. Scores are awarded.

This should be the MVP mode.

The `answerPhaseEndsAt` timestamp is authoritative. A guess received at or after that instant is rejected with HTTP `409` and `{"error":"answer phase has expired"}`. The repository checks the deadline again while holding the round lock, so a request that waits past the cutoff cannot be persisted. Expiry closes submissions but does not automatically reveal or advance the round in the MVP.

### Sequential Mode

A more classic turn-based mode.

Flow:

1. A random player order is generated per round.
2. Players answer one at a time.
3. Previously revealed answers cannot be reused.
4. Each player consumes their own timer, like a chess clock.

This can be implemented after simultaneous mode.

### Team Mode

Useful for Zoom/team-building.

Flow:

1. Players are grouped into teams.
2. Scores accumulate by team.
3. Players may answer individually, but points count toward the team.

This can reuse simultaneous mode initially.

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
- `POST /api/rooms/{code}/start`
- `POST /api/rooms/{code}/players/{playerID}/heartbeat`
- `POST /api/rooms/{code}/rounds/{roundID}/guesses`
- `POST /api/rooms/{code}/rounds/{roundID}/reveal`
- `GET /api/rooms/{code}/state`

Guess submission returns `409 Conflict` with `{"error":"answer phase has expired"}` when server time is equal to or later than the round deadline.

Room responses use one phase-aware public projection across create, join, start, submit, fetch, reveal, advance, and override operations. During `answering`, the response includes the question, board hash, deadline, player list, per-player `submissionMade` status, and scores through the last revealed round. It omits the board, every guess field, match and duplicate results, awarded points, and current-round score changes. During `revealed`, the response includes the frozen board, guesses and their outcomes, and the updated scoreboard. Player tokens appear only in the top-level `player` returned when that player creates or joins a room; tokens never appear in `room.players`.

Host-only endpoints:

- `POST /api/rooms/{code}/settings`
- `POST /api/rooms/{code}/next-round`
- `POST /api/rooms/{code}/override-match`
- `POST /api/rooms/{code}/end`

### WebSocket Events

Suggested server-to-client events:

- `room.updated`
- `player.joined`
- `player.left`
- `game.started`
- `round.started`
- `timer.updated`
- `guess.submitted`
- `round.revealed`
- `score.updated`
- `game.ended`
- `error`

Suggested client-to-server events:

- `join_room`
- `set_ready`
- `start_game`
- `submit_guess`
- `request_reveal`
- `next_round`
- `override_match`

## Configuration

Environment variables:

```bash
DATABASE_URL=postgres://...
OPENAI_API_KEY=...
HTTP_ADDR=:8080
APP_ENV=development
CORS_ALLOWED_ORIGINS=http://localhost:5173
DEFAULT_PREDICTION_MODEL=gpt-4.1-mini
DEFAULT_QUESTION_MODEL=gpt-4.1-mini
```

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
make verify
```

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

Room creation accepts only simultaneous mode, 1–5 rounds, a 15–120 second answer timer, locale `en`, and the prediction model configured by `DEFAULT_PREDICTION_MODEL` (default `gpt-4.1-mini`). Omitted settings retain the defaults of five rounds and 45 seconds. Room names must contain 3–48 Unicode characters and display names 2–24; control characters are rejected. Route room codes must be six characters from the invite-code alphabet.

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

Semantic judge-model matching is deferred until after the deterministic MVP. A future judge response may include:

- matched answer id or null;
- confidence;
- short hidden rationale;
- whether host review is recommended.

The current correction mechanism is the post-reveal host override. It can turn a hit into a miss, a miss into a hit, or select a different board answer; an override cannot create a second scoring claim for an answer.

Submission scoring is authoritative inside the repository transaction. The transaction locks the round, resolves the match against its frozen board, and checks existing positive-scoring claims before inserting the guess and score event together. Only the earliest transaction to commit a claim receives the answer score; later equivalent guesses remain visible as duplicates and score zero. Host overrides use the same locked claim rule and append an auditable score event when they change a score.

### PostgreSQL scoring tests

Set `TEST_DATABASE_URL` to a PostgreSQL database where the test user may create schemas, then run:

```bash
cd backend
TEST_DATABASE_URL='postgres://postgres:postgres@localhost:5432/postgres?sslmode=disable' go test -race ./...
```

Each integration test creates a unique schema, applies all migrations, and removes the schema afterward. Without `TEST_DATABASE_URL`, PostgreSQL integration tests are skipped while unit tests still run.

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
- clients receive real-time state updates;
- the final scoreboard is available.
