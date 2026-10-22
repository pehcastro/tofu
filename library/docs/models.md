---
topic: models
title: Models
summary: sign in to a subscription, name a model as source/model, bind the model each role runs, add a custom model, and set the tiers
verbs: models, login, logout, usage
---

## What it is

A model is named by a slug, `source/model`, where the source is what pays
for it: `claude-sub/claude-opus-5` is Claude Opus 5 paid by your Claude
subscription, and `codex-sub/gpt-5.6-sol` is GPT-5.6 Sol paid by your Codex
subscription. The same model reached two ways bills two ways, so the source
is always written. A key spends money: `meta` reaches `meta/muse-spark-1.3`,
`-1.2` and `-1.1`, and the cheaper `-1.3-contributor` and `-1.2-contributor`,
which Meta may train on; `tofu models` says so beside each contributor model.

Three roles run a model, and each runs what you bind, since tofu writes no
model, provider or order into its code. The `orchestrator` is the model you
talk to; with nothing bound, setup asks before the first turn. The
`classifier` judges the calls; with nothing bound the gate is off and no call
is judged. `sub-agent` is the model every sub-agent runs that names no model
of its own; with nothing bound, it runs the orchestrator's. A tier is a name a
sub-agent file can point at, `@genius`, `@smart`, `@worker` and `@dumb`; a
file Claude Code also reads may say `opus`, `sonnet` or `haiku` for the first
three. `thinkingSummary` picks the thinking a request asks for: `summarized`,
the default, or `omitted`, the body Claude Code and codex send. A spawn asking
for an effort the model lacks runs at the nearest one it has.

## Where it lives

- `~/.tofu/agent.db`: what `tofu login` stores, the subscriptions and each
  key under the model type it pays for
- `~/.tofu/roles/<role>.yaml`, `.tofu/roles/<role>.yaml`: one line,
  `model: source/model`, for `orchestrator`, `classifier` or `sub-agent`
- `~/.tofu/models/llms/*.json`, `~/.tofu/models/classifiers/*.json`, and the
  same under a project's `.tofu`: a custom model, one JSON object naming
  `provider` and `id`, and optionally `subscription`, `use`, `reason`,
  `notice`, `vision`, `plan`, `verbosity`, `reasoning_context`, `efforts` and
  `served_as`. A chat model runs on anthropic or openai through a
  subscription, or on openrouter or meta with a key; a classifier on typesafe
  or openrouter with a key. A file tofu cannot use is listed under `refused`
  with its reason and blocks nothing
- `~/.tofu/models/<provider>/<model>.yaml`, `.tofu/models/...`: your changes
  to one model, field by field. `plan: no` takes the plan tool away;
  `verbosity` and `reasoning_context` follow [codex](https://github.com/openai/codex)
  and go out on the codex wire as `text.verbosity` and `reasoning.context`.
  `codex-sub/gpt-5.6-sol` ships with `plan: no`, `verbosity: low` and
  `reasoning_context: all_turns`
- `~/.tofu/catalog/models/<provider>/<model>.yaml`: models `tofu models
  reload` found on your accounts, with `from` and `found`
- `models/windows/<provider>.yaml`, shipped, and `~/.tofu/model-windows.json`
  from models.dev: context windows, tool calls, efforts and image input
- the `modelTier.*` settings: the model each tier names

The project wins over your home, your home over the catalog, and the catalog
over what tofu ships.

## Change it

Sign in, once per subscription, through the browser; add `--paste` when the
browser cannot reach tofu:

    tofu login llm claude-sub
    tofu login llm codex-sub

`tofu login llm meta` and `tofu login llm openrouter` take a key, shown by
its shape and never the key, stored only once the provider accepts it.

Bind a role for every project; it reaches every open window and the session
holder on the next turn and the next spawn, with no `/reload`:

    tofu models bind orchestrator claude-sub/claude-sonnet-5
    tofu models bind classifier typesafe/jev-latest
    tofu models bind sub-agent claude-sub/claude-sonnet-5

In the app, `/models` picks the model for the next task until tofu
restarts, and its Roles tab binds the lead for this project.
`tofu run --model` sets one run. Point a tier at a model; an empty tier runs
the sub-agent binding:

    tofu settings set modelTier.smart claude-sub/claude-sonnet-5

Stop a model running with `~/.tofu/models/anthropic/claude-sonnet-4-6.yaml`
holding `use: excluded` and a `reason:`. When a subscription runs out, work
moves to the model `continueOnSubscription` or `continueOnAPIModel` names;
see `tofu docs usage`.

## Check it

    tofu models

prints a section per subscription and keys, a row per model with its use,
window, and the layer of one you added, `●` default, `✓` allowed, `○` excluded, `⚠` a notice, then
each role and what it is bound to, `unbound` when nothing is, and a `refused`
line per custom file tofu could not use. `--json` carries every field,
`layer` (`library`, `catalog`, `global` or `project`) and the reason a model
is excluded.

`tofu models reload` asks each signed-in account which models it serves,
writes each new one into the catalog, allowed and never the default, pins
the newest Jev build, and prints `+` new, `~` changed and `-` dropped, and
a `✗` line per tier that no longer resolves.

    tofu login --status

prints a card per credential with a bar per quota window, then each key by
its role, `✓` with its last four characters or `○ not set`. While the app or
`tofu serve` runs, an account whose token expires within 10 minutes, or
untouched for 8 days, is refreshed. Turns spread over accounts with equal
room, least recently used first. `tofu usage` prints the quota windows.

## Undo it

Delete the role file, the custom model or the model file you wrote; the next
layer down wins, and a role with no file is unbound. Exclude a catalog
model in `~/.tofu/models` to keep a reload from bringing it back. Delete a
`modelTier` line from `settings.json` to empty the tier.
`tofu login --disable <number>` sets a credential aside and `--enable`
brings it back; `tofu logout llm claude-sub 2` deletes account 2.
