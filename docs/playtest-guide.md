# Structured Playtest Guide

Use this guide for facilitated tests of the existing simultaneous mode. Record room size, device/browser, timings, and direct observations. Do not convert opinions into measured facts.

For Open Trivia, additionally confirm the ruleset and pacing controls are understood as separate choices; the typed field is announced as a trivia answer; no solution or correctness appears before reveal; and reveal clearly communicates the canonical answer, personal answer, correct/incorrect result, points, and ranking. Ask the host to correct one result in each direction and verify the visible score follows the auditable server update.

For Choice Trivia, confirm exactly four options retain the displayed order, form a 2×2 grid on tablet/desktop and a usable single column at 360 px, work with keyboard and touch, submit only once under rapid activation, and retain the selected label while waiting. Inspect accessible names and the answering network payload for any correct-answer clue. After reveal, confirm text distinguishes the correct option, the player's choice, every incorrect option, award, and ranking.

## Before play

For a living-room check, use one TV/laptop creator browser and at least two separate phone/browser sessions. Record device/browser descriptions without names. Confirm the QR resolves to exactly the displayed same-origin join URL; the display cannot answer or appear in rankings; Open Trivia hides its solution and Choice Trivia shows four ordered, unmarked options while answering; all-submitted reveal happens once; a later round reaches deadline reveal; the solution, Choice answer labels, correctness, awards, and rankings remain readable for roughly the configured eight seconds while raw Open Trivia answers stay off the shared TV; disconnect/reconnect catches up; and the final scoreboard appears automatically. Exercise **Play again** and verify the clean lobby retains ruleset/pacing but gets a new code and newly sampled questions, then verify **Back to home**. Record scanner/readability observations as human evidence rather than automated claims.

- Ask each participant to explain the premise after reading the home page. Record whether they understand that they are predicting a model-generated board, not survey answers.
- Ask the host to create and share a room without coaching. Note hesitation, failed joins, and time to start.
- Include at least one phone-sized or physical mobile participant.
- Record anonymous device/browser descriptions and whether the host uses a projector, television, or screen-sharing software. Do not record participant names.

## During each round

- Record time from question visibility to the first and last submission.
- Ask whether the countdown, submitted state, expiry, reveal ownership, and duplicate rule are clear.
- Confirm joined-player phones never show host settings, roster, or management/review controls. Model Says reveal should show only the host-display waiting state; trivia reveal should show only the correct answer, that player's answer/result, award, and current ranking.
- Record every disputed deterministic match, the raw guess, expected answer, and whether the host override was discoverable and sufficient.
- Count host interventions and estimate host effort per round.
- Observe the live-update indicator during normal play, a backend restart, and a short offline/online cycle. Record mutation-to-visible delay, reconnect delay, stale state, and whether fallback polling is understandable.
- On a phone or throttled mobile-network profile, background and restore the tab. Confirm it catches up without revealing hidden state, duplicating actions, or requiring a manual refresh.
- Complete the keyboard-only, screen-reader, reduced-motion, and Presentation mode checks in [`accessibility.md`](accessibility.md).

## After play

Ask each participant:

1. Explain the scoring and duplicate rule in your own words.
2. Which moment felt slow or confusing?
3. Did any result feel wrong, and how should it have been handled?
4. Was the host doing too much? Which action should be automatic?
5. Could you comfortably play on your device?
6. Would you play another round? Why?
7. Choose one next improvement: results/replay, teams, sequential turns, faster live updates, semantic match suggestions, or something else.

## Evidence classification

- **Measured:** timestamps, request metrics, observed failures, task completion, intervention counts.
- **Reported:** participant statements or preferences, attributed anonymously.
- **Hypothesis:** a proposed explanation or product change requiring another test.

Summarize results in a copy of `docs/release-evidence.md`. Preserve outliers and negative findings.
