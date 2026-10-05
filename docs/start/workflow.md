---
title: Workflow
description: How a session with tofu goes, from a task to a checked answer.
order: 4
draft: true
updated: 2026-10-04
---

A session is one conversation with the lead. You type a task; the lead hands
pieces to sub-agents that run in the background, and you keep talking to it.

![A task handed to go-dev, running in the background while the chat stays open](./media/tui-task-working.png)

## Why the lead delegates

The lead keeps its context for the plan and for you, and each sub-agent
starts clean with only the rules for its piece. A language sub-agent such as
`go-dev` is sent back until its checks ran after its last edit, and the lead
reads the diff and runs the checks itself before it answers. With five
sub-agents running at once, a task's first turn took 4 min 58 s, against
21 min 18 s when none overlapped.

![The finished answer, with the go-dev report folded and the lead verifying](./media/tui-task-answer.png)

## Keys

**Alt+2** opens the sub-agents feed, **Alt+3** the file edits, **Alt+4** the
shells. **Esc** or the first **Ctrl+C** stops only the lead's turn.
`tofu --continue` reopens the last session.

## Commands

```
tofu session list|info|trace|reads|resume|rename <name|id> [--json]
tofu run --dir <path> [arguments] <task>
```
