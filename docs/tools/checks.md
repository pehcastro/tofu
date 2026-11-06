---
title: Checks
description: typecheck and test check Go, Rust, Python and TypeScript with the project's own tools, and write and edit return the checker's errors.
order: 4
updated: 2026-11-06
---

`typecheck` and `test` check a project in Go, Rust, Python or TypeScript with
the tools the project already uses. The files under the path pick the
checker: a `go.mod`, a `Cargo.toml`, a Python project, or a `package.json`.
Both answer with counts and `path:line`, never the raw log.

## Why they exist

**A check the model reads in one look.** A raw test log prints every passing
test, every package line and the whole stack of a failure, and the model has
to find the one line that matters. `test` and `typecheck` pick the runner
from the project, run one package or file rather than the suite, and return
the counts and each failure with its `path:line`, so the model reads what
broke and where.

**TypeScript stays warm.** For TypeScript, `typecheck` runs the project's own
`tsc` in `--watch` mode, found through `node_modules`, the package manager or
`npx`, and `test` runs Vitest through a `node` script that stays alive. A
watching process rechecks only what changed, including changes made through
the shell. In a large TypeScript repository, a test call took 449 ms at the
median against 5 to 16 s for a cold `npx vitest run <file>`, and a 33-file
rename took 94 s against 675 s without the warm checks.

## typecheck

`typecheck` checks the code under `path` and returns the error count and
each error as `path:line`. The checker comes from the files under the path:

| Project | Checker |
|---|---|
| Go, the nearest `go.mod` | `go build`, then `go vet` |
| Rust crate | `cargo check`, or `rustc` for a `.rs` file outside a crate |
| Python | `pyright`, or `mypy` when pyright isn't there |
| TypeScript | `tsc`, `vue-tsc` or `svelte-check`, as `package.json` declares |

A checker missing from `PATH` is named, with the command that installs it.

On a Go module with one type error:

```bash
tofu tool typecheck '{"path":"broken"}' --dir .
```

```text
typecheck: go build -gcflags=-e -o NUL ./broken/... in 242 ms: 1 error
broken/broken.go:4:18: cannot use "three" (untyped string constant) as int value in variable declaration
```

### Explaining an error

With `explain`, `typecheck` runs nothing. It takes the raw text of a
compiler, checker or test failure and returns each error with five lines of
code around it and the declarations the error names. The same error at many
places folds into one, so every error a missing import causes reads as one.

```bash
tofu tool typecheck '{"explain":"broken/broken.go:4:18: cannot use \"three\" (untyped string constant) as int value in variable declaration"}' --dir .
```

```text
explain: 1 errors, 1 after folding duplicates

broken/broken.go:4:18: cannot use "three" (untyped string constant) as int value in variable declaration
     2 | 
     3 | func Label() int {
>    4 | 	var count int = "three"
     5 | 	return count
     6 | }
```

## test

`test` runs the tests for one path and returns the pass, fail and skip
counts, each failure with its `path:line` and what it got against what it
wanted, and the skipped tests by name. The nearest project file above the
path picks the runner:

| Project file | Runner |
|---|---|
| `go.mod` | `go test -json` on that one package, never `./...` |
| `Cargo.toml` | `cargo test` on that crate |
| a Python project | `pytest` when the project declares it, `unittest` when it doesn't |
| `package.json` | Vitest, kept warm |

A path with no tests says `0 tests found`. A runner missing from `PATH` is
named, with how to install it.

```bash
tofu tool test '{"path":"."}' --dir .
```

```text
test: go test -json -count=1 . in C:\work\shop\go, 1629 ms
go test: 1 passed, 1 failed, 1 skipped
FAIL TestDiscounted order_test.go:17: got 650, want 675
skipped: TestShipping
```

### Repeat and changed

`repeat` runs one test several times and gives a verdict: `real` when every
run fails, `flaky` when some do, and `passes` when none do. The first failing
run follows the verdict.

```bash
tofu tool test '{"path":".","name":"TestDiscounted","repeat":3}' --dir .
```

```text
test: repeated go test -json -count=1 -run ^TestDiscounted$ . 3 of 3 times: 0 passed, 3 failed, verdict real
the first failing run:
test: go test -json -count=1 -run ^TestDiscounted$ . in C:\work\shop\go, 1081 ms
go test: 0 passed, 1 failed
FAIL TestDiscounted order_test.go:17: got 650, want 675
```

`changed` runs the tests of every file the turn changed, instead of a path.
A test file is matched to a changed file by its stem, such as
`order_test.go` for `order.go`.

## Parameters

The model calls both tools on its own. Neither has a command of its own;
`tofu tool` runs either one by hand.

| Tool | Parameter | What it does |
|---|---|---|
| `typecheck` | `path` | a file or directory, the working directory when left out |
| `typecheck` | `explain` | the raw text of a failure to explain; nothing runs |
| `test` | `path` | a directory, a test file, or a source file whose tests then run |
| `test` | `name` | one Go, Rust or Python test by name |
| `test` | `repeat` | run it this many times and give a verdict |
| `test` | `changed` | run the tests of every file this turn changed |

## After write and edit

`write` and `edit` on a `.go`, `.rs`, `.py`, `.ts` or `.tsx` file end with
the errors the same checker finds, so a change comes back with what it
broke. A clean change ends with `0 errors`. An edit that would leave a Go
file unable to parse is refused, and nothing is written. See
[Files](/docs/tools/files).

## In a sub-agent's report

When any check a sub-agent ran failed, its report lists every check it ran,
in order, each with its exit code and the first error line it printed:

```text
checks it ran, each with its exit code and first error line:
  go vet ./... exited 0
  go test ./... exited 1: --- FAIL: TestAdd (0.00s)
  cargo test 2>&1 | tail -5 exited 0 at the last stage of its pipe, and printed a failure: test result: FAILED. 0 passed; 1 failed
  go test ./... exited 0
noted: sent back once: go test exited 1: --- FAIL: TestAdd (0.00s)
```

A check is a shell command, `test` or `typecheck`. A report where every check
passed has no list. A send-back quotes the same line, so the sub-agent reads
what failed rather than a bare exit code.

A pipe exits with its last stage, so `go test | tail` exits 0 when the test
failed. For `go test`, `go vet` and `cargo test`, tofu reads what the command
printed and counts a failure it shows as a failure, whatever the pipe's exit.
