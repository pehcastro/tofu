---
title: Setup
description: Connect tofu to your subscriptions and your own keys, store the classifier key, and check the result with tofu doctor.
order: 3
updated: 2026-10-05
---

Setup gives tofu credentials and settings. tofu takes two kinds of
credential, from any provider you enable:

- **Subscriptions**: `claude-sub` and `codex-sub`. tofu signs in through your
  browser the way each vendor's own command line does, and calls their models
  on that plan.
- **Your own keys (BYOK)**: `meta` for Meta's models, `openrouter` or
  `typesafe` for the classifier, `brave` for web search. A key call is billed
  by its provider.

Credentials live in `~/.tofu/agent.db`. Settings live in
`~/.tofu/settings.json`, or a project's `.tofu/settings.json`, which wins.

## How credentials are kept apart

A model is named by what pays for it, so `claude-sub/claude-opus-5` and a
key-paid model never look alike in the picker or the bill. `tofu login`
names the role a credential serves, `llm`, `classifier` or `search`, so a
key is never stored without saying what it is for. A key is never taken as
an argument, so it never lands in your shell history; while you paste it,
tofu shows its shape, `sk-or-v1-…3f9a  73 chars  prefix ok`, never the key,
so you can see the paste worked. A classifier or Meta key is checked
against its provider before it is written. `tofu doctor` reads exactly what a run reads, so what it reports is what a turn will meet.

## Sign in and add keys

Sign in to the subscriptions you have, add the keys you want, then check:

```
tofu login llm claude-sub
tofu login llm codex-sub
tofu login llm meta
tofu login classifier openrouter
tofu doctor
```

- Add `--paste` to a sign-in when the browser cannot reach this machine.
- `tofu logout <role> <provider>` removes what a login stored.
- Without a classifier key the app still opens, and no tool call is judged.
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
  ✗ jev         there is no openrouter key, so jev judges no tool call
    → tofu login classifier openrouter
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
