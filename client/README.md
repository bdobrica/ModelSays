# Model Says — Client

Model Says is a real-time party game where players try to guess what an AI model thinks the most common answers are.

This client is a React app for joining rooms, playing rounds, submitting answers, watching reveals, and viewing scores. It should be optimized for parties, Zoom/team-building sessions, and casual play.

The current creation form follows the backend MVP contract: simultaneous mode, 1–5 rounds, a 15–120 second timer, English locale, and the default `gpt-4.1-mini` prediction model. Room and display-name inputs are capped at the server's 48- and 24-character limits. Invite links stop accepting new players once the host starts the game.

The playable client uses one request at a time with sequenced polling, refetches after every mutation, slows polling in a hidden tab, and stops it after completion. It validates a matching local token with the backend on refresh instead of trusting the stored host role. The answering view derives its countdown from the server deadline and disables local input at expiry; the server remains authoritative. Final scores are ranked with winners and ties identified. WebSockets, presence heartbeats, and cross-device session transfer are deferred.

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
- WebSocket client
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

The current MVP uses REST for actions and resilient polling for live game state. WebSockets are a post-MVP transport improvement.

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

### WebSocket

Expected incoming events:

- room updated;
- player joined;
- player left;
- game started;
- round started;
- timer updated;
- guess submitted;
- round revealed;
- score updated;
- game ended;
- error.

The client should recover gracefully from WebSocket disconnects by refetching room state.

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
- the host can correct disputed matches after reveal.

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
VITE_WS_URL=ws://localhost:8080/ws
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
- the lobby updates in real time;
- the host can start a game;
- players see the current question;
- players can submit one answer per round;
- submitted state is obvious;
- the reveal screen shows the AI board;
- matched guesses and misses are shown;
- scores update correctly;
- the final scoreboard is displayed.
