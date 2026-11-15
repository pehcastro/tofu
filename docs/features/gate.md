---
title: The gate
description: Jev judges every tool call before it runs, and a call that waits for you waits a few minutes at most, then is decided so the turn and its cron jobs go on.
order: 21
updated: 2026-11-14
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

## When Jev cannot answer

**You are told the moment the judge changes.** tofu tries every classifier
your settings bind, the one bound to `classifier` first, then each other
classifier with a stored key. A classifier that answers 402, out of credits,
or refuses the key is skipped for the rest of the session; one that times
out or is unreachable is tried again on the next call.

Only when every classifier failed does a language model judge the call: the
cheapest model of the plan you are running on, Haiku on `claude-sub`, Luna
on `codex-sub`. It never runs on an API key. The chat says so once, muted,
with the model in blue:

```text
· judge is running on claude-sub/claude-haiku-4-5-20251001 because typesafe is out of credits
```

With two classifiers that both refuse:

```text
· judge is running on claude-sub/claude-haiku-4-5-20251001 because all classifiers configured are out of credit
```

When that model cannot judge either, the regex arm decides, and every
verdict it gives is recorded as ask, never allow:

```text
· judge is running on the regex arm because typesafe is out of credits, and claude-sub/claude-haiku-4-5-20251001 could not judge: its reply held no json object
```

The footer keeps one line for as long as it lasts:

```text
jev out of credits · judge claude-sub/claude-haiku-4-5-20251001
```

A working classifier prints nothing. Each verdict a model judged is a
ledger row that says so, and `tofu session trace --kind library` shows it:

```text
jev tool_gate asked 4 questions, allow · judged by claude-sub/claude-haiku-4-5-20251001 because every classifier failed: typesafe billing 402
```

The time it took shows in `tofu walls` under `jev tool_gate`. A frontend over
`tofu serve` reads the same status as `judge` on each `decision` event and
on `query.usage`: every classifier tried with its state (`ok`,
`out of credits`, `refusing`, `unreachable`) and HTTP status, the judge
running now, and the line the chat said.

## Every refusal becomes a case

**A call the gate refused or asked about is kept as a block case.** tofu
appends it to `~/.tofu/projects/<project>/gate/cases.jsonl`, one JSON line
with the tool, its input, the folder, your task and what happened: the
reason it was refused, or who allowed it when it ran. Each line is labelled
`block`, in the same shape as the gate's own corpus.

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

See every always here and never here answer standing in this session, what
it allows or refuses, where it holds, when it was given and until when:

```text
/permissions
```

Take one back by its number, so the next call on its target asks you again:

```text
/permissions revoke 1
```

A frontend over `tofu serve` reads the same list with `approval.list` and
takes one back with `approval.revoke`.

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
