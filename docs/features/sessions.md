---
title: Sessions
description: Every session is recorded as it runs, outside your project, and one command reopens it where you left off.
order: 6
updated: 2026-10-04
---

A session is one conversation with tofu: every message, tool call and
sub-agent, appended to disk as it happens. It lives in your home, under
`~/.tofu/projects/<project>/sessions/<id>/`, as `session.json` (the header:
task, model, outcome, tokens, cost, sub-agents) and `events.jsonl` (one line
per event). A new session gets a three-word name, like `clear-sable-eagle`.

![tofu --continue reopening the last session](./media/tui-continue.png)

## Why sessions live outside the project

**Nothing lands in your project.** No `.tofu` folder of logs to ignore or
commit by mistake.

**Appended, not saved at the end,** so a crash keeps everything up to the
last event, and `tofu --continue` picks up from there.

**Never rewritten.** When a session forks to stay under its context budget,
the old one keeps its whole record and points at the new one. tofu never
deletes a session either; that is yours to do.

## Continuing, naming and removing

- **Continue the last session**: `tofu --continue`, or `/resume` in the app.
- **Continue another**: `tofu session resume <name|id>`.
- **Start fresh**: `/new`.
- **Name one**: `tofu session rename <name|id> "checkout redesign"` makes it
  `checkout-redesign`.
- **Delete one**: remove its folder under `sessions/`.
- **Move sessions an older tofu left in the project**: `tofu migrate --dry-run`,
  then `tofu migrate`.

## Commands

```sh
tofu session list
```

```text
Sessions · 123 sessions                                 ● head clear-sable-eagle

  ● clear-sable-eagle     26 Sep 14:26  5 steps   stopped         explain this
    repository to me
```

`tofu session info <name>` prints one session with its model, cost and
context budget, `tofu session trace <name>` every request with its tokens,
and `tofu session reads <name>` every file it read. Each takes `--json`.
