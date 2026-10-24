---
topic: sessions
title: Sessions
summary: continue where you left off, find an old session, and where sessions and running shells are kept
verbs: session, host, context, shells, migrate
---

## What it is

A session is one conversation with tofu: every message, tool call and sub-agent, recorded as it happens. You can continue it later, read it back, or give it a name. The session you last worked in is the head, and that is the one `tofu --continue` opens. A reply cut short by Esc, a closed terminal or a hangup is kept as far as it got and shows as interrupted on `tofu --continue`; quitting waits at most 3 s for it. On Linux and macOS a hangup quits tofu the way a person's quit does, saving first.

A conversation keeps one name for its whole life, such as `tidy-moss-vole#12cp3.2`: the name, the family tag after `#`, and the generation after `.`. A fork to stay under the context budget, `/compact` and a move to another account start the next generation, so the name stays and the number rises. `/new` starts a new family, and so does a branch, which records where it branched. Hooks see the family id.

Every message in the chat shows the id it was recorded under, such as `[message#9c2d40]`. Type that
reference in a message and the lead quotes it, across forks and `--continue`. A fork carries every
message you typed, word for word, and keeps the model you chose. A resumed session or sub-agent keeps its pictures and its model, and a long chat holds a window of its newest rows and reads the rest as you scroll.

Every session runs in the project's `tofu host`, one process per project. The terminal, the desk and `tofu serve` each join it as a member, so closing one window leaves the others working, and a prompt, Esc, a steer or an approval from any member runs in the host. The first window starts it, and it exits 5 s after its last member leaves; the last member leaving stops the session's turn and ends its shells. A member silent for 10 s is let go. A newer tofu joining an older host says so and runs on the older build until the last member leaves.

A shell an agent started, like a dev server, keeps running after the task and stops when tofu exits, with every process it started, even when tofu is killed. With the `persistentRegistry` setting on, a shell keeps running after tofu exits, until you stop it, and the next launch lists it.

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

The host's socket is under `~/.tofu/h`, or on Windows a temp folder only you can open, and its log is `~/.tofu/h/<key>.log`, kept under 1 MiB.

None of this is written into the project. A session idle longer than `sessionStaleDays`, 90 days by default, is deleted when the host starts; `0` keeps every session. Opening a project moves what older versions kept in its `.tofu` once; a clash keeps the home's file (a `log/` day merges both).

A folder from before this layout, named after the whole path (`C--code-shop`), is renamed to the new
name, instantly, the first time `tofu`, `tofu session`, `tofu serve` or `tofu run` opens the project.
If an older tofu holds a file in it, tofu says so in one line, reads the old folder, and retries.

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
    tofu session set <name|id> --model claude-sub/claude-sonnet-5
    tofu session branch <name|id> --side [--preset notes] [--seed none]

`session set --model` runs the session on that model from its next resume, or its next turn when a host holds it. `session branch --side` opens a side chat, never the head and never spawning, from the fork summary. It writes `read` nothing, `notes` `.md` and `.html`, `files` anything, or `--owns` globs; `sideChatAccess` sets the default, `read`.

Clean up old sessions, stop the host, move what an older version left in the project, or stop and restart a shell an agent left running:

    tofu session clean --dry-run
    tofu session clean
    tofu host stop
    tofu migrate --dry-run
    tofu migrate
    tofu shells stop <name>
    tofu shells restart <name>

`session clean` deletes the sessions idle past `sessionStaleDays` and keeps the head, a session a kept one continues or branches from, one open or busy, and one with a live cron job; `--dry-run` lists them first.

## Check it

    tofu host status

prints the host's process, socket, project, version, members and start time; `tofu host ensure` starts it when none answers. Both take `--dir` and `--json`.

    tofu session list

lists this project's conversations, one row per family, newest first, `●` on the head, each with its handle, age, steps, state and task, and counts the sessions idle past `sessionStaleDays`. A session that cannot be read goes under `unreadable` with a `✗`. `--all` adds the side chats.

    tofu session <name|id>

prints the family: when it started, how long it was worked on (each turn from its first event to its
last), and each generation with how it began, its steps, active time and size, its branches and
sub-agents, in about 0.7 s on 80 generations.

    tofu session find <name|id> --tool bash

finds calls across every generation. `--command`, `--file`, `--text`, `--agent`, `--since` and `--until` (`2h`, or a time) narrow it together. `--text` also matches a call's result and names its tool; `--failed` lists only failed calls and calls a gate refused (`bash refused`).

    tofu session info <name|id>

prints one session: its id, task, counts, its `history` (`fresh, holds 40 messages`, or `12 messages from
<handle>` for one carried over, `nothing to resume` when empty), whether it ended, its model and cost, why
it ended in an error, and the command that continues it. `tofu session reads <name|id>` lists every file it read, and `tofu session drain <name|id>` where its tokens went, in `tofu docs drain`.

    tofu session trace <name|id>

prints a summary: lead turns and how they ended, each sub-agent with its state, calls and last report, errors by kind, every stop with what it took down, and the commands that drill in. Every request, call and sub-agent run says `start_ms`, milliseconds from the session's start, and a message tofu added rather than you typed reads `from <kind>/<sub>`, such as `from tofu/shell.exit`: every message carries a typed origin, so only your words show as you.
`--since 1h`, `--from 04:10 --to 04:20`, `--agent <name>` or `lead`, `--turn <id>`, `--kind stop,error,gate` (a wrong kind names them all), `--grep <text>` and `--last 20` list the rows behind it, one line each, and combine; `--wide` prints each row whole, `--full` every request, call, hook and message, and `--json` the same rows. `--kind notice` lists each sub-agent move, spawned, reviewed, reopened or accepted, with its round and reason.
`tofu session request <name|id> <request id>` prints one request exactly as sent, each wire attempt, and the response, then what went into it: each prompt part with its tokens and whether it was switched off, the skills, the memory, tokens by kind, images, and the messages new or dropped since the last request (`not recorded` for one an older tofu sent).

    tofu context
    tofu shells list
    tofu shells log <name>

print the newest session's context bands, the shells, and one shell's output. Every verb here takes
`--json` and prints one JSON document.

## Undo it

Delete a session's folder under `sessions/` to remove it for good. A deleted session does not come back, so set `sessionStaleDays` to `0` before the host starts to keep every session. A rename is undone by renaming again. `tofu migrate` moves and converts, and has no undo, which is why `--dry-run` lists every move first.
