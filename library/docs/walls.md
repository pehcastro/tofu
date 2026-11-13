---
topic: walls
title: Walls: how long each operation took
summary: one record of milliseconds per operation, and tofu walls printing n, p50, p90 and max for each over a window
verbs: walls
---

## What it is

A wall is how long one operation took, from start to end, in whole
milliseconds. Every wall is the same record:

- `op`: what kind of operation, such as `verb`, `turn`, `request`,
  `tool`, `hook` or `jev`.
- `name`: which one, such as the verb `doctor`, the tool `bash`, the hook
  event `PreToolUse` or the decision point `tool_gate`.
- `ms`: how long it took.
- `at`: when it started, in UTC milliseconds.
- `session`: the session it ran in, when there was one.
- `ok`: whether it ended without an error.

tofu records the time and prints it. It does not call a time fast or
slow; you judge that.

## Where it lives

- A verb you run, such as `tofu doctor` or `tofu boardy list`, writes one
  line to the project's `walls/<day>.jsonl` in its state folder under
  `~/.tofu/projects`, timed from the moment the process started. `tofu run
  --dir <dir>` writes to the project of `<dir>`.
- A directory tofu has never opened has no state folder, and a verb run
  there writes nothing and creates nothing.
- The app, `tofu --continue`, `tofu serve` and `tofu host run` live for
  hours and write no verb wall.
- Day files older than 30 days are deleted when a new day's file starts.
- Turns, model requests, tool calls, hooks and Jev decisions already keep
  their own times in the session and the decision ledger. `tofu walls`
  reads those where they are, so nothing is written twice.

## Change it

Nothing to set. The 14 day window is the default of `--since`.

## Check it

- `tofu walls` prints one row per op and name over the last 14 days: n,
  p50, p90 and max in ms.

```text
Walls · since 2026-10-30 09:12:40 · 3 rows

    op    name          n    p50 ms  p90 ms  max ms
    verb  doctor        5    8701    9104    9295
    hook  PreToolUse    812  77      158     3994
    jev   tool_gate     640  202     243     1298
```

- `--since 36h` or `--since 7d` changes the window.
- `--op hook` keeps one op. A name that is not an op is refused with the
  list of ops.
- `--all` reads every project under `~/.tofu/projects`, not only this one.
- `--json` prints one envelope whose `rows` carry `op`, `name`, `n`,
  `p50`, `p90` and `max`, plus `bad_lines` and `unread_files`: a line that
  does not parse is skipped and counted, never guessed.

## Undo it

A wall is a record and changes nothing. To forget the verb walls of one
day, delete that `walls/<day>.jsonl` in the project's state folder; the
times kept in sessions and the decision ledger stay where they are.
