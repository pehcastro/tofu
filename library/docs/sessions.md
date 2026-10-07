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

A conversation keeps one name for its whole life, such as
`tidy-moss-vole#12cp3.2`: the name, the family tag after `#`, and the
generation after `.`. A fork to stay under the context budget, `/compact`
and a move to another account start the next generation, so the name
stays and the number rises. `/new` starts a new family, and so does a
branch, which records where it branched. Hooks see the family id.

Every message in the chat shows the id it was recorded under, such as
`[message#9c2d40]`. Type that reference in a message and the lead quotes
it, across forks and `--continue`. A fork carries every message you
typed, word for word.

A shell an agent started, like a dev server, keeps running after the task
and stops when tofu exits, with every process it started, even when tofu
is killed. With the `persistentRegistry` setting on, a shell keeps running
after tofu exits, until you stop it, and the next launch lists it.

## Where it lives

Each project has its own folder in your home,
`~/.tofu/projects/<project>/`, where `<project>` is the project's path
with every character that is not a letter or a digit turned into `-`, so
`C:\code\shop` becomes `C--code-shop`. In it:

- `sessions/<id>/`: one folder per generation, holding `session.json`
  and `events.jsonl`
- `log/`: the decision ledger
- `shells/`: the shells an agent left running, and their logs

None of this is written into the project. tofu never deletes a session
on its own. Older versions kept all this in the project's `.tofu` folder;
opening the app there, or `tofu migrate`, moves it into your home.

## Change it

Continue the head, or any other session:

    tofu --continue
    tofu session resume <name|id>

A session is named by its name (the newest generation), its id, or a
handle: `tidy-moss-vole#12cp3.1` is the first generation, `12cp3` the
family. A name from before this naming still finds its own generation,
and `[session#9ff700]` from the header finds its generation too.

Give a conversation a name you will find again, on every generation:

    tofu session rename <name|id> "checkout redesign"

In the app, `/resume` carries the last session into your next message,
and `/new` starts fresh.

Move what an older version left in the project, or stop and restart a
shell an agent left running:

    tofu migrate --dry-run
    tofu migrate
    tofu shells stop <name>
    tofu shells restart <name>

## Check it

    tofu session list

lists this project's conversations, one row per family, newest first, `●`
on the head, each with its handle, age, steps, state and task. A session
that cannot be read goes under `unreadable` with a `✗`.

    tofu session <name|id>

prints the family: when it started, how long it was worked on (each turn
from its first event to its last), and each generation with how it
began, its steps, active time and size, its branches and sub-agents.

    tofu session find <name|id> --tool bash

finds calls across every generation. `--command`, `--file`, `--text`,
`--agent`, `--since` and `--until` (`2h`, or a time) narrow it together.

    tofu session info <name|id>

prints one session: its id, task, counts, whether it ended, its model and
cost, why it ended in an error, and the command that continues it.
`tofu session reads <name|id>` lists every file it read.

    tofu session trace <name|id>

lists every request, message with its `[message#id]`, tool call with its
arguments, gate verdict and hooks, each message tofu added, each fork,
compaction and resume, and each turn that ended in an error.
`tofu session request <name|id> <request id>` prints one request exactly
as sent, each wire attempt, and the response.

    tofu context
    tofu shells list
    tofu shells log <name>

print a bar per context band of the newest session, then the shells with
their state, pid and command, and one shell's output. Every verb here
takes `--json` and prints one JSON document.

## Undo it

Delete a session's folder under `sessions/` to remove it for good. A
rename is undone by renaming again. `tofu migrate` moves and converts,
and has no undo, which is why `--dry-run` lists every move first.
