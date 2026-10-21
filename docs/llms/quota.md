---
title: Subscription quota
description: How full each subscription's windows are and when they reset, read from replies tofu already gets, shared by every tofu process, and never blank after a refusal.
order: 5
updated: 2026-10-14
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
caller. `tofu usage` checks every account at once rather than one after
another.

**A refusal is honoured, and the number stays.** When the endpoint answers
429 or fails, tofu waits exactly as long as its `Retry-After` says, or 1
minute, then 2, 4, 8 and at most 10, and every process honours the same
wait. Meanwhile you see the last reading, marked stale with its age, never
a blank row.

**It looks sooner when it matters.** A reading stays fresh about 5 minutes,
spread a little so accounts and processes don't ask together. Near a limit
it is asked sooner: after 2 minutes at 75% used, 1 minute at 90%, and 30
seconds at 99%, unless a reply already said more recently.

## When a limit is reached

**Another account of the same subscription first.** When a reply comes back
with the rate limit, tofu asks the same step again on your next signed-in
account of that subscription, `claude-sub` to `claude-sub`, without asking
you. The screen notes the move and `tofu serve` sends `turn.account` with
the reason `moved`. Each account is tried at most once per step. The footer
names the account that served the last request, so it follows a rotation, a
pick in the model picker and a new login.

**Then you choose, because another source is other money.** When every
account of the subscription is at its limit and another subscription you
are signed in to has room, tofu asks: `continue on codex-sub`, or `I'll wait
for the limit to reset`. Continuing keeps the conversation, so the other
model picks up where the first stopped.

**Waiting resumes by itself.** The screen says when the turn resumes, from
the window's reset time, or the reply's `Retry-After`, or 10 minutes when
neither says. At that time the turn goes on with no input. Typing goes on
at once with what you typed, and `esc` cancels the wait. A cron turn never
asks: it waits.

**A refusal says what to do.** When a vendor refuses for billing, sign-in,
an organization rule or an unknown model, the chat shows one sentence: which
account, what the vendor said, and the next step, such as setting up billing,
`tofu login llm claude-sub` again, or another model with **Ctrl+L**. The raw
answer stays in the work card.

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
