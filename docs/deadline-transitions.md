# Durable deadline transitions

PostgreSQL-backed servers automatically reveal an answering round when server time is equal to or later than `answerPhaseEndsAt` plus `AUTO_REVEAL_GRACE_SECONDS`. The default grace period is zero. Submissions remain closed at the original answer deadline; grace delays reveal only.

Automatic reveal publishes the already-frozen board and persisted guesses/scores. It does not advance to the next round or complete the game. The revealed phase is an unlimited host review window: the host can inspect semantic suggestions, apply auditable overrides, and then explicitly select **Next round**. Early host reveal remains supported.

In sequential mode the same worker uses `turnEndsAt`: an expired turn is recorded as a pass and advances to the next frozen player, while expiry of the final turn reveals. The configured grace applies to turn processing without reopening the expired turn.

In living-room mode, the participant set is frozen at start and excludes the persisted `host_display` role. The final accepted submission reveals under the same locked transaction with reason `all_participants_submitted`; otherwise the existing deadline pass records `answer_deadline_elapsed`. Both set the persisted `reveal_phase_ends_at`. The worker processes already-revealed phases before newly due answer deadlines and uses the unadjusted wall clock for reveal pauses, so a new reveal cannot advance in the same pass and the answer grace cannot shorten or extend the result display. A next round receives its full configured answer interval starting at activation. Future rounds are generated before start and stored as `pending`, so worker transactions never wait on a provider.

## Durability and concurrency

Each worker queries due PostgreSQL rounds in deadline order and claims their rows with `FOR UPDATE SKIP LOCKED`. The state change, immutable `round_transitions` audit record, room revision, and invalidation commit in one transaction. Partial unique indexes allow one reveal per round and one advancement per turn index. Competing workers skip a claimed row; a process or database failure rolls back and releases the row lock so another worker or a restarted server can retry.

The audit record distinguishes host reveal and simultaneous deadline reveal from sequential advancement caused by a submission, explicit pass, or turn timeout. A submission waiting on the same round lock is rechecked against the authoritative cutoff and current player. A host reveal racing the worker has one winner; the loser observes the already-revealed state and cannot create another transition or event.

## Configuration and operations

- `AUTO_REVEAL_ENABLED=true` enables processing. Set it to `false` as an emergency rollback to retain manual host reveal.
- `AUTO_REVEAL_GRACE_SECONDS=0` delays reveal after the answer cutoff without reopening submissions.
- `TRANSITION_POLL_INTERVAL_MS=250` controls due-work discovery.
- `TRANSITION_BATCH_SIZE=25` bounds one transaction (maximum 100).
- `LIVINGROOM_REVEAL_PAUSE_SECONDS=8` sets the TV result pause (validated range 3–30 seconds).

`GET /healthz` reports process liveness. `GET /readyz` reports `503` until the enabled worker has completed a successful database pass and whenever it fails or becomes stale. Automatic processing requires PostgreSQL; an enabled in-memory server remains live but not ready. Disabling automation makes readiness independent of the worker and preserves the prior manual flow.

Migration `000009_round_transitions.sql` introduced reveal transitions; `000012_sequential_mode.sql` adds persisted turn state and per-turn audit uniqueness; `000013_livingroom_mode.sql` adds player roles, reveal-pause persistence, and unique start/completion transitions. Disable automation before rolling a transition schema back. Before rolling back `000013`, finish or discard living-room rooms and deploy code that no longer creates them. `AUTO_REVEAL_ENABLED=false` is an emergency stop, not a playable living-room fallback: those games remain at their current phase until automation is restored.
