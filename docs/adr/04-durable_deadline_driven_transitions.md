# ADR-004: Durable deadline-driven transitions

- Status: Accepted
- Date: 2026-07-19
- Decision owners: Model Says maintainers
- Derived from: FUTURE-03 and FUTURE-07

## Context

Browser timers and host presence are unreliable authorities for answer cutoffs and phase changes. Living-room play additionally needs automatic reveal and advancement when everyone answers, without subtracting the reveal pause from the following round.

## Decision

Persist phase deadlines and execute transitions through a server-side worker using transactional locks, idempotent state checks, and auditable actor/reason/revision records. Answering, reveal pause, and the next answering phase have independent timestamps. A new round receives its complete configured answer duration only after the reveal phase ends. Multiple workers and restarts may observe work, but only one transition commits.

## Consequences

- Clients display server deadlines and correct for local clock drift; they do not advance the game.
- New automatic modes must use the same transition machinery.
- Tests need exact timing-boundary, restart, and concurrent-worker coverage.
- Operational readiness includes transition-lag and worker-health metrics.

## Evidence

Implemented by `3492cd5` and `d7581e3`; timing semantics are documented in `docs/deadline-transitions.md`.
