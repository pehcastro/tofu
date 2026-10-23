---
topic: context
title: Context
summary: how a long conversation stays under its target, when old results are trimmed, when a session forks, and why neither breaks the prompt cache
---

## What it is

Every step resends the whole conversation. tofu measures each request
against a target, 80% of the usable window and never more than 360,000
tokens, and against a ceiling, 450,000 tokens at most, the `contextCeiling`
setting. Two things keep a
conversation under them, both in the loop rather than in the model:

- The trim shrinks tool results the model has already read to an
  artifact handle. It shrinks a result only where the prompt cache allows
  it: when less than 8,000 tokens follow the result, when the session has
  been idle for 90 minutes, longer than the one-hour cache lives, or when
  the next request would otherwise go over the ceiling. It runs only when
  that brings the conversation down to 80% of the target, so it does not
  fire again a few steps later. Where no fork can come, with forks off or
  a history no fork can shrink, it still brings the conversation under the
  target, shrinking the newest results first so the longest prefix stays
  in cache.
- The fork ends the session whole and starts the next generation with
  the task, a carry of what was done and read, and the newest steps word
  for word. It comes when the conversation crosses the target and the trim
  would not bring it under, and it always works on the history the last
  step sent, untrimmed.

A sub-agent's fork asks the model for its working state first. That
request is the last step's history plus one question, so the provider
reads nearly all of it from cache and writes only the newest step.

On Anthropic's wire the history is marked for the cache from its first
message, and the last settled exchange once the history passes 4,096
characters. Tool descriptions say only what a model needs before a call; the
guidance for a rarer case arrives in the error when that case happens. A
`tofu run` whose task names a few files in one folder leaves out the
sub-agent text. A request is about a fifth smaller for it: on the rivals Go
task with Sonnet 5.5, median input is about 74k tokens where 0.5.14 sent
105k, at the same quality.

No request past the ceiling is sent. A conversation continued or resumed
over it forks before its first request, and when neither the fork nor a
trim brings a request under, the turn ends saying so instead of sending it.

## Where it lives

Each trim and each fork is a list change in the session log, with the
step, the tokens before and after, and what was shrunk. A shrunk result is
kept whole under `~/.tofu/projects/<project>/artifacts/`, and the model
reads any range of it back with `artifact_fetch`. A fork's earlier
generations stay in the session store, and `lookup` brings back any call
the carry names from any of them.

## Change it

- One run with a different ceiling: set `TOFU_CONTEXT_CEILING`, for
  example `TOFU_CONTEXT_CEILING=100000 tofu`. The target follows at 80% of
  it, 80,000 here.
- Every run with a different cap: `tofu settings set contextCeiling 250000`
  brings back the 250,000 cap of 0.5.12 and earlier from the next turn. A
  model whose window is smaller still stops at its window less the
  answer's reserve, and `TOFU_CONTEXT_CEILING` wins over the setting.
- Free room before the next turn: type `/compact` in the app. Every old
  tool result is shrunk to its handle at once, with no model call, and the
  shrunk history becomes a new session.
- What a lead's fork carries: `tofu settings set episodes off` makes the
  model write the working state at the fork instead of carrying the
  episode view.

The 8,000 token suffix, the 90 minute idle gap and the 80% band are fixed
in tofu and have no setting.

## Check it

```
tofu context
```

prints how full the session is, band by band, against the target and the
ceiling. With no session yet, it prints the ceiling the next turn takes.
Below the bands it names what fills the identity band, each with its
tokens, such as `tofu's prompt`, `rules`, `skills`, `sub-agent`, `instruction
files` and `tool schemas`, and `memory` in the working set. `--json`
and `query.context` carry the same as `items`. `tofu session drain`
says what those tokens cost over the whole session.

```
tofu session trace <name>
```

lists every request with its message count, and under `list changes` each trim as
`compaction ... after step N` and each fork as `fork ... continuation fork
N after step M, A to B tokens`. With `--json`, each request carries
`cache_read_tokens` and `cache_write_tokens`, so a trim that broke the
cache shows as a request that read little and wrote a lot, and the trace
names it under its cache breaks.

## Undo it

A trim keeps every result whole in its artifact, so nothing is lost; the
model fetches it back when it needs it. A fork leaves the earlier
generation whole in the session store, and `tofu session trace <id>`
reads it by the id the fork's list change names. Unset
`TOFU_CONTEXT_CEILING` to go back to the ceiling taken from the model's
window, and `tofu settings set contextCeiling 450000` puts the default cap
back.
