# Where alternating_test.go came from

**Written**, on 2026-09-21, one file, 526 bytes, and written is right: a flake detector needs a test with a known flake pattern, and a recorded flake is by definition one nobody could reproduce on demand.

Two functions, one deterministic and one not:

- `TestAlternatesOnAMarkerFile` passes on its first run and fails on every run after, by writing a marker into the directory named by `FLAKE_FIXTURE_DIR`. With that variable set, three runs give pass, fail, fail, and `Measure` must report exactly this function as disagreeing. With the variable empty it skips every run, and `Measure` must count the skip as a result rather than as an absence.
- `TestAlwaysPasses` is the control. An instrument that flags it is broken.

**One fixture, two arms, because the environment variable is the switch.** The same file is the flake case and the always-skipped case, which is why the skip path has a real test rather than a promise.

## Leakage: present in the names, and it cannot reach the instrument

**Method, run on 2026-09-23.** `Measure` runs `go test` and parses its output: it learns a name and an outcome per run and computes disagreement from the outcome sequence alone. Nothing in it reads source text, so a name cannot influence a verdict.

**Count: 1 of 1 flaky test functions names its own behaviour**, `TestAlternatesOnAMarkerFile`, and its failure message says the same thing again. That is deliberate here, because the reader of a failing run is a person and the file is a probe rather than a scored corpus. It becomes leakage only if a judged arm is ever asked to predict flakiness from source, and this file would tell it the answer. Not repaired.

## What it does not carry

Scanned on 2026-09-23 for every identity and credential pattern in `bench/corpus.Scrub`: no hit.
