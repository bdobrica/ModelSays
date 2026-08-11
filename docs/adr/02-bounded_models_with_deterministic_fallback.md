# ADR-002: Bounded model calls with deterministic fallback

- Status: Accepted
- Date: 2026-07-19
- Decision owners: Model Says maintainers
- Derived from: PB-00, FUTURE-01A, FUTURE-01B, FUTURE-08

## Context

AI-generated content makes Model Says distinctive, but provider latency, cost, malformed output, and missing credentials cannot make local play unreliable. Model output and optional semantic suggestions also must not silently alter authoritative scoring.

## Decision

Provider calls use strict versioned schemas, model allowlists, timeouts, bounded retries, per-game call/cost budgets, circuit breaking, redacted audit metadata, and disabled-by-default raw capture. Generated content is validated before persistence. Failure falls back to a validated curated provider, and curated questions are cryptographically sampled without replacement within a game.

Deterministic server matching remains authoritative. A semantic judge may suggest a post-reveal host correction for a deterministic miss, but it never awards points directly. Games remain fully playable without credentials or external network calls.

## Consequences

- Every new generated content kind needs a strict schema, validator, audit purpose, prompt version, and curated fallback.
- Tests use fakes and must prove malformed, timeout, budget, and fallback paths.
- Offline banks require editorial review and enough unique entries for the maximum round count.
- Provider sophistication cannot weaken secrecy or deterministic scoring.

## Evidence

Implemented by `8aa256d`, `905bbbb`, `cf2abc8`, and `9739ed1`; operational details live in `docs/provider-operations.md`.
