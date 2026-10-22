---
title: Overview
description: How models work in tofu, providers and sources, what each model can do, which job each one does, and how the catalog, slugs, tiers, roles and the picker fit together.
order: 1
updated: 2026-10-22
---

A **provider** builds and serves a model: Anthropic, OpenAI, Meta, and for
the classifier TypeSafe. A **source** is what pays for a call:
a subscription, `claude-sub` or `codex-sub`, or a provider's key you bring,
such as `meta`. A model is named `source/model`:

- `claude-sub/claude-opus-5`: Claude Opus 5 on your Claude subscription
- `codex-sub/gpt-5.6-sol`: GPT-5.6 Sol on your Codex subscription
- `meta/muse-spark-1.3`: a Meta model on your Meta key

Each model in the catalog carries what it can do: its kind (`llm` or
`classifier`), its context window, the thinking efforts it accepts, and
whether it reads images. Each one is `default` (one per subscription),
`allowed`, or `excluded` with a reason. A model file with `plan: no` runs without the plan tool; `codex-sub/gpt-5.6-sol` ships that way, which cut its median requests on the rivals task from 11 to 7. A model file can also set `verbosity` (`low`, `medium` or `high`) and `reasoning_context` (`all_turns` or `current_turn`), which follow [codex](https://github.com/openai/codex): tofu sends them on the codex wire as `text.verbosity` and `reasoning.context`, and `codex-sub/gpt-5.6-sol` ships with `low` and `all_turns`, as codex sends, which cut its median output on the rivals task from 4,268 to 3,830 tokens.

## Which model does which job

Four jobs take a model:

| Job | Model it runs |
|---|---|
| **The lead** (orchestrator) | the `orchestrator` role; with nothing bound, setup asks before the first turn |
| **Sub-agents** | the agent's own model, or `agent-models.yaml`, or a tier, else the `sub-agent` role, else the lead's model |
| **The browser** | `browserModel`, else `modelTier.dumb`, then `modelTier.worker`, then the turn's model |
| **The classifier** | the `classifier` role; with nothing bound, the gate is off. See [Classifiers](/docs/classifiers/models) |

tofu writes no model, provider or order into its code. Every role runs what
you bind, and a role with nothing bound says so in one sentence with the
command that binds it, rather than running a model you never chose.

The catalog comes in layers, each over the last: what tofu ships, then
models `tofu models reload` found on your accounts, then `~/.tofu/models/`,
then a project's `.tofu/models/`.

![The model picker filtered to opus-5, showing the source claude-sub and that a subscription pays](./media/tui-models.png)

## Why models are split by source and job

The same model reached two ways bills two ways, so the source is part of the
name and the picker shows what pays. Splitting the work by job is where the
money goes: on one live browsing task, handing the browser to a Sonnet
sub-agent cut what Opus read from 1,604,780 tokens to 118,401. Tiers,
`@genius`, `@smart`, `@worker` and `@dumb`, let an agent file name a class of model rather than a model, so the same file runs on
whatever you point the tier at. Layers let you change one field of one model
without copying the list.

## Picking and binding models

- **Pick the model for the next task**: **Ctrl+L** opens on the model in use,
  then type to filter, **Shift+←/→** for the effort, **Enter**. It lasts
  until tofu restarts.
- **Keep it**: the picker's **Roles** tab binds the lead, which writes
  `.tofu/roles/orchestrator.yaml` and runs the next turn on it at once, the
  footer showing it as you pick; a subscription or a key, either is yours to
  pick. For every project, `tofu models bind orchestrator <source/model>`
  writes `~/.tofu/roles/orchestrator.yaml`; `classifier` and `sub-agent`
  bind the same way. A change reaches every open window and the session
  holder on the next turn and the next spawn, with no `/reload`.
- **Add a model tofu does not list**: one `.json` file in
  `.tofu/models/llms` or `.tofu/models/classifiers`, in your home or a
  project, below.
- **Find new models**: **F5** in the picker.
- **Point a tier**, or the browser, at a model:
  `tofu settings set modelTier.smart claude-sub/claude-sonnet-5`,
  `tofu settings set browserModel claude-sub/claude-sonnet-5`.
- **Exclude a model**: write `~/.tofu/models/<provider>/<model>.yaml` with
  `use: excluded` and a `reason:`. Delete the file to undo.

`tofu models reload` also finds the newest build of Jev, the model that judges each tool call, and pins it; when the gate was calibrated on an older build, reload says the gate runs in shadow until it is calibrated on the new one. The app's start-up reload only reports a newer build.

## Custom models

A file in `~/.tofu/models/llms/` or a project's `.tofu/models/llms/` adds a
chat model, and one in `models/classifiers/` adds a classifier. It is one
JSON object naming `provider` and `id`, and it may name `subscription`,
`use`, `reason`, `notice`, `vision`, `plan`, `verbosity`,
`reasoning_context`, `efforts` and `served_as`:

```json
{"provider": "openrouter", "id": "acme/coder-2", "efforts": ["low", "high"]}
```

`tofu models` then lists `openrouter/acme/coder-2` with its layer, and it
can be bound or picked like any other. A chat model runs on `anthropic` or
`openai` through a subscription, or on `openrouter` or `meta` with a key; a
classifier runs on `typesafe` or `openrouter` with a key. A file tofu cannot
use blocks nothing: `tofu models`, `tofu run` and the picker each show it
under `refused` with the reason, and every other model still loads:

```
✗ refused
  ~/.tofu/models/llms/broken.json:
  tofu has no llm module for anthropic with a key: an llm runs on anthropic or
  openai through a subscription, or on openrouter or meta with a key
```

Delete the file to remove the model.

## Commands

```
tofu models [reload] [--json]
tofu models bind <orchestrator|classifier|sub-agent> <source/model> [--json]
tofu agents set [--global] <name> <source/model>
tofu run --dir . --model <source/model> --effort <level> "<task>"
```

`tofu models`, trimmed:

```
claude-sub
  ✓ claude-sub/claude-haiku-4-5-20251001   allowed   200k
  ○ claude-sub/claude-opus-4-6             excluded  1M
  ● claude-sub/claude-opus-5               default   1M
  ✓ claude-sub/claude-sonnet-5             allowed   1M

codex-sub
  ● codex-sub/gpt-5.6-sol    default   1.05M

api key
  ✓ meta/muse-spark-1.3              allowed  1.05M
  ⚠ meta/muse-spark-1.3-contributor  allowed  1.05M
  ✓ typesafe/jev-latest              allowed         classifier
  ⚠ Meta may train on what you send to this model

roles
  ● orchestrator                           claude-sub/claude-opus-5-5
  ● classifier                             typesafe/jev-latest
  ○ sub-agents with no model of their own  unbound
```

`●` default or bound, `✓` allowed, `○` excluded or unbound, `⚠` a notice.
`--json` carries each excluded model's reason.
