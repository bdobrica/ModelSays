# Model Says — Client

Model Says is a live-updating party game where players try to guess what an AI model thinks the most common answers are.

This client is a React app for joining rooms, playing rounds, submitting answers, watching reveals, and viewing scores. It should be optimized for parties, Zoom/team-building sessions, and casual play.

The current creation form supports individual simultaneous, team, or sequential play, 1–5 rounds, a 15–120 second timer, English locale, and the default `gpt-5.6-luna` prediction model. Room and display-name inputs are capped at the server's 48- and 24-character limits. Invite links stop accepting new players once the host starts the game.

The playable client authenticates an SSE stream with a request header and treats every event as an invalidation: authoritative state still comes from a sequenced room refetch. It coalesces bursts, resumes from the last applied room revision, detects gaps, reconnects with bounded exponential backoff, and automatically retains slower polling as recovery. Polling and reconnect work are reduced in hidden tabs and stop at game completion or when the room route is left. A polite status indicator reports live, reconnecting, offline, and complete states without blocking play.

## Supported MVP

The client supports individual simultaneous, team, and sequential game paths: create/join, team setup, live lobby, host start, server-authoritative countdowns, guesses and passes, automatic expiry transitions, host early reveal and override, private post-reveal semantic suggestions for the host, rankings/ties, replay, play-again, and same-browser refresh recovery. The layout collapses to one column below 900 px for phone-sized player views.

The client suite covers the event contract and every event type, header authentication, burst coalescing, duplicate/out-of-order/gapped revisions, malformed/secret-bearing payload rejection, reconnect/resume, offline/online and stop behavior, plus room-flow stale-response protection. The PostgreSQL lifecycle gate separately exercises one host and two player identities across the persisted API flow.

The root `make baseline` command builds the production client and records its uncompressed asset size alongside 3-, 8-, and 12-player API polling workloads. Current bundle evidence and beta budgets are documented in [`docs/baselines/pb-00.md`](../docs/baselines/pb-00.md).

## Live updates and recovery

The client opens `GET /api/rooms/{code}/events` with `X-Player-Token`; credentials never enter the URL. Live invalidations normally produce an immediate room refetch, while a 30-second safety poll recovers an event-publication failure. When streaming is unavailable the UI reports reconnecting and polls every five seconds while retrying the stream from its last applied revision. Hidden tabs poll every 30 seconds. Returning to a visible tab or an online network triggers immediate reconnect/refetch.

## Known Limitations

Next-round advancement remains a host action, and automatic reveal/turn timeout requires the backend's PostgreSQL worker. Matching corrections happen only after reveal. Sessions do not transfer across devices, and presence is not tracked. Animations and a dedicated browser end-to-end harness are deferred. Physical-device and lossy mobile-network playtesting is still recommended before public release.

## Core Experience

Players are not guessing real survey data.

They are guessing the model.

Suggested copy:

> What would the AI say?

or:

> Guess what the model thinks people would answer.

## User Roles

### Host

The host can:

- create a room;
- share the room code/link;
- configure the game;
- choose model settings;
- start the game;
- reveal rounds;
- advance rounds;
- override questionable matches;
- end the game.

### Player

A player can:

- join a room;
- choose a display name;
- optionally choose a team;
- submit answers;
- see revealed results;
- see their score.

During answering, room API data intentionally exposes only scores from previously revealed rounds plus each player's submission status. Board contents, guess text, match/duplicate outcomes, awarded points, and current-round score changes become available only after reveal.

## Primary Screens

### Home

Purpose:

- introduce the game;
- let users create or join a room.

Elements:

- app name: Model Says;
- short tagline;
- `Create room` button;
- `Join room` input;
- optional explanation of the premise.

### Create Room

Purpose:

- create a new game room.

Elements:

- host display name;
- game mode selector;
- number of rounds;
- answer timer;
- locale;
- model selector;
- team-building safe mode toggle;
- create button.

### Join Room

Purpose:

- join an existing room.

Elements:

- room code;
- display name;
- join button.

### Lobby

Purpose:

- wait for players and configure the game.

Elements:

- room code;
- share link;
- player list;
- team list if team mode is enabled;
- settings summary;
- ready states if implemented;
- start game button for host only.

### Round Question

Purpose:

- show the current question and collect answers.

Elements:

- round number;
- question text;
- timer;
- answer input;
- submit button;
- submitted state;
- player submission indicators;
- model identity if known mode is enabled.

### Reveal

Purpose:

- reveal the board and scores.

Elements:

- question text;
- generated answer board;
- canonical answers;
- points per answer;
- player guesses matched to answers;
- misses;
- score changes;
- board hash;
- next round button for host.

### Scoreboard

Purpose:

- show current and final scores.

Elements:

- player/team ranking;
- points;
- round-by-round score deltas;
- winner callout;
- play again button.

### Host Review

Purpose:

- allow the host to correct deterministic matches and misses after reveal.

Elements:

- raw player answer;
- suggested match;
- board answers;
- override dropdown;
- accept/reject controls.

This can be part of the reveal screen in the MVP.

## Suggested Stack

- React
- TypeScript
- Vite
- React Router
- TanStack Query or simple fetch wrapper
- authenticated SSE invalidation client with polling fallback
- CSS modules, Tailwind, or another simple styling choice
- Optional state manager: Zustand

## Client State

The backend is the source of truth.

The client may store:

- room code;
- player reconnect token;
- last selected display name;
- UI preferences.

Do not compute authoritative scores on the client.

## API Integration

The current client uses REST for actions and resilient polling for live game state. The backend SSE invalidation contract is ready; client reconnect/refetch and polling fallback are the next transport step.

### REST

Expected calls:

- create room;
- join room;
- validate/recover a stored player session;
- get current room state;
- start game;
- submit guess;
- reveal round;
- advance round;
- override match.

### Server-Sent Events

Expected incoming events:

- `player_joined`;
- `game_started`;
- `submission_progress_changed`;
- `round_revealed`;
- `score_changed`;
- `round_started`;
- `game_completed`.

Every type arrives in the versioned `room_invalidation` envelope and only signals a refetch; it carries no game content. The client should recover gracefully from SSE disconnects or revision gaps by refetching room state and retaining polling fallback. See [`docs/events.md`](../docs/events.md).

## Routing Sketch

Suggested routes:

```txt
/
 /create
 /join
 /room/:code
 /room/:code/play
 /room/:code/reveal
 /room/:code/scoreboard
```

A single room route can also work if the UI renders based on server state.

## UX Notes

### Zoom / Team-Building

Optimize for screen sharing:

- large text;
- readable room code;
- high-contrast answer board;
- clear countdown;
- minimal clutter;
- fun reveal animation;
- no tiny controls required from players.

### Mobile

Players may join from phones while the host shares the main board.

Prioritize:

- fast join flow;
- simple answer input;
- large submit button;
- clear submitted state.

### Fairness

The UI should make it clear that:

- the board was generated before answers;
- the board is AI-generated;
- the game is about guessing the model;
- MVP matching is exact after case, punctuation, and whitespace normalization;
- the host can review advisory semantic suggestions and correct disputed matches after reveal;
- no model suggestion changes a score until the host explicitly applies an override.

### Copy Ideas

- “What would the model say?”
- “The board is frozen. Answers locked.”
- “You’re not guessing people. You’re guessing the AI.”
- “Not an exact match? The host can review it after reveal.”
- “Board generated before answers.”

## Environment Variables

Example:

```bash
VITE_API_BASE_URL=http://localhost:8080
```

## Local Development

Use Node.js 22 or newer with npm. From the repository root, install the exact dependency versions from `package-lock.json`:

```bash
make bootstrap
make client
```

For client-only development:

```bash
cd client
npm ci
npm run dev
npm run build
npm run test
```

Run `make verify` from the repository root to check Go formatting and vetting, backend and client tests, and the production client build together. GitHub Actions runs the client tests and build from the locked npm dependency tree on every push and pull request.

## MVP Definition

The client MVP is complete when:

- a host can create a room;
- players can join by code;
- the lobby updates through safe polling;
- the host can start a game;
- players see the current question;
- players can submit one answer per round;
- submitted state is obvious;
- the reveal screen shows the AI board;
- matched guesses and misses are shown;
- scores update correctly;
- the final scoreboard is displayed.
