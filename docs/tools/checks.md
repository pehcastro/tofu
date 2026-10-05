---
title: Checks
description: typecheck wraps tsc and test wraps Vitest, both kept warm for the session.
order: 4
updated: 2026-10-04
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
