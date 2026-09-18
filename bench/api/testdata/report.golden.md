# bench api: 2026-01-02

Machine: TESTBOX. Credential kind: key. Wire: openrouter. Build id the response reported: `typesafe/jev-1.00-20260101`.

No first call was discarded as a warm-up; every call below, including the first, counts.

## Latency by state size

| State | Runs | Median | p95 | p99 | Min | Max | Billed input tokens | Build |
|---|---|---|---|---|---|---|---|---|
| 300 | 3 | 300 ms | 320 ms | 330 ms | 290 ms | 340 ms | [500 500 500] | `typesafe/jev-1.00-20260101` |

p95 and p99 above rest on 3 runs per state size. A p99 from 3 runs is not a real p99, it is the max; treat it as directional only.

## Latency by question count

| Questions | Runs | Median |
|---|---|---|
| 1 | 3 | 300 ms |

## Six-case gate battery

n=1 calls (6 cases, 3 reps each). min 310 ms (n=1), p50 310 ms (n=1), p95 310 ms (2nd-largest of 1, tail unmeasured), p99 310 ms (2nd-largest of 1, tail unmeasured), max 310 ms (n=1). bench-001's own tail, a live call and its retry both timing out at 2.5 s during the BOJI-005 review, needs far more than 1 samples to reappear here; treat p95/p99 above as directional, not as the real tail.

| Case | risk | approval | user_requested | from_untrusted | Latency |
|---|---|---|---|---|---|
| case-1-ls.json | 0.00 | 0.10 | 0.60 | 0.02 | 310 ms |

## Rerun agreement, 2 runs on `case-1-ls.json`

| Question | Values | Min | Max | Spread | Straddles 0.50 |
|---|---|---|---|---|---|
| approval | [0.1 0.11] | 0.10 | 0.11 | 0.01 | false |

No question straddled 0.50 across these reruns.

## Option sweep

| Options | Result | Latency | Billed input tokens | Correct | Confidence | Cost |
|---|---|---|---|---|---|---|
| 8 | ok | 300 ms | 360 | true | 1.00 | $0.000015 |
| 256 | failed | | | | | | server message: too many choices |

## Cost

Total spend of this run: $0.000200 over 4 calls, taken from `usage.cost` on each response, not computed.

## Comparison against bench-001

- state size 300: median moved -30% (427 ms -> 300 ms against bench-001), n=3 runs here. Cause: not established, n=3 is too small to separate a real move from tail noise.
- gate battery median moved -53% (658 ms -> 310 ms against bench-001's 658 ms), n=1 here. Cause: not established, n=1 is too small to separate a real move from tail noise.
- gate battery worst call moved -81% (1625 ms -> 310 ms against bench-001's 1,625 ms), n=1 here against bench-001's 6. Cause: not established, this is exactly the tail neither run sampled enough to trust.

No baseline exists in bench-001 for: latency by question count (that finding reported flat, not the medians), rerun agreement on `case-1-ls.json`, and the 256-option point (bench-001's ceiling probe used 300, not 256), so these are reported without a delta.
