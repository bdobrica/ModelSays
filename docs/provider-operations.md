# Model provider operations and privacy

Provider calls are governed by server-owned settings. A room cannot select a model outside the prediction, question, and judge allowlists. Calls have a 10-second default timeout, each operation is attempted at most twice, and generation plus semantic judging share a game-wide bound of 20 paid calls and an estimated USD 0.10. Process-wide and per-room fixed-window circuit breakers are checked before every paid attempt; an open circuit records a private `budget_exhausted`/`circuit_breaker` audit and makes no paid request. When paid generation is unavailable, curated generation remains available; when judging is unavailable or over budget, the deterministic miss remains authoritative.

The circuit defaults are 120 paid attempts per server process per hour and 20 per room per hour. Configure them with `PROVIDER_LIMIT_GLOBAL_REQUESTS`, `PROVIDER_LIMIT_GLOBAL_WINDOW_SECONDS`, `PROVIDER_LIMIT_ROOM_REQUESTS`, and `PROVIDER_LIMIT_ROOM_WINDOW_SECONDS`. These process-local safeguards supplement, rather than replace, per-game persisted budgets. Multi-replica deployment constraints and the full abuse threat model are documented in [`security.md`](security.md).

Pricing is an operational estimate, not billing authority. Review the allowlist and the estimate in `internal/llm/openai_client.go` together when adding a model or when provider pricing changes.

## Audit records

Migration `000006_provider_call_audits.sql` adds room/game/round-scoped records for purpose, provider, model, prompt version, provider request ID, attempt/path, outcome/error category, latency, token counts, estimated cost, timestamps, and retention classification. API keys are never persisted.

Public room JSON never contains audit data. The host may retrieve its room history with:

```http
GET /api/rooms/{code}/provider-audits
X-Player-Token: <host player token>
```

Missing, non-host, and wrong-room credentials receive `403`. This is a room-host operational boundary; a separate deployment-admin identity remains future work.

## Semantic judge review

`DEFAULT_JUDGE_MODEL` must appear in `ALLOWED_JUDGE_MODELS`. The judge receives only the frozen question/board and one submitted deterministic miss. Its `judge-v1` response is restricted to a frozen answer ID or null, confidence from 0 to 1, and a rationale category. Suggestions are stored by migration `000007_judge_suggestions.sql`, separately from authoritative guesses and score events.

Room responses never include judge data. After reveal, the host retrieves review records with:

```http
GET /api/rooms/{code}/rounds/{roundId}/judge-suggestions
X-Player-Token: <host player token>
```

High confidence starts at `0.85`, medium at `0.60`, and lower values are low. These bands affect highlighting only. The host accepts the suggestion, chooses another answer, or retains the miss through `POST /api/rooms/{code}/override-match`; the locked override transaction enforces duplicate claims and records score changes. Timeout, malformed output, budget exhaustion, and provider absence never alter the deterministic result.

## Raw responses and retention

`MODEL_CAPTURE_RAW_RESPONSES` defaults to `false`, leaving `rawResponse` empty. Enabling it is an explicit privacy decision: responses are best-effort redacted, capped by `MODEL_RAW_RESPONSE_MAX_BYTES`, marked `provider_audit_30d`, and visible only through the authenticated audit endpoint. Operators must delete retained raw responses within 30 days until automated cleanup is added.

The server never logs raw provider bodies or API keys. Remote provider error messages are intentionally not copied into Go errors.

## Migration and rollback

Apply migrations 000006 and 000007 before deploying this code. Both are additive and do not change public room projections or deterministic no-key scoring. Rolling back 000007 deletes judge-review history; rolling back 000006 deletes provider-audit history. Roll back the application before running either down migration.
