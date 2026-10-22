---
topic: agents
title: Sub-agents
summary: add a sub-agent for every project or for one, choose the model it runs, and remove it
verbs: agents
---

## What it is

A sub-agent is a helper the model can hand one piece of a task to, like a
planner or a reviewer. Each one has a name, a description the model reads
to decide when to use it, the model it runs on, and the tools it may use.
tofu ships a few, and you can add your own. `rust-dev` also builds gpui-ce
interfaces, with the `gpui_` rules when a Cargo.toml depends on gpui-ce.

When the model hands one a piece of work, it names the paths that
sub-agent may change. A sub-agent given no paths still starts: it reads,
runs commands and reports, and every write or edit it tries is refused.

In a session the model steers its sub-agents with two tools. `subagents`
lists them with the paths each holds, and with a name it reads one's
report and conversation or diagnoses what it is doing now. `message`
sends one more work, and its `do` stops, kills, releases or backs one up,
or with `owns` grants, revokes or replaces the paths it holds. None of
these asks the gate. Each sub-agent also has a scratch folder outside the
project, under `~/.tofu/projects/<project>/scratchpad/`, where it writes
and deletes with no ask. See [Scratchpad](scratchpad).
A sub-agent from before a restart holds no paths until a message resumes it, so a new one can start on them. One still working when tofu closed comes back errored, the model is told at which step it stopped, and a report it finished before the close still reaches the model.
A sub-agent's one-hour cap counts only its own work, not time spent waiting on an answer. A crash inside one ends only that sub-agent, as errored with the stack in its trace, and a crash in a turn's background task ends only that task or turn. Sub-agents settled for over two hours fold into one row in the sub-agents tab and the chat; a click, `enter`, `f` or a search opens them; a search or a call's reference opens only the agent it found. With `projectManagement` set to `boardy`, a spawn names a `ticket`:
its owns become the paths, and the sub-agent moves it to review itself. See [Boardy](boardy). On a ticket the sub-agent proves each acceptance line with an evidence entry naming that line, says where it ended in one or two sentences, and moves the ticket to review leaving the lines that are not its to prove; tofu writes one reported entry from those, with a verdict (done, done with concerns, blocked or needs you) and each line as met, unmet or not mine to prove. A move to review with an unmet line is refused, naming it. Without a ticket, its last message is the report.
Clean reports finishing close together reach the model as one turn: each
waits 1.5 seconds for the next, 10 at most, and goes at once when no
sub-agent still runs. An ask, a report that is blocked, has concerns or needs
context, and a report from a run the model resumed never wait. A check never
starts a turn. A message you send mid-turn is answered first, before any other call; while only sub-agents run, the lead is idle and free to talk. Each report ends with a board line, the running count, tickets in review and free work; a lead that ends with free work and empty slots is checked in on while tofu is idle, and any message drops that check-in. The chat keeps only the conversation: the lead's own calls sit under the orchestrator in the sub-agents tab, and a report reads as a sentence, `finished` a ticket `and waits for your review`. A spawn asking for an effort the model lacks runs at the nearest one it has. A sub-agent allocates about 35% less than in 0.5.14: under one vCPU and 128 MB, tofu runs ten at about 0.7 MB each.
The lead may change up to 200 source lines a turn itself, in any folder, before it must spawn: a write counts only the lines it changes, and a file read with `cat` counts as read. On the rivals long edit the lead takes 78 s where it took 141 s.

A name is one plain lower-case word: letters, digits and `-`, starting
with a letter, like `planner` or `ts-dev`.

## Where it lives

A sub-agent is one file, `<name>.md`, read from these places, the first
one found winning:

- `.tofu/agents/` in a project, then `.agents/agents/` and
  `.claude/agents/`, in the order the `agentSources` setting names
- `~/.tofu/agents/`: your own sub-agents, in every project
- the sub-agents tofu ships

The model a sub-agent runs can also be set apart from its file, in
`agent-models.yaml`, one `name: source/model` line each. A line in the
project's `.tofu/agent-models.yaml` beats one in `~/.tofu/agent-models.yaml`,
and either one beats the model written in the file.

## Change it

Add a sub-agent to this project, run from inside the project:

    tofu agents add planner --description "plans the work before anyone writes" --model claude-sub/claude-opus-5

Add one to every project with `--global`, and give it only some tools
with `--tools`:

    tofu agents add --global reviewer --description "reviews a change" --model codex-sub/gpt-5.6-sol --tools read,search,glob

From anywhere else, name the project with `--dir`:

    tofu agents add --dir C:\code\shop planner --description "plans the work" --model claude-sub/claude-opus-5

Run a sub-agent on another model, including one tofu ships:

    tofu agents set planner codex-sub/gpt-5.6-sol
    tofu agents set --global qa claude-sub/claude-sonnet-5

`set` writes `agent-models.yaml` in the project, or in `~/.tofu` with
`--global`. `inherit` instead of a model makes it run on the same model
as the task that spawned it. `tofu models` lists every model you can name.

A model tofu does not know, a tool it does not have, a name that is not
one plain word, and a name that already has a file in the same place are
all refused, and nothing is written.

Every write prints what changed, `+` added, `~` changed or `-` removed,
the file, and the command that undoes it:

    + sub-agent planner  ~/code/shop/.tofu/agents/planner.md
      → undo: tofu agents remove planner

A refusal prints one `✗` line on stderr and the command to run instead,
and exits 1. With `--json`, a write prints one document instead.

## Check it

    tofu agents

opens with `Sub-agents · <n> defined`, then a row per sub-agent: `●` when
its model is set, `○` when it inherits, its name, its model, where it was
read from, `project`, `global` or `library`, and its description, with its
tools and references below. A file that cannot be read goes under
`broken` with a `✗` and the reason, and the command exits 1.

    tofu agents --json

prints one JSON document, whose `data` carries every field.

## Undo it

    tofu agents remove planner
    tofu agents remove --global reviewer

deletes the file `add` wrote and its line in `agent-models.yaml` in the
same place. Without `--global` it only looks in the project, and with it
only in your home. It prints the `add` command that brings it back.

A sub-agent tofu ships cannot be removed, since it is not a file of yours.
`tofu agents remove qa` refuses and names `tofu agents set qa` instead.

To undo a `set`, run `set` again with the model it had before, which the
`→ undo:` line names.
