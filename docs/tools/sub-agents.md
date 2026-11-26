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

## How long a spawn takes, before it starts

`spawn` with `estimate` set to `true` answers how long the work takes in
all and starts nothing. The answer is the whole work, not the model's time
alone: the agent's own time plus the time its tests, builds and verify
steps take, each part with how many past runs had it. It is the answer
[estimate_time](/docs/tools/own-context#estimate_time) gives, from the 10
newest finished runs of the same agent on the same model in this project,
widened one named step at a time when there are none. Every real spawn
names the same estimate in its result, so the lead sees it even when it
never asks.

Name the agent in `agent`, or leave it out and the agent is picked as in
[A spawn with no agent](#a-spawn-with-no-agent). With finished runs
recorded, the answer reads:

```bash
tofu tool spawn '{"estimate":true,"agent":"go-dev","task":"fix the rounding in the invoice total and add a test"}' --dir .
```

```text
estimate for go-dev on any model, the whole work: about 5m0s, done by 7m30s. agent 2m34s, tests 1m24s (3 of 3 runs tested), builds 37s (3 built), verify 25s (3 verified). basis: the 5 newest go-dev runs in this project on any model; sized to the 3 of them nearest this work's 52 chars; each part is its average share of the middle half of those runs by time, applied to their median. nothing was started
```

`done by` is the figure with a margin; a basis from another project takes a
wider one. With no finished run of that agent or of any agent on the model
anywhere, it says so:

```text
estimate for go-dev on any model: first run, and no finished run of go-dev or of any agent on this model is recorded in any project, so there is no estimate. nothing was started
```

An agent tofu doesn't know is refused with the list of the ones it does:

```text
error: spawn refused: nope is no sub-agent tofu found; the enabled sub-agents are advisor, go-dev, hunter, py-dev, referee, review, rust-dev, skeptic, ts-dev, zig-dev, browser, research, qa
```

## race

`race` builds two or three whole approaches to one job at once. Each brief
runs on its own sub-agent, in its own copy of the project under the
scratchpad, so two approaches that change the same file never meet in your
working tree.

### When to race

Race when you ask for two or three whole approaches built and compared, for
example "try two fixes for this bug and keep the one that passes", or "build
both and keep the smaller one". Building them one after another in the tree
means saving, restoring and deleting the loser by hand; `race` does it in
one call and the loser never touches your files. For one or two small
edits to existing files, the lead uses [whatif](/docs/tools/history#whatif)
instead, and `edit` for one fix. Every run is a sub-agent you pay for, and
the result says what each one cost.

### Keep first, or keep all and pick

When a run ends, its edits are checked on the real project through an
overlay, with `check` set to `build`, `test` or `command`. `keep` follows
your words:

- `keep: first`, the default, is for when any passing approach will do. The
  first run that passes is applied to the working tree and the others are
  stopped before their checks. The result lists every run's outcome, its
  cost, its requests and its check, and the winner's diff.
- `keep: all` is for when you want the approaches compared, or one kept by a
  rule such as the smaller, simpler or faster one. Every run goes to its
  check and nothing is applied. The result starts with a race id and lists
  every run's outcome, check, size in files and lines, and diff. The lead
  compares them by your rule and calls `race` again with only `race` set to
  that id and `pick` set to the chosen run number, which applies that run
  alone. A race nobody picks from is swept after 60 minutes, and one is gone
  once a run is picked.

When no run passes, nothing is applied and the tree is byte for byte as it
was. A picked run is not written when a file it changes has changed in the
working tree since the race ran.

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
| `spawn` | `estimate` | `true` returns how long the whole work takes, agent, tests, builds and verify, from that agent's finished runs, and starts nothing |
| `race` | `briefs` | 2 to 3 briefs, one whole approach each, its tests included |
| `race` | `check` | `build`, `test` or `command`, run on each run's edits |
| `race` | `path`, `name` | where the check runs, `.` when left out, and for `test` the one test to run |
| `race` | `command` | for `command`, the program and its arguments as a list |
| `race` | `keep` | `first`, the default, applies the first run that passes; `all` checks every run, applies none, and returns a race id |
| `race` | `race`, `pick` | a `keep: all` race id and the run number to apply, with no briefs |
| `message` | `to`, `text`, `stop` | more work, a correction, or a stop; resumes a finished one |
| `subagents` | none | each sub-agent's state, steps and last tool |
| `ask` | `question`, `why`, `default` | at most 300, 1,200 and 300 bytes; the default stands when no answer comes |
| `ask` | `options`, `refs` | 2 to 4 choices of 120 bytes, up to 5 places of 200 the lead may open; an ask over a cap is refused naming the field |

`subAgentsPerTurn` (10) and `subAgentDepth` (2) are settings, and `tofu
agents` lists the sub-agents `agent` can name.

## A spawn with no agent

A spawn with no `agent` runs as the defined agent that claims every source
path it owns, by that agent's `matches:` globs or its language, or every
path its task names when it owns none. The result says why, such as `it
runs as go-dev because go-dev claims every source path it owns`. When two
agents claim one path, when the paths split between agents, or when no
enabled agent claims a source file, it says `no agent was picked` and why,
and runs as a sub-agent on the lead's model.

That sub-agent gets the tools such sub-agents called in
real sessions, not every tool the lead holds. The list is the `unnamed`
file tofu ships: read, write, edit, bash, glob, search, test, typecheck,
format, eval, plan, fetch, whatif, note, digest, checkpoint, forget,
project_report, changes, trail, budget, skill and spawn, with ask,
artifact_fetch, history, reference, the scratch tools, shell, message and
subagents always beside them, and the ticket tools when the spawn names a
ticket. Its first request carries 34 tools and 40,395 bytes of tool schemas,
against 65 and 82,615 with every tool the lead holds, and it can still spawn
its own sub-agents.

Put an `unnamed.md` in `.tofu/agents/` or `~/.tofu/agents/` to change the
list. A tool tofu does not have is refused when the file is read, `tofu
agents` names it, and a spawn with no agent says the same thing until the
file is fixed:

```text
spawn refused: an unnamed sub-agent cannot run: its default tools in .tofu\agents\unnamed.md names the tool "nosuchtool", which tofu does not have
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

A sub-agent's report reaches the lead as a card of at most 7,500 bytes:
7,000 for the card and 500 for its board lines, so a run that pasted a
whole check log does not fill the lead's context. Each part of the card has
its own cap: the head 300, the full report line 300, findings 1,600 (600
each), checks that did not pass 1,200 (240 each), attempts 400 (160 each),
files written 400, impact 500, asks 300 and warnings 300, and the
sub-agent's last message what is left of the 7,000, up to 4,000 of its own
bytes. Checks
that passed are a count. A sub-agent's shells stop when it ends, and the
report names each one with any failure lines it had printed, such as
`bash-1 had printed failure lines: error: FileNotFound`. A cut part keeps its head and tail around
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
`tofu models`, `tofu version`, `tofu serve --schema` and `tofu drive desk`
run there, and every other tofu verb is refused. Of `.tofu`, it reads only
`.tofu/rules`.

A sub-agent that holds only part of the project runs a command without
asking when every step reads or runs one of the project's own checks. Reads
include `awk` without `system()`, pipes or `print >`, `for` loops over
reads, and `xargs` or `find -exec` handing paths to a pure read, never to
`rm`, `cp`, `mv`, `sed -i` or a check; `find -delete` is refused. Checks
include `zig version`, `zig fmt --check`, and `zig build` of steps named
`test`, `check`, `lint`, `typecheck` or `vet`; `zig build` of any other
step runs in a build root it holds a path in, and `--prefix`, `--cache-dir`
and `--build-file` are refused. The project's own built binaries run by
their relative path under `zig-out/bin`, `target/debug` or
`target/release`, with arguments inside the project.

`cargo build`, `cargo run` and a binary they built run for a crate the
sub-agent holds a path in, and every cargo command builds into the
project's one shared target, so `--target-dir` is refused. `powershell
-File` on a script inside a path the sub-agent holds runs, and `timeout
<n>` wraps any command it may run. A command the gate
would ask you about is refused in a sub-agent rather than asked, so a
sub-agent never waits on a question nobody sees.
