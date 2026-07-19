# FUTURE-04A abuse-control evidence

Date: 2026-07-19
Environment: local PostgreSQL 16, Go race detector, curated provider path

## Acceptance evidence

- Fixed-window unit tests cover exact capacity, deterministic retry duration, reset, key isolation, the 10,000-key memory ceiling, 100 concurrent attempts, trusted/untrusted/malformed proxy chains, moderation whole-word behavior, and global/room provider circuits.
- HTTP tests cover a successful boundary request followed by a `429` with matching `Retry-After`/JSON metadata, moderation before mutation, and unlimited health checks.
- Service tests prove an open provider circuit makes zero primary-client calls, records the private circuit decision, and completes with the curated board.
- The representative policy test admits 12 players sharing one client address, including 240 projected room lookups per minute, joins, and guesses, while remaining within the bounded key store.
- PostgreSQL-backed `make verify` and PostgreSQL-backed `go test -race ./...` passed. No migration was added.
- Two `make baseline` runs completed all 3-, 8-, and 12-player curated workloads. Request, poll, query, and bundle counts were identical. The 12-player workload projected 240 polls/minute, completed 62 HTTP requests and 767 database queries, and observed mutation visibility of 19.291 ms then 19.842 ms. No external provider calls were made.

The final shared-address lookup ceiling is 360/minute, leaving 50% headroom over the PB-00 three-second, 12-player polling projection. The global client ceiling is 600/minute. Tighter action-specific limits handle creation, joining, guesses, event connection attempts, and provider calls.

## Security review

Limiter responses contain a stable error code, scope category, and retry delay only. They never include the source address, its salted hash, room/player credentials, or request body. Existing server logs do not log successful request bodies or limiter keys; hidden guesses and player tokens therefore remain absent. Provider circuit decisions use the existing private host-only audit boundary and store no secret or raw request content.

This implementation uses bounded process-local state. A public beta must use one API replica or compensate with trusted edge limits until shared limiter storage and multi-instance operational evidence are added. Observability and privacy-safe limiter decision metrics remain assigned to FUTURE-04B.
