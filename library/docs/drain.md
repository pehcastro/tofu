---
topic: drain
title: Where a session's tokens went
summary: tofu session drain weighs every call of a session, says what woke the lead and each sub-agent, which context band and cache rebuilds took the most, and names eight patterns that waste tokens
---

## What it is

`tofu session drain` reads one session after the fact and says where its
tokens went. On a session of 20 MB and 11,298 events it answers in 0.7 to
1.0 s, over 12 runs.

Every call is counted in weighted tokens, so a cache read and an output
token are not the same unit. The default weights are list price ratios:
input 1, cache write for 5 minutes 1.25, for an hour 2, cache read 0.1 and
output 5. A model the catalog prices uses its own ratios instead.

The report opens with a short summary:

- `sub-agents`: their share of the weighted tokens
- `lead woken`: what woke the lead for most of its tokens
- `context`: the context band that took the most
- `rebuilds`: how many cache rebuilds, their share, and how much came
  after the cache went idle
- `unread`: the share spent on cache writes the next call never read
- `outside`: the memory writer, which is kept out of the total

Then it lists, at most ten rows each: findings, what woke the lead (you,
tofu, a child report, memory, the classifier), what woke the sub-agents
(a spawn brief, a message, tofu), the context bands (`<50k` to `400k+`),
residency (each kind of block, how big and how many calls it stayed for),
cache rebuilds (cause `prefix changed` or `idle past ttl`, with the first
part that differed), agent types, and each sub-agent with its peak
context.

The findings come from eight rules, each named in the row:

- `context-pressure`: one call reached 75% of the ceiling, a warning at 90%
- `context-jump`: one call added 25% of the ceiling at once
- `idle-cache-rewrite`: lead calls after a gap of 5 minutes or more wrote
  the cache again, a warning from the second
- `post-compaction-refill`: the first call after a compaction was back at
  75% of what it was before
- `edit-test-thrash`: 3 failing test runs in a row, a warning at 4
- `repeated-file-reads`: 4 reads of a file an agent had read twice
  already, or one file read 4 times, a warning at 8
- `turn-cost-concentration`: one turn took half the session, with 3 turns
  or more
- `cache-writes-never-read`: cache writes the next call did not read came
  to 5% of the session

## Where it lives

Nothing is written. The report is made each time from the session's
`events.jsonl`, its recorded requests, and the decision ledger rows its
turns wrote. A sub-agent that kept running in the session before a continue
is counted in the session it worked for.

Over `tofu serve`, `query.drain` takes `session` and answers what
`tofu session drain --json` prints.

## Change it

There is no setting. The weights follow the prices in the model catalog:
`tofu models reload` brings new ones, and the `--json` report names the
default weights and where they came from under `default_weights`.

Pick the session by name, id or handle:

    tofu session drain
    tofu session drain brisk-amber-heron#4mhr1.2
    tofu session drain brisk-amber-heron --json

With no name, it reads the head, the session `tofu --continue` opens.

## Check it

    tofu session drain

prints `Drain`, the session's handle, its calls, weighted tokens and
cost, then the summary and every section above. A finding with `⚠` is a
warning; each one names the agent, the turn and the call to read with
`tofu session trace` or `tofu session request`.

## Undo it

Nothing to undo: `tofu session drain` only reads.
