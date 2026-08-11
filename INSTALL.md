# Install and Play Model Says

This guide runs the complete game locally: PostgreSQL in Docker, the Go API, and the React client. An OpenAI key is optional; without one, the game uses its five curated English rounds.

## Requirements

Install:

- Git;
- Go 1.25 or newer;
- Node.js 22 or newer with npm;
- Docker Engine 24 or newer;
- Docker Compose v2 (`docker compose version`).

Ports `5173`, `8080`, and `5432` must be available. Docker must be running before installation.

## Install and start

Clone the repository and enter it:

```bash
git clone <repository-url>
cd ModelSays
```

Create your local configuration:

```bash
cp .env.example .env
```

The defaults work with the included Docker Compose database. To use generated questions and boards, set `OPENAI_API_KEY` in `.env`; leave it empty to use the curated offline content. Provider calls remain bounded by allowlist, timeout, retry, per-game budgets, and process-wide/room circuit breakers. Public API limits and English whole-word moderation are enabled by default. Leave `TRUSTED_PROXY_CIDRS` empty for direct local access; configure it only for exact proxy networks you control. See [`docs/security.md`](docs/security.md).

Install the locked Go and npm dependencies, start PostgreSQL, apply migrations, and launch both servers:

```bash
make install
```

The first run downloads dependencies and may take a few minutes. Keep this terminal open. When startup completes:

- game: `http://localhost:5173`
- API liveness: `http://localhost:8080/healthz`
- API readiness (including automatic reveal processing): `http://localhost:8080/readyz`

Local installation is not a production deployment. Production mode validates database, CORS, metrics authentication, and raw-provider-capture safety at startup. Follow [`docs/operations.md`](docs/operations.md) for migration order, graceful drain, secrets, backup/restore, retention, and incident handling.

Press `Ctrl+C` to stop the backend and client. PostgreSQL continues in Docker so game state survives restarts. Stop it with:

```bash
make postgres-down
```

After the first installation, `make start` starts the stack without reinstalling dependencies.

## Play a game

Use separate browser profiles, private windows, or devices so each participant has separate browser storage.

1. The host opens `http://localhost:5173`, selects **Create room**, chooses **Model Says**, **Open Trivia**, or **Choice Trivia** separately from individual, team, sequential, or living-room pacing, enters the remaining settings, then creates the room. Trivia supports every pacing except sequential.
2. Select **Copy invite** and share the join link, or share the six-character room code before starting. Invite links pre-fill the code.
3. Each player opens **Join room**, enters the code and a display name, and joins a focused lobby that confirms their name and waits for the host.
4. The host waits for everyone to appear. In team mode, create 2–4 teams and assign every player; every team must have a player. Then select **Start game**. Team assignments cannot change afterward.
5. Each participant submits one answer before the countdown expires. Model Says asks for a board guess, Open Trivia asks for the single correct answer, and Choice Trivia asks the player to select one of four buttons. The frozen solution, correctness, and round scoring stay hidden during this phase.
6. At expiry the server reveals automatically; the host can reveal early if desired.
7. In Model Says, the host reviews the frozen board, matches, duplicate claims, and live scores while joined players see a short waiting state. In either trivia kind, every player sees the correct answer, their submitted answer or choice, explicit correct/incorrect text, awarded points, and current ranking; the host can audit every submission and mark it correct or incorrect.
8. The host selects **Next round**. After the final reveal, advancing once more opens the ranked final individual and, when applicable, team scoreboard on every device.

Only the earliest accepted guess matching a board answer earns its points. A later canonical or alias match for the same answer is shown as a zero-point duplicate. Automatic matching ignores capitalization, punctuation, and repeated whitespace. Optional judge suggestions are advisory, hidden until reveal, and never score without a host override.

Open Trivia has no first-claim rule: every correct participant receives 100 points. Matching is intentionally conservative and accepts only the frozen canonical answer or explicit aliases after case, punctuation, whitespace, and Unicode normalization. Near answers score zero unless the host corrects the revealed result.

Choice Trivia also awards every correct participant 100 points. The four options retain their server-issued order. Selecting a button immediately submits its opaque identifier and locks the grid; the correct option is identified only after reveal. At ordinary widths the choices form a 2×2 grid and collapse to one column on the narrowest supported phones.

In team mode, guesses and answer claims remain individual and global: a player on one team can claim an answer before every other team. Team totals are the sum of member score events, so host overrides update individual and team results together.

In sequential mode, players act in lobby join order. The timer resets for each player; submit or choose **Pass turn** to advance immediately. Prior raw claims are visible, but their match and score outcomes stay hidden. An expired or disconnected player's turn is passed automatically, and the last turn reveals the round. Sequential and team modes cannot be combined.

In living-room mode, open the creator session on the shared TV and optionally select **Fullscreen**. Players scan the displayed QR (or use its ordinary join link/code) and enter names on their phones. The QR contains only the same-origin join URL—never a session token or replay/provider data. Once at least two people have joined, select **Start game**. The display cannot answer and is absent from rankings. The TV uses the full viewport for the complete question and shared timer, reveals early when everyone answers or at the deadline, keeps results readable for the configured pause, then gives the following round its full timer or completes automatically. Phones show a waiting state during TV reveal and the final scoreboard at completion. Select **Back to home** on the TV scoreboard to create or join another game.

Refreshing the same browser restores its player session. Do not clear browser storage during a game. Cross-device session transfer is not currently supported.

Joined-player screens intentionally omit room settings, roster, and management controls. During answering they show only the current question/timer/action, mode-required sequential prior claims, and clear submitted, expired, reconnecting, or waiting feedback. Model Says keeps its board/review on the host display; both trivia kinds reveal the correct answer, the player's own answer/result, points, and current ranking to that player. The host keeps the complete operational view and correction controls.

When the final round is complete, use **Share results** to share or copy the unguessable replay link. The replay shows winners/ties and every revealed round without exposing session credentials or private provider records. The host can select **Play again** to create a fresh lobby with the same room name and settings; other players join the new code, and no prior guesses, claims, boards, deadlines, scores, or credentials carry over.

For a projector or screen share, the host can select **Presentation mode**. The browser enters fullscreen, removes setup details, and enlarges revealed answers while keeping host controls available. Press `Esc` or select **Presentation mode** again to exit. Keyboard users can use the initial **Skip to game content** link; phase changes move focus to the current phase heading, and live states are announced. The reveal respects the operating system/browser reduced-motion preference. See [`docs/accessibility.md`](docs/accessibility.md) for the supported viewport contract and release checklist.

## Optional OpenAI content

Edit `.env`:

```bash
OPENAI_API_KEY=your-development-key
```

Then restart `make install` or `make start`. Model output is validated, retried once, and falls back to an unused curated round if generation fails. Never commit `.env`.

Raw provider response capture is disabled by default. See [`docs/provider-operations.md`](docs/provider-operations.md) before changing provider limits or retention settings.

The browser uses authenticated SSE invalidations for prompt updates and automatically falls back to polling if streaming is unavailable. The room page shows a non-blocking live/reconnecting/offline indicator. If it remains in reconnecting mode, verify that reverse proxies do not buffer `text/event-stream`, preserve `X-Player-Token` and `Last-Event-ID`, and allow long-lived responses. Play can continue through polling recovery. See [`docs/events.md`](docs/events.md) for the event contract and connection-limit configuration.

PostgreSQL reveals expired rounds automatically. `LIVINGROOM_REVEAL_PAUSE_SECONDS` controls the living-room result pause and must be 3–30 seconds (default `8`). `AUTO_REVEAL_GRACE_SECONDS` may delay reveal without extending the answer cutoff. `GET /readyz` fails if the enabled transition worker cannot complete database passes. For emergency manual-only play, set `AUTO_REVEAL_ENABLED=false` and restart the backend. That emergency stop pauses living-room games until automation returns. See [`docs/deadline-transitions.md`](docs/deadline-transitions.md) before changing worker cadence, batch size, or rolling back transition migrations.

## Verification

Run the same formatting, vetting, test, and client-build gate used by CI:

```bash
make verify
```

To include PostgreSQL integration tests:

```bash
TEST_DATABASE_URL='postgres://postgres:postgres@localhost:5432/postgres?sslmode=disable' make verify
```

## Troubleshooting

If Docker or PostgreSQL does not start:

```bash
make postgres-logs
```

If port `5432` is already occupied, stop the other PostgreSQL service or adjust both the Compose port and `DATABASE_URL`.

If dependencies are missing or stale:

```bash
make bootstrap
```

If the backend reports `default model is not present in its server allowlist`, every customized `DEFAULT_*_MODEL` in `.env` must also appear in its matching `ALLOWED_*_MODELS` comma-separated list. For example, `DEFAULT_PREDICTION_MODEL=my-model` requires `ALLOWED_PREDICTION_MODELS=my-model`. The development launcher now stops both child servers when either one exits, so the original startup error remains visible instead of leaving a client with no API.

To erase all local database state and rebuild from empty migrations:

```bash
make postgres-reset
make start
```

`make postgres-reset` permanently deletes the local Docker database volume.
