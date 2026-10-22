---
title: Models and providers
description: The classifier models tofu can use, the providers that serve them, how each is paid for, and how a build is pinned.
order: 3
updated: 2026-10-21
---

The classifier is a role, `classifier`, bound to one model the way the lead
is. A classifier model has kind `classifier` in the catalog and is reached
through a provider on your key. Today tofu ships one classifier model, Jev,
from TypeSafe, the lab that trains it:

| Model | Provider | Key |
|---|---|---|
| `typesafe/jev-latest` | TypeSafe, `api.typesafe.ai/v1/systemone` | `TYPESAFE_API_KEY` |

The role runs it unbound. The key counts whether it is stored, in the
environment or in `.env`. Jev has one route: when TypeSafe cannot answer, a
tool call Jev would have judged is never run on a guess: under
`gatePrompt auto` it is refused with the reason, and under `ask` it is put
to you. `tofu login --status` names TypeSafe as Jev's route and the build it
is pinned to.

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
build it was fitted on. `tofu models reload` asks TypeSafe which build answers
`jev-latest` and pins it. When the build changes, the point drops to shadow
until it is fitted again, so a new build never changes a live verdict
unmeasured.

## Storing the key

- Store it: `tofu login classifier typesafe`. Remove it with
  `tofu logout classifier typesafe`.
- See the role: **Ctrl+K**, type `classifier`, under
  **Models & roles / Classifier model**, or
  `~/.tofu/roles/classifier.yaml`.

![Ctrl+K search showing Models & roles / Classifier model](./media/tui-search.png)

## Commands

```
tofu models
tofu judge [--dry-run] [--no-cache] [--no-rule] [--json] < request.json
```

`tofu judge --dry-run` prints the request it would send and sends nothing:

```
echo '{"state":{"tool":"bash","input":"rm -rf build"},"library":"tool_gate@6"}' | tofu judge --dry-run
```

```
{"model":"jev-latest","state":{...},"questions":{"risk":{"type":"score", ...
```
