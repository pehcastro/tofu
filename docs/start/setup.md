---
title: Setup
description: Connect tofu to your subscriptions and your own keys, store the classifier key, and check the result with tofu doctor.
order: 3
updated: 2026-10-22
---

Setup gives tofu credentials and settings. tofu takes two kinds of
credential, from any provider you enable:

- **Subscriptions**: `claude-sub` and `codex-sub`. tofu signs in through your
  browser the way each vendor's own command line does, and calls their models
  on that plan.
- **Your own keys (BYOK)**: `meta` for Meta's models, `openrouter` for
  models on OpenRouter, `typesafe` or `openrouter` for the classifier,
  `brave` for web search. A key call is billed by its provider, and each
  key is stored under the model type it pays for.

A credential says who pays. Which model each role runs is a binding you
make, `tofu models bind <role> <provider/model>`: tofu writes no model or
provider into its code, so nothing runs that you did not choose.

Credentials live in `~/.tofu/agent.db`. Settings live in
`~/.tofu/settings.json`, or a project's `.tofu/settings.json`, which wins.

## How credentials are kept apart

A model is named by what pays for it, so `claude-sub/claude-opus-5` and a
key-paid model never look alike in the picker or the bill. `tofu login`
names the role a credential serves, `llm`, `classifier` or `search`, so a
key is never stored without saying what it is for. A key is never taken as
an argument, so it never lands in your shell history; while you paste it,
tofu shows its shape, `tsk-…3f9a  51 chars`, never the key,
so you can see the paste worked. A classifier or Meta key is checked
against its provider before it is written. `tofu doctor` reads exactly what a run reads, so what it reports is what a turn will meet.

## The setup screen

The first time you open `tofu` in a project, it opens on a setup screen
instead of the chat. It has two steps, and the chat opens once both are
done:

1. **Language model**: the Claude subscription or the Codex one, which sign
   in through your browser, or a Meta API key, then the model the
   orchestrator runs: each signed-in model is offered as `bind <model>`.
2. **Classifier, jev, required**: a TypeSafe or OpenRouter key, then the
   classifier to bind. jev judges every tool call and shapes how tofu works,
   so a model alone is not enough to start.

```
set up tofu: two steps before the first turn

✓ 1. language model   meta · muse-spark-1.3

› 2. classifier · jev, required
   there is no typesafe key, so jev judges no tool call

   TypeSafe key
   › tsk-…3f9a  51 chars  prefix not checked

[enter] check and store   [esc] back
```

- Press the number of a choice. A subscription opens the browser sign-in
  and comes back to the screen; a key opens a field that shows the key's
  shape, never the key.
- Enter checks the key with its provider and stores it, the same check
  `tofu login` runs. A refused key stays in the field with the reason, and
  esc goes back to the choices.
- A step already set, by a login, the environment or the project's `.env`,
  shows as done and is skipped.
- A login run in another terminal moves the screen on within a second.
- Esc or q leaves.

## Sign in and add keys

The same steps from a terminal. Sign in to the subscriptions you have, add
the keys you want, then check:

```
tofu login llm claude-sub
tofu login llm codex-sub
tofu login llm meta
tofu login classifier typesafe
tofu models bind orchestrator claude-sub/claude-opus-5
tofu models bind classifier typesafe/jev-latest
tofu doctor
```

- Add `--paste` to a sign-in when the browser cannot reach this machine.
- `tofu logout <role> <provider>` removes what a login stored.
- `tofu login --disable <n>` sets a credential aside and `--enable <n>`
  brings it back, by the number `tofu login --status` shows.

## Settings

Press **Ctrl+K** in the app and type a setting's name, or open
`/settings`.

![Ctrl+K search over every setting, filtered to browser](./media/tui-search.png)

## What tofu doctor prints

`tofu doctor` exits 0 when tofu can run here and 1 when it cannot, naming
the command that fixes each problem. On a fresh machine:

```
  ✗ claude-sub  no subscription is signed in, so no model can answer
    → tofu login llm claude-sub
  ✗ jev         there is no typesafe key, so jev judges no tool call
    → tofu login classifier typesafe
```

Signed in, its `access` section reads:

```
access
  ✓ claude-sub  5h 5% · 7d 10% · 7d:fable 0%
  ✓ codex-sub   7d 0%
  ✓ jev         key · credential store
  ✓ anthropic   subscription
  ✓ codex       subscription
  ✓ openrouter  money
```

| Line | Do |
|---|---|
| `the credential is broken` | Run the `tofu login` it names again |
| `every window is spent, back at <time>` | Wait, or use the other subscription; `/status` shows each window |
| a model you expect is missing | **F5** in the model picker, or `tofu models reload` |
| `tofu browser` prints `✗` | `tofu browser install`, then load the folder it prints in `chrome://extensions` |

`tofu settings` lists every setting with where it came from;
`tofu settings set [--scope project] <key> <value>` changes one.
