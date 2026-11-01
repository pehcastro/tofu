---
title: Sub-agent tools
description: spawn, message, subagents and ask, the tools the lead and its sub-agents use to hand out work and talk while it runs.
order: 7
updated: 2026-11-01
---

Four tools, tofu's own Go code. The lead gets `spawn`; the loop adds
`message` and `subagents` beside it; each sub-agent gets `ask`. How the lead
and sub-agents work together is on [Sub-agents](/docs/features/orchestration).

## Why spawn returns at once

`spawn` returns at once, so the lead keeps talking to you while sub-agents
work, and several run side by side when their paths don't overlap. That's
what took one app task from 21 min 18 s to 4 min 58 s. `effort` is per spawn
because a mechanical change doesn't need deep thinking: at low effort a hard
task finished in 63 s against 78 s at medium, with every hidden test passing.
`ask` carries a default, so a sub-agent never waits forever.

## One raise per acceptance

A spawn on a ticket writes its effort into the ticket's thread, so `tofu
boardy show` lists each spawn with the level it asked for. The first raise
goes through. A second raise under the same acceptance is refused, naming the
efforts already tried, because more thinking does not reach an outcome the
first raise missed: the lead rewrites the outcome with `tofu boardy set
<ID> --acceptance ... --reason ...`, or tells you what cannot be done, and
the count starts again under the new acceptance. A spawn with no ticket is
not counted.

## Parameters

The lead calls them. You watch on the **sub-agents** tab, and raise the
limits in settings.

| Tool | Parameter | What it does |
|---|---|---|
| `spawn` | `task` | the brief, kept whole |
| `spawn` | `owns` | the paths the sub-agent may write |
| `spawn` | `agent` | a defined sub-agent, on its own model |
| `spawn` | `effort` | `none` to `max`; left out, the lead's; one the model lacks runs at the nearest it has; on a ticket, a second raise under one acceptance is refused |
| `message` | `to`, `text`, `stop` | more work, a correction, or a stop; resumes a finished one |
| `subagents` | none | each sub-agent's state, steps and last tool |
| `ask` | `question`, `why`, `default` | the default stands when no answer comes |

`subAgentsPerTurn` (10) and `subAgentDepth` (2) are settings, and `tofu
agents` lists the sub-agents `agent` can name.

## Before the lead takes a hand-back

When a sub-agent says it is done, the `doneReview` setting reads its work
before the lead does. By default, `screen` sends it back while an acceptance
line in its brief has no evidence entry naming that line, and the report the
lead gets says `sent back` with each line it was sent back for. It makes no
model or Jev call. `tofu settings set doneReview off` takes the hand-back as
it is. See [Settings](/docs/features/settings).

## What a sub-agent's shell runs

A sub-agent reads its own session and board from its shell: `tofu session
find`, `trace` and `info`, `tofu boardy show`, `list` and `triage list`,
`tofu models`, `tofu version` and `tofu serve --schema` run there, and
every other tofu verb is refused. Of `.tofu`, it reads only `.tofu/rules`.
`cargo build`, `cargo run` and a binary they built run for a crate the
sub-agent holds a path in, and every cargo command builds into the
project's one shared target, so `--target-dir` is refused. `powershell -File` on a script inside a path the sub-agent holds
runs, and `timeout <n>` wraps any command it may run. A command the gate
would ask you about is refused in a sub-agent rather than asked, so a
sub-agent never waits on a question nobody sees.
