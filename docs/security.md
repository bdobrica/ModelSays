# Public API abuse controls

The API applies in-process, fixed-window limits before room handlers run. Limits are layered by a privacy-preserving client-IP key, room code, player-token hash, action, and paid-provider budget. Each map has a hard key bound (`RATE_LIMIT_MAX_KEYS`, default 10,000); the oldest entry is evicted when that bound is reached. Rejections are deterministic HTTP `429` JSON responses:

```json
{
  "error": "rate limit exceeded",
  "code": "rate_limited",
  "scope": "create",
  "retryAfterSeconds": 60
}
```

`Retry-After` contains the same whole-second delay. Health/readiness endpoints, CORS preflight, and the internal deadline worker are outside these limits. An SSE connection is checked before it occupies a stream slot; the existing global live-connection ceiling remains a second layer.

## Threat model and controls

| Threat | Control |
| --- | --- |
| Room creation floods | global client-IP and create-per-IP windows |
| Room-code enumeration and session-token guessing | client-IP lookup/session windows; invalid rooms and credentials retain their existing non-secret errors |
| Join floods or room-code targeting | join-per-IP and join-per-room windows |
| Guess spam | client-IP, room, and player-token-hash guess windows; repository duplicate/cutoff checks remain authoritative |
| Host mutation/token guessing | client-IP, room/action, and player-token-hash action windows plus existing host authorization |
| Event connection exhaustion | client-IP and room connection-attempt windows plus `EVENT_MAX_CONNECTIONS` |
| Provider-cost amplification | existing per-game call/cost budget plus global and per-room paid-call circuit breakers; denial makes no paid call and uses curated generation or deterministic misses |
| Abusive room/display names and submitted answers | bounded English whole-word deny list after existing rune/control-character validation; rejected with HTTP `422`, never silently scored or mutated |
| Forwarded-header spoofing | forwarded identity is ignored unless the immediate peer is in `TRUSTED_PROXY_CIDRS`; malformed chains fail closed to the peer address |

Rate limiting reduces brute force and resource exhaustion; it is not authentication. Player tokens remain bearer credentials and must be protected by TLS in a public deployment. Room codes are invitations, not secrets.

## Proxy trust

The default trusted-proxy list is empty. In that mode `X-Forwarded-For` is always ignored and the TCP peer address is used. Behind a known reverse proxy, configure only its exact CIDR(s), for example:

```dotenv
TRUSTED_PROXY_CIDRS=10.20.0.0/24
```

The server walks a syntactically valid `X-Forwarded-For` chain from the trusted peer toward the client and selects the first untrusted address. Never use `0.0.0.0/0` or `::/0`. Ensure the proxy overwrites, rather than appends to, client-supplied forwarding headers.

Client IPs are not stored in API responses, logs, or PostgreSQL. Limiter keys use a process-random salted hash and disappear on restart. Player tokens are also represented only by truncated hashes inside the bounded in-memory limiter.

## Configuration

Each policy has `<PREFIX>_REQUESTS` and `<PREFIX>_WINDOW_SECONDS`. Defaults are:

| Prefix | Requests/window | Scope |
| --- | ---: | --- |
| `RATE_LIMIT_IP` | 600/minute | all public API requests per client |
| `RATE_LIMIT_CREATE` | 10/minute | room creation per client |
| `RATE_LIMIT_LOOKUP` | 360/minute | room reads and session recovery per client |
| `RATE_LIMIT_JOIN_IP` | 30/minute | joins per client |
| `RATE_LIMIT_JOIN_ROOM` | 30/minute | joins per room |
| `RATE_LIMIT_PLAYER_ACTION` | 60/minute | authenticated mutations per player and room/action |
| `RATE_LIMIT_GUESS_PLAYER` | 12/10 seconds | guesses per player |
| `RATE_LIMIT_GUESS_ROOM` | 180/minute | guesses per room |
| `RATE_LIMIT_EVENT_IP` | 20/minute | event connection attempts per client |
| `RATE_LIMIT_EVENT_ROOM` | 20/minute | event connection attempts per room |
| `PROVIDER_LIMIT_GLOBAL` | 120/hour | paid calls in this server process |
| `PROVIDER_LIMIT_ROOM` | 20/hour | paid calls per room in this server process |

`MODERATION_DENY_WORDS` is a comma-separated English whole-word list. Replacing it is an operator decision: test locale-specific false positives before deployment. Do not put private user data in this list.

The limit store and provider circuit breakers are process-local. This is bounded and fail-open across restarts, which preserves party play, but limits are multiplied by the number of API replicas. Until a shared limiter is implemented, deploy one API replica or divide configured limits by the replica count and enforce an additional edge limit. FUTURE-04B owns limiter metrics, privacy-safe rejection logs, and multi-instance operational evidence.
