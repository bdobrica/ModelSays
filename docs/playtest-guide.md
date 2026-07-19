# Structured Playtest Guide

Use this guide for facilitated tests of the existing simultaneous mode. Record room size, device/browser, timings, and direct observations. Do not convert opinions into measured facts.

## Before play

- Ask each participant to explain the premise after reading the home page. Record whether they understand that they are predicting a model-generated board, not survey answers.
- Ask the host to create and share a room without coaching. Note hesitation, failed joins, and time to start.
- Include at least one phone-sized or physical mobile participant.

## During each round

- Record time from question visibility to the first and last submission.
- Ask whether the countdown, submitted state, expiry, reveal ownership, and duplicate rule are clear.
- Record every disputed deterministic match, the raw guess, expected answer, and whether the host override was discoverable and sufficient.
- Count host interventions and estimate host effort per round.
- Observe the live-update indicator during normal play, a backend restart, and a short offline/online cycle. Record mutation-to-visible delay, reconnect delay, stale state, and whether fallback polling is understandable.
- On a phone or throttled mobile-network profile, background and restore the tab. Confirm it catches up without revealing hidden state, duplicating actions, or requiring a manual refresh.

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
