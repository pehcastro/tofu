---
topic: sessions
title: Sessions
summary: continue where you left off, find an old session, and where sessions and running shells are kept
verbs: session, context, shells, migrate
---

## What it is

A session is one conversation with tofu: every message, tool call and
sub-agent, recorded as it happens. You can continue it later, read it
back, or give it a name. The session you last worked in is the head, and
that is the one `tofu --continue` opens.

A shell an agent started, like a dev server, keeps running after the task
and stops when tofu exits, with every process it started. That holds when
tofu is killed too. With the `persistentRegistry` setting on, a shell keeps
running after tofu exits, until you stop it, and the next launch lists it.

## Where it lives

Each project has its own folder in your home,
`~/.tofu/projects/<project>/`, where `<project>` is the project's path
with every character that is not a letter or a digit turned into `-`, so
`C:\code\shop` becomes `C--code-shop`. In it:

- `sessions/<id>/`: one folder per session, holding `session.json` and
  `events.jsonl`
- `log/`: the decision ledger
- `shells/`: the shells an agent left running, and their logs

None of this is written into the project. tofu never deletes a session
on its own.

Older versions kept all this in the project's `.tofu` folder. Opening the
app in a project moves it into your home; `tofu migrate` does the same
from the command line, and also converts sessions recorded in the older
layout.

## Change it

Continue the session you last worked in:

    tofu --continue

Continue any other, by its name or its id:

    tofu session resume <name|id>

Give a session a name you will find again:

    tofu session rename <name|id> "checkout redesign"

In the app, `/resume` carries the last session into your next message,
and `/new` starts fresh.

Move what an older version left in the project:

    tofu migrate --dry-run
    tofu migrate

Stop or restart a shell an agent left running:

    tofu shells stop <name>
    tofu shells restart <name>

## Check it

    tofu session list

lists this project's sessions, newest first, `●` on the head and `○` on
the rest, each with its age, steps, state and task:

    Sessions · 2 sessions                    ● head store-walk
      ● store-walk  10m ago  1 step   stopped  explain the session store
      ○ older-task  3h ago   0 steps  done     the older task

A session that cannot be read goes under `unreadable` with a `✗`.

    tofu session info <name|id>

prints one session: its id, task, counts, whether it ended, its model and
cost, why it ended in an error when it did, and the command that continues
it, `→ tofu --continue` for the head. `tofu session reads <name|id>` lists
every file it read.

    tofu session trace <name|id>

lists every request and its new messages, every tool call with its
arguments, gate verdict and hooks, a failed one marked `✗` with its reason,
each message tofu added (a steer, a sub-agent check or report, a fork's
carry) with when it was posted and taken, each fork, compaction and resume,
and each turn that ended in an error. `tofu session request <name|id>
<request id>` prints one request exactly as sent: its messages, each wire
attempt with status, provider request id, body and error body, then the
response. A request id is any ending of the id the trace prints.

    tofu context

prints a bar per context band of the newest session, how full it was and
what filled it. Name a session id to see another one.

    tofu shells list
    tofu shells log <name>

list the shells, each with its state, pid and command, and print one's
output. In the app, `/shells` shows the same. Every verb here takes
`--json` and prints one JSON document.

## Undo it

Delete a session's folder under `sessions/` to remove it for good. There
is no command for that, on purpose.

A rename is undone by renaming again. `tofu migrate` moves and converts,
and has no undo, which is why `--dry-run` lists every move first.
