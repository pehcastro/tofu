---
topic: models
title: Models
summary: sign in to a subscription, name a model as source/model, choose the model a task runs, and set the tiers
verbs: models, login, usage
---

## What it is

A model is named by a slug, `source/model`, where the source is what pays
for it: `claude-sub/claude-opus-5` is Claude Opus 5 paid by your Claude
subscription, and `codex-sub/gpt-5.6-sol` is GPT-5.6 Sol paid by your Codex
subscription. The same model reached two ways bills two ways, so the source
is always written.

tofu speaks to a model only through a subscription you signed in to. Each
subscription has one default model, used when nothing else is chosen.

The orchestrator is the model you talk to. A sub-agent runs its own model,
and a tier is a name for a model a sub-agent file can point at:
`@genius`, `@smart`, `@worker` and `@dumb`. A file shared with Claude Code,
in `.claude/agents`, may say `opus`, `sonnet` or `haiku` instead, which
mean the genius, smart and worker tiers when those are set.

## Where it lives

- `~/.tofu/agent.db`: the credentials `tofu login` stores
- `~/.tofu/roles/orchestrator.yaml`, `.tofu/roles/orchestrator.yaml`: the
  model the orchestrator runs, one line, `model: source/model`. A
  `sub-agent.yaml` beside it names the model a spawn that names no
  sub-agent runs
- `~/.tofu/models/<provider>/<model>.yaml`, `.tofu/models/...`: your
  changes to one model, field by field, over the list tofu ships
- the `modelTier.genius`, `modelTier.smart`, `modelTier.worker` and
  `modelTier.dumb` settings: the model each tier names
- `~/.tofu/model-windows.json`: the context window table `tofu models --refresh` wrote

The project wins over your home, and your home wins over what tofu ships.
A role with nothing bound runs the subscription's default.

## Change it

Sign in, once per subscription. It opens the browser:

    tofu login claude-sub
    tofu login codex-sub

When the browser cannot reach tofu, add `--paste` and paste the address
or the code it shows.

Run the next task on another model: in the app, type `/models`, pick a
model. That lasts until tofu restarts. To make it stay,
bind the orchestrator in the same list, which writes
`.tofu/roles/orchestrator.yaml` in the project. By hand, for every project:

    model: claude-sub/claude-sonnet-5

in `~/.tofu/roles/orchestrator.yaml`. `tofu run` takes `--model` for one run.

Point a tier at a model:

    tofu settings set modelTier.smart claude-sub/claude-sonnet-5

A tier left empty runs the orchestrator's model.

Stop tofu from running a model with a file of your own,
`~/.tofu/models/anthropic/claude-sonnet-4-6.yaml`:

    use: excluded
    reason: too slow for me

## Check it

    tofu models

lists the models each subscription serves, the defaults at the top, and
why a model is excluded. `--json` prints the same with every field.

    tofu models --discover

asks each signed-in subscription which models it serves and compares that
with the list. `tofu models --refresh` reads the context window table again.

    tofu login --status

lists every stored credential with its number and state, and
`tofu usage` prints each subscription's quota windows and when they reset.

## Undo it

Delete the role file or the model file you wrote; the next layer down
wins. Delete a `modelTier` line from `settings.json` to empty the tier.
`tofu login --disable <number>` sets a credential aside without deleting it,
and `tofu login --enable <number>` brings it back.
