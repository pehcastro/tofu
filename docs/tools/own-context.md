---
title: The agent's own context
description: context, budget, forget, note and digest, the tools the model uses to see and manage its own window, and the notice tofu sends when a request passes 70% of the context ceiling.
order: 11
updated: 2026-11-06
---

These tools let the model look after its own context window. `context` says
what fills it, `budget` says what the work has cost, `estimate_time` says
how long work will take, `forget` drops a result it no longer needs, `note`
keeps a fact through every fork and compaction, and `digest` answers a
question about a long result without reading it again. All six are tofu's
own Go code.

## Why they exist

Every step resends the whole conversation, so a 30,000 byte log read early
is paid for on every request after it. tofu already trims and forks a long
conversation on its own, as [Context](/docs/features/context) says. These
tools let the model act sooner and on purpose: it knows which result is the
biggest, keeps the one line it needs from it, and drops the rest while
dropping is still cheap.

`tofu context` and `tofu session drain` show you the window and the cost.
These tools show the model its own, during the turn.

## context

`context` reports what fills the model's window: the tokens used against
the ceiling as the last request measured them, what is in the window by
kind, the biggest tool results with their call ids, and every file it read
that changed on disk since.

```bash
tofu tool context '{}' --dir . --session head
```

```text
context of session solid-opal-vole#qerkc.1, the lead, turn turn-18e593f55b2f7190
used 38581 of 450000 tokens (8%): 38581 as the last request measured, and 0 in results since
in the window since the session started, by kind: 8 kinds
  read 4 blocks 14042 tokens
  message 3 blocks 8488 tokens
  assistant 6 blocks 1506 tokens
  forget 1 blocks 220 tokens
  test 2 blocks 161 tokens
  glob 1 blocks 40 tokens
  edit 1 blocks 80 tokens
  note 1 blocks 19 tokens
biggest results: 5 of 10
  toolu_016dDgYohpxMbAqVVCcTEKjJ read build.log 13706 tokens
  toolu_01BJ7BDbJhyufen5W6WnjuYe forget 220 tokens
  toolu_018AuFFU22RRX3uBc4RtDA9g read order_test.go 193 tokens
  toolu_014wER6Wy5qwH3UwBdBuy8SZ read order.go 105 tokens
  toolu_013c7N2yzHjzFZfy5EBVTJDA test order_test.go 81 tokens
stale reads: 4 of 4 files seen
  build.log seen by toolu_016dDgYohpxMbAqVVCcTEKjJ at 2026-11-06 19:05:18, changed on disk at 2026-11-06 20:14:28
  order.go seen by toolu_01UNvSrp4LdMtgv5qjqxo6oR at 2026-11-06 19:05:36, changed on disk at 2026-11-06 20:13:08
  order_test.go seen by toolu_018AuFFU22RRX3uBc4RtDA9g at 2026-11-06 19:05:28, changed on disk at 2026-11-06 20:13:08
  pricing/pricing.go seen by toolu_01QmyQPvBFHgewpxinytwrWt at 2026-11-06 19:05:28, changed on disk at 2026-11-06 20:13:08
```

The call ids are the ones `forget` and `digest` take. A stale read is a
file whose copy in the window is older than the file on disk, so the model
reads it again before it edits. A project with no session yet says `no
session is recorded in this project yet, so nothing fills a window: 0
tokens used`.

## budget

`budget` reports what the work has cost: calls, weighted tokens, USD and
time for the current turn, the whole session, the lead and each sub-agent.
It counts exactly as `tofu session drain` counts, so the model and you read
the same figures. A session on a subscription reads 0 USD.

```bash
tofu tool budget '{}' --dir . --session head
```

```text
budget of session solid-opal-vole#qerkc.1 for the lead, counted as tofu session drain counts it
turn turn-18e593f55b2f7190: 6 calls, 83539.50000000001 weighted tokens, 0 USD, model 29835 ms, wall 36851 ms
session: 6 calls, 83539.50000000001 weighted tokens, 0 USD, model 29835 ms, wall 36851 ms
  tokens: input 18, output 1506, cache read 211895, cache write 5m 0, cache write 1h 27401
lead: 6 calls, 83539.50000000001 weighted tokens, 0 USD, model 29835 ms, wall 36851 ms
memory writer, outside the session total: 0 calls, 0 weighted tokens, 0 USD
sub-agents: 0 of 0
```

## estimate_time

`estimate_time` answers how long a piece of work will take before it
starts, so asking the lead "how long will this take?" gets numbers, not a
guess. It makes no model call and runs nothing: it reads earlier runs of
the same model in this project, the ones whose task was closest in length,
and on a first run here the same model's runs in other projects. It names
the estimate, the done-by promise, where the figure came from, and the part
spent in tests and builds. The call is recorded and settled when the next
lead turn ends, so `tofu session trace <s> --kind estimate` shows whether it
held.

The earlier runs come from one file per project, `estimates.jsonl` beside
its sessions, with one line for each turn or sub-agent that finished. A
call reads that file and no session, so it answers in a few milliseconds
however long the history is. A project that has no such file yet gets it
built once from its sessions, and standard error says so.

```bash
tofu tool estimate_time '{"work":"add a --json flag to tofu session reads","model":"claude-sub/claude-opus-5"}' --dir .
```

```text
estimate for the lead on claude-sub/claude-opus-5: about 47s, done by 1m47s, from 20 earlier runs in this project on this model (agent 47s, tests and builds 0s: no test or build call recorded)
```

Model time is summed over the requests. Wall time runs from the first event
to the last.

## forget

`forget` drops tool results the model no longer needs from its window. Each
dropped result becomes one line naming the call, from the next request on.

Only a result after the last cache breakpoint can go, which is a result the
last request showed the model for the first time. Dropping an older one
would rewrite the prefix the provider has cached, and every request after it
would pay to write that prefix again, so `forget` refuses it and drops
nothing. The answer says the tokens freed and what the cache pays for it.

In this session the model read a 31,676 byte `build.log`, kept the one
failure as a note, and dropped the log in the same reply:

```text
dropped: 1 results after the last cache breakpoint, at message 2, the end of what the request before your last one sent
  toolu_016dDgYohpxMbAqVVCcTEKjJ read build.log: 11192 tokens
tokens_freed: 11156
cache_cost: about 458 tokens from message 3 on are written to the cache again on your next request rather than read
kept_summary: each dropped result is one line naming its call; the next request reads about 11313 tokens of messages from the cache, the prefix up to the breakpoint, plus the tool definitions
```

A dropped `read` no longer counts as read: the next `read` of that file is
whole. `forget` runs only inside a turn, so `tofu tool forget` says so:

```text
error: forget: no turn is running, so there is no window to drop from; the model calls forget inside a turn
```

### When the context passes 70% of its ceiling

When a request passes 70% of the context ceiling, tofu tells the lead which
result `forget` can still drop, by call id, tool, bytes and tokens, and that
its next reply is the only one that can drop it. When the window also holds
a result that is already too old for `forget`, the notice names the largest
one and says to keep what it needs from it with `note` and let compaction
trim it. The notice
comes once each time the window crosses the line, and only when the model
holds `forget`.

`tofu session trace <name> --kind list,notice` lists each notice and each
`forget` with the time it happened.

In the session above, the notice named the log the model then dropped:

```text
the largest result forget can still drop is call toolu_016dDgYohpxMbAqVVCcTEKjJ, read build.log, 31676 bytes, about 11192 tokens: your next reply is the only one that can drop it, so read it now, keep what you still need from it with note, and call forget with that call id in that same reply. keep it if you will need it whole: reading it again costs more than keeping it.
```

## note

`note` keeps one short fact in the model's own context, word for word,
through every fork and compaction of the session: a decision, a constraint,
a path, or a number it must not lose. `text` adds a note, `done` drops one
by its id, and a call with neither lists the notes.

A note is one line of at most 500 bytes, and at most 20 notes are kept. Past
that a new note is refused until the model marks one done. A note lasts for
the session and its forks. A rule you want kept for later sessions is
`remember`'s job.

The model in the session above kept the one failure from the log before it
dropped the log:

```text
kept as note 1; your next request carries it
```

Every later request carries the notes as they stand:

```text
the notes you keep, carried word for word through every fork and compaction; this list replaces any earlier one:
1: build.log: only failure is TestDiscounted in fixture.test/orders, order_test.go:17: got 650, want 675. rest is dep downloads and cache cleanup.
```

Like `forget`, `note` runs only inside a turn.

## digest

`digest` answers one question about one earlier tool result, such as which
tests failed in a long log, without putting the result back in the window.
The cheap model `modelTier.dumb` names reads the result whole and answers
in a few sentences. Each line it cites is quoted from the real result, and
the last line is the cost of the call.

`digest` needs `modelTier.dumb` set:

```bash
tofu tool digest '{"call":"toolu_016dDgYohpxMbAqVVCcTEKjJ","question":"which test failed"}' --dir . --session head
```

```text
error: digest: digest asks the cheap model modelTier.dumb names, and none is set: tofu settings set modelTier.dumb <source/model>
```

A call id that isn't in the session, or any session it was carried from, is
refused with the closest one:

```text
error: digest: no call toolu_016dDgYohpxMbAqVVCcTEKjX in session solid-opal-vole#qerkc.1 or any session it was carried from; the closest is toolu_016dDgYohpxMbAqVVCcTEKjJ
```

## Getting a result back

A dropped or trimmed result is not lost. The `history` tool brings back a
recorded call whole with mode `lookup`, a turn or call you quoted with mode
`quote`, and matching items with mode `recall`. See
[history](/docs/tools/work#history).

## Parameters

The model calls these tools. `session`, where a tool takes it, names another
recorded session by name, id or `head`; left out, it is the session the
model runs in.

| Tool | Parameter | What it does |
|---|---|---|
| `context` | `session` | the session to read |
| `budget` | `session` | the session to count |
| `estimate_time` | `work` | what the work is; required |
| `estimate_time` | `agent` | the sub-agent definition that runs it; the lead when left out |
| `estimate_time` | `model` | the model that runs it; the turn's model when left out |
| `forget` | `calls` | the call ids to drop, as `context` lists them; required |
| `note` | `text` | adds one note, one line, at most 500 bytes |
| `note` | `done` | drops the note with that id |
| `digest` | `call` | the call id of the result to ask about; required |
| `digest` | `question` | what to ask about it; required |
| `digest` | `session` | the session that holds the call |

## Commands

You don't call these tools; the model does. `tofu tool context`, `tofu tool
budget` and `tofu tool digest` run one call by hand with `--session`, and
print what the model would read. See [One tool call by hand](/docs/cli/tool).
For your own view of the window, run `tofu context`, as
[Context](/docs/features/context) shows.
