---
title: Claude subscription
description: The claude-sub source, how tofu signs in to your Claude plan, and how its prompt is cached.
order: 2
updated: 2026-10-28
---

`claude-sub` is the source for Claude models on your Claude plan. tofu signs
in at `claude.ai` the way Claude's own command line does, stores the
credential in `~/.tofu/agent.db`, refreshes it as it ages, and speaks
Anthropic's protocol on the anthropic wire. Its default model is
`claude-sub/claude-opus-5`, and its quota comes in a five hour and a seven
day window.

## Quota and the prompt cache

The plan you already pay for runs the lead and the sub-agents, so a long
session spends quota rather than money. The wire writes the tools and the
system prompt to the cache once and reads them back on every later step:
over 24 recorded turns, 88.0% of the billed input was a cache read. Effort runs from
`low` to `max`; a level outside that is refused rather than swapped for a
neighbour.

## Signing in and managing accounts

- Sign in: `tofu login llm claude-sub`. Add `--paste` when the browser
  cannot reach this machine.
- Add a second account: sign in again with it. tofu runs on the one with
  room and moves when it runs out.
- Set one aside or bring it back: `tofu login --disable <n>`,
  `--enable <n>`.
- See why one was signed out: when Anthropic refuses a refresh, tofu sets
  the account aside, and its card in `tofu login --status` says when and
  what Anthropic answered, such as
  `refresh failed 2h ago · 400 invalid_grant: Refresh token [redacted] not found or invalid`.
  tofu keeps the last 20 attempts for each account, with the time, the
  status and the answer, and never the token. `tofu login --status --json`
  lists them under `attempts`, and `tofu logout` clears them.
- Run several windows at once: one tofu process on the machine keeps the
  signed-in accounts fresh for every tofu running, and the others wait to
  take over when it closes, so two windows never refresh one token twice.
- Sign one out: `tofu logout llm claude-sub`. With two accounts signed in,
  tofu names both and asks for the number: `tofu logout llm claude-sub <n>`.
- Pick a Claude model: **Ctrl+L**, provider `claude-sub`. `/status` shows
  each window.

## Commands

```
tofu login llm claude-sub [--paste]
tofu logout llm claude-sub [n]
tofu login --status [--redact]
tofu usage [--history]
```

`tofu doctor` shows the windows used, such as
`✓ claude-sub  5h 5% · 7d 10% · 7d:fable 0%`. How each reading is taken and
shared is on [Subscription quota](/docs/llms/quota).
