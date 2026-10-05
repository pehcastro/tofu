---
title: Sub-agent tools
description: spawn, message, subagents and ask, the tools the lead and its sub-agents use to hand out work and talk while it runs.
order: 7
updated: 2026-10-04
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

## Parameters

The lead calls them. You watch on the **sub-agents** tab, and raise the
limits in settings.

| Tool | Parameter | What it does |
|---|---|---|
| `spawn` | `task` | the brief, kept whole |
| `spawn` | `owns` | the paths the sub-agent may write |
| `spawn` | `agent` | a defined sub-agent, on its own model |
| `spawn` | `effort` | `none` to `max`; left out, the lead's |
| `message` | `to`, `text`, `stop` | more work, a correction, or a stop; resumes a finished one |
| `subagents` | none | each sub-agent's state, steps and last tool |
| `ask` | `question`, `why`, `default` | the default stands when no answer comes |

`subAgentsPerTurn` (10) and `subAgentDepth` (2) are settings, and `tofu
agents` lists the sub-agents `agent` can name.
