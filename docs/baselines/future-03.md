# FUTURE-03 deadline-transition evidence

Date: 2026-07-19

The PB-00 beta target is deadline-transition p95 lag of at most one second. The default worker checks every 250 ms. The PostgreSQL integration gate creates 12 independent due rooms through the real service/repository path and requires their single bounded transition transaction to complete in less than 750 ms, leaving one full poll interval for discovery. It also proves the exact cutoff, a locked-row/crashed-worker retry, two concurrent workers, duplicate delivery, repository reconstruction, one audit record, and one room invalidation.

This is deterministic local database evidence, not production saturation telemetry. Staging release evidence should still record observed deadline-to-visible p95 under the deployment's normal database and client load.
