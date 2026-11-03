# Where measured_with_checker@1.yaml came from

**Written**, on 2026-09-21, one file, 319 bytes, and written is right: it is a negative probe for the loader rather than for the gate. It declares `kind: measured` and also names a `checker`, which `rule.LoadFS` refuses. `qadomain_test.go:TestARuleTheLoaderRejectsIsReportedWithTheLoadersReason` asserts `QARuleFaults` returns the loader's own words, `a measured rule names a measurement instead`, rather than swallowing the error and reporting a clean rule set.

It is a different failure from `../emptyevidence`, which parses and is then judged a fault. This one never parses. Two probes because the two paths through `QARuleFaults` are different and only one of them was covered before.

Its `source` and `evidence` fields carry a real skill path and a real measured figure copied from a rule that `library/qa` really carries, so the refusal cannot be blamed on some other field being empty.

## Leakage: the id names the fault, and the loader does not read the id

**Method, run on 2026-09-23.** The refusal happens in `rule.LoadFS` on the `Kind` and `Checker` fields. `QARuleFaults` reads `Kind`, `Source` and `Evidence`. **The id reaches nothing but the message a person reads.**

**Count: 1 of 1 rows names its own answer**, in the id and the file name, and the directory is called `rejected`. Deliberate, inert while every arm is a loader, live the day a judged arm reads the file as text. Not repaired.

## What it does not carry

Scanned on 2026-09-23 for every identity and credential pattern in `bench/corpus.Scrub`: no hit.
