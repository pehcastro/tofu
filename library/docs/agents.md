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
tofu ships a few, and you can add your own.

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

Set up three at once by running `add` three times, one model each:

    tofu agents add planner --description "plans the work" --model claude-sub/claude-opus-5
    tofu agents add coder --description "writes the code" --model claude-sub/claude-sonnet-5
    tofu agents add checker --description "checks the result" --model codex-sub/gpt-5.6-sol

Run a sub-agent on another model, including one tofu ships:

    tofu agents set planner codex-sub/gpt-5.6-sol
    tofu agents set --global qa claude-sub/claude-sonnet-5

`set` writes `agent-models.yaml` in the project, or in `~/.tofu` with
`--global`. `inherit` instead of a model makes it run on the same model
as the task that spawned it. `tofu models` lists every model you can name.

A model tofu does not know, a tool it does not have, a name that is not
one plain word, and a name that already has a file in the same place are
all refused, and nothing is written.

Every write prints what changed, the file, the command that undoes it, and
the sub-agent as `tofu agents` now shows it:

    added planner to the project sub-agents
    file: C:\code\shop\.tofu\agents\planner.md
    undo: tofu agents remove planner

## Check it

    tofu agents

lists every sub-agent: its name, the model it runs, where that model was
chosen, where it was read from, and its file. A line with `refused` says
why the sub-agent cannot run.

    tofu agents --json

prints the same as JSON.

## Undo it

    tofu agents remove planner
    tofu agents remove --global reviewer

deletes the file `add` wrote and its line in `agent-models.yaml` in the
same place. Without `--global` it only looks in the project, and with it
only in your home. It prints the `add` command that brings it back.

A sub-agent tofu ships cannot be removed, since it is not a file of yours.
`tofu agents remove qa` refuses and names `tofu agents set qa` instead.

To undo a `set`, run `set` again with the model it had before, which the
`undo:` line names.
