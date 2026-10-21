---
title: Sub-agents
description: You talk to one lead. It plans, splits the work, and checks it. Sub-agents write the code in the background, each in its own paths and with the rules for its language.
order: 2
updated: 2026-10-21
---

You talk to one model, the **lead**. The lead does not write the code. It
plans the work and splits it into pieces. It hands each piece to a
**sub-agent**, checks what comes back, and answers you. Sub-agents run in the
background, so the lead stays free to talk to you while they work.

![The sub-agents tab: each agent on the left, grouped by state, and the selected agent's feed on the right](./media/tui-subagents.png)

## How a task runs

```steps
# You ask the lead
Type the task in the chat. The lead reads enough of the project to plan, and
no more.

# The lead splits the work
Each piece goes to a sub-agent with four things:
- **the brief**: what to do and what done looks like
- **the agent**: `ts-dev`, `go-dev`, `rust-dev`, `py-dev`, `qa`, or one of yours
- **its paths** (`owns`): the files it may change
- **its effort**: low for a mechanical change with exact text, higher for design work in unfamiliar code

# Sub-agents work in the background
`spawn` returns at once. Sub-agents whose paths do not overlap run side by
side. Each one gets its own conversation, its own model, and only the rules
for its language and framework. Every 30 minutes tofu writes a check on
each one still running: its steps, the files it changed, its gate, its last
tool and its cost. tofu builds it from what it records, with no model call.
A check never starts a lead turn on its own: it rides with the next one.

# The gate holds them to their checks
A language agent that changed a file in its language is sent back until its
checks ran after its last edit and passed: `go vet` and `go test` for go-dev,
`ruff` and `pytest` for py-dev, and so on. A project with no tests is not
blocked on tests.

# Reports come back to the lead
Each sub-agent ends by calling `finish` with a typed result: how the work
ended (done, done with concerns, blocked, or needs context), a summary, the
files it changed, the checks it ran with their exit codes, its findings
sorted into act on, consider, noted, and dismissed, its confidence, and its
open questions. tofu checks the files and exit codes against what it saw
itself, and a claim it did not see becomes a finding to act on. One that
ends without `finish` is sent back once, then ends blocked. The lead checks
the work and answers you.
```

## When the lead writes and when it spawns

Claude Code and Codex are general agents: the orchestrator, the sub-agents
and their rules are yours to build. tofu is a coding harness that brings
them.

**The lead does one small piece itself.** The lead may change up to 200
source lines a turn, in any folder: it edits, checks and answers without
spawning. A write counts only the lines it changes, and a file the lead read
with `cat` counts as read. Past the budget an edit is refused, and the
refusal names the sub-agent to spawn. A program's own output redirected into
a file, such as a schema printer, is not counted. On a long edit task the
lead finishes in 78 s where it took 141 s, and omp takes 91 s.

**Each sub-agent starts clean**, with only the rules for its own piece. A
sub-agent that writes Go never reads the React rules. Against the same agents
without the library, Vue undo passed 4/4 against 0/4, React kept details in
the URL 2/2 against 0/2, a Svelte sorted list kept its rows 3/3 against 1/3,
and Rust caught a stack overflow on deep input in 3 of 3 runs against 1 of 6.

**Side by side is faster.** The same app task took 4 min 58 s with 5 of 5
sub-agents running at once, and 21 min 18 s when none overlapped. Low effort
finished the same hidden tests in 63 s where medium took 78 s, so the lead
sets effort per spawn. An effort the model lacks runs at the nearest one it
has, so a spawn never fails on it.

**A sub-agent is light.** Each one allocates about 35% less than it did, and
under one vCPU and 128 MB of memory tofu runs ten sub-agents at about 0.7 MB
each.

**Nothing you type waits on a sub-agent.** Sub-agents used to run inside the
lead's tool call, and a message you typed waited 9 minutes for the lead.

## Talking while they work

When the lead's reply only starts sub-agents, its turn ends right there, on
its own words, or on one line per sub-agent saying what it is doing. It costs
no request to announce the work. Sub-agents started in one reply are named in
the order the reply lists them.

The lead has one inbox. Your messages, finished reports, asks and checks
all go into it. In the middle of a turn, the lead reads it at its next step.
When idle, each kind wakes it differently:

- **A message or an ask** starts a turn at once and never waits. An ask goes
  alone, so the reports around it are not spent on a turn that only answers.
- **A report that needs the lead** goes at once like an ask: one that ended
  blocked, with concerns or needing context, and one from a run the lead
  resumed with `message`.
- **Other finished reports** wait 1.5 seconds for more, up to 10 seconds from
  the first, and reach the lead as one turn. They go at once when no
  sub-agent is still running, or when you type. On the `bench/fanin` cassettes, three
  sub-agents finishing within a second and a half cost 1 lead turn instead
  of 3.
- **A check** never starts a turn; it rides with the next one.

- **The lead to a sub-agent**: `message` adds work or corrects it. A
  finished sub-agent starts again with its conversation kept, from its
  latest fork, and takes its paths back if nobody else holds them.
- **The lead controls a sub-agent**: `message` with `do`:
  - `stop`: a running one ends after the call it is in and keeps its paths
  - `kill`: it ends now, its shells are killed, its paths are freed
  - `release`: one that is not running frees its paths with no run
  - `backup`: its conversation and the files it wrote are kept under a
    name, and `message` with `from` resumes it from there
  - `grant`, `revoke`, `replace` with `owns`: add paths, take paths back,
    or set them outright, running or not. A path another sub-agent holds is
    refused, as at spawn. A grant resumes one that ended, so the write it was
    refused runs again.

  None of these asks the gate: they act only on the lead's own sub-agents.
- **A sub-agent to the lead**: `ask` sends one question with a default. With
  no reply in 30 seconds, the default stands, and the report says so.
- **A sub-agent's call the gate asks about**: it goes to the lead, never to
  you, with the call, jev's verdict and its risk. The lead answers with
  `message` and `answer` allow, deny or `allow_here`, which stands for that
  kind of call from that sub-agent until its run ends, so the lead is asked
  once rather than at every call. A lead turn that only answers asks ends
  without a chat message. The sub-agent waits up to 5 minutes, and with no
  answer the call is refused. The call's arguments are cut
  to 4 KB, and `lookup` with the call id shown returns it whole. A call that
  stays inside the sub-agent's scratch folder runs without asking the lead. A PreToolUse hook's ask goes
  to the lead the same way. A sub-agent's own sub-agent
  asks the sub-agent that started it.
- **Progress**: `subagents` lists each sub-agent's state, its last tool
  call, the paths it holds and every grant or revoke of them. With a name, `show read` gives its report,
  files and conversation without resuming it, so far when it still runs,
  and `show diagnose` gives its open calls, the last 10 lines of each of its
  shells, its last request and its failures. `do backup` on a running one
  keeps its conversation so far.

After a restart, the sub-agents of the session come back holding no paths,
so the lead can start a new one on the same files at once. One that was
still working when tofu closed comes back errored, and the lead is told at
which step it stopped; a report it finished before the close still reaches
the lead.

A sub-agent's one-hour cap counts only its own work: time it spends waiting
on the lead or on you does not count. A crash inside a sub-agent ends only
that sub-agent, as errored with the stack in its trace, and the lead and the
app carry on; a crash in a parallel tool call or another background task of
a turn ends only that task or that turn, its stack in a crash file. A crash in the interface turns mouse reporting and the
full-screen mode off before it prints the error, so the terminal stays usable.

## Paths

A sub-agent may change only the paths it owns, and two running
sub-agents never hold the same path. See [Ownership](./ownership).

A sub-agent that only runs a command, reads or researches is spawned with
no `owns`. It starts at once, and every `write` or `edit` it tries is
refused with the reason, so it reports what it found instead of inventing a
file to hold. A sub-agent it spawns holds no paths either.

Outside its paths, a sub-agent's `bash` only reads: it reads, lists,
searches, inspects and runs the project's own checks, and a command that
would write, delete or run other code is refused with the reason. See
[Ownership](/docs/features/ownership).

Every sub-agent also gets a scratch folder outside the project, under
`~/.tofu/projects/<project>/scratchpad/sessions/<session>/agents/<name>/`, for
its logs, captures and probes. It writes and deletes there with no paths held
and no ask, and nothing it writes there reaches the repository. See
[Scratchpad](/docs/features/scratchpad).

With `projectManagement` set to `boardy` and a board in the project, the lead
spawns a sub-agent with `ticket: DEMO-2` rather than owns. A project with no
board gets neither the board rules nor the ticket tools until `tofu boardy
init` makes one. The ticket's owns become the sub-agent's paths, the
ticket is assigned to `<session>/DEMO-2` and moves to doing, and the
sub-agent logs its evidence and moves the ticket to review itself. Every shell
tofu starts carries `TOFU_SESSION`, so `tofu boardy` run there acts as that
agent. The lead's session must manage the board: `tofu boardy manager add
<session>`. See [Boardy](/docs/features/boardy).

## What you see

The chat stays on the conversation with the lead. The lead's own reads,
edits and shell calls are not rows in the chat: they sit under the
orchestrator in the sub-agents tab and in All activity, and one status line
above the composer names the newest call by its `[tool#id]`, such as
`working | 1s bash sleep 3 · (2) tools · shell (1) [tool#100bad]`. A running
sub-agent shows as one line saying what it works on, such as
`running [&go-dev-1] TOFU-12 read the note`, and its report reads as a
sentence, such as `[&go-dev-1] finished TOFU-12 and waits for your review`.
One that has been on a call for
`subAgentWatchSeconds` says what it is doing, such as `building, 12m`, and
reads `stalled` only when nothing has moved for that long.

![The finished answer, with go-dev's report folded under it](./media/tui-task-answer.png)

The **sub-agents** tab lists every agent by state, **Active**, **Waiting** or
**Dead**, with the lead first, and the selected agent's thinking and tool
calls on the right. **All activity** shows every agent at once. Sub-agents
settled for over two hours fold into one row there and in the chat; a click,
`enter`, `f` or a search opens them, and a search or a call's reference
opens only the agent it found: at 450 folded sub-agents, opening one by search holds 18.5 MB where it
held 166.7 MB, and the search itself streams the session file, allocating
658 MB where it took 4.64 GB. At 450 sub-agents this keeps the app at about 150 MB and a
chat frame at about 2 ms.

The lead keeps its slots filled. Each report it gets ends with a board line,
such as `board: 0 of 4 sub-agents running; TOFU-1 is review; free todo:
TOFU-5, TOFU-6`. A lead that ends its turn with free work and empty slots is
checked in on after `subAgentCheckSeconds` while tofu is idle, and any
message you send drops that check-in and runs at once.

To run the lead alone: `tofu settings set turnMaySpawn false`.

## Keys and settings

| Key or setting | Does |
|---|---|
| `Alt+2` | Open the **sub-agents** tab |
| `Alt+3` | Open the **file edits** tab |
| `t` on the sub-agents tab | Show or hide thinking |
| `agentFeeds` (`full`) | `full` keeps every event, `summary` the latest 200, `off` only the list |
| `chatShowsTools` (off) | Show every tool call in the chat too |
| `subAgentsPerTurn` (10) | Sub-agents running at once |
| `subAgentDepth` (2) | How deep sub-agents may spawn their own |
| `subAgentCheckSeconds` (1800) | Seconds between checks on a running sub-agent; 0 sends none |
| `subAgentWatchSeconds` (600) | Seconds a sub-agent's call runs before its row says what it is doing, and with nothing moving, before it reads stalled |
| `verifySubAgents` (true) | On: a report tofu reads as clean, done with its gate passed after the last edit, is answered from as it stands; for any other, before it calls the work done, the lead reads what the sub-agent changed, runs the build or tests again or opens what it made, and says what it saw. Off: the lead takes a finished report as the result, unless the report says something failed. `tofu rules off verify_sub_agents` also turns it off |
| `turnMaySpawn` (`true`) | Whether the lead may spawn at all |
