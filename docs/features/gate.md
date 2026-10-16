---
title: The gate
description: Jev judges every tool call before it runs, and a call that waits for you waits a few minutes at most, then is decided so the turn and its cron jobs go on.
order: 21
updated: 2026-10-16
---

Before a tool call runs, such as a shell command or a file write, Jev, a
small classifier model, judges it: allow, ask or deny. `gatePrompt` says
what an ask does. Under `auto`, the default, it runs and is recorded; under
`ask`, it waits for you. A call that changes tofu's own settings, hooks or
hook trust waits for you in both, unless Jev denies it.

## Why an ask has a timer

**No ask waits forever.** An ask waits `gateAskWaitSeconds`, 180 by
default, under `auto` and under `ask` alike. The chat says when it will be
decided:

```text
· if nobody answers by 15:29:39, in 3m0s, gateAskTimeout refuse decides the ask about write .tofu/settings.json
```

When the time passes, `gateAskTimeout` decides it and the turn goes on.

**Refuse is the default** because a call only you may allow changes
settings, hooks or the harness itself. The call does not run, and the lead
reads why in the tool result:

```text
this call did not run: only the person allows a call that changes tofu's settings or a harness file, and it writes .tofu/settings.json, and the person could not be asked: nobody answered in 3m0s, and gateAskTimeout is refuse, so the call did not run
```

With `allow`, the call runs instead, and the chat says so:

```text
· the ask about write .tofu/settings.json: nobody answered in 3m0s, and gateAskTimeout is allow, so the call runs
```

**A cron job never queues behind you.** A cron job that fires while a call
waits for you decides that ask at once, the same way the timer would, and
its prompt reaches the turn at the next step:

```text
· the ask about write .tofu/settings.json: a cron job fired while it waited, so nobody is there to answer, and gateAskTimeout is refuse, so the call did not run
```

A turn a cron job started asks nobody, and refuses every call that would ask.

## How to

Give yourself longer to answer:

```sh
tofu settings set gateAskWaitSeconds 600
```

Let an unanswered ask run rather than be refused:

```sh
tofu settings set gateAskTimeout allow
```

Wait for you on every call Jev would ask about, still with the timer:

```sh
tofu settings set gatePrompt ask
```

The next ask reads a new value, so a change needs no restart.

## Settings

| Setting | Default | Values |
|---|---|---|
| `gatePrompt` | `auto` | `auto`, `ask` |
| `gateAskWaitSeconds` | `180` | seconds, 1 or more |
| `gateAskTimeout` | `refuse` | `refuse`, `allow` |

An approval card takes five answers: `1` allow once, `2` deny, `3` always
here, `4` never here, `5` cancel. See [Asking you](/docs/features/asking)
for each, and for the questions the lead asks with `ask_person`, which have
their own wait, `askPersonWaitSeconds`.
