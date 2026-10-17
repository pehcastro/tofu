---
title: Codex subscription
description: The codex-sub source, how tofu signs in to your Codex plan, and how it mixes with Claude in one session.
order: 3
updated: 2026-10-05
---

`codex-sub` is the source for OpenAI models on your Codex plan. tofu signs in
at OpenAI the way the Codex command line does, receives the result on port
1455 of this machine, stores it beside any Claude credential, and speaks the
Codex protocol. Its default model is `codex-sub/gpt-5.6-sol`, and it accepts
every effort from `none` to `max`. It runs without the plan tool, which
took a task on the rivals bench from 11 requests to 7.

## Claude and Codex in one session

Both subscriptions can be signed in at once, and every job picks its own
model, so the lead can run on Claude while a sub-agent runs on Codex, each
spending its own plan's quota. On one five-route task, the Codex command
line read 580,201 input tokens and took 285 s; tofu read 111,543 and took
66 s.

A run talks to Codex over one websocket, as the Codex command line does:
each request after the first sends only what is new with
`previous_response_id`, so the prompt cache holds through the run, and if
the socket fails tofu falls back to https for that request.

## Signing in and using it

- Sign in: `tofu login llm codex-sub`. Add `--paste` when port 1455 is
  taken or the browser runs elsewhere.
- Run a sub-agent on Codex: `tofu agents set qa codex-sub/gpt-5.6-sol`.
- Pick a Codex model for the lead: **Ctrl+L**, provider `codex-sub`.
- Several accounts, `--disable` and `--enable` work as for
  [Claude](/docs/llms/claude-subscription).

## Commands

```
tofu login llm codex-sub [--paste]
tofu agents set [--global] <name> codex-sub/<model>
```
