---
title: Customization
description: Where tofu reads your own rules, sub-agents, skills and instruction files, including .claude, .agents, CLAUDE.md and AGENTS.md, which one wins, and how to override a rule tofu ships.
order: 4
updated: 2026-10-05
---

You add to the library with plain files, for every project or for one. tofu
reads its own `.tofu` folders, and also the `.agents` and `.claude` folders
and the `AGENTS.md` and `CLAUDE.md` files other coding agents use.

| What | Read from, in order | Wins |
|---|---|---|
| Rules | built-in, `~/.tofu/rules/`, `.tofu/rules/` | The last with that id |
| Sub-agents | `.tofu/agents/`, `.agents/agents/`, `.claude/agents/`, `~/.tofu/agents/`, built-in | The first with that name |
| Skills | `.tofu/skills/`, `.agents/skills/`, `.claude/skills/`, from the working directory up to the git root, then `~/.tofu/skills/` | The first with that name |
| Instructions | `~/.tofu/AGENTS.md`, then the nearest `AGENTS.md` and `CLAUDE.md` | All sent; the later outranks |
| Settings | `~/.tofu/settings.json`, `.tofu/settings.json` | The project |

From your home, tofu reads only `~/.tofu`, never `~/.claude` or `~/.agents`.

## Why tofu reads other agents' files

**A project set up for Claude Code works as it is.** A Claude Code agent file
is read with its tool names mapped (`Grep` is `search`, `WebFetch` is
`fetch`), and its `references` and `language` are ignored, since those are
tofu's.

**Your workflow layers on top of tofu's.** Claude Code leaves the orchestrator
and sub-agent workflow to you; tofu brings its own, and your files add to it
or replace a piece of it, never the whole.

**Skills cost little until used.** The model sees each skill's name and
description and loads the rest when the task matches. With skills listed, it
loaded the right one in 2 of 3 tasks, against 0 of 3 without, for 247 cached
tokens a request.

**Only `~/.tofu` from home,** so another tool's global files never change
tofu's behaviour behind your back.

## Adding your own

- **Rules**: `tofu rules add [--global] <id> "<text>"` and
  `tofu rules remove <id>`. Changing a rule tofu ships is an override, below.
- **Sub-agents**: `tofu agents add`, `set` and `remove`, or a file in
  `.tofu/agents/`. `agentSources` (`tofu,agents,claude`) sets the folder order.
- **Skills**: a folder with a `SKILL.md` under `.tofu/skills/`.
  `tofu settings set skills off` turns them off.
- **Instructions**: when `AGENTS.md` and `CLAUDE.md` share a folder,
  `instructionSources` picks `agents-first` (default), `claude-first` or
  `both`. They're cut at `projectInstructionsCap`, 32,768 bytes.

## Overriding a rule

An override switches off one of tofu's rules, or replaces what it says, for
this project or for every project. It is a file in `.tofu/rules/` or
`~/.tofu/rules/` that names the rule and its version and says why.

**The rules are written for everyone, and your project can need the
opposite.** A public SDK wants unit tests that `no_unit_test_after_code`
forbids. The reason is required so whoever reads the project later knows why
the rule is off.

**tofu asks before it works around a rule.** When a rule stops it doing what
you asked, it names the rule and the rule's own reason, and waits:

```text
· A rule stops me: no_unit_test_after_code.
  Its reason: this is the ordering that makes a test worthless rather than the test itself. ...
  You asked: unit tests on the parser. Override it?
? override no_unit_test_after_code?
  [1] this project  [2] everywhere  [3] no
```

**1** writes the override into `.tofu/rules/`, **2** into `~/.tofu/rules/`,
both marked `by: asked`. **3** writes nothing, and tofu works within the rule.
Each question is asked fresh, never answered from an earlier one. From the
next turn the prompt says the rule was switched off on purpose.

**An override names the version it changed.** When a new tofu changes the
rule, the override is marked stale and the rule runs as shipped until you
look at it again.

## Commands

```sh
tofu rules off no_unit_test_after_code --project --reason "public SDK"
tofu rules add no_unit_test_after_code --project --reason "public SDK" "<new text>"
tofu rules overrides
tofu rules restore no_unit_test_after_code --project
```

`off` and `add` write an override. `overrides` lists each one with its layer,
reason and date, and whether it is stale. `restore` removes it. `tofu rules
list` marks every overridden rule, and `tofu doctor` counts them.

```sh
tofu settings get instructionSources
```

```text
agents-first
```

`tofu rules list`, `tofu agents` and `tofu reload` show what tofu read from
every layer.
