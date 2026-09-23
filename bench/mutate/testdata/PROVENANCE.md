# Where policy-gremlins.txt came from

**Recorded**, on 2026-09-20, 5,865 bytes, 116 lines. It is the raw standard output of one real `gremlins unleash` run over `internal/judge/policy`, captured by TOFU-215 before TOFU-223 fixed anything. No line was added, removed or reordered, including the `Starting...` and the coverage timing at the top.

`gremlins_test.go` and `tool_test.go` parse and tally it. It is the only reason the before figure in `report-2026-09-20.md` can be reproduced offline: the run it came from cannot be repeated against the tree as it stands, because the code it mutated has since been fixed.

## What a reader should not conclude

**These are not the current numbers.** The file is a snapshot of one package on one day before a known repair. The after figures in the report are taken from `BOARD.md` rather than from a rerun, and this file has nothing to do with them.

The run covers one package. Nothing here says anything about mutation scores elsewhere in the tree.

## Leakage

**Recorded, so the written-corpus rule does not apply**, and it was checked on 2026-09-23. Method: the outcome scored from each line is the `KILLED` or `LIVED` verdict, and leakage would be some other part of the line predicting it. Each line carries a verdict, a mutator name, a file and a line number and nothing else, and the same mutator at the same site appears with both verdicts across the file. **Count: zero lines where a field other than the verdict names the verdict.** There is no prose in this file for an answer to hide in.

## What it does not carry

Scanned on 2026-09-23 for every identity and credential pattern in `bench/corpus.Scrub`: no hit. The file names source paths relative to the mutated package, never an absolute path off the recording machine.
