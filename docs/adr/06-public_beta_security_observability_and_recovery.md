# ADR-006: Public-beta security, observability, and recovery gates

- Status: Accepted
- Date: 2026-07-19
- Decision owners: Model Says maintainers
- Derived from: PB-00, FUTURE-04A, FUTURE-04B, FUTURE-05D

## Context

A multiplayer service exposed beyond local development needs abuse resistance, diagnosable failures, recovery procedures, accessibility, and evidence that changes remain within latency and bundle budgets. Feature completeness alone is not a release criterion.

## Decision

Public endpoints and provider usage have scoped limits with trustworthy proxy handling. Structured logs and metrics cover requests, provider calls/cost, rooms, event transport, and transition lag without leaking credentials or hidden guesses. Health/readiness, migration rollback, backup/restore, retention, moderation, incident response, and staging playtests are documented and rehearsed. CI runs backend, client, PostgreSQL lifecycle, migration, race, secrecy, and production-build checks without external model calls.

Accessibility and responsive behavior are release requirements: keyboard operation, screen-reader feedback, focus management, color-independent state, touch targets, reduced motion, and tested phone/TV/desktop layouts. Performance changes are compared with recorded PB-00 baselines and release evidence.

## Consequences

- Each feature commit includes tests and affected public documentation.
- Release steps require automated evidence plus authorized human playtesting for real-device behavior.
- Secrets, raw hidden content, and pre-reveal results remain excluded from logs/metrics.
- Failed gates block a commit rather than being documented as success.

## Evidence

Implemented through `a62f9b7`, `45d2d77`, `3e646a9`, `ac3cf1e`, and `d7decf6`. Supporting records live under `docs/` and `docs/baselines/`.
