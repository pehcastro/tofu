---
title: Models and providers
description: The classifier models tofu can use, the providers that serve them, how each is paid for, and how a build is pinned.
order: 3
updated: 2026-10-05
---

The classifier is a role, `classifier`, bound to one model the way the lead
is. A classifier model has kind `classifier` in the catalog and is reached
through a provider on your key. Today tofu ships one classifier model, Jev,
from two providers:

| Model | Provider | Key |
|---|---|---|
| `openrouter/jev-latest` | OpenRouter's decisions endpoint | `OPENROUTER_KEY` |
| `typesafe/jev-latest` | TypeSafe directly | `TYPESAFE_API_KEY` |

Unbound, the role runs OpenRouter when that key is stored, else TypeSafe.

## Why the classifier is a role

The role keeps the classifier swappable: a decision point asks "the
classifier", never a named model, so another model or provider is a catalog
file and a binding, not a code change. Prices differ per classifier and per
provider, so tofu does not assume one: it reads the cost each response
reports and keeps it on that decision's ledger row. A tool-gate judgment
answers in a median of 346 ms over 18 calls, so asking at every tool call
does not stall a turn.

## Pinning a build

`jev-latest` is an alias. Each answer names the dated build that served it,
such as `typesafe/jev-1.13-20260917`, and a live point's calibration lock
names the build it was fitted on. When the build changes, the point drops to
shadow until it is fitted again, so a new build never changes a live verdict
unmeasured.

## Choosing the provider

- Store a key: `tofu login classifier openrouter` or
  `tofu login classifier typesafe`. Remove it with `tofu logout classifier`
  and the same name.
- Choose the provider: **Ctrl+K**, type `classifier`, and pick under
  **Models & roles / Classifier model**. Or write
  `model: typesafe/jev-latest` in `~/.tofu/roles/classifier.yaml`, or the
  project's `.tofu/roles/classifier.yaml`.
- Go back to the default: delete that file.

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
{"model":"~typesafe/jev-latest","state":{...},"questions":{"risk":{"type":"score", ...
```
