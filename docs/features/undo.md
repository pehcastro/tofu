---
title: Undo
description: Put back the files the last turn changed, or the last N turns, including changes made through bash, without touching your git history.
order: 12
updated: 2026-10-05
---

Undo puts back every file a turn changed. A file the turn edited gets its old
bytes back, a file it created is deleted, and a file it deleted comes back,
byte for byte. It covers every way a turn writes: `edit`, `write`, `bash` and
a sub-agent alike. The conversation stays as it was; only the files move.

## Why undo works the way it does

**Bash is covered too.** tofu records the project at the start and the end of
every turn, so a file a script or a build rewrote comes back just like one
the model edited.

**Your edits are safe.** A file changed after the turn ended, by you or by a
background shell, is left alone and named in the result. `--force` puts it
back anyway when that is what you want.

**Your git history is never touched.** The record is a private git folder in
your home, one per project: `~/.tofu/projects/<project>/undo.git`. The
project's own `.git` is never read for this and never written.

**It cleans up after itself.** Each session keeps its list of turns in
`sessions/<session>/undo-turns.jsonl`. Seven days after a session's last
turn, its list is dropped and the files only it needed are pruned from the
store.

Ignored files and new files over 10 MiB are never recorded, and undo needs
`git` on the PATH.

## Undoing a turn

- **The last turn**: type `/undo` between turns, or run `tofu undo` in the
  project.
- **The last N turns**: `/undo 2` or `tofu undo 2`. A second `/undo` goes one
  turn further back.
- **See first**: `tofu undo --dry-run` lists what would change and writes
  nothing.
- **Include files changed since**: `tofu undo --force`.
- **Another session or folder**: `tofu undo --session <id>` and
  `tofu undo --dir <path>`.

## Commands

```
/undo [N]
tofu undo [N] [--dir path] [--session id] [--force] [--dry-run] [--json]
```
