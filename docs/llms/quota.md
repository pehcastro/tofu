---
title: Subscription quota
description: How full each subscription's windows are and when they reset, read from replies tofu already gets, shared by every tofu process, and never blank after a refusal.
order: 5
updated: 2026-10-09
---

`tofu usage` shows each signed-in subscription account, its quota windows,
how full each one is and when it resets. A Claude plan has a 5 hour and a 7
day window, and sometimes a 7 day window for one model; a ChatGPT plan has
the windows it carries. The app shows the fullest window in its footer, and
tofu moves to an account with room when one runs out.

## Why it never costs you a limit

**Most readings are free.** The vendor puts the window figures on every
model reply, so each turn refreshes the reading with no extra request. tofu
asks the vendor's usage endpoint only when it holds no fresh reading.

**One reading for every process.** Every tofu on the machine, the app, `tofu
serve` and the desk, shares one cache in `~/.tofu/quota/latest/`, with a
lock per account so only one of them asks at a time. Three `tofu serve`
processes opened at once on a cold cache ask once per account, not once per
caller.

**A refusal is honoured, and the number stays.** When the endpoint answers
429 or fails, tofu waits exactly as long as its `Retry-After` says, or 1
minute, then 2, 4, 8 and at most 10, and every process honours the same
wait. Meanwhile you see the last reading, marked stale with its age, never
a blank row.

**It looks sooner when it matters.** A reading stays fresh about 5 minutes,
spread a little so accounts and processes don't ask together. Near a limit
it is asked sooner: after 2 minutes at 75% used, 1 minute at 90%, and 30
seconds at 99%, unless a reply already said more recently.

## Reading it

```sh
tofu usage
```

Each account card ends with a `read` line: when the reading was taken, its
age, where it came from, and `stale` when the endpoint couldn't refresh it.
A reading from a turn reads `from the reply headers`, and a `retry` line
says when the endpoint is asked again. In the app the footer shows the age
beside a stale percentage, such as `read 12m ago`.

- **Every reading over time**: `tofu usage --history`, from the readings log
  in `~/.tofu/quota/<date>.jsonl`.
- **For a script**: `tofu usage --json`. Each provider row carries
  `read_at`, `source` (`reply headers`, `usage endpoint` or `readings log`),
  `stale` and `retry_at`.
- **Forget every held reading and wait**: delete `~/.tofu/quota/latest/`;
  the next check asks the endpoint.

There is nothing to set.

## Commands

```
tofu usage [--history] [--json]
tofu doctor
```

`tofu doctor` shows the windows used on one line per subscription, and
`tofu serve` answers the same reading as `query.usage` and pushes it as
`quota.updated`.
