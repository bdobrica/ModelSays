# Release Evidence Template

Copy this template for every beta candidate. Keep observed evidence separate from targets and hypotheses, and retain regressions rather than overwriting them.

## Candidate

- Version/commit:
- Date and operator:
- Environment: OS, CPU/memory, Go, Node/npm, PostgreSQL, browser/device:
- Configuration changes:
- Database migration version:
- External providers enabled:

## Automated verification

| Check | Command | Result | Evidence or notes |
| --- | --- | --- | --- |
| Full repository gate | `make verify` | | |
| PostgreSQL race suite | `TEST_DATABASE_URL=... go test -race ./...` from `backend/` | | |
| Gameplay baseline, pass 1 | `make baseline` | | Attach JSON |
| Gameplay baseline, pass 2 | `make baseline` | | Attach JSON |
| Migration upgrade/rollback | Step-specific command | | |

For the two baseline passes, compare deterministic request, polling, query, and bundle counts exactly. Response bytes should be within 1%. Treat timing as host-dependent: investigate a greater than 25% increase in p95 backend latency or mutation-to-visible latency when the environment is unchanged.

## Service-level and resource budgets

| Measure | Target | Observed | Pass/fail |
| --- | ---: | ---: | --- |
| HTTP p95 latency, representative 12-player room | ≤ 200 ms | | |
| HTTP error rate excluding expected 4xx | < 1% | | |
| Pushed update p95 after committed mutation | ≤ 500 ms | | |
| Reconnect/refetch p95 | ≤ 3 s | | |
| Deadline transition p95 lag | ≤ 1 s | | |
| Provider p95 duration per call | ≤ 10 s | | |
| Provider retries | ≤ 1 | | |
| Provider cost per five-round game | ≤ US$0.10 by configured estimate | | |
| Client production assets | ≤ 350 KiB uncompressed, ≤ 110 KiB compressed | | |

Deterministic local push and deadline-transition evidence is recorded under `docs/baselines/`; staging must still measure physical-network pushed-update and deployment-load deadline p95. Provider rows remain `Not measured` until observed in an authorized environment. A target is not evidence.

## Lifecycle and recovery scenarios

- [ ] Three-, eight-, and twelve-player curated workloads complete without provider calls.
- [ ] Answering data remains private until reveal.
- [ ] Duplicate claims and overrides remain authoritative.
- [ ] Client polling/refetch fallback works.
- [ ] Client reconnects after a backend restart.
- [ ] Provider timeout/failure falls back safely.
- [ ] Deadline transition survives concurrent workers and restart.
- [ ] Backup/restore rehearsal preserves an active and a completed room.

## Human playtest

- Participants/devices:
- Room size and duration:
- Facilitator:
- Consent/recording notes:
- Prompt responses from `docs/playtest-guide.md`:
- Disputed matches and host action:
- Mobile/accessibility observations:
- Requested next mode:

## Regressions, decisions, and follow-up

| Observation | Evidence | Severity | Owner/follow-up | Decision |
| --- | --- | --- | --- | --- |
| | | | | |

## Release decision

- Decision: Go / No-go / Conditional
- Accepted limitations and owner:
- Rollback trigger and procedure:
- Approver:
