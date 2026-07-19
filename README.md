# Model Says

**Model Says** is a real-time party game where players try to guess what an AI model thinks the most common answers are.

It is inspired by survey-answer party games, but with a different premise:

> You are not guessing what people said.
> You are guessing what the model thinks people would say.

This makes the game especially fun for people who work with AI, use AI tools, or enjoy arguing with model vibes.

## Concept

A room host starts a game and invites players using a room code or link.

For each round:

1. A question is shown to all players.
2. Before players answer, an AI model generates a hidden answer board.
3. Players submit their guesses.
4. The server matches normalized guesses exactly to canonical answers or curated aliases.
5. The board is revealed.
6. Points are awarded.
7. The next round begins.

The generated board is stored persistently so every player competes against the same frozen set of answers.

## Example

Question:

> Name something people pretend to understand but probably do not.

A model might predict answers like:

| Rank | Answer                  | Points |
| ---: | ----------------------- | -----: |
|    1 | Cryptocurrency          |     50 |
|    2 | Quantum physics         |     35 |
|    3 | Taxes                   |     25 |
|    4 | Artificial intelligence |     15 |
|    5 | Wine                    |     10 |

Players score points by guessing answers that match the model-generated board.

## Why This Is Fun

The game is not trying to produce real survey data.

The fun comes from trying to predict the model’s cultural assumptions, stereotypes, priors, and weirdly confident guesses.

Different models may produce different boards for the same question, which makes model selection part of the game.

Possible game variants include:

* known model mode;
* blind model mode;
* random model per round;
* team mode;
* Zoom/team-building mode;
* simultaneous answers;
* sequential turn-based answers.

## Project Structure

This repository is split into two main applications:

```txt
.
├── backend/
│   ├── README.md
│   └── TODO.md
├── client/
│   ├── README.md
│   └── TODO.md
├── LICENSE
└── README.md
```

### Backend

The backend is responsible for:

* room creation;
* player sessions;
* game state;
* question generation;
* AI-generated answer boards;
* persistent board storage;
* answer matching;
* scoring;
* timers;
* WebSocket or real-time updates;
* host controls.

See [`backend/README.md`](backend/README.md) for more details.

### Client

The client is responsible for:

* creating and joining rooms;
* displaying the lobby;
* showing questions;
* collecting answers;
* showing timers;
* revealing answer boards;
* displaying scores;
* supporting host controls.

See [`client/README.md`](client/README.md) for more details.

## Local Development

The repository now includes a root `compose.yml` for PostgreSQL and a root `Makefile` for the common local workflows.

Prerequisites:

- Go 1.25 or newer;
- Node.js 22 or newer with npm;
- Docker Engine 24 or newer with Docker Compose v2.

PostgreSQL 16 runs through Compose, so a separate local PostgreSQL installation is not required. Migrations use Goose `v3.27.2` through `go run`; Goose does not need to be installed globally.

Quick start:

```bash
cp .env.example .env
make bootstrap
make start
```

That target will:

* start PostgreSQL in Docker;
* apply backend SQL migrations;
* run the Go backend;
* run the Vite client.

When a root `.env` file is present, the Makefile loads it automatically and passes those overrides into the backend, migration commands, and client dev server.

Useful split targets:

```bash
make postgres-up
make migrate-up
make backend
make client
```

Verification:

```bash
make verify
```

`make verify` checks Go formatting, runs backend and client tests, and type-checks/builds the client. Narrower targets such as `make test-backend`, `make test-client`, `make build-client`, and `make check-format` are also available.

Without `OPENAI_API_KEY`, games use a five-question English curated bank and support the full 1–5 round MVP flow. With OpenAI enabled, question and board responses use strict JSON schemas and are validated independently of the provider. Invalid or failed generation is retried once, then the server selects the unused curated entry for that round. Only failure of both paths returns a temporary content-unavailable response.

## Suggested Tech Stack

### Backend

* Go
* PostgreSQL
* OpenAI API
* WebSocket or Server-Sent Events
* REST API for setup and host actions

### Client

* React
* TypeScript
* Vite
* WebSocket client
* Responsive UI for desktop and mobile

## Core Design Principles

### The board must be frozen before answers

The prediction board should be generated and stored before players submit answers.

This keeps the game fair and avoids the feeling that the model changed its answers after seeing player guesses.

### The model is the game

Players are trying to guess the selected model, not a real survey.

The UI should make this clear.

Good copy examples:

* “What would the model say?”
* “Guess the AI’s answer board.”
* “The board was generated before answers.”
* “You are guessing the model, not the public.”

### The backend is authoritative

The frontend should not make final decisions about:

* scoring;
* answer matching;
* round state;
* timers;
* revealed answers;
* host permissions.

### Host override should exist

Deterministic matching will sometimes miss a reasonable semantic equivalent that is not listed as an alias.

That is part of the fun, but it should not break the game.

The host can correct a match or miss after reveal.

## Game Modes

### Simultaneous Mode

Recommended MVP mode.

All players answer at the same time. The server accepts guesses only before the published `answerPhaseEndsAt` deadline. At expiry, answering closes and the host reveals the board; automatic reveal is intentionally deferred for the MVP. The host may also reveal early.

While a round is accepting answers, every public API response hides the frozen board, guess text, normalized answers, match and duplicate outcomes, awarded points, and the current round's score changes. Players can see who has submitted and the scoreboard through the last revealed round. Reveal publishes the frozen board and round results and applies the new totals to the public scoreboard.

For the MVP, matching is deterministic: Unicode letters and numbers are lowercased, punctuation is removed, and whitespace is collapsed. The result must exactly equal a similarly normalized canonical board answer or alias; accents remain significant, and no semantic or fuzzy inference is performed. A board is rejected if the same normalized phrase belongs to multiple answers. Only the earliest committed guess that claims a board answer receives its points. Later guesses matching that same answer are recorded as duplicates and score zero. After reveal, the host can correct a hit, miss, or disputed answer with an override.

This is ideal for:

* parties;
* Zoom calls;
* team-building sessions;
* larger groups;
* mobile players.

### Sequential Mode

Players answer one at a time, with a timer.

This creates more pressure and strategy, but has more downtime and is better for smaller groups.

### Team Mode

Players are grouped into teams and scores are accumulated per team.

This is especially useful for remote team-building.

## Persistence

Model Says should store generated boards in PostgreSQL.

A generated board should include:

* question text;
* model provider;
* model name;
* prompt version;
* temperature;
* canonical answers;
* aliases;
* ranks;
* scores;
* confidence values;
* raw model response;
* board hash.

This allows games to be replayed, audited, and kept consistent.

## AI Model Roles

The system can use separate models for different jobs.

### Question Model

Generates fun, broad, party-safe questions.

### Prediction Model

Generates the hidden answer board.

This is the model players are trying to guess.

### Future Judge Model

Semantic model-based matching is not active in the MVP. It is a post-MVP option for suggesting broader matches; the frozen board, transactional duplicate rule, and post-reveal host override remain authoritative.

## Development Status

This project has a working static/polling simultaneous-game path and is being hardened toward a playable MVP.

Recommended implementation order:

1. Backend room and lobby API.
2. PostgreSQL schema and migrations.
3. React room creation and join flow.
4. WebSocket room updates.
5. Simultaneous game mode.
6. Static questions and static boards.
7. OpenAI-generated boards.
8. Deterministic canonical/alias matching.
9. Scoring and reveal UI.
10. Host override.
11. Team mode.
12. Sequential mode.

See the TODO files for a more detailed implementation plan:

* [`backend/TODO.md`](backend/TODO.md)
* [`client/TODO.md`](client/TODO.md)

## Environment Variables

Example backend variables:

```bash
DATABASE_URL=postgres://...
OPENAI_API_KEY=...
HTTP_ADDR=:8080
DEFAULT_PREDICTION_MODEL=gpt-4.1-mini
```

Example client variables:

```bash
VITE_API_BASE_URL=http://localhost:8080
VITE_WS_URL=ws://localhost:8080/ws
```

## Safety Notes

Generated questions should avoid:

* hateful or harassing content;
* sexual content;
* medical, legal, or financial advice;
* personally identifying prompts;
* workplace-sensitive topics in team-building mode;
* anything likely to create an uncomfortable HR situation.

Team-building mode should use stricter filtering.

## License

This project is licensed under the Apache License 2.0.

See [`LICENSE`](LICENSE) for details.
