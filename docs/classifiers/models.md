---
title: Models and providers
description: The classifier models tofu can use, the providers that serve them, how each is paid for, and how a build is pinned.
order: 3
updated: 2026-10-22
---

The classifier is a role, `classifier`, bound to one model the way the lead
is. A classifier model has kind `classifier` in the catalog and is reached
through a provider on your key. Today tofu ships one classifier model, Jev,
from TypeSafe, the lab that trains it, reached two ways:

| Model | Provider | Key |
|---|---|---|
| `typesafe/jev-latest` | TypeSafe, `api.typesafe.ai/v1/systemone` | `TYPESAFE_API_KEY` |
| `openrouter/jev-latest` | OpenRouter, serving `~typesafe/jev-latest` | `OPENROUTER_KEY` |

Bind the one you pay for, `tofu models bind classifier typesafe/jev-latest`,
and every classifier call goes through that provider and no other. With
nothing bound the gate is off, and tofu says so: `the gate is off because no
classifier is bound`, naming any classifier key already stored. The key
counts whether it is stored, in the environment or in `.env`. When the bound
provider cannot answer, a tool call Jev would have judged is never run on a
guess: under `gatePrompt auto` it is refused with the reason, and under `ask`
it is put to you. `tofu login --status` names Jev's route and the build it
is pinned to.

A classifier tofu does not ship is one `.json` file in
`~/.tofu/models/classifiers/` or a project's `.tofu/models/classifiers/`,
naming `provider` and `id`; a classifier runs on `typesafe` or `openrouter`
with a key. See [Overview](/docs/llms/overview).

## Why the classifier is a role

The role keeps the classifier swappable: a decision point asks "the
classifier", never a named model, so another model or provider is a catalog
file and a binding, not a code change. Prices differ per classifier and per
provider, so tofu does not assume one: it reads the cost each response
reports and keeps it on that decision's ledger row. A tool-gate judgment
answers in a median of 346 ms over 18 calls, so asking at every tool call
does not stall a turn.

## Pinning a build

`jev-latest` is an alias. Each answer names the versioned build that served
it, such as `jev-1.13.0`, and a live point's calibration lock names the
build it was fitted on. `tofu models reload` asks the bound provider which build answers
`jev-latest` and pins it. When the build changes, the point drops to shadow
until it is fitted again, so a new build never changes a live verdict
unmeasured.

## Storing the key

- Store it: `tofu login classifier typesafe`, or
  `tofu login classifier openrouter`. Remove it with
  `tofu logout classifier <typesafe|openrouter>`.
- Bind the role: `tofu models bind classifier <provider/model>`, which
  writes `~/.tofu/roles/classifier.yaml`.
- See the role: **Ctrl+K**, type `classifier`, under
  **Models & roles / Classifier model**, or `tofu models`.

![Ctrl+K search showing Models & roles / Classifier model](./media/tui-search.png)

## Commands

```
tofu models
tofu models bind classifier <provider/model>
tofu judge [--dry-run] [--no-cache] [--no-rule] [--json] < request.json
```

`tofu judge --dry-run` prints the request it would send and sends nothing:

```
echo '{"state":{"tool":"bash","input":"rm -rf build"},"library":"tool_gate@6"}' | tofu judge --dry-run
```

```
{"model":"jev-latest","state":{...},"questions":{"risk":{"type":"score", ...
```
