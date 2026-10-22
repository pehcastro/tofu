---
topic: usage
title: Subscription quota
summary: how tofu reads each subscription's 5 hour and 7 day windows, shares one reading between every tofu process, and keeps the last one when the vendor says no, and where work continues when a subscription runs out
---

## What it is

`tofu usage` prints each signed-in subscription account, its quota
windows, how full each one is and when it resets. A Claude subscription
has a 5 hour and a 7 day window, and sometimes a 7 day window for one
model; a ChatGPT subscription has the windows its plan carries. The app
shows the fullest window in its footer, and `tofu serve` answers the same
reading in `query.usage` and pushes it as `quota.updated`. `tofu usage`
checks every account at once, not one after another.

The footer names the account that served the last request, and follows a
rotation, a pick in the model picker and a login. When a subscription's
account is spent mid-turn, tofu asks the same step again on the next signed-in
account of the same source without asking you, each account once per step.
When every account of that source is spent, the turn moves on its own to
the model you chose for that case, since another source is other money,
with one line: `every claude-sub account is at its limit, so this turn
continues on codex-sub/gpt-5.6-sol, as Continue on another subscription
says`. The lead, its sub-agents and cron follow the same choice. Three
settings, in Settings > Models & roles, make it:

- `continueOnSubscription`: one model per other subscription, tried first
- `subscriptionOrder`: the order those are tried in, such as
  `codex-sub,claude-sub`; both show once you have two subscriptions
- `continueOnAPIModel`: a model paid by your key, tried last

A model at its limit, with no account left, or with a window too small for
the conversation is skipped with its reason. With none set, the work stops
with one sentence naming the fields, then waits for the reset and resumes
by itself; typing resumes at once and `esc` cancels the wait.

Every reading says where it came from and when:

- `reply headers`: the vendor puts the window figures on every model
  reply, so a turn refreshes the reading for free. tofu keeps at most one
  of these a minute per account, and keeps one at once when a window
  reads full.
- `usage endpoint`: the vendor's usage page, asked when no fresh reading
  exists. A reading stays fresh about 5 minutes, spread by a quarter
  either way so that accounts and processes do not ask together. Near a
  limit it is asked sooner: after 2 minutes at 75% used, 1 minute at 90%,
  30 seconds at 99%, unless a reply header already said more recently.
- `readings log`: the newest reading tofu wrote down, used only when
  nothing else is held and the endpoint cannot be asked.

## Where it lives

`~/.tofu/quota/latest/` holds one file per account with the newest
reading, when it is fresh until, and any cooldown. Every tofu process on
the machine reads the same files, and one file lock per account lets
only one of them ask the endpoint at a time; the others wait for its
answer. Three `tofu serve` processes opened at once ask once per
account, not once per caller.

`~/.tofu/quota/<date>.jsonl` is the readings log, one line per reading,
which `tofu usage --history` prints.

## Change it

    tofu settings set continueOnSubscription codex-sub/gpt-5.6-sol
    tofu settings set continueOnAPIModel meta/muse-spark-1.3

The readings need nothing set. When the endpoint fails, tofu does not ask again for a
while, and every process honours that:

- a 429 or any other refusal with a `Retry-After` waits exactly that long;
- without one, 1 minute, then 2, 4, 8, and 10 at most, back to nothing
  after the next good answer;
- a 429 is never retried inside one check.

While it waits, the last reading is shown rather than nothing, marked
stale with its age.

## Check it

```
tofu usage
```

Each account card ends with a `read` line: the time of the reading, its
age, its source, and `stale` when the endpoint could not refresh it. A
`retry` line says when the endpoint is asked again. A turn's reading
reads `from the reply headers`.

`tofu usage --json` and `query.usage` carry the same facts on each
provider: `read_at`, `source`, `stale` and `retry_at`. `query.usage`
also carries `read_at` and `age_ms` for the whole answer, taken from its
oldest reading.

In the app, the footer shows `read 12m ago` beside a stale percentage.

## Undo it

`tofu settings set continueOnSubscription ""`, and the same for
`continueOnAPIModel`, has the work stop at a limit again.

Delete `~/.tofu/quota/latest/` to forget the held readings and every
cooldown; the next check asks the endpoint. Delete `~/.tofu/quota/` to
forget the readings log as well.
