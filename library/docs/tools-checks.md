---
topic: tools-checks
title: Checks, files and shell tools
summary: what test, typecheck, mutate, read, write, edit, bash and shell give the model, and how to see it for yourself
verbs: tool
---

## What it is

These tools run the project's own checks, read and change its files, and
run its commands. Each result is the answer rather than a log: counts and
`path:line` for a check, one line for a file the model already read.

- `test` runs one package, crate or file and returns pass, fail and skip
  counts, each failure with its `path:line` and got against want, and the
  skipped tests by name. `go.mod` picks `go test`, `Cargo.toml` picks
  `cargo test`, a Python project picks `pytest` when it declares it and
  `unittest` when not, and `package.json` picks Vitest, kept warm. A path
  with no tests says `0 tests found`.
- `test` with `repeat` runs one test several times and says `real` when
  every run fails, `flaky` when some do, and `passes` when none do.
  `changed` runs the tests of every file the turn changed, and every test
  that reaches a changed declaration through `symbols impact`, in another
  file or package.
- `mutate` plants small bugs in the file at `path` one at a time, a
  flipped comparison, a swapped operator, a constant plus one, and runs
  the one test named in `test` against each in an overlay. It returns
  `mutants`, `killed` and every survivor as `path:line` with the change
  the test missed, and never changes the project's files. For Go,
  `test_path` names the test's package when it is not the file's own.
- `typecheck` runs `go build` then `go vet` for Go, `cargo check` for Rust,
  `pyright` or `mypy` for Python, and `tsc`, `vue-tsc` or `svelte-check`
  for TypeScript, and returns the error count and each error as
  `path:line`. A checker missing from `PATH` is named with its install
  command. With `explain` it runs nothing: it takes the text of a failure
  and returns each error with five lines of code around it and the
  declarations it names, the same error at many places folded into one.
- `write` and `edit` on a `.go`, `.rs`, `.py`, `.ts` or `.tsx` file end
  with the errors that file's checker finds, or `0 errors`. An `edit` that
  would leave a Go file unable to parse is refused with the parser's line,
  and nothing is written.
- `edit` with `symbol` replaces one whole declaration in a `.go`, `.rs`,
  `.ts`, `.tsx` or `.py` file, or with `insert` `before` or `after` adds
  code beside it. `read` with `at`, a line number or a name a `.go`, `.rs`,
  `.ts`, `.tsx`, `.py` or `.zig` file declares, such as `Order.Total`,
  reads about 100 lines around it and states the range.
  A name the file doesn't declare is refused with the closest names it
  does.
- A second `read` of the same lines of an unchanged file answers with one
  line, `order.go: unchanged since your read at step 1, whose result is
  still above`, whatever ran between. Once the file changes, or `forget` or
  a compaction drops the earlier result, the next read is whole.
- `bash` folds the output of `go test`, `go build`, `go vet`, `cargo test`,
  `cargo check`, `pytest`, `unittest` and `pyright --outputjson`: a passing
  run is one line, a failure keeps its name, `path:line`, got and want, and
  a build error keeps its `path:line` and message. What the test itself
  printed for `go test`, `unittest` and `pytest` follows under `printed by
  the program:`. A test command always runs as written, never through rtk,
  and rtk's history goes to `~/.tofu/rtk/history.db`, never the project. It
  ends with `bash: changed on disk under the project:` and the paths a
  command changed.
- `shell wait` with `for` returns once a kept shell is ready: a line
  matching a regular expression, `port:3000`, `file:dist/app.js`, or an
  `http://` address answering 200. `shell logs` folds what a shell printed
  into templates with counts, and a later call prints only
  `new_since_last_call`.

```text
test: go test -json -count=1 . in C:\work\shop\go, 1629 ms
go test: 1 passed, 1 failed, 1 skipped
FAIL TestDiscounted order_test.go:17: got 650, want 675
skipped: TestShipping
```

## Where it lives

Every call runs in the working directory, against the project's own
checkers and test runners on `PATH`. tofu installs none of them.

## Change it

`readBeforeEdit`, on by default, refuses `write` and `edit` on a file the
session hasn't read:

    tofu settings set readBeforeEdit false

## Check it

    tofu tool test '{"path":"."}' --dir .
    tofu tool typecheck '{"path":"broken"}' --dir .
    tofu tool read '{"path":"order.go","at":"Total"}' --dir .
    tofu tool mutate '{"path":"order.go","test":"TestTotal"}' --dir .

## Undo it

    tofu settings set readBeforeEdit true

`/undo` puts back the files the last turn changed, whichever of these
tools changed them.
