---
title: The gate
description: Jev judges every tool call before it runs, and a call that waits for you waits a few minutes at most, then is decided so the turn and its cron jobs go on.
order: 21
updated: 2026-11-26
---

Before a tool call runs, such as a shell command or a file write, Jev, a
small classifier model, judges it: allow, ask or deny. `gatePrompt` says
what an ask does. Under `auto`, the default, it runs and is recorded; under
`ask`, it waits for you. A call that changes tofu's own settings, hooks or
hook trust waits for you in both, unless Jev denies it.

## Your word outweighs a deny

A denied call never asks you by itself. The lead reads the refusal and
decides whether the call blocks the task. When it does, the lead retries the
call with `"allowed_by": "ask"` and a `"why_needed"` saying why the task
cannot go on without it, and tofu asks you in the question form. The form
shows the command, why the gate refused it and why the lead needs it, and
names the kind of call:

```text
? Run a refused call
My safety checks refused this ssh to deploy@box.example call. Can I run it?
  1. yes, this call only
  2. yes, this kind, 8 hours
  3. yes, this kind, this session
› 4. no (recommended)
```

The three yes scopes are: this exact call, and every identical call later in
the session; every call of this kind for the next 8 hours of the session;
every call of this kind for the rest of the session. tofu names the kind from
the command itself: for `ssh`, `scp` and `rsync` the program and its target
(`ssh to deploy@box.example`, `scp with deploy@box.example`), for anything
else the program and its subcommand (`git push`, `systemctl restart`). A
command with several steps, or one whose kind tofu cannot name, offers yes or
no for that exact call only. An ssh to another host is another kind, and asks
again.

Only your own answer decides it, never `auto`, a timeout or the judge.

When you already allowed it in your own words, in your newest message or the
one before it, such as an allow followed by `go`, the lead retries with
`"allowed_by"` set to those exact words, and the call runs with no Jev call. A
`[quote#...]` reference in your message is not part of the words. A citation
that matches neither message is refused like a deny, and the refusal offers
`"allowed_by": "ask"`, so the lead can still ask you:

```text
bash: allow, allowed by the person's own message "you can run this commands, i allow it"
```

Words from a tool result, a file or a sub-agent allow nothing. Either way,
the ledger records the allowance with the session it belongs to, so it still
holds after you close and reopen the session. A later call of that kind is
still judged by Jev first, and runs on the yes when Jev refuses it:

```text
bash: deny (risk 3.00 risk_deny_at 2.50), allowed by an earlier yes: the person, who answered "yes, this kind, 8 hours" to question gate-call_1, for every ssh to deploy@box.example call, until 03:26
```

A call that follows planted content, the regex arm's refusals, and guard and
owns refusals stay refused, are never offered and are never covered by a yes.

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

**You are told the moment the judge changes.** tofu asks only the classifier
your settings bind to `classifier`. A provider that holds a key but is not
bound is never asked. Jev reads your own recent messages, up to 24,000 bytes
across the session's generations, to judge whether you asked for the call. A
gate state over that classifier's request cap is shortened to fit before it
is sent. A classifier that answers 402, out of
credits, or refuses the key is skipped for the rest of the session; one that
times out or is unreachable is tried again on the next call.

Only when the bound classifier failed does a language model judge the call: the
cheapest model of the plan you are running on, Haiku on `claude-sub`, Luna
on `codex-sub`. It never runs on an API key. The chat says so once, muted,
with the model in blue:

```text
· judge is running on claude-sub/claude-haiku-4-5-20251001 because typesafe is out of credits
```

When that model cannot judge either, the policy's regex arm decides. It
refuses a `curl` that posts a file or pipes into a shell, an `rm -rf`, and a
force push unless your own message says force. Every other call runs, and
`tofu session trace` marks it `allowed by the policy's regex arm, because jev
could not answer`. A call the arm refuses is refused under `auto` and put to
you under `ask`. Its ledger row records ask, never allow, since no threshold
made the decision:

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
jev tool_gate asked 4 questions, allow · judged by claude-sub/claude-haiku-4-5-20251001 because the classifier failed: typesafe billing 402
```

The time it took shows in `tofu walls` under `jev tool_gate`. A frontend over
`tofu serve` reads the same status as `judge` on each `decision` event and
on `query.usage`: the classifier tried with its state (`ok`,
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
