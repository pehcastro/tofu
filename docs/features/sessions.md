---
title: Sessions
description: Every session is recorded as it runs, outside your project, and one command reopens it where you left off.
order: 6
updated: 2026-10-09
---

A session is one conversation with tofu: every message, tool call and
sub-agent, appended to disk as it happens. It lives in your home, under
`~/.tofu/projects/<project>/sessions/<id>/`, as `session.json` (the header:
task, model, outcome, tokens, cost, sub-agents) and `events.jsonl` (one line
per event). A new conversation gets a three-word name and a family tag,
like `brisk-amber-heron#4mhr1.1`, and keeps both for its whole life.

![tofu --continue reopening the last session](./media/tui-continue.png)

## Why sessions live outside the project

**Nothing lands in your project.** No `.tofu` folder of logs to ignore or
commit by mistake.

**Appended, not saved at the end,** so a crash keeps everything up to the
last event, and `tofu --continue` picks up from there.

**Never rewritten.** When a session forks to stay under its context budget,
the old one keeps its whole record and points at the new one, and the new
one carries every message you typed, word for word. tofu never deletes a
session either; that is yours to do.

**One name for one conversation.** A fork to stay under the context
budget, `/compact` and a move to another account start the next
generation of the same family: the name stays and the number after the
`.` rises, so `brisk-amber-heron#4mhr1.3` is the third. `/new` starts a new
family. A branch, another way from a point, is a new family that records
where it branched. A name from an older tofu still finds the generation
that carried it, and hooks see the family id as `session_id`.

**Every message has an id you can point at.** The chat shows each message,
yours, the lead's and each sub-agent report, with the id it was recorded
under, such as `[message#9c2d40]`. Type that reference and the lead quotes
the message itself rather than its memory of it, across forks and
`--continue`. `tofu session trace` lists the same ids.

**A continued session opens as it was.** Each session file is read once,
so `tofu --continue` opens in under a second on a chain of 21 sessions
with 84 sub-agents. Each message keeps its own time, each sub-agent report
draws as a report, and a conversation over the context target forks
before its first request instead of resending the whole history.

## Side chats

A side chat is a second conversation beside the lead, for a question you
don't want in the main thread: what a function does, a draft of release
notes, a look at a log. It starts from a summary of the session it branches
from, answers while the lead's own turn keeps running, and never becomes the
session `tofu --continue` opens.

**It reads by default.** A side chat writes nothing unless you say so, so
it can't touch the files the lead's sub-agents are changing. The `notes`
preset lets it write `.md` and `.html` files, `files` lets it write
anywhere, and `--owns` names exact globs. Whatever its access, a side chat
never spawns sub-agents, schedules a job, remembers anything or changes a
setting. The `sideChatAccess` setting picks the default, `read`.

```sh
tofu session branch brisk-amber-heron --side
tofu session branch brisk-amber-heron --side --preset notes
tofu session branch brisk-amber-heron --side --owns "docs/**" --seed none
```

`--seed none` starts it with no summary. `tofu session list` hides side
chats; `tofu session list --all` shows them. A frontend opens one over
`tofu serve` with `session.branch` and changes its access with
`session.access`.

## Continuing, naming and removing

- **Continue the last session**: `tofu --continue`, or `/resume` in the app.
- **Continue another**: `tofu session resume <name|id>`.
- **Start fresh**: `/new`.
- **Name one**: `tofu session rename <name|id> "checkout redesign"` makes it
  `checkout-redesign`, on every generation.
- **Point at one generation**: `brisk-amber-heron#4mhr1.2`; the name alone
  is the newest.
- **Delete one**: remove its folder under `sessions/`.
- **Move sessions an older tofu left in the project**: `tofu migrate --dry-run`,
  then `tofu migrate`.

## Commands

```sh
tofu session list
```

```text
Sessions · 123 sessions                                 ● head brisk-amber-heron

  ● brisk-amber-heron     26 Sep 14:26  5 steps   stopped         explain this
    repository to me
```

One row per conversation. `tofu session <name>` prints the family: when it
started, how long it was worked on, counting each turn from its first
event to its last, and every generation with how it began, its steps,
active time and size, its branches and its sub-agents.

```sh
tofu session find brisk-amber-heron --tool bash
```

finds every call across every generation, and `--command`, `--file`,
`--text`, `--agent`, `--since` and `--until` narrow it, together.

`tofu session info <name>` prints one session with its model, cost,
context budget and the error it ended on, `tofu session trace <name>` every
request, message, tool call, hook run, message tofu added, fork and
failure, and on a continued session the sub-agents that kept working in
the session before it, `tofu session
request <name> <request id>` one request exactly as it was sent with its
response or error body, and `tofu session reads <name>` every file it read.
Each takes `--json`.
