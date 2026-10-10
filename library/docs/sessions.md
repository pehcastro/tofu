---
topic: sessions
title: Sessions
summary: continue where you left off, find an old session, and where sessions and running shells are kept
verbs: session, context, shells, migrate
---

## What it is

A session is one conversation with tofu: every message, tool call and sub-agent, recorded as it
happens. You can continue it later, read it back, or give it a name. The session you last worked in
is the head, and that is the one `tofu --continue` opens.

A conversation keeps one name for its whole life, such as `tidy-moss-vole#12cp3.2`: the name, the
family tag after `#`, and the generation after `.`. A fork to stay under the context budget,
`/compact` and a move to another account start the next generation, so the name stays and the number
rises. `/new` starts a new family, and so does a branch, which records where it branched. Hooks see
the family id.

Every message in the chat shows the id it was recorded under, such as `[message#9c2d40]`. Type that
reference in a message and the lead quotes it, across forks and `--continue`. A fork carries every
message you typed, word for word.

A shell an agent started, like a dev server, keeps running after the task and stops when tofu exits,
with every process it started, even when tofu is killed. With the `persistentRegistry` setting on, a
shell keeps running after tofu exits, until you stop it, and the next launch lists it.

## Where it lives

Each project has its own folder in your home, `~/.tofu/projects/<folder>-<hash>/`: the project
folder's own name and eight characters of a hash of its full path, so `C:\code\shop` becomes
`shop-7cd21bfb`. The path is read after links and Windows junctions are followed, and without case
on Windows and macOS, so two ways to reach one folder share one record and `C:\a-b` and `C:\a b`
never do. `~/.tofu/projects.json` lists which paths own which folder. In it:

- `project.toml`: the project's paths, git remote, root commit and when tofu first opened it
- `sessions/<id>/`: one folder per generation, holding `session.json` and `events.jsonl`
- `log/`: the decision ledger
- `shells/`: the shells an agent left running, and their logs

None of this is written into the project, and tofu never deletes a session. `tofu migrate` moves
what older versions kept in `.tofu`.

A folder from before this layout, named after the whole path (`C--code-shop`), is copied to the new
name the first time `tofu`, `tofu serve` or `tofu run` opens the project, and is kept. Close older
tofu sessions first: what they write after the copy stays in the old folder.

Move or rename a git repository and open it in its new place: tofu finds its folder by the git
remote or the first commit, and offers to relink it. The app asks; `tofu serve` and `tofu run` print
the offer, and `tofu migrate --relink` in the new place accepts it.

## Change it

Continue the head, or any other session:

    tofu --continue
    tofu session resume <name|id>

A session is named by its name (the newest generation), its id, or a handle:
`tidy-moss-vole#12cp3.1` is the first generation, `12cp3` the family; `[session#9ff700]` from the
header finds its generation too. In the app, `/resume` carries the last session and `/new` starts
fresh.

    tofu session rename <name|id> "checkout redesign"
    tofu session branch <name|id> --side [--preset notes] [--seed none]

opens a side chat, never the head and never spawning, from the fork summary. It writes `read`
nothing, `notes` `.md` and `.html`, `files` anything, or `--owns` globs; `sideChatAccess` sets the
default, `read`.

Move what an older version left in the project, or stop and restart a shell an agent left running:

    tofu migrate --dry-run
    tofu migrate
    tofu shells stop <name>
    tofu shells restart <name>

## Check it

    tofu session list

lists this project's conversations, one row per family, newest first, `●` on the head, each with its
handle, age, steps, state and task. A session that cannot be read goes under `unreadable` with a
`✗`. `--all` adds the side chats.

    tofu session <name|id>

prints the family: when it started, how long it was worked on (each turn from its first event to its
last), and each generation with how it began, its steps, active time and size, its branches and
sub-agents.

    tofu session find <name|id> --tool bash

finds calls across every generation. `--command`, `--file`, `--text`, `--agent`, `--since` and
`--until` (`2h`, or a time) narrow it together.

    tofu session info <name|id>

prints one session: its id, task, counts, whether it ended, its model and cost, why it ended in an
error, and the command that continues it. `tofu session reads <name|id>` lists every file it read.

    tofu session trace <name|id>

lists every request, message with its `[message#id]`, tool call with its arguments, gate verdict and
hooks, each message tofu added, each fork, compaction and resume, and each turn that ended in an
error. `tofu session request <name|id> <request id>` prints one request exactly as sent, each wire
attempt, and the response.

    tofu context
    tofu shells list
    tofu shells log <name>

print the newest session's context bands, the shells, and one shell's output. Every verb here takes
`--json` and prints one JSON document.

## Undo it

Delete a session's folder under `sessions/` to remove it for good. A rename is undone by renaming
again. `tofu migrate` moves and converts, and has no undo, which is why `--dry-run` lists every move
first.
