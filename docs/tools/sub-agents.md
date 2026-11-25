---
title: Sub-agent tools
description: spawn, race, message, subagents and ask, the tools the lead and its sub-agents use to hand out work, weigh its cost and talk while it runs.
order: 7
updated: 2026-11-25
---

Five tools, tofu's own Go code. The lead gets `spawn` and `race`; the loop
adds `message` and `subagents` beside them; each sub-agent gets `ask`. How
the lead and sub-agents work together is on
[Sub-agents](/docs/features/orchestration).

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

## What a spawn costs, before it starts

`spawn` with `estimate` set to `true` answers what a sub-agent usually costs
in this project and starts nothing. It reads up to the last 20 finished runs
of that agent type in the project's sessions and gives the median USD, the
median number of model calls and the median wall time.

The lead weighs a spawn before it pays for one, from what that agent type
has cost here before rather than a guess. Every real spawn names the same
three medians in its result, so the lead sees them even when it never asks.

Name the agent type in `agent`, or leave it out for a sub-agent on the
lead's own model. A project with no finished run of that type says so:

```bash
tofu tool spawn '{"estimate":true,"agent":"go-dev"}' --dir .
```

```text
no history: go-dev has no finished run recorded in this project, so there is no estimate. nothing was started
```

With history the answer reads `estimate for go-dev, the median of its last
N finished runs in M sessions:` and then the USD, the calls and the wall
time. When none of those runs recorded a cost, it says `no cost recorded`
in place of the USD. An agent type tofu doesn't know is refused with the
list of the ones it does:

```text
error: spawn refused: nope is no sub-agent tofu found; the enabled sub-agents are advisor, go-dev, hunter, py-dev, referee, review, rust-dev, skeptic, ts-dev, browser, research, qa
```

## race

`race` builds two or three whole approaches to one job at once and keeps the
first one whose check passes. Each brief runs on its own sub-agent, in its
own copy of the project under the scratchpad, so two approaches that change
the same file never meet in your working tree.

Use it when you ask for rival approaches built and compared, for example
"try two fixes for this bug and keep the one that passes". Building them one
after another in the tree means saving, restoring and deleting the loser by
hand; `race` does it in one call and the loser never touches your files.
Every run is a sub-agent you pay for, and the result says what each one
cost.

When a run ends, its edits are checked on the real project through an
overlay, with `check` set to `build`, `test` or `command`. The first run
that passes is applied to the working tree, and the others are stopped. The
result lists every run's outcome, its cost, its requests and its check, and
the winner's diff. When no run passes, nothing is applied and the tree is
byte for byte as it was.

The sub-agents run on your model and spawn no sub-agents of their own. A
call with the wrong number of briefs is refused before anything runs:

```bash
tofu tool race '{"briefs":["round at the end"],"check":"test","path":"."}' --dir .
```

```text
error: race: 1 briefs, and a race takes 2 to 3
```

## Parameters

The lead calls them. You watch on the **sub-agents** tab, and raise the
limits in settings.

| Tool | Parameter | What it does |
|---|---|---|
| `spawn` | `task` | the brief, kept whole |
| `spawn` | `owns` | the paths the sub-agent may write |
| `spawn` | `agent` | a defined sub-agent, on its own model |
| `spawn` | `effort` | `none` to `max`; left out, the lead's; one the model lacks runs at the nearest it has; on a ticket, a second raise under one acceptance is refused |
| `spawn` | `estimate` | `true` returns the median USD, calls and wall time of that agent type's finished runs here, and starts nothing |
| `race` | `briefs` | 2 to 3 briefs, one whole approach each, its tests included |
| `race` | `check` | `build`, `test` or `command`, run on each run's edits |
| `race` | `path`, `name` | where the check runs, `.` when left out, and for `test` the one test to run |
| `race` | `command` | for `command`, the program and its arguments as a list |
| `message` | `to`, `text`, `stop` | more work, a correction, or a stop; resumes a finished one |
| `subagents` | none | each sub-agent's state, steps and last tool |
| `ask` | `question`, `why`, `default` | at most 300, 1,200 and 300 bytes; the default stands when no answer comes |
| `ask` | `options`, `refs` | 2 to 4 choices of 120 bytes, up to 5 places of 200 the lead may open; an ask over a cap is refused naming the field |

`subAgentsPerTurn` (10) and `subAgentDepth` (2) are settings, and `tofu
agents` lists the sub-agents `agent` can name.

## A spawn with no agent

A sub-agent spawned with no `agent` gets the tools such sub-agents called in
real sessions, not every tool the lead holds. The list is the `unnamed`
file tofu ships: read, write, edit, bash, glob, search, test, typecheck,
format, eval, plan, fetch, whatif, note, digest, checkpoint, forget,
project_report, changes, trail, budget, skill and spawn, with ask,
artifact_fetch, history, reference, the scratch tools, shell, message and
subagents always beside them, and the ticket tools when the spawn names a
ticket. Its first request carries 34 tools and 40,395 bytes of tool schemas
where it carried 65 and 82,615, and it can still spawn its own sub-agents.

Put an `unnamed.md` in `.tofu/agents/` or `~/.tofu/agents/` to change the
list. A tool tofu does not have is refused when the file is read, `tofu
agents` names it, and a spawn with no agent says the same thing until the
file is fixed:

```text
spawn refused: a sub-agent on the lead's model cannot run: its default tools in .tofu\agents\unnamed.md names the tool "nosuchtool", which tofu does not have
```

## Before the lead takes a hand-back

When a sub-agent says it is done, the `doneReview` setting reads its work
before the lead does. By default, `screen` sends it back while an acceptance
line in its brief has no evidence entry naming that line, and the report the
lead gets says `sent back` with each line it was sent back for. It makes no
model or Jev call. `tofu settings set doneReview off` takes the hand-back as
it is. See [Settings](/docs/features/settings).

## When the work cannot be done as asked

A sub-agent on a ticket that finds the platform or the code does not give
what the ticket asks calls `ticket_not_possible` with the reason and two to
four options. Its report says `not_possible`, its ticket moves to
needs-decision with that question, and tofu puts the question, the reason
and every option to you in the chat in the same turn, whatever the lead does.
The lead cannot close it or overrule it: a move it makes, by tool or in its
shell, is refused until you answer there or in catch up. The done review does
not send such a run back for unmet acceptance lines. A call with no reason goes
back to the sub-agent, naming the missing reason. See
[Boardy](/docs/features/boardy).

## What the lead reads

A sub-agent's report reaches the lead as a card of at most 8,000 bytes, so
a run that pasted a whole check log does not fill the lead's context. Each
part has its own cap: findings 1,600 (600 each), checks that did not pass
1,200 (240 each), files written 400, warnings 300, board lines 500, and the
sub-agent's last message what is left, up to 4,000 of its own bytes. Checks
that passed are a count. A cut part keeps its head and tail around
`...(N bytes cut)...`, and a cut list ends `+K more ... in the full report`.

When anything was cut, the whole report is kept as one artifact and the card
names it under its counts:

    full report: artifact 0eb58161518280f6128acef55c8badd7 holds all 100432 bytes, every check, finding and line whole: read any range with artifact_fetch

When it cannot be written, the line says so and why, and the card keeps its
caps. On reports from a real session, one of 13,462 bytes reaches the lead as
about 6,200, one of 39,469 as about 6,400, and one of 119,364, whose finding
carried a check script's whole output, as about 7,300.

## Who calls what it changed

A sub-agent's report lists each file it wrote once, and then an impact
list: each exported Go, Rust or TypeScript item it changed, with the file
and line of every call site outside that file, as the symbols tool finds
them by name. In a git project, changed means lines that differ from
`HEAD`; a new file, or a project without git, counts every exported item in
the file. A file the scan cannot read is named in the list as unread.

```
wrote calc/calc.go
impact, each exported item it changed and the call sites outside its file that the symbols tool matched by name:
  func Add in calc/calc.go: main.go:9, stats/stats.go:8
```

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
