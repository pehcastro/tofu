# Where main.go came from

**Written**, on 2026-09-19, 232 bytes, and written is right: it is a compile probe, not a corpus. It exists to be handed to the compiler by `bench/cost/spend_compile_test.go` and to succeed, proving that money and list price can be carried side by side through `ledger.Spend.Plus` and read back as separate fields.

Its twin `../spendsum/main.go` is the arm that turns the mechanism off: the same two values added with `+`, which must fail to compile. **Neither file proves anything on its own.** The pair is the measurement, and the answer is the compiler's exit status.

Nothing here could be recorded. There is no session in which a compiler was asked this question.

## Leakage

**Not applicable, established rather than assumed.** The file carries no question and no label, and the expectation lives in the test. The answer is produced by `go build`, which reads the types and cannot read a name. Even if the package were renamed to state its own outcome, the compiler would return the same status. Count: zero eligible rows, zero leaks.

## What it does not carry

Scanned on 2026-09-23 for every identity and credential pattern in `bench/corpus.Scrub`: no hit.
