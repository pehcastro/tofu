---
title: Context
description: How tofu keeps a conversation small and cheap to resend, from the cached prompt prefix to forks that carry handles instead of text.
order: 5
updated: 2026-10-04
---

Everything the model sees on a step is resent on the next one. tofu manages
that in eight layers, each in the code of the loop, not in the model's
judgment:

| Layer | What it does |
|---|---|
| **Prompt prefix and cache** | The tools, the system prompt and the settled history are marked for the provider's cache, so a resend reads them from cache |
| **Bands** | Each request is measured against a 63,000 token target, split into identity, facts, working set and recent |
| **Result cap and artifact handles** | A tool result over 32,768 bytes is stored whole on disk; the model gets its first and last 512 bytes and a handle |
| **Middle elision** | What the model reads of a large result is its two ends, with a marker saying how many bytes were left out and where they are |
| **Memo of repeated calls** | A read, glob, search or fetch repeated in a turn, with nothing written since, is answered from memory |
| **Browser pages** | Past 4 whole page snapshots in a conversation, older ones shrink to a 600 byte summary and a handle, keeping 2 whole |
| **Forks and their carry** | Past the target, the session ends whole and a new one starts with the task and a carry of what was read, as handles |
| **Recall** | `artifact_fetch` reads any range of any handle, so nothing that left the conversation is lost |

**Compaction** is not built. The `compaction` setting exists and no value of
it changes a turn; forks keep sessions under the target instead.

## Why each layer exists

**A cache read costs a fraction of fresh input**, so the prefix is laid out
to stay identical between requests. On Anthropic's wire, tofu marks the end
of the tool list, the last stable system block, and up to two points in the
history: the newest message and the last settled exchange, once the history
passes 4,096 characters. The cache lives one hour on a subscription. On the
Codex wire, requests carry the session as a cache key. Over 24 turns, 88% of
billed input was a cache read.

**A fixed target, not the model's window.** A million-token window does not
make a million-token conversation cheap. The target is the same 63,000 tokens
on every model, a quarter of a 250,000 ceiling. Separately, a request larger
than the model's own window is refused before it is sent.

**Handles instead of text.** A full listing or a long log is mostly noise
after the first read. Storing it and sending its ends keeps the step small,
and the handle means nothing is thrown away. Capping file listings alone cut
the result bytes the model read by 88.4%.

**Memo**, because a model often reads the same file twice in one turn. A
repeat answers with `cached:` and the earlier result. Any write, edit or shell
call clears the memo, so a cached answer is never stale.

**Forks instead of summaries.** A summary written by the model loses what it
didn't think mattered. A fork keeps the old session whole on disk and hands
the new one a list of what was read, each with its handle, and the last thing
said. A fork shrank one session from 66,162 tokens to 17,865 and another from
57,326 to 7,811, and neither fetched again anything it had dropped. The fork's
first request read 9,457 and 6,105 tokens from the cache and wrote none.

## Watching and changing it

- **See how full a session is**: `tofu context`, or the context meter in the
  status bar.
- **See the cache at work**: `tofu session trace <name> --json` gives each
  request's `cache_read_tokens` and `cache_write_tokens`.
- **Change the target for one run**: set `TOFU_CONTEXT_CEILING`, for example
  `TOFU_CONTEXT_CEILING=20000 tofu`. The bands scale with it.
- **Read back a stored result**: the model calls `artifact_fetch` with the
  handle, an offset and a length. You don't need to.
- **Watch a fork**: the chat shows **⟳ forking**, and `tofu session list`
  lists the new session.

## Commands

```sh
tofu context
```

```text
Context · clear-sable-eagle · 5 steps                                   ✓ step 5

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
tofu session trace clear-sable-eagle --json
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
