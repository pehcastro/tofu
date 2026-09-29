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
