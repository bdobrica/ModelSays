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

The defaults work with the included Docker Compose database. To use generated questions and boards, set `OPENAI_API_KEY` in `.env`; leave it empty to use the curated offline content.

Install the locked Go and npm dependencies, start PostgreSQL, apply migrations, and launch both servers:

```bash
make install
```

The first run downloads dependencies and may take a few minutes. Keep this terminal open. When startup completes:

- game: `http://localhost:5173`
- API health check: `http://localhost:8080/health`

Press `Ctrl+C` to stop the backend and client. PostgreSQL continues in Docker so game state survives restarts. Stop it with:

```bash
make postgres-down
```

After the first installation, `make start` starts the stack without reinstalling dependencies.

## Play a game

Use separate browser profiles, private windows, or devices so each participant has separate browser storage.

1. The host opens `http://localhost:5173`, selects **Create room**, enters a name, chooses 1–5 rounds and a 15–120 second timer, and creates the room.
2. Share the six-character room code or the room URL before starting.
3. Each player opens **Join room**, enters the code and a display name, and joins the lobby.
4. The host waits for everyone to appear, then selects **Start game**.
5. Each participant submits one answer before the countdown expires. The answer board and round scoring stay hidden during this phase.
6. At expiry—or earlier if desired—the host selects **Reveal round**.
7. Everyone reviews the frozen board, matches, duplicate claims, and scores. The host can correct a disputed result with the override controls.
8. The host selects **Next round**. After the final reveal, advancing once more opens the ranked final scoreboard.

Only the earliest accepted guess matching a board answer earns its points. A later canonical or alias match for the same answer is shown as a zero-point duplicate. Matching ignores capitalization, punctuation, and repeated whitespace, but it is not semantic; the host override is the correction mechanism.

Refreshing the same browser restores its player session. Do not clear browser storage during a game. Cross-device session transfer is not currently supported.

## Optional OpenAI content

Edit `.env`:

```bash
OPENAI_API_KEY=your-development-key
```

Then restart `make install` or `make start`. Model output is validated, retried once, and falls back to an unused curated round if generation fails. Never commit `.env`.

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

To erase all local database state and rebuild from empty migrations:

```bash
make postgres-reset
make start
```

`make postgres-reset` permanently deletes the local Docker database volume.
