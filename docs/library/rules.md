---
title: How a rule fires
description: A rule is one instruction with triggers, and it reaches a prompt only when the task matches them.
order: 2
updated: 2026-10-04
---

A rule is one YAML file, `<id>@1.yaml`, holding one instruction the model
reads (`text`), why it exists (`notes`), a `domain`, a `kind` and a `concern`.
A rule with no trigger reaches every prompt. A rule that declares triggers
reaches a prompt only when all of them match:

| Trigger | Fires when |
|---|---|
| `language` | A path the task names is in that language, or the sub-agent declares it |
| `framework` | A `package.json` the task reaches lists it (`next` counts as `react`, `nuxt` as `vue`) |
| `scope` | A named path falls inside the glob |
| `condition` | The regular expression matches the task |
| `task` | The task contains `debug`, `explore`, `review` or `write` |
| `role` | The prompt is the `orchestrator`'s or a `sub-agent`'s |

The paths a task names are its words with a `/`, a `\` or a file extension,
plus a sub-agent's `owns`. The `concern` orders rules in the prompt. A rule
that fires for every task of an agent goes in the system prompt; one that
fires for this task goes beside the task.

The lead gets rules about what to build, not how to write it. A rule for how
code is written, tested or built reaches the sub-agent that writes it: every
language and framework code rule, and any rule that declares
`shapes: writing`. A frontend rule about what the person sees, such as the
four states of a view, declares `shapes: design` and reaches the lead too, so
its brief can ask for it. A lead running alone, with `turnMaySpawn` off,
writes the code itself and gets every rule. On a React task this cuts the
lead's system prompt from 27,743 bytes to 23,297.

A `human` rule is text only. A `structural` or `decision` rule names a
checker that `tofu rules check` runs over files: in `shadow` mode a finding
warns, in `enforced` it blocks, and `off` drops the rule.

## Why rules fire only where they apply

**A rule that doesn't apply costs tokens and attention.** A Go rule never
reaches a Python task, and a React rule never reaches a project without
React. With a general agent you gather these rules into skills yourself;
tofu brings them and sends each one only where it fires.

**Rules are kept only when they change the result.** With the frontend rules,
against the same build without them, a Vue delete with undo passed 4/4
quality checks against 0/4, React details kept in the URL 2/2 against 0/2, and
a keyed Svelte list 3/3 against 1/3, with svelte-check finding nothing in 16
runs against 2 warnings and 2 errors.

**System prompt or task message** decides what the provider's cache can
reuse: rules that don't change with the task stay in the cached prefix.

## Checking, adding and turning off

- **See why a rule fired or didn't**: `tofu rules index "<task>" <path>`.
  Add `--role orchestrator` or `--role sub-agent` for the rules the lead or a
  sub-agent gets.
- **Add one**: `tofu rules add no_yaml "never write yaml by hand"`, then add
  triggers to the file.
- **Turn one off**: `tofu rules off em_dash`; `tofu rules remove` undoes it.

## Commands

```sh
tofu rules index "fix the failing go test in internal/turn/loop.go" internal/turn/loop.go
```

```text
Rules index · from the project                                  ● 32 of 157 fire

  task      fix the failing go test in internal/turn/loop.go
  paths     internal/turn/loop.go
  concerns  code_rules, process_discipline, safety, output_shape, tool_guidance
```

Each rule follows on its own line, `●` when it fires and `○` when it doesn't,
with the reason:

```text
○ fe_accessible_names         no package.json the task reaches lists react or
  svelte or vue or solid or angular or astro or preact
● go_context_first            the language go reached internal/turn/loop.go
● em_dash                     always on, the rule declares no trigger
```
