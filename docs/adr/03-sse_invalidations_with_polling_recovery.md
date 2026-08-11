# ADR-003: SSE invalidations with polling recovery

- Status: Accepted
- Date: 2026-07-19
- Decision owners: Model Says maintainers
- Derived from: FUTURE-02A and FUTURE-02B

## Context

Polling alone delayed visible multiplayer changes and amplified request volume. Gameplay mutations remain ordinary authenticated HTTP requests, so a bidirectional socket protocol was not justified. Mobile networks and sleeping tabs still require recovery from disconnects and missed events.

## Decision

Use authenticated Server-Sent Events to publish ordered, room-scoped invalidations. Events contain revision and transition metadata but no hidden gameplay content; clients refetch authoritative REST projections. Reconnect uses event IDs where available, detects gaps, and performs a full refetch. Bounded fallback polling remains active when the stream is unavailable or stale.

## Consequences

- REST projections and mutation endpoints remain authoritative and independently testable.
- Event transport cannot become a second gameplay state model.
- All new room behavior benefits from push updates if it increments revisions correctly.
- Operational metrics must distinguish healthy streams, reconnects, gaps, and polling fallback.

## Evidence

Implemented by `136082d` and `5756126`; the public event contract is documented in `docs/events.md`.
