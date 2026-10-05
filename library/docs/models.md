---
topic: models
title: Models
summary: sign in to a subscription, name a model as source/model, choose the model a task runs, and set the tiers
verbs: models, login, logout, usage
---

## What it is

A model is named by a slug, `source/model`, where the source is what pays
for it: `claude-sub/claude-opus-5` is Claude Opus 5 paid by your Claude
subscription, and `codex-sub/gpt-5.6-sol` is GPT-5.6 Sol paid by your Codex
subscription. The same model reached two ways bills two ways, so the source
is always written.

tofu speaks through a subscription, with one default model each, or a key,
which spends money. The `meta` key reaches `meta/muse-spark-1.3`, `-1.2`
and `-1.1`, and the cheaper `-1.3-contributor` and `-1.2-contributor`, which
Meta may train on; `tofu models` says so beside each contributor model.

The orchestrator is the model you talk to. A sub-agent runs its own model,
and a tier is a name for a model a sub-agent file can point at:
`@genius`, `@smart`, `@worker` and `@dumb`. A file Claude Code also reads,
in `.claude/agents`, may say `opus`, `sonnet` or `haiku` instead, which
mean the genius, smart and worker tiers when those are set.

## Where it lives

- `~/.tofu/agent.db`: the credentials `tofu login` stores, the
  subscriptions, the openrouter key and the meta key, `META_MUSE_API_KEY`
- `models/windows/<provider>.yaml`, shipped: the window a vendor publishes
  for a model models.dev lacks; a reload never erases it
- `~/.tofu/roles/orchestrator.yaml`, `.tofu/roles/orchestrator.yaml`: the
  model the orchestrator runs, one line, `model: source/model`. A
  `sub-agent.yaml` beside it names the model a spawn that names no
  sub-agent runs
- `~/.tofu/catalog/models/<provider>/<model>.yaml`: the models
  `tofu models reload` found on your accounts that tofu does not ship.
  Each carries `from`, what listed it, and `found`, the day it was found
- `~/.tofu/models/<provider>/<model>.yaml`, `.tofu/models/...`: your
  changes to one model, field by field, over the list tofu ships
- the `modelTier.genius`, `modelTier.smart`, `modelTier.worker` and
  `modelTier.dumb` settings: the model each tier names
- `~/.tofu/model-windows.json`: the models.dev table `tofu models reload`
  wrote, with context windows, tool calls, efforts and image input

The project wins over your home, your home wins over the catalog, and the
catalog wins over what tofu ships. A role with nothing bound runs the
subscription's default.

## How a new model reaches tofu

`tofu models reload` asks each signed-in account which models it serves,
reads models.dev for their facts, and writes each id tofu does not know
into the catalog, allowed and never the default. Efforts and image input
come from the newest allowed model of its family, so `claude-sonnet-5-5`
follows `claude-sonnet-5`, else from models.dev. A model without tool
calls, or one the account stopped serving, is excluded. A catalog file
for a model tofu now ships is deleted, so the shipped file wins. It
prints a section per account, `✓ <n> served`, or `✗` and the command that
fixes it, a row per model, `+` new, `~` changed, `-` dropped, and a `✗`
line per tier that no longer resolves.

## Change it

Sign in, once per subscription. It opens the browser:

    tofu login llm claude-sub
    tofu login llm codex-sub

When the browser cannot reach tofu, add `--paste` and paste the address
or the code it shows. `tofu login llm meta` asks for a Meta Model API key,
shows its shape while you paste it, never the key, stores it only when
Meta's model list accepts it, and prints its last four characters; `tofu run --model meta/muse-spark-1.3` then uses it.

Run the next task on another model: in the app, type `/models`, pick a
model. That lasts until tofu restarts. To make it stay, bind the
orchestrator in the same list, which writes
`.tofu/roles/orchestrator.yaml` in the project. By hand, for every project:

    model: claude-sub/claude-sonnet-5

in `~/.tofu/roles/orchestrator.yaml`. `tofu run` takes `--model` for one run.

Point a tier at a model; a tier left empty runs the orchestrator's model:

    tofu settings set modelTier.smart claude-sub/claude-sonnet-5

Stop tofu from running a model with a file of your own,
`~/.tofu/models/anthropic/claude-sonnet-4-6.yaml`:

    use: excluded
    reason: too slow for me

## Check it

    tofu models

prints a section per subscription and keys, a row per model with its use
and window, `●` default, `✓` allowed, `○` excluded, `⚠` a notice, then
the roles and where the windows came from. `--json` prints one document
whose `data` carries every field, the reason a model is excluded among
them: `layer` is `library`, `catalog`, `global` or `project`, and `from`
says what listed a catalog model.
`--refresh` and `--discover` still run it for one release.

    tofu login --status

prints a card per credential with a bar per quota window, then each key,
`✓` by its last four characters or `○ not set`. `tofu usage` prints the
quota windows and when they reset. Both take `--json`.

## Undo it

Delete the role file or the model file you wrote; the next layer down
wins. A reload writes a deleted catalog file again while the account
serves it, so exclude the model in `~/.tofu/models` to keep it out.
Delete a `modelTier` line from `settings.json` to empty the tier.
`tofu login --disable <number>` sets a credential aside without deleting it,
and `tofu login --enable <number>` brings it back. `tofu logout llm claude-sub`
deletes the account; with two accounts signed in it names both and wants
the number, `tofu logout llm claude-sub 2`. `tofu logout llm meta` removes
the Meta key.
