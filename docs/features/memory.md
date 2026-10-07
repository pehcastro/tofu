---
title: Memory
description: Tell tofu once. What you ask it to remember reaches the lead in every later session, for every project or for one.
order: 14
updated: 2026-10-07
---

Memory keeps the things you told tofu so you do not have to say them again
in the next session. Each entry is one line, with your own words kept beside
it, and the lead reads every entry in its system message on every request.

## Why memory works the way it does

**You decide what is kept.** A message that begins with `remember` gets an
offer card: for you in every project, for this project, or no. Nothing is
written until you pick. After 50 answers, if more than 45 said yes, tofu
writes at once and prints the undo on the same line.

**Two scopes, and the closer one wins.** Global entries live in
`~/.tofu/memory/`; project entries live beside that project's sessions in
`~/.tofu/projects/<project>/memory/`, never in the repository. Both are
sent, global first, and the project entry wins where they disagree.

**It stays small.** Each scope holds at most 4096 bytes, about 1,000
tokens. A write past it is refused with the entries listed and the command
that removes one; nothing is dropped behind your back.

**The lead only.** Sub-agents never get memory: the lead puts what matters
into their brief. A fork keeps it, because it sits in the system message.

**Plain files.** One `<id>.yaml` per entry, so a hand edit or a deletion is
one small file.

## Using memory

- **Keep something**: type `remember: never run cargo with more than 2 jobs`
  in the app, or `/remember <what>`, and pick a scope.
- **From the command line**: `tofu memory add "<statement>"` for this
  project, `--global` for every project.
- **See it**: `tofu memory`, or `/memory` in the app.
- **See what the lead gets**: `tofu run --show-prompt "<task>"`.
- **Forget one**: `tofu memory remove <id>`, with `--global` for a global
  entry.

## Commands

```
/remember <what>
/memory
tofu memory [list] [--json]
tofu memory add [--global] [--kind person|project|reference] [--said "<words>"] [--replace <id>] [--dir path] "<statement>"
tofu memory remove [--global] [--dir path] <id>
```
