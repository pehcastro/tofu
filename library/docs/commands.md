---
topic: commands
title: Commands in the app
summary: every slash command the app's composer takes, how /compact shrinks the history the next turn carries, and how /undo puts files back
verbs: context, session, undo
---

## What it is

Typing `/` at the start of the composer opens the command list. Typing more
filters it, the arrow keys pick a row, tab completes, and enter runs it.
`/simplify`, `/debug` and `/hunt` start a model turn: the model gets the
command's skill, and the chat shows what you typed. Every other command runs
in tofu and never reaches the model. `tofu serve` answers the list as
`query.commands`; a command that starts a turn says `turn` or `diff-turn`.

- `/help`: every command and what it does, one line each
- `/simplify`: the first message carries the simplify skill and the last
  diff, or the last commit on a clean tree; cuts wait for your approval
- `/debug <symptom>`: reproduce, narrow, fix the cause, show it go green
- `/hunt [where]`: spawns `hunter`, then `skeptic`, then `referee` after
  both report; only what can be shown now is kept, nothing is fixed
- `/chat`, `/sub-agents`, `/file-edits`, `/shells`, `/settings`: open that screen
- `/models`: choose the model the next turn runs
- `/status`: each subscription's quota windows and when they reset
- `/attach`: reference a file in this workspace
- `/keys`: every key the app answers to, with the key bound now; type to filter
- `/links`: every link this conversation carried, newest first
- `/quote`: cite a past turn by id, as `[message#id]` typed does
- `/copy`, `/copy-call`: put the last answer, or the last tool call and its result, on the clipboard
- `/reload`: read settings, rules, skills, sub-agents, models, instructions and keys again
- `/resume`: pick a session of this project, newest first, and carry it into
  the next task; `/resume <name or id>` takes that session straight away
- `/side`: a side chat on this session's cache, as `sideChatAccess` allows
- `/new`: start fresh, carrying nothing from the last session
- `/compact`: shrink the old tool results the next turn carries
- `/undo`, `/undo N`: put back the files the last turn, or the last N, changed
- `/cron`, `/loop`, `/goal`: scheduled and repeated prompts
- `/quit`: leave tofu
- `!git status`, or any `!` prompt: runs in the project as the `bash` tool
  does, sends nothing; the next prompt carries its output, keys redacted.
  `Esc` stops it, and a stopped one is not carried. Refused mid-turn

`/compact` keeps the head of the history byte for byte, so the next request
still reads it from cache: the system prompt, the tools and every message
before the newest 8,000 tokens. With no model call, each old tool result
after it becomes a note and a handle `artifact_fetch` reads back; the last
step's results stay whole. `tofu session trace <name> --kind compaction`
names each cut, with its handle and bytes.

`/undo` puts back every file the last turn changed, whether by `edit`,
`write`, `bash` or a sub-agent: a file it created is deleted and a file it
deleted comes back, byte for byte. The conversation stays as it was. A file
changed after the turn ended, by you or a background shell, is left alone and
named. Ignored files and new files over 10 MiB are never touched. `tofu undo`
does the same from a terminal, and needs git on the PATH.

## Where it lives

The shrunk history is written as a new session that continues the old one,
in the same folder as every session: `~/.tofu/projects/<project>/sessions`.
The head moves to it, so `tofu --continue` after a restart carries the
shrunk history. The old session is kept whole and marked as forked into the
new one. The stored results are in `~/.tofu/projects/<project>/artifacts`.

What `/undo` puts back is one private git folder per project,
`~/.tofu/projects/<project>/undo.git`, beside `sessions/`, written at the
start and the end of each turn. Each session keeps its list of turns in
`sessions/<session>/undo-turns.jsonl`. A turn older than 7 days is pruned and
can no longer be undone. The project's own `.git` is never read for this and
never written.

## Change it

Between turns, type `/compact`. The chat says what it shrank and kept:

    compacted 2 old tool result(s) after the first 6 message(s), which stay
    as they were so the next request reads them from cache: the history went
    from about 21332 tokens to 11218, and the next turn carries it as
    session 84bbad34-9a69-4cb9-9aa2-f6d2d128587d

While the lead works, `/compact` does nothing and says to stop the turn
first with ctrl+c. While the lead is idle but waits on its sub-agents,
`/compact`, `/new` and `/undo` say so and run when the turn ends. With no
history yet, or nothing left to shrink, it says so and changes nothing.
In an app that joined the project's tofu host, `/compact` shrinks the
session the host holds, and every member moves to the new session.

Between turns, `/undo 2` undoes the last two turns, and a second `/undo`
goes one turn further back. From a terminal:

    tofu undo --dry-run
    tofu undo 2 --force

`--dry-run` lists what would change and writes nothing, and `--force` puts
back a file changed after the turn too. `tofu undo --turn <id>` undoes that turn only while it is
the newest recorded, and otherwise names the newest; two undos on one session run one at a time.

## Check it

    tofu session list

lists the new session at the head, beside the one it continues.

    tofu session info <old session>

shows the fork, its kind and the counts:
`fork  into 6eae6fe1-... · compact · 1599 → 695 tokens`.

    git status --short

after `/undo` matches what it printed before the turn.

## Undo it

`/compact` does not change the old session. To carry its history whole
again:

    tofu session resume <old session>
