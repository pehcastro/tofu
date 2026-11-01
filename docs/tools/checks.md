---
title: Checks
description: typecheck wraps tsc and test wraps Vitest, both kept warm for the session.
order: 4
updated: 2026-11-01
---

Two wrappers for TypeScript projects. `typecheck` runs the project's own
`tsc`, found through `node_modules`, the package manager, or `npx`, in
`--watch` mode. `test` runs Vitest through a `node` script that stays alive.
Both start with the session.

## Why they stay warm

A cold `tsc` or `vitest run` pays its startup on every call. A watching
process rechecks only what changed, including changes made through the shell.
In a large TypeScript repository, a test call took 449 ms at the median
against 5 to 16 s for a cold `npx vitest run <file>`, and a 33-file rename
took 94 s against 675 s without the warm checks. `write` and `edit` use the
same `tsc`, which is why a `.ts` change comes back with its errors.

Output is capped at 20 lines, with 3 lines per test error, so a failing suite
doesn't flood the context.

## Parameters

The model calls them on its own. In a project without Vitest, the result says
how the project runs its tests, and those run through `bash`.

| Tool | Parameter | What it does |
|---|---|---|
| `typecheck` | `path` | the errors under a file or directory; the rest are counted |
| `test` | `path` | a test file, or a source file whose `<stem>.test.*` and `<stem>.spec.*` run |

Both are tools for the model, with no command of their own.

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
