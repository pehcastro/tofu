---
title: Walls
description: tofu walls prints how long each kind of operation took, in milliseconds, as n, p50, p90 and max over a window.
order: 5
updated: 2026-11-13
---

`tofu walls` prints how long each kind of operation took: one row per op and name, with the count, the
median, the 90th percentile and the maximum, in milliseconds. tofu records and prints the number; it
never labels a time fast or slow.

## Why it exists

**One shape for every timing.** A wall is one record: `op`, `name`, `ms`, `at`, `session` and `ok`.
The op says what kind of operation it was, such as a command line verb, a turn, a model request, a
tool call, a hook or a Jev decision, and the name says which one. One unit, whole milliseconds, and one
field name, `ms`.

**Read where it already is.** Turns, model requests, tool calls, hooks and Jev decisions already keep
their times in the session and the decision ledger. `tofu walls` reads them there rather than copying
them, so no time is stored twice.

## How to use it

Run a verb, then read its wall:

```
tofu doctor
tofu walls --op verb
```

```text
Walls · since 2026-10-30 09:12:40 · 1 row

    op    name    n  p50 ms  p90 ms  max ms
    verb  doctor  5  8701    9104    9295
```

A verb's wall runs from the moment the process started to the moment the verb returned, so it agrees
with an outside clock such as PowerShell's `Measure-Command` to within the time the process takes to
exit.

Look at hooks over the last week, in every project:

```
tofu walls --since 7d --op hook --all
```

## Reference

| Flag | Does |
|---|---|
| `--since <14d\|36h>` | the window; 14 days by default |
| `--op <op>` | keep one op; an unknown op is refused with the list |
| `--all` | read every project under `~/.tofu/projects` |
| `--json` | one envelope; `rows` carry `op`, `name`, `n`, `p50`, `p90`, `max` |

Ops: `verb`, `tool`, `turn`, `request`, `first_token`, `last_byte`, `http_headers`, `http_stream`,
`permission_wait`, `hook`, `jev`, `advisor`, `fork_blocked`, `browser_step`, `shell`, `sift`,
`startup`, `startup_step`, `spawn`, `spawn_step`, `first_frame`, `session_open`, `session_resume`,
`compaction`, `memory_request`, `undo_snapshot`, `host_start`, `host_join`, `wire`, `cron`,
`doctor_step`.

A verb writes its wall to `walls/<day>.jsonl` in the project's state folder, only when the project
has one, so a verb run in a folder tofu never opened creates nothing. `tofu run --dir <dir>` writes to
the project of `<dir>`. The app, `tofu --continue`, `tofu serve` and `tofu host run` write no verb
wall. Day files older than 30 days are deleted when a new day starts. A line that does not parse is
skipped and counted in `bad_lines`.
