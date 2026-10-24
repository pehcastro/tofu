---
title: Where tokens went
description: tofu session drain weighs every call of a session and says what woke each agent, which context band and cache rebuilds took the most, and which of eight patterns wasted tokens.
order: 6
updated: 2026-10-24
---

`tofu session drain` reads a session after the fact and says where its
tokens went: the lead against its sub-agents, what woke each of them, how
big the context was on each call, which cache rebuilds were paid for, and
which habits wasted tokens. It reads the session's own record and writes
nothing. It reads a Claude Code transcript the same way, so you can weigh a
session you ran in the `claude` command line.

## Why it helps

**It answers in under a second on a large session.** On a session of
20 MB and 11,298 events, `tofu session drain` took 0.7 to 1.0 s over 12
runs.

**One unit for every token.** A cache read costs a tenth of an input
token and an output token five times one, so raw counts hide where the
money went. Each call is counted in weighted tokens: input 1, a cache
write kept 5 minutes 1.25, one kept an hour 2, a cache read 0.1, and
output 5. A model the catalog prices uses its own ratios instead, taken
from the price of the wire the request went out on, so the same model on a
subscription and on an API key is priced each its own way. The summary
names each model and the source that priced it.

**It names the cause, not only the total.** Each finding comes from a
rule, says the agent, the turn and the call, and gives the number behind
it, so you can open that call with `tofu session request`.

## What the report holds

The report opens with a summary of six lines, then lists up to ten rows
per section:

| Section | What it says |
|---|---|
| **summary** | the sub-agents' share; what woke the lead for most of its tokens; the heaviest context band; cache rebuilds and how much came after idle; cache writes never read; the memory writer, kept out of the total |
| **findings** | each rule that fired, a `⚠` on a warning |
| **lead woken by** | you, tofu, a child report, memory and the classifier, each with its share, weighted tokens, calls and cost |
| **sub-agents woken by** | a spawn brief, a message from the lead, tofu |
| **context band** | calls by context size, from `<50k` to `400k+` |
| **residency** | each kind of block, how many tokens it held and how many calls it stayed for |
| **cache rebuilds** | each rebuild, `prefix changed` or `idle past ttl`, with the part that differed first |
| **agent types** and **sub-agents** | each type and each run, with its peak context, rebuilt and unread tokens |

## The eight rules

Each rule has its own threshold, and most turn into a warning past a
second one:

| Rule | Fires when |
|---|---|
| `context-pressure` | one call reached 75% of the context ceiling; a warning at 90% |
| `context-jump` | one call added 25% of the ceiling at once |
| `idle-cache-rewrite` | lead calls after a gap of 5 minutes or more wrote the cache again; a warning from the second |
| `post-compaction-refill` | the first call after a compaction was back at 75% of what it was before |
| `edit-test-thrash` | 3 failing test runs in a row; a warning at 4 |
| `repeated-file-reads` | 4 reads of files an agent had read twice already, or one file read 4 times; a warning at 8 |
| `turn-cost-concentration` | one turn took half the session, in a session of 3 turns or more |
| `cache-writes-never-read` | cache writes the next call did not read came to 5% of the session |

## Commands

```sh
tofu session drain
tofu session drain brisk-amber-heron#4mhr1.2
tofu session drain brisk-amber-heron --json
```

With no name, it reads the head, the session `tofu --continue` opens.

```sh
tofu session drain --claude ~/.claude/projects/shop/1f2e3d4c.jsonl
```

reads a Claude Code transcript instead, a `.jsonl` file under
`~/.claude/projects`, and prints the same report. Where nothing recorded
a cost, the summary says `no cost recorded` rather than a guess.

This is the start of a report, with made-up figures:

```text
  sub-agents  38% of weighted tokens across 4 sub-agents
  lead woken  by tofu for 61% of the lead's weighted tokens
  context     50-100k band took 54%
  rebuilds    3 cache rebuilds, 12% of the total, 33% of it after idle
  unread      2% went to cache writes the next call never read

findings
  ⚠ repeated-file-reads  go-dev-2  turn turn-18e0f1a2b3c4d5e6  9
    redundant reads (3rd or later read of a file by one agent); the most read is
    internal/shop/cart.go, 5 reads, last named · toolu_01AbCdEfGhIjKlMnOpQr
```

`--json` names the weights it used under `default_weights`, with where
they came from, the source that priced each model under `priced_by`, and
whether a cost was recorded as `cost_recorded`. Over [tofu serve](/docs/cli/serve), `query.drain` takes
`session` and answers the same report.
