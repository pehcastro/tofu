# Where recorded-turn.json came from

One real recorded turn, `turn-18d6a7961d933ee8`, the Hono task-list change, **recorded**, added 2026-09-20 beside `forks.jsonl` in `bench/suites/context/recall/testdata` and moved here unchanged by TOFU-2284. It is the fixture for the replay and ceiling tests and **it is not part of the fork corpus**. No fork number is computed from it.

Read by `bench/suites/context/recall` (`replay_test.go`), `bench/suites/agent/turn` (`replay_test.go`) and `bench/suites/context/forkcache` (`corpus.go`).

Scanned on 2026-09-23 for every identity and credential pattern in `bench/kit/corpus.Scrub`: no hit.
