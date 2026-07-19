# FUTURE-04B operations drill evidence

Date: 2026-07-19. Environment: local authorized disposable PostgreSQL 16 and the repository's single-process backend topology. This is recovery rehearsal evidence, not a production deployment claim or physical-network staging evidence.

Automated tests prove privacy-safe logs/metrics, bounded labels, readiness failure/recovery, active transition-pass drain, SSE cancellation, provider fallback without paid calls, and PostgreSQL migration/reconstruction behavior. PostgreSQL-backed verification rehearses process reconstruction, dependency interruption, migration `000009` down/up, and full lifecycle recovery.

The first custom-format restore correctly exposed that the local source database was still at migration 8; it was not counted as a pass. After the normal forward migration to 9, the repeated restore reported `rooms=1`, `games=1`, and `migration=9`; event and transition tables were queryable. The checked-in command was then exercised separately: its unconfirmed invocation made no change, its explicitly confirmed restore into `modelsays_restore_script` reported the same counts/version, and retention reported zero eligible audits without deleting anything.

Deterministic tests meet the public-beta error, deadline, reconnect, fallback, privacy, and recovery contracts. Real TLS proxy behavior, physical-network latency, host resource saturation, off-host backup storage, external provider outage, and measured RPO/RTO still require the first operator-controlled staging environment. Public beta must remain a single API replica.
