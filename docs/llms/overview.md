---
title: Overview
description: How models work in tofu, providers and sources, what each model can do, which job each one does, and how the catalog, slugs, tiers, roles and the picker fit together.
order: 1
updated: 2026-10-04
---

A **provider** builds and serves a model: Anthropic, OpenAI, Meta, and for
the classifier OpenRouter or TypeSafe. A **source** is what pays for a call:
a subscription, `claude-sub` or `codex-sub`, or a provider's key you bring,
such as `meta`. A model is named `source/model`:

- `claude-sub/claude-opus-5`: Claude Opus 5 on your Claude subscription
- `codex-sub/gpt-5.6-sol`: GPT-5.6 Sol on your Codex subscription
- `meta/muse-spark-1.3`: a Meta model on your Meta key

Each model in the catalog carries what it can do: its kind (`llm` or
`classifier`), its context window, the thinking efforts it accepts, and
whether it reads images. Each one is `default` (one per subscription),
`allowed`, or `excluded` with a reason.

## Which model does which job

Four jobs take a model:

| Job | Model it runs |
|---|---|
| **The lead** (orchestrator) | the `orchestrator` role, else the subscription's default |
| **Sub-agents** | the agent's own model, or `agent-models.yaml`, or a tier, else the lead's model |
| **The browser** | `browserModel`, else `modelTier.dumb`, then `modelTier.worker`, then the turn's model |
| **The classifier** | the `classifier` role. See [Classifiers](/docs/classifiers/models) |

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

- **Pick the model for the next task**: **Ctrl+L**, type to filter,
  **Shift+←/→** for the effort, **Enter**. It lasts until tofu restarts.
- **Keep it**: the picker's **Roles** tab binds the lead, which writes
  `.tofu/roles/orchestrator.yaml`. For every project, write
  `model: <source/model>` in `~/.tofu/roles/orchestrator.yaml`.
- **Find new models**: **F5** in the picker.
- **Point a tier**, or the browser, at a model:
  `tofu settings set modelTier.smart claude-sub/claude-sonnet-5`,
  `tofu settings set browserModel claude-sub/claude-sonnet-5`.
- **Exclude a model**: write `~/.tofu/models/<provider>/<model>.yaml` with
  `use: excluded` and a `reason:`. Delete the file to undo.

## Commands

```
tofu models [reload] [--json]
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
  ✓ openrouter/jev-latest            allowed         classifier
  ⚠ Meta may train on what you send to this model

roles
  ● orchestrator         claude-sub/claude-opus-5-5
  ● classifier           openrouter/jev-latest
  ○ (unnamed sub-agent)  unbound
```

`●` default or bound, `✓` allowed, `○` excluded or unbound, `⚠` a notice.
`--json` carries each excluded model's reason.
