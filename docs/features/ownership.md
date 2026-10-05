---
title: Ownership
description: Every sub-agent holds the paths it may write, tofu checks every write against them, and sub-agents with different paths run side by side.
order: 4
updated: 2026-10-04
---

Every sub-agent that can write holds a list of paths, its `owns`: a file, a
folder, or a glob such as `internal/turn/*.go`. The lead gives it in the
`spawn` call. tofu checks every write against it before anything changes.

![The file edits tab: each changed file and the sub-agent that changed it](./media/tui-file-edits.png)

- `write` and `edit` outside `owns` fail.
- A `bash` command is read before it runs. A redirect, `tee`, `cp`, `mv`,
  `sed -i` or `perl -i` that writes outside `owns` refuses the whole command.
  The system's temporary folder is always allowed.
- A source file, such as `.go` or `.ts`, changes only through `edit` or
  `write`, never the shell.
- Commands over the whole tree, such as `go test ./...`, are refused; they
  are the lead's to run once, after the sub-agents finish.

Two running sub-agents never hold overlapping paths. A spawn that would is
refused and told to `message` the holder instead.

## Why paths are held

**Parallel work needs disjoint files.** Two agents editing one file overwrite
each other. Because paths can't overlap, spawns in one answer start together:
3 of 3 ran at once and the turn took 3 s, where the same shape ran 0 of 4 at
once before.

**Every change shows in the diff.** A source edit through the shell skips the
read check, the diagnostics and the **file edits** tab. Routing it through
`edit` keeps all three honest.

**A refusal is a question, not a loss.** The refused path goes into the
sub-agent's report, so the lead can widen the brief or hand the path to its
holder.

## Seeing who changed what

- **See who changed what**: the **file edits** tab, `Alt+3`.
- **Refuse edits to unread files**: `readBeforeEdit`, on by default, refuses
  an edit or write to a file this session hasn't read, or that changed since.
- You don't write `owns` yourself; the lead does, per spawn.

## Commands

```sh
tofu settings get readBeforeEdit
```

```text
true
```
