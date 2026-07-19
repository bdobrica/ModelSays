# Room event architecture

Model Says uses Server-Sent Events (SSE) for authenticated server-to-client room invalidations. SSE was selected because gameplay mutations remain authoritative REST requests and the server only needs to notify browsers that room state changed. Bidirectional WebSockets would add protocol and connection-state complexity without a current product requirement.

## Contract

Subscribe with a room-scoped player token in a header:

```http
GET /api/rooms/{code}/events
X-Player-Token: <player token>
Accept: text/event-stream
Last-Event-ID: <last applied room revision>
```

Credentials in `token` or `playerToken` query parameters are rejected so long-lived tokens do not enter URLs, browser history, proxy logs, or referrer data. Missing, invalid, or cross-room tokens return `403`. Browser origins must match `CORS_ALLOWED_ORIGINS`; non-browser clients may omit `Origin`.

Each message uses SSE event name `room_invalidation`. Its SSE `id` is the decimal room revision. The versioned JSON envelope is:

```json
{
  "version": 1,
  "id": "evt_opaque",
  "roomCode": "ABC234",
  "type": "player_joined",
  "roomRevision": 1,
  "occurredAt": "2026-07-19T12:00:00Z"
}
```

Public types are `player_joined`, `game_started`, `submission_progress_changed`, `round_revealed`, `score_changed`, `round_started`, and `game_completed`. Events contain no names, tokens, questions, boards, guesses, match results, scores, provider data, or judge suggestions. They are invalidations only: after an event, refetch `GET /api/rooms/{code}` and use that public projection as the source of truth.

## Ordering and recovery

Successful mutations commit before their event is appended. PostgreSQL increments the room revision and inserts the event atomically in a separate transaction. An event-publication failure never rolls back an already successful game mutation; existing polling/refetch remains the recovery path.

Events are ordered by monotonically increasing room revision and the most recent 1,000 events per room are retained. Reconnect with the last applied revision in `Last-Event-ID`; the server returns later events in order. Clients must tolerate duplicates and detect a revision gap by comparing the received revision to their last applied revision, then perform a full room refetch. A reconstructed backend reads the same durable event log, so reconnect works across restarts and instances.

## Connection behavior

The server sends comment heartbeats, bounds concurrent streams per process, applies a write deadline to disconnect slow consumers, and closes streams during graceful HTTP shutdown. Configure:

- `EVENT_POLL_INTERVAL_MS` — durable-log check interval, default `250`;
- `EVENT_HEARTBEAT_SECONDS` — heartbeat interval, default `15`;
- `EVENT_MAX_CONNECTIONS` — process-wide stream limit, default `100`;
- `EVENT_WRITE_TIMEOUT_SECONDS` — per-write slow-consumer deadline, default `5`.

The browser consumes this endpoint with a fetch-stream client because native `EventSource` cannot attach the required authentication header. It validates the exact versioned envelope, coalesces event bursts, discards duplicate and stale revisions, identifies gaps, and always refetches the public room projection rather than applying event data as state.

The last successfully refetched room revision is sent as `Last-Event-ID` on reconnect. Every successful connection performs a full refetch before applying replayed invalidations, and revision gaps take the same full-refetch path. Retry uses bounded exponential backoff from 500 milliseconds to 15 seconds. A stream failure is non-blocking: the browser reports its connection state and polls every five seconds until live updates recover, with a 30-second safety poll while live. Hidden tabs defer event-driven refetches and use a 30-second poll; visibility and network recovery trigger immediate reconnect/refetch, and completion or route exit closes the stream.
