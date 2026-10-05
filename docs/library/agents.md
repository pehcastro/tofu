---
title: Agents
description: "An agent is a Markdown file that defines a sub-agent: its model, tools, references and gate. tofu brings seven."
order: 3
updated: 2026-10-04
---

An agent file defines a sub-agent the lead can spawn. Its front matter says
what the agent is for and how it runs; its body is the agent's instructions.

| Field | Meaning |
|---|---|
| `name`, `description` | What the lead reads to pick the agent |
| `domain` | `dev`, `qa` or `general`: which rules it reads |
| `model` | A slug, `inherit`, a tier such as `@worker`, or `none` to disable it |
| `effort` | `none` to `max` |
| `tools` | The tools it may use; left out, all of the lead's |
| `references` | Library references placed whole in its prompt, up to 16,384 bytes |
| `language`, `gate` | For a language agent: its language and the checks it must pass |

![go-dev-1 at work, its tool calls in the sub-agents feed](./media/tui-subagents.png)

tofu brings seven: `go-dev`, `py-dev`, `rust-dev` and `ts-dev` write code
under a gate; `qa` verifies a change and files what is wrong; `research`
answers numbered questions on the `@worker` tier; `browser` does one
browsing task in a tab of tofu's own.

## Why tofu brings its own agents

**A general agent starts with no team.** With Claude Code or Codex you write
the agents and keep them up. tofu's are ready on the first run, and yours sit
beside them.

**A reference is long, so it goes only where it's named.** `go-concurrency`
reaches `go-dev` and nobody else. Against the same build without its library
folder, `rust-dev` handled deep recursion in 3 of 3 runs against 1 of 6 and
added `.unwrap()` 0 times against 2, `py-dev` left 0 ruff and mypy findings
against 3, and `go-dev` left 2 lint findings against 4.

**The model is set apart from the file.** A line in `agent-models.yaml`
overrides the file's `model`, so you change what an agent runs on without
editing it.

## Changing, adding and disabling

- **List them**: `tofu agents`.
- **Change a model**: `tofu agents set ts-dev claude-sub/claude-sonnet-5-5`,
  or `--global` for every project. `inherit` runs it on the lead's model.
- **Disable one**: add `qa: none` to `.tofu/agent-models.yaml`.
- **Add one**: `tofu agents add planner --description "plans the work" --model claude-sub/claude-opus-5`.
- **Remove yours**: `tofu agents remove planner`.

## Commands

```sh
tofu agents
```

```text
  ● ts-dev    claude-sub/claude-sonnet-5-5     library  Writes and fixes
    TypeScript in the project it is given. Reads before it edits, changes the
    least it can, and is done only when the project's own typecheck, lint and
    tests pass.
    tools      read, glob, search, symbols, edit, typecheck, test, write, bash
    from       agent-models.yaml
    reference  ts-strict-config  2.8 KB
```

`●` means the model is set, `○` that it inherits. A file that doesn't read is
listed under `broken` with the reason.
