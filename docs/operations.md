# Production operations

This is the operational contract for the supported public-beta topology: one Linux host, one backend process, the built client served by a TLS reverse proxy, and PostgreSQL 16. It is an example and a runbook, not evidence that Model Says has been deployed to production. One API replica remains required because abuse counters are process-local.

## Deploy and migrate

Inject secrets through the host service manager or a root-readable environment file; never bake `.env`, `DATABASE_URL`, `OPENAI_API_KEY`, or `METRICS_TOKEN` into an image. Set `APP_ENV=production`, an HTTPS browser origin in `CORS_ALLOWED_ORIGINS`, and a random metrics bearer token. Startup rejects a missing database/metrics token, malformed origins, unsupported environment, or raw provider-response capture in production.

Use expand/contract migrations:

1. Back up and verify the dump listing with `make ops-backup BACKUP_DIR=/secure/path`.
2. Apply backward-compatible additive migrations with `make migrate-up`.
3. Start one new backend, then require `/healthz` and `/readyz` to pass.
4. Serve the new client after the API is ready.
5. Remove old columns or behavior only in a later release after every old process is gone.

Never run `migrate-down` against production merely to roll back application code. Prefer rolling the binary/client forward or back while retaining an additive schema. A migration rollback is allowed only when its down SQL has passed a disposable restore rehearsal, no newer process is running, a verified backup exists, and data loss is explicitly accepted.

On `SIGTERM`, the backend stops accepting connections, cancels event streams and the transition loop, drains active HTTP work, and confirms no transition repository pass remains, bounded by `SHUTDOWN_TIMEOUT_SECONDS`. The load balancer should remove readiness before sending the signal and allow at least that timeout. SSE clients recover from the durable revision log or polling.

## Signals

`GET /healthz` is process-only liveness. `GET /readyz` checks PostgreSQL connectivity, the clean current migration version, and recent successful transition-worker processing. A failure returns only `not_ready`; details remain in private logs. The expected version is `db.LatestSchemaVersion` and must advance with the highest numbered migration.

`GET /metrics` uses `Authorization: Bearer $METRICS_TOKEN` when configured and must not be exposed publicly. Metrics cover bounded HTTP route/status/latency, active rooms and event connections, subscriptions/reconnects, transition passes/latency, database-pool state, provider outcomes/tokens/estimated cost, and limiter decisions. Labels are fixed categories—never room codes, IPs, player IDs, tokens, prompts, or guesses.

JSON logs use `request_id`, bounded route/method/status/duration, transition outcomes, provider purpose/outcome/path/usage/cost, database startup/readiness errors, and rejected limiter scope. Request bodies, forwarding addresses, room codes, player credentials, provider keys, raw provider content, and unrevealed guesses are never logged. Forward `X-Request-ID` from the trusted proxy or let the backend create one.

Alert initially on readiness failure for two checks, HTTP 5xx above 2%, p95 request latency above 500 ms, transition failure or p95 above one second, database acquired connections near maximum, provider cost/circuit rejections above the configured budget, and sustained limiter rejections. Tune only from measured beta evidence.

## Secrets and origins

Rotate a provider or metrics secret by installing the new environment value, draining/restarting the single backend, checking readiness, then revoking the old value. Rotate database credentials by adding a new PostgreSQL role/password, switching and checking the process, then revoking the old role. `CORS_ALLOWED_ORIGINS` is an exact comma-separated allowlist; change it and restart before changing the browser hostname. Configure `TRUSTED_PROXY_CIDRS` only for the directly connected proxy network.

## Backup, restore, and retention

`make ops-backup` creates a PostgreSQL custom-format dump and validates its catalog. Store encrypted copies outside the host and define owner-tested RPO/RTO; the public-beta starting target is daily backups retained 14 days and a monthly restore drill. A backup is not verified until it restores and the migration, active room, completed room, provider audits, events, and transitions can be queried.

`make ops-restore BACKUP_FILE=... RESTORE_DATABASE=modelsays_restore_drill` is a dry run. Destructive recreation occurs only when the separately supplied `CONFIRM_RESTORE` exactly matches the constrained disposable database name. Never point the restore drill at the live database.

Provider audits/raw responses are classified for 30-day retention. `make ops-retention RETENTION_DAYS=30` only reports eligible rows. Deletion requires separate authorization and `APPLY_RETENTION=yes`. Backups expire under the backup policy; logs/metrics should use the platform retention policy and contain no user content. Public replay links use random 128-bit identifiers and currently remain available for as long as their completed game is retained; an absent/deleted identifier returns the same not-found response as an invalid one. Game/replay deletion or anonymization is not automated yet: respond to a request by restricting access, locating records under controlled database access, recording approval, deleting in dependency order, and verifying backups age out. Do not improvise destructive SQL during an incident.

## Incident response

1. Declare the incident, timestamp it, name an owner, and preserve privacy-safe logs/metrics.
2. Reduce impact: remove readiness, disable paid providers by removing `OPENAI_API_KEY`, or set `AUTO_REVEAL_ENABLED=false` only when manual reveal is acceptable.
3. Diagnose by request ID and bounded signal categories. Never paste tokens, database URLs, raw provider responses, or guesses into tickets/chat.
4. Recover with curated fallback, process restart, database failover/restore, or application rollback under the rules above.
5. Verify health/readiness, one complete room lifecycle, event reconnect, transition processing, and budget/limiter behavior.
6. Record timeline, user impact, data exposure assessment, corrective work, and evidence in `docs/release-evidence.md`.

Database interruption makes readiness fail and mutations unavailable; do not switch to in-memory storage. Provider interruption must retain curated generation/deterministic judging. If migration compatibility fails, keep the process out of service and correct the deployment order.
