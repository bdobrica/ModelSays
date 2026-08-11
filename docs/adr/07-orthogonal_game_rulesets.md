# ADR-007: Model game rules separately from pacing modes

- Status: Accepted
- Date: 2026-08-11
- Decision owners: Model Says maintainers
- Extends: ADR-005

## Context

The existing `mode` field describes how players are organized and how rounds advance: simultaneous, teams, sequential, or living-room. Open-answer and four-choice trivia instead change question content, submission shape, correctness, and scoring. Encoding both concerns in one enum would create combinations such as `livingroom_trivia_choice` and duplicate transition and presentation logic.

## Decision

Add an orthogonal `gameKind` field with `model_says`, `trivia_open`, and `trivia_choice`. `mode` remains the pacing/player-organization axis. Missing persisted or API values default to `model_says` for backward compatibility, and rooms copy both axes into the game and replay records.

The initial compatibility matrix is:

| Game kind | Simultaneous | Teams | Sequential | Living room |
| --- | --- | --- | --- | --- |
| Model Says | Supported | Supported | Supported | Supported |
| Open Trivia | Supported | Supported | Deferred | Supported |
| Choice Trivia | Supported | Supported | Deferred | Supported |

Unsupported combinations fail room validation. Team mode is admitted because it aggregates individual score events without changing trivia correctness. Sequential trivia is deferred until its turn and submission interaction has dedicated lifecycle evidence.

## Consequences

- Generation, frozen content, scoring, projections, replay, and UI branch on game kind only where rules differ.
- Timers, SSE invalidations, teams, living-room automation, and participant roles continue to branch on mode.
- Existing rooms, games, clients, and replays remain Model Says by default.
- Adding a future ruleset does not require multiplying pacing modes.
