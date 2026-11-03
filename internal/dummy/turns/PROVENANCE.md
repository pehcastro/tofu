# turns

Recorded turn rows, each a real session file taken out of `.tofu/sessions` or `.boji/sessions`. Each folder carries its own notes on where its turns came from.

| folder | origin | readers |
|---|---|---|
| labelled | the 19 turns the stop check hand labels were written against (BOJI-111, 2026-09-19) | bench/suites/judge/stopcheck, bench/corpus, bench/kit/harness, bench/kit/harness/task, bench/suites/context/transform, bench/suites/context/forkcache, apps/cli |
| edit | one turn, the Hono task-list change, the fixture for the recall replay and ceiling tests (2026-09-20) | bench/suites/context/recall, bench/suites/agent/turn, bench/suites/context/forkcache |
| tofu-v1 | one hono v1 run with its 91 ledger rows (TOFU-437, 2026-09-23), the default transcript of `bench harness --offline` | bench/cmd, bench/kit/harness |

Moved here unchanged by TOFU-2284 from `bench/stopcheck/corpus`, `bench/suites/context/recall/testdata/recorded-turn.json` and `bench/kit/harness/testdata/tofu-v1`. `bench/suites/judge/stopcheck/unique_test.go` walks `bench` and `internal/dummy` and refuses a second copy of any recorded turn; the one it allows is `tofu-v1/session.json`, byte for byte `labelled/turn-18d69bfed2bf52a0.json`.
