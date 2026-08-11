# ADR-001: Server-authoritative persisted gameplay

- Status: Accepted
- Date: 2026-07-19
- Decision owners: Model Says maintainers
- Derived from: completed MVP and post-MVP plans

## Context

Multiplayer clients can race, reconnect, refresh, or disagree about timers. Hidden boards and score changes must not depend on browser state. The application also needs restart-safe scores, replays, and auditable host corrections.

## Decision

PostgreSQL and the Go service are authoritative for room identity, frozen round content, deadlines, one-submission rules, matching, score events, transitions, and replay history. Clients submit validated intent and render server projections; they never calculate authoritative awards. Mutations use transactional and idempotent repository boundaries, with constraints or locked claims where concurrency can otherwise double-award.

Round content is frozen before answering and hidden fields are omitted—not merely visually concealed—from public projections until reveal. Overrides create auditable deltas rather than rewriting history invisibly. The in-memory repository follows the same domain contract for fast tests but is not the production authority.

## Consequences

- Refreshes, concurrent servers, and backend restarts reconstruct the same game.
- New rulesets must define their scoring inside the server transaction rather than reuse client assumptions.
- Schema changes require backward-compatible migrations and reconstruction tests.
- More backend lifecycle coverage is required, but UI behavior becomes simpler and safer.

## Evidence

The completed MVP steps established atomic answer claims, secrecy projections, durable score events, lifecycle reconstruction, replays, and host overrides. Relevant implementation commits include `7a9dffe`, `5812b9e`, `8dc9335`, and `e7a5ff9`; the remaining decision history is summarized by the other ADRs in this directory.
