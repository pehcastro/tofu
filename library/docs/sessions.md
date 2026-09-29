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

lists this project's sessions, newest first, with the head's id at the top.

    tofu session info <name|id>

prints one session: its task, its counts, whether it ended, and its model.
`tofu session trace <name|id>` lists every request and tool call in it,
and `tofu session reads <name|id>` every file it read. Each takes `--json`.

    tofu context

prints how full the context window of the newest session was, and what
filled it. Name a session id to see another one.

    tofu shells list
    tofu shells log <name>

list the running shells, and print one's output. In the app, `/shells`
shows the same.

## Undo it

Delete a session's folder under `sessions/` to remove it for good. There
is no command for that, on purpose.

A rename is undone by renaming again. `tofu migrate` moves and converts,
and has no undo, which is why `--dry-run` lists every move first.
