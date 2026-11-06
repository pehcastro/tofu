---
topic: tools
title: What the tools return
summary: what read, edit, write, bash, shell, typecheck, test, plan, fetch, web_search, history, env, http, record, profile, shape and db give the model, and how to see it for yourself
verbs: tool
---

## What it is

The model calls tools; tofu runs each call and hands back a result. Each
result is shaped to be the answer rather than a log: counts and
`path:line` for a check, one line for a file the model already read, the
date a page states. `tofu tool <name> '<json>' --dir .` runs any of them
by hand and prints the exact text the model would read.

## Checks

- `test` runs one package, crate or file and returns pass, fail and skip
  counts, each failure with its `path:line` and got against want, and the
  skipped tests by name. `go.mod` picks `go test`, `Cargo.toml` picks
  `cargo test`, a Python project picks `pytest` when it declares it and
  `unittest` when not, and `package.json` picks Vitest, kept warm. A path
  with no tests says `0 tests found`.
- `test` with `repeat` runs one test several times and says `real` when
  every run fails, `flaky` when some do, and `passes` when none do.
  `changed` runs the tests of every file the turn changed.
- `typecheck` runs `go build` then `go vet` for Go, `cargo check` for Rust,
  `pyright` or `mypy` for Python, and `tsc`, `vue-tsc` or `svelte-check`
  for TypeScript, and returns the error count and each error as
  `path:line`. A checker missing from `PATH` is named with its install
  command.
- `typecheck` with `explain` runs nothing: it takes the text of a failure
  and returns each error with five lines of code around it and the
  declarations it names, the same error at many places folded into one.

```text
test: go test -json -count=1 . in C:\work\shop\go, 1629 ms
go test: 1 passed, 1 failed, 1 skipped
FAIL TestDiscounted order_test.go:17: got 650, want 675
skipped: TestShipping
```

## Files

- `write` and `edit` on a `.go`, `.rs`, `.py`, `.ts` or `.tsx` file end
  with the errors that file's checker finds, or `0 errors`.
- An `edit` that would leave a Go file unable to parse is refused with the
  parser's line, and nothing is written.
- `read` with `at`, a line number or a declared name such as
  `Order.Total`, reads about 100 lines around it and states the range. A
  name the file doesn't declare is refused with the closest names it does.
- A second `read` of the same lines of an unchanged file answers with one
  line, `order.go: unchanged since your read at step 1, whose result is
  still above`, whatever ran between. Once the file changes, or `forget` or
  a compaction drops the earlier result, the next read is whole.

## Shell

- `bash` folds the output of `go test`, `go build`, `go vet`, `cargo test`,
  `cargo check`, `pytest`, `unittest` and `pyright --outputjson`: a passing
  run is one line, a failure keeps its name, `path:line`, got and want, and
  a build error keeps its `path:line` and message. Any other command prints
  as it always did.
- `bash` ends with `bash: changed on disk under the project:` and the paths
  a command changed, and with nothing when it changed none.
- `shell wait` with `for` returns once a kept shell is ready: a line
  matching a regular expression, `port:3000`, `file:dist/app.js`, or an
  `http://` address answering 200. It answers `met`, `after_ms` and the
  matched line, or `met` false with the last 5 lines.
- `shell logs` folds what a shell printed into templates with counts, and a
  later `shell logs` prints only `new_since_last_call`.

## Work and web

- A `plan` item may carry a `check`, a command or an acceptance line.
  `done` on that item needs `call`, the id of the earlier tool call in the
  session that ran the check, and is refused without one or with an id no
  earlier call has.
- `history` is one tool with a `mode`: `quote` for a `[quote#...]`
  reference you wrote, `zoom` and `recall` over the memory views, and
  `lookup` for a call a fork's carry names.
- `fetch` puts `date: published 2024-03-06`, `modified ...`, or `date: none
  stated` under its header. `web_search` puts each result's date beside its
  title when the provider gives one.

## Environment, observe and data

- `env` says `ready` or `not ready`, each toolchain the project declares
  with the version wanted and the one on `PATH`, and each key
  `.env.example` names as set or missing. It never prints a value.
- `shell` with `op` `ports` gives one JSON line per port: `pid`, `command`,
  the kept shell and `started_by` as `this_session`, `other_session` or
  `outside`, or `free`. It never kills, and it needs a running session.
- `http` calls a server on this machine only, found from the kept shell
  that listens or from `port`, and returns the status, the time, the
  headers worth reading and the body. A JSON array comes back as its first
  3 items and the type of every item.
- `record` with `start` saves the plain http a command makes, through a
  proxy only that command sees, and `replay` answers the same command from
  the recording with the server down. `https` is not intercepted.
- `profile` runs `go test` with one package, `node` or `python` under a
  profiler and returns the ten costliest functions with `flat`, `cum` and
  `path:line`, then the hottest call path.
- `shape` prints the type of a JSON sample in the project's language,
  with every field that is optional or a union marked and the item it
  first happens in.
- `db` reads SQLite or Postgres read only: the schema with no `query`, or
  the first 20 rows and the count of one statement. The connection comes
  from a key in `.env`, `DATABASE_URL` by default, and is never printed.

```text
not ready: 1 of 2 env keys not set
toolchains:
go: want 1.22 from go.mod, have 1.25.7
env keys from .env.example:
ORDER_API_URL: set in .env
ORDER_DB_PATH: missing from .env
```

## How to

- `tofu tool test '{"path":"."}' --dir .` runs the tests of one package.
- `tofu tool typecheck '{"path":"broken"}' --dir .` checks one directory.
- `tofu tool read '{"path":"order.go","at":"Total"}' --dir .` reads around
  a declaration.
- `tofu tool env '{}' --dir .` says what this project is missing here.
- `tofu tool db '{"env":"ORDER_DB_PATH"}' --dir .` prints a database's
  schema.
- `tofu tool shape '{"path":"orders.json"}' --dir .` types a JSON file.
- `tofu tool list --dir .` names every tool a run offers.
- `tofu docs tool` says how `tofu tool` itself works.
