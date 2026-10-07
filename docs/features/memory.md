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

**You decide what is kept.** Nothing you type is offered as memory, even a
message that says `remember`. You add an entry with `tofu memory add` or
`/remember <what>`. The lead can offer one short rule, quoting your own
words, on a card titled `[&orchestrator] wants to add a project memory`:
yes, no, or always, with `tab` to switch to a global memory. Its rule is
one line of at most 160 bytes and names no one. Answer always and tofu
keeps the lead's later rules at once, printing the undo on the same line.

**You can see every write.** Each one is a `[memory#m3]` row in the chat, a
notice in `tofu session trace`, and a line the lead is told in the same
turn, so it never asks you to save it again.

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

- **Keep something**: type `/remember never run cargo with more than 2 jobs`
  in the app; it is kept for every project at once.
- **From the command line**: `tofu memory add "<statement>"` for this
  project, `--global` for every project.
- **See it**: `tofu memory`, or `/memory` in the app, where enter on an
  entry removes it or puts it in the composer to edit.
- **Turn it off**: `tofu settings set memory false`; `autoMemory false`
  asks before every rule the lead offers again.
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
