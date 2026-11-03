# Where report.golden.md came from

**Written**, on 2026-09-18, and written is right for it: it is a golden file. It is the exact text `report.Render` produces from the `api.Result` literal in `bench/api/report_golden_test.go`, so a change to the renderer shows as a diff instead of an argument. There is no recording that could play this role, because the artefact being pinned is the renderer's output and not a measurement.

Every figure in it is invented and is meant to be: machine `TESTBOX`, date `2026-01-02`, build `typesafe/jev-1.00-20260101`, three runs at one state size, two rerun values. **No number in this file is a measurement of anything.** A reader who quotes a latency out of it is quoting the test's input.

One file, 2,722 bytes.

## Leakage

**Not applicable, and here is how that was established rather than assumed.** A leakage check asks whether a question names a component of its own answer. This file carries no question and no label: it is the answer, and the input that produces it lives in the test rather than here. Mechanically: the file has no `label`, `verdict`, `expected` or question field, and nothing reads it except a byte comparison against `report.Render`'s output. Count: zero rows with a question, so zero leaky rows, out of zero eligible.

## What it does not carry

Nothing off the recording machine. Scanned on 2026-09-23 for every identity and credential pattern in `bench/corpus.Scrub`: no hit.
