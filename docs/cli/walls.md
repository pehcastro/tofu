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

**One shape for every timing.** A wall is one record: `op`, `name`, `ms`, `at`, `session`, `ok` and `outside_ms`.
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
| `--own` | print own time, `ms` minus `outside_ms`, with a tier column; leave out walls recorded without `outside_ms` and count them under the table |
| `--tier <S\|A\|B\|C\|D\|E>` | keep the rows whose own p50 is in that tier; implies `--own`; any other letter is refused with the list |
| `--json` | one envelope; `rows` carry `op`, `name`, `n`, `p50`, `p90`, `max`, and `tier` under `--own` |

## Own time and tiers

`outside_ms` is the part of a wall spent waiting on something tofu does not run: a model, Jev, the
network, a person or another process. Waits that overlap inside one wall count once, so `outside_ms`
is never more than `ms`, and a wait inside a nested wall counts in every wall around it. `--own`
prints `ms - outside_ms`, and leaves out a wall whose whole time was outside. Today tool calls and
hooks record `outside_ms`, a hook counting the process it ran; a Jev decision records it when its
ledger row carries `outside_ms`; and `tofu judge` and `tofu sift` record it on their verb wall,
counting their Jev waits. Every other wall has none yet and `--own` leaves it out rather than calling
all of its time own.

A `jev` wall is a decision Jev was asked: the ledger's `inline_code` and `standing` rows record an
inline code run and a standing approval, not a Jev call, and are not read as walls. When a turn holds a wall record of the same op as an older
timing field, the record is read and the older field is not, so no time counts twice.

| Tier | own p50 ms |
|---|---|
| S | under 100 |
| A | 100 to 300 |
| B | 300 to 700 |
| C | 700 to 1000 |
| D | 1000 to 1500 |
| E | 1500 and over |

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
