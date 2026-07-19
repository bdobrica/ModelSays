# Durable deadline transitions

PostgreSQL-backed servers automatically reveal an answering round when server time is equal to or later than `answerPhaseEndsAt` plus `AUTO_REVEAL_GRACE_SECONDS`. The default grace period is zero. Submissions remain closed at the original answer deadline; grace delays reveal only.

Automatic reveal publishes the already-frozen board and persisted guesses/scores. It does not advance to the next round or complete the game. The revealed phase is an unlimited host review window: the host can inspect semantic suggestions, apply auditable overrides, and then explicitly select **Next round**. Early host reveal remains supported.

In sequential mode the same worker uses `turnEndsAt`: an expired turn is recorded as a pass and advances to the next frozen player, while expiry of the final turn reveals. The configured grace applies to turn processing without reopening the expired turn.

## Durability and concurrency

Each worker queries due PostgreSQL rounds in deadline order and claims their rows with `FOR UPDATE SKIP LOCKED`. The state change, immutable `round_transitions` audit record, room revision, and invalidation commit in one transaction. Partial unique indexes allow one reveal per round and one advancement per turn index. Competing workers skip a claimed row; a process or database failure rolls back and releases the row lock so another worker or a restarted server can retry.

The audit record distinguishes host reveal and simultaneous deadline reveal from sequential advancement caused by a submission, explicit pass, or turn timeout. A submission waiting on the same round lock is rechecked against the authoritative cutoff and current player. A host reveal racing the worker has one winner; the loser observes the already-revealed state and cannot create another transition or event.

## Configuration and operations

- `AUTO_REVEAL_ENABLED=true` enables processing. Set it to `false` as an emergency rollback to retain manual host reveal.
- `AUTO_REVEAL_GRACE_SECONDS=0` delays reveal after the answer cutoff without reopening submissions.
- `TRANSITION_POLL_INTERVAL_MS=250` controls due-work discovery.
- `TRANSITION_BATCH_SIZE=25` bounds one transaction (maximum 100).

`GET /healthz` reports process liveness. `GET /readyz` reports `503` until the enabled worker has completed a successful database pass and whenever it fails or becomes stale. Automatic processing requires PostgreSQL; an enabled in-memory server remains live but not ready. Disabling automation makes readiness independent of the worker and preserves the prior manual flow.

Migration `000009_round_transitions.sql` introduced reveal transitions; `000012_sequential_mode.sql` adds persisted turn state and per-turn audit uniqueness. Disable automation before rolling either transition schema back.
