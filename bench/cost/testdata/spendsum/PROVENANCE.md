# Where main.go came from

**Written**, on 2026-09-19, 178 bytes, and written is right: it is a compile probe that must fail. `bench/cost/spend_compile_test.go:TestSummingAnActualSpendWithAListPriceDoesNotCompile` builds it and fails the suite if it succeeds, or if it fails for any reason other than `mismatched types ledger.Money and ledger.ListPrice`.

It is the negative half of a pair. `../spendapart/main.go` carries the same two values kept apart through `ledger.Spend.Plus` and must build. **Neither file proves anything alone**; the answer is the compiler's exit status on both.

Nothing here could be recorded. No session ever produced this question.

## Leakage

**Not applicable, established rather than assumed.** No question, no label, no prose. The expectation lives in the test, and the arm is `go build`, which reads types and not names. Count: zero eligible rows, zero leaks.

## What it does not carry

Scanned on 2026-09-23 for every identity and credential pattern in `bench/corpus.Scrub`: no hit.
