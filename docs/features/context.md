---
title: Context
description: How tofu keeps a conversation small and cheap to resend, from the cached prompt prefix to forks that carry the conversation and point back at every call.
order: 5
updated: 2026-10-09
---

Everything the model sees on a step is resent on the next one. tofu manages
that in ten layers, each in the code of the loop, not in the model's
judgment:

| Layer | What it does |
|---|---|
| **Prompt prefix and cache** | The tools, the system prompt and the settled history are marked for the provider's cache, so a resend reads them from cache |
| **Bands** | Each request is measured against a target taken from the model's window, 144,000 tokens on a 200,000 token model, split into identity, facts, working set and recent |
| **Result cap and artifact handles** | A tool result over 32,768 bytes is stored whole on disk; the model gets its first and last 512 bytes and a handle |
| **Middle elision** | What the model reads of a large result is its two ends, with a marker saying how many bytes were left out and where they are |
| **Memo of repeated calls** | A read, glob, search or fetch repeated in a turn, with nothing written since, is answered from memory |
| **Browser pages** | Past 4 whole page snapshots in a conversation, older ones shrink to a 600 byte summary and a handle, keeping 2 whole |
| **Trim before fork** | When the conversation crosses the target, results the model has already read are shrunk to a handle, pictures first and then oldest first, down to 80% of the target, and the fork comes only if that is not enough |
| **Forks and their carry** | Past the target, the session ends whole and a new one starts with the task once, the episode view of the conversation, every message you typed, word for word, and a list of what was read, each naming the call that holds it |
| **Recall** | `lookup` returns any call the carry names, with its result, from any session of the line; `artifact_fetch` reads any range of any handle |
| **Pictures** | A picture a tool read counts as 1,600 tokens, only the newest 20 are sent, and a shrink drops pictures, oldest first, before any text |

**A full window** is recovered in the turn. When the model says a request is
over its window, tofu shrinks the oldest tool results to their handles and
asks once more, and the chat says so in one line. Forks keep a session under
the target, so this is rare, and nothing about it needs a setting.

## Why each layer exists

**A cache read costs a fraction of fresh input**, so the prefix is laid out
to stay identical between requests. On Anthropic's wire, tofu marks the end
of the tool list, the last stable system block, and up to two points in the
history: the newest message and the last settled exchange, once the history
passes 4,096 characters. The cache lives one hour on a subscription. On the
Codex wire, requests carry the session as a cache key. Over 24 turns, 88% of
billed input was a cache read.

**A target from the model's window, with a cap.** tofu holds 20,000 tokens of
the window for the answer and forks at 80% of the rest, so a 200,000 token
model forks at 144,000. A million-token window does not make a million-token
conversation cheap, so the usable window is capped at 250,000 and no model
forks later than 200,000. A model with no known window uses 63,000, a quarter
of that cap. Separately, a request larger than the model's own window is
refused before it is sent.

**Handles instead of text.** A full listing or a long log is mostly noise
after the first read. Storing it and sending its ends keeps the step small,
and the handle means nothing is thrown away. Capping file listings alone cut
the result bytes the model read by 88.4%.

**Memo**, because a model often reads the same file twice in one turn. A
repeat answers with `cached:` and the earlier result. Any write, edit or shell
call clears the memo, so a cached answer is never stale.

**Forks carry the conversation, and point back for the rest.** A fork
carries the [episode view](/docs/features/memory): the whole conversation
of the project as summary lines, recent ones whole, that the lead opens
with `zoom` down to the original message. It is already built, so a fork
makes no model call. With `episodes` off, the model writes at the fork, on
your own subscription, where the work stands instead: the goal, what was
decided, what is done and half done, and what comes next. The carry holds
that, and a list of what was read, each line
naming the call that holds it; `lookup` brings back exactly that call and its
result, from this session or any earlier one of the line, so a file or a
script is pointed at rather than copied. The task is sent once, and the
carried steps go without their thinking, so the next turn reads its cache.
On a driven chain of 30 reads at a 60,000 token ceiling, the share of the
first 20 reads after a fork that repeated one from just before it went from
12.0% to 8.7%.

**A trim comes before a fork.** A fork starts a new session and rewrites the
whole cache, so when the conversation crosses its target tofu first shrinks
results the model has already read, and forks only when that does not bring
it under. A result that has just come back is never cut before the model
reads it.

**Your words are carried, not summarised.** Every fork's carry lists what
you typed in this line of sessions, oldest first, each message word for
word on its own line, so a correction made three forks ago still holds.
It keeps the newest 24,000 bytes, and a message over 4,000 bytes keeps its
start. A conversation continued or resumed over the target forks before
its first request, so the whole history is never sent first.

## Watching and changing it

- **See how full a session is**: `tofu context`, or the context meter in the
  status bar.
- **See the cache at work**: `tofu session trace <name> --json` gives each
  request's `cache_read_tokens` and `cache_write_tokens`.
- **Change the ceiling for one run**: set `TOFU_CONTEXT_CEILING`, for example
  `TOFU_CONTEXT_CEILING=100000 tofu`. It replaces the ceiling taken from the
  model's window, and the fork comes at 80% of it, 80,000 here, the same as a
  window that size.
- **Read back a stored result**: the model calls `artifact_fetch` with the
  handle, an offset and a length, or `lookup` with a call id from a fork's
  carry. You don't need to.
- **Free room before the next turn**: type `/compact` in the app. Every old
  tool result in the history is shrunk to its handle, with no model call,
  and the chat says how many and the tokens before and after. The shrunk
  history is a new session, so `tofu --continue` carries it after a restart.
  Typed while the lead waits on its sub-agents, it says so and runs when
  the turn ends.
- **Watch a fork**: the chat shows **⟳ forking**, and `tofu session list`
  lists the new session.

## Commands

```sh
tofu context
```

```text
Context · brisk-amber-heron · 5 steps                                   ✓ step 5

  task         explain this repository to me
  identity     ▓▓░░░░░░░░░░  16%  1920 / 12000
  facts        ░░░░░░░░░░░░   0%  0 / 3000
  working set  ░░░░░░░░░░░░   0%  0 / 18000
  recent       ▓▓▓▓▓▓▓▓░░░░  63%  19014 / 30000
  total        ▓▓▓▓░░░░░░░░  33%  20934 / 63000
  ceiling      ▓░░░░░░░░░░░   8%  20934 / 250000
  caps         recorded by the step
  bytes        2310 per thousand tokens
```

```sh
tofu session trace brisk-amber-heron --json
```

```text
"cache_read_tokens": 0
"cache_write_tokens": 7478
"cache_read_tokens": 7478
"cache_write_tokens": 10472
"cache_read_tokens": 17950
```

Those are the cache fields of the first three requests, filtered from the
JSON: each request reads from cache what the one before it wrote.
