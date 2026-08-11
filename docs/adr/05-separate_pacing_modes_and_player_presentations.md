# ADR-005: Separate pacing modes and player presentations

- Status: Accepted
- Date: 2026-07-22
- Decision owners: Model Says maintainers
- Derived from: FUTURE-05B, FUTURE-05C, FUTURE-06, FUTURE-07

## Context

The product supports individual, team, sequential, and living-room play. These concepts affect different concerns: score aggregation, turn scheduling, automation, and what a host or participant sees. Treating every variation as ad-hoc page logic made it easy to leak operational detail to players or couple TV behavior to scoring rules.

## Decision

Keep pacing/player-organization modes explicit and server-validated: simultaneous, teams, sequential, and living-room. Joined participants receive a focused presentation showing only what they need in the active phase. Operational hosts retain controls and audit surfaces. Living-room creators are non-playing TV displays with QR onboarding, shared question/timer/reveal/rankings, and automatic server-owned pacing; phones reuse the focused participant experience.

Mode combinations are opt-in through a tested compatibility matrix. Independent rulesets should be modeled on a separate axis instead of multiplying pacing enum values.

## Consequences

- Adding trivia should introduce a ruleset/game-kind field rather than values such as `livingroom_trivia_choice`.
- Projection tests must cover role, phase, and mode boundaries.
- Shared client components should express presentation roles without moving authority into the UI.
- Unsupported combinations fail validation explicitly.

## Evidence

Implemented by `22f57f5`, `bf44414`, `01d9a8f`, and `d7581e3`.
