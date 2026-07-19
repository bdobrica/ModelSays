# Model provider operations and privacy

Provider calls are governed by server-owned settings. A room cannot select a model outside `ALLOWED_PREDICTION_MODELS`. Calls have a 10-second default timeout, generation is attempted at most twice, and a game is bounded to 20 paid calls and an estimated USD 0.10. When the paid path is unavailable or its call/cost budget is consumed, curated generation remains available and is recorded as `curated_fallback` with zero provider cost.

Pricing is an operational estimate, not billing authority. Review the allowlist and the estimate in `internal/llm/openai_client.go` together when adding a model or when provider pricing changes.

## Audit records

Migration `000006_provider_call_audits.sql` adds room/game/round-scoped records for purpose, provider, model, prompt version, provider request ID, attempt/path, outcome/error category, latency, token counts, estimated cost, timestamps, and retention classification. API keys are never persisted.

Public room JSON never contains audit data. The host may retrieve its room history with:

```http
GET /api/rooms/{code}/provider-audits
X-Player-Token: <host player token>
```

Missing, non-host, and wrong-room credentials receive `403`. This is a room-host operational boundary; a separate deployment-admin identity remains future work.

## Raw responses and retention

`MODEL_CAPTURE_RAW_RESPONSES` defaults to `false`, leaving `rawResponse` empty. Enabling it is an explicit privacy decision: responses are best-effort redacted, capped by `MODEL_RAW_RESPONSE_MAX_BYTES`, marked `provider_audit_30d`, and visible only through the authenticated audit endpoint. Operators must delete retained raw responses within 30 days until automated cleanup is added.

The server never logs raw provider bodies or API keys. Remote provider error messages are intentionally not copied into Go errors.

## Migration and rollback

Apply migration 000006 before deploying code that writes audits. It is additive and does not change room projections or deterministic no-key behavior. Its down migration drops only `provider_call_audits` and its index, permanently deleting audit history; roll back the application before running it.
