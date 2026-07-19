# Durable deadline transitions

PostgreSQL-backed servers automatically reveal an answering round when server time is equal to or later than `answerPhaseEndsAt` plus `AUTO_REVEAL_GRACE_SECONDS`. The default grace period is zero. Submissions remain closed at the original answer deadline; grace delays reveal only.

Automatic reveal publishes the already-frozen board and persisted guesses/scores. It does not advance to the next round or complete the game. The revealed phase is an unlimited host review window: the host can inspect semantic suggestions, apply auditable overrides, and then explicitly select **Next round**. Early host reveal remains supported.

## Durability and concurrency

Each worker queries due PostgreSQL rounds in deadline order and claims their rows with `FOR UPDATE SKIP LOCKED`. The state change, immutable `round_transitions` audit record, room revision, and `round_revealed` invalidation commit in one transaction. A unique `(round_id, action)` constraint and the answering-state predicate make delivery idempotent. Competing workers skip a claimed row; a process or database failure rolls back and releases the row lock so another worker or a restarted server can retry.

The audit record distinguishes `actor=host, reason=host_reveal` from `actor=scheduler, reason=answer_deadline_elapsed`. A submission waiting on the same round lock is rechecked against the authoritative cutoff. A host reveal racing the worker has one winner; the loser observes the already-revealed state and cannot create another transition or event.

## Configuration and operations

- `AUTO_REVEAL_ENABLED=true` enables processing. Set it to `false` as an emergency rollback to retain manual host reveal.
- `AUTO_REVEAL_GRACE_SECONDS=0` delays reveal after the answer cutoff without reopening submissions.
- `TRANSITION_POLL_INTERVAL_MS=250` controls due-work discovery.
- `TRANSITION_BATCH_SIZE=25` bounds one transaction (maximum 100).

`GET /healthz` reports process liveness. `GET /readyz` reports `503` until the enabled worker has completed a successful database pass and whenever it fails or becomes stale. Automatic processing requires PostgreSQL; an enabled in-memory server remains live but not ready. Disabling automation makes readiness independent of the worker and preserves the prior manual flow.

Migration `000009_round_transitions.sql` is additive. Rollback removes only transition audit history and the due-round index; disable automation before rolling it back.
