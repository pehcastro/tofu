# Where turn-legacy.json and turn-modern.json came from

**Written**, on 2026-09-19, two files, 1,302 bytes, and written is right for both: they are schema probes, not a corpus. `turn-modern.json` is the current lowercase turn schema and must be read, keeping its command, its failure and its gate decision id. `turn-legacy.json` is the older PascalCase schema and must be skipped with a stated reason, because it carries no step the reader can bind and no wall clock. `session_test.go` asserts exactly one of each.

**No measurement in `bench/stopcheck` reads these.** The corpus every stop-check number comes from is `bench/stopcheck/corpus/`, 19 real turns with its own `PROVENANCE.md`. These two are parser fixtures that happen to live under a directory called `sessions`, and confusing the two directories would be easy, which is why this file says it plainly.

A recording could not do this job. The point is to hold two schema shapes side by side in one directory, and no single real session ever produced both.

## The model id in them is not a real one

Both carry a model string naming a vendor model this project does not use and refuses to call. It is there because a turn file has a model field and something had to fill it, and it was never a route these fixtures could reach. **It is not evidence that any such model ever ran here.** It is also the reason nobody should copy these two files as a template.

## Leakage

**Not applicable, established rather than assumed.** The question is whether the parser reads a file, and the answer is a struct or an error. There is no label, no verdict and no question text: the expectation is in `session_test.go`. A fixture cannot name an answer that is the act of parsing it. Count: zero eligible rows, zero leaks.

## What it does not carry

Scanned on 2026-09-23 for every identity and credential pattern in `bench/corpus.Scrub`: no hit. Every byte of both files was invented, so there was nothing off a recording machine to remove.
