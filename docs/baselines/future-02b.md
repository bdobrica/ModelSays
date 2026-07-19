# FUTURE-02B live-client evidence

Measured 2026-07-19 in the local jsdom/Vitest client environment with fake time and the production TypeScript build. The backend event durability and 250 ms event-log polling behavior are covered separately by the FUTURE-02A PostgreSQL and HTTP suites.

## Results

| Measure | PB-00 target | Observed | Result |
| --- | ---: | ---: | --- |
| Invalidation coalescing to authoritative-refetch dispatch | pushed mutation p95 ≤ 500 ms | 10 ms configured test window; all 7 event types in one refetch batch | Pass |
| Live idle room GET volume | reduce the 20 requests/minute three-second baseline | 2 safety GETs/minute/client | Pass (90% reduction) |
| Stream-unavailable room GET volume | retain bounded recovery | 12 fallback GETs/minute/client | Pass (40% below baseline) |
| First reconnect attempt | reconnect/refetch p95 ≤ 3 s | 500 ms configured; test uses equivalent 100 ms fake-time scale | Pass |
| Maximum reconnect delay | bounded recovery | 15 s, with five-second fallback polling continuing | Documented degraded mode |
| Production client assets | ≤ 350 KiB uncompressed, ≤ 110 KiB compressed | 263.65 kB uncompressed, 83.11 kB gzip | Pass |

The coalescing and reconnect observations are deterministic client scheduler measurements, not physical-network latency. With the server's default 250 ms durable-log interval, a healthy connection has 225 ms of remaining budget for delivery, the refetch, and render before the 500 ms target. A browser/network playtest is still required to claim real end-to-end p95 evidence.

## Recovery and compatibility evidence

- The stream token is present only in `X-Player-Token`, never the URL.
- Each supported invalidation type produces the same authoritative refetch path.
- Duplicate and stale revisions are ignored; out-of-order bursts are sorted; a missing revision is reported as a gap and forces the same full refetch.
- A failed refetch does not advance the resume cursor.
- Streaming failure, hidden tabs, and offline state preserve periodic polling; visibility and online recovery trigger immediate work.
- Existing REST mutations, response sequencing, and polling remain compatible. Events never supply room, board, guess, score, provider, judge, or token state.
