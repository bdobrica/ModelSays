# Model Says

**Model Says** is a playable live-updating party game where players try to guess what an AI model thinks the most common answers are.

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
4. The server scores normalized canonical/alias matches deterministically and may privately ask an optional judge for suggestions on misses.
5. The board is revealed.
6. Points are awarded.
7. The next round begins.

The generated board is stored persistently so every player competes against the same frozen set of answers.

Rooms may use individual simultaneous, team, sequential, or living-room mode. Team mode aggregates individual scores into 2–4 host-configured teams. Sequential mode gives every player one timed turn in lobby join order. Living-room mode turns the creator into a non-playing TV display while every human plays from a phone.

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
* authenticated live room invalidations with polling recovery;
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
make install
```

`make install` downloads the locked Go and npm dependencies, then:

* start PostgreSQL in Docker;
* apply backend SQL migrations;
* run the Go backend;
* run the Vite client.

See [`INSTALL.md`](INSTALL.md) for the complete installation, gameplay, shutdown, and troubleshooting guide. After the first installation, use `make start` to launch without reinstalling dependencies.

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

`make verify` matches the complete CI gate: it checks Go formatting and vetting, runs backend and client tests, and type-checks/builds the client. Set `TEST_DATABASE_URL` when running it to include the isolated PostgreSQL integration suite; without that variable, those tests skip. Narrower targets such as `make vet-backend`, `make test-backend`, `make test-client`, `make build-client`, and `make check-format` are also available.

GitHub Actions runs separate backend, client, and PostgreSQL integration jobs on pushes and pull requests. The database job covers a host-and-two-player, two-round HTTP lifecycle—including secrecy, equivalent claims, deadline rejection, session recovery, reveal, host override, completion, and reload—using curated content, so verification never needs an OpenAI key or external model request.

Before performance-sensitive beta changes, build the client and run the repeatable 3-, 8-, and 12-player PostgreSQL baseline:

```bash
make baseline
```

The command prints versioned JSON and never calls an external model. See [`docs/baselines/pb-00.md`](docs/baselines/pb-00.md) for the initial two-pass results and budgets, [`docs/release-evidence.md`](docs/release-evidence.md) for the release record, and [`docs/playtest-guide.md`](docs/playtest-guide.md) for structured human feedback.

The required automated smoke gate stays at the API boundary to keep CI small and deterministic. For a human usability/fun check before wider distribution, run:

1. Run `make start`, open `http://localhost:5173` in three separate browser sessions (one at a phone-sized viewport), and create a two-round room in the first.
2. Join from both player sessions, start as host, and confirm all sessions show the same question and board hash without revealing the board.
3. Submit equivalent aliases from both players, refresh during answering, wait for expiry, confirm further input is disabled/rejected, then reveal as host.
4. Confirm the first claim scored and the later equivalent is a duplicate, correct the duplicate to another answer with the host override controls, and refresh during reveal.
5. Advance, play and reveal round two, confirm all sessions show the same final ranking, open the shareable round-by-round replay, then use the host's **Play again** action and confirm it creates an empty new lobby.

Without `OPENAI_API_KEY`, games randomly sample without replacement from a five-question English curated bank and support the full 1–5 round MVP flow, so each new game can receive a different question order while a question never repeats within that game. With OpenAI enabled, question and board responses use strict JSON schemas and are validated independently of the provider. Invalid or failed generation is retried once, then the server randomly selects an unused curated entry for that round. Only failure of both paths returns a temporary content-unavailable response.

Provider calls use server-owned model allowlists, a 10-second timeout, two-attempt retry ceiling, and shared per-game call/cost budgets. Every generated question, board, and semantic-judge call—including zero-cost curated behavior—is recorded in a private room audit; raw response capture is disabled by default. A judge only evaluates deterministic misses, never changes a score, and exposes its bounded suggestion to the host only after reveal. See [`docs/provider-operations.md`](docs/provider-operations.md) for configuration, privacy, host review/audit access, retention, and rollback.

The supported room contract keeps modes isolated: simultaneous, team, sequential, or living-room mode; 1–5 rounds; a 15–120 second answer timer; English (`en`); and the server-configured `DEFAULT_PREDICTION_MODEL`. Room names are 3–48 Unicode characters and display names are 2–24; control characters are rejected. Room codes are six characters from the displayed invite alphabet. New players may join only while the room is in the lobby; attempting to reuse an invite after start returns a conflict response. JSON mutation requests are limited to 16 KiB and reject unknown fields, trailing values, and malformed input. The browser validates its stored player token with the server after refresh and offers a clear-session action.

## Suggested Tech Stack

### Backend

* Go
* PostgreSQL
* OpenAI API
* Server-Sent Events for authenticated room invalidations
* REST API for setup and host actions

### Client

* React
* TypeScript
* Vite
* resilient polling
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

## Supported MVP

The controlled MVP is playable with one host and at least two players:

- simultaneous, team, sequential, or living-room English games with 1–5 rounds and 15–120 second timers;
- PostgreSQL-persisted rooms, frozen boards, guesses, score events, and final rankings;
- a five-round curated bank that works without an OpenAI key, plus validated OpenAI generation when configured;
- deterministic canonical/alias matching with first committed claim wins;
- server-enforced deadlines, hidden answering-state outcomes, durable automatic reveal, early host reveal, and post-reveal overrides;
- authenticated ordered SSE invalidations, authoritative sequenced refetches, bounded reconnect/resume, polling recovery, and server-validated same-browser session recovery;
- role-aware accessible responsive layouts: the host retains operational controls while joined players receive a focused action/waiting/final-ranking surface, with skip/focus/live feedback, 48 px controls, phone/tablet breakpoints, reduced-motion reveal, invite copying, and fullscreen presentation;
- automated backend, client, and PostgreSQL lifecycle gates through `make verify`.

The repeatable MVP playtest gate uses a host and two player identities across two curated rounds. It verifies a shared board hash, refresh/session recovery during answering and reveal, equivalent-answer duplicate handling, expiry rejection, host override, final ranking, and score recovery after backend reconstruction. This technical operator simulation passed on 2026-07-19. The browser room-flow suite covers the corresponding UI states; an external-player fun/usability session is still recommended before a public launch.

## Known Limitations

- Live invalidations contain no game content and only trigger authoritative room refetches. Streaming failures automatically retain five-second polling recovery and a visible non-blocking connection status.
- Automatic scoring remains deterministic. Optional semantic suggestions still require an explicit post-reveal host decision.
- Expiry closes answering and the PostgreSQL worker automatically reveals; round advancement remains a manual host action.
- Reconnect is limited to the same browser’s stored room token; there is no presence heartbeat or cross-device transfer.
- Team and sequential are separate modes and cannot be combined. Completed games have a non-enumerable results link, and **Play again** creates a clean lobby with the same mode and settings.
- Automated axe and component checks cover primary entry routes, focus/live feedback, all modes, and the declared responsive/motion policy. The physical phone, screen-reader, screen-share, and external-participant checklist in [`docs/accessibility.md`](docs/accessibility.md) remains a release gate.
- Public API/provider abuse boundaries, privacy-safe logs and bounded metrics, dependency-aware readiness, graceful drain, and recovery controls are documented and tested. The supported public-beta topology remains one API replica; an operator-controlled TLS/proxy and physical-network staging drill is still required before uncontrolled public traffic.

## Game Modes

### Simultaneous Mode

Recommended MVP mode.

All players answer at the same time. The server accepts guesses only before the published `answerPhaseEndsAt` deadline. PostgreSQL-backed servers reveal automatically at expiry (after an optional reveal-only grace period), and the host may reveal early. Next-round advancement stays manual so the host has an unbounded review window for suggestions and score corrections. The durable transition and emergency-disable contract is documented in [`docs/deadline-transitions.md`](docs/deadline-transitions.md).

While a round is accepting answers, every public API response hides the frozen board, guess text, normalized answers, match and duplicate outcomes, awarded points, and the current round's score changes. The joined-player presentation additionally omits roster, settings, submission progress for others, and live scores so the question remains the only task. Reveal publishes the frozen board and round results to the host view and applies the new totals; joined players wait for the next round and see rankings only when the game completes.

After completion, the room exposes a random replay identifier. Anyone with its link can view final rankings (including ties), revealed boards and guesses, answer matches, and per-round score deltas. Replay responses exclude player tokens, normalized guesses, answer aliases, provider/model/prompt metadata, audits, judge records, and raw provider content. Replays currently follow completed-game retention and do not have an automatic expiry; see [`docs/operations.md`](docs/operations.md).

For the MVP, matching is deterministic: Unicode letters and numbers are lowercased, punctuation is removed, and whitespace is collapsed. The result must exactly equal a similarly normalized canonical board answer or alias; accents remain significant, and no semantic or fuzzy inference is performed. A board is rejected if the same normalized phrase belongs to multiple answers. Only the earliest committed guess that claims a board answer receives its points. Later guesses matching that same answer are recorded as duplicates and score zero. After reveal, the host can correct a hit, miss, or disputed answer with an override.

This is ideal for:

* parties;
* Zoom calls;
* team-building sessions;
* larger groups;
* mobile players.

### Sequential Mode

Players take one turn each in deterministic lobby join order; the configured answer timer applies independently to every turn. A player may submit one guess or pass. Disconnecting has the same result as waiting: the durable worker passes that player when their turn expires. Raw prior claims are visible, but their hidden-board match, duplicate, and score outcomes remain secret until reveal.

First claim remains global and scores exactly as in simultaneous mode. Submission or pass advances immediately, the final action reveals automatically, the host may reveal early, and every new round restarts the same frozen player order. Teams and sequential cannot be combined.

### Team Mode

Players are grouped into teams and scores are accumulated per team.

### Living-room Mode

Choose **Living-room TV host** when the creator browser is shown on a shared television. The creator receives an authenticated non-playing `host_display` session; it can start the game but cannot guess and never appears in submission counts, claims, scores, rankings, or replays. The TV lobby shows a locally generated QR containing only the ordinary same-origin `/join?code=…` URL, the room code, joined participants, copy fallback, connection state, fullscreen, and **Start game**. At least two joined participants are required.

Rounds use simultaneous deterministic scoring. The server reveals as soon as every frozen participant submits, or at the authoritative deadline otherwise. Results remain on the TV until the persisted reveal-pause deadline (eight seconds by default), after which the PostgreSQL transition worker starts the prepared next round or completes the game exactly once. Phones reuse the focused question/waiting/final-ranking surface. Living-room mode cannot be combined with teams or sequential turns.

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

This project has a working simultaneous-game browser flow with deadline countdowns, authenticated live invalidations, sequenced refetch and polling recovery, same-browser session recovery, host controls, and ranked final scores. Cross-device session transfer, presence, and automatic deadline reveal remain follow-up work.

The controlled MVP implementation is complete. The next work should be chosen from measured playtest feedback; likely candidates are semantic match suggestions, pushed updates, automatic round progression, and public-launch hardening.

See the TODO files for a more detailed implementation plan:

* [`backend/TODO.md`](backend/TODO.md)
* [`client/TODO.md`](client/TODO.md)

## Environment Variables

Example backend variables:

```bash
DATABASE_URL=postgres://...
OPENAI_API_KEY=...
HTTP_ADDR=:8080
DEFAULT_PREDICTION_MODEL=gpt-5.6-luna
ALLOWED_PREDICTION_MODELS=gpt-5.6-luna
MODEL_TIMEOUT_SECONDS=10
MODEL_MAX_ATTEMPTS=2
MODEL_MAX_COST_USD_PER_GAME=0.10
TRUSTED_PROXY_CIDRS=
RATE_LIMIT_CREATE_REQUESTS=10
RATE_LIMIT_CREATE_WINDOW_SECONDS=60
PROVIDER_LIMIT_GLOBAL_REQUESTS=120
PROVIDER_LIMIT_GLOBAL_WINDOW_SECONDS=3600
```

The complete limiter matrix, proxy rules, threat model, privacy behavior, and `429` contract are in [`docs/security.md`](docs/security.md). Keep `TRUSTED_PROXY_CIDRS` empty unless the API is directly behind a proxy network you control.

The selected single-host deployment lifecycle, metrics/log contract, secret rotation, backup/restore, retention, and incident runbook are in [`docs/operations.md`](docs/operations.md). Local drill evidence is in [`docs/baselines/future-04b.md`](docs/baselines/future-04b.md); it does not claim a production deployment.

Example client variables:

```bash
VITE_API_BASE_URL=http://localhost:8080
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
