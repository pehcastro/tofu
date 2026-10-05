---
topic: commands
title: Commands in the app
summary: every slash command the app's composer takes, how /compact shrinks the history the next turn carries, and how /undo puts files back
verbs: context, session, undo
---

## What it is

Typing `/` at the start of the composer opens the command list. Typing more
filters it, the arrow keys pick a row, tab completes, and enter runs it. A
command never reaches the model.

- `/chat`, `/sub-agents`, `/file-edits`, `/shells`, `/settings`: open that screen
- `/models`: choose the model the next turn runs
- `/status`: each subscription's quota windows and when they reset
- `/attach`: reference a file in this workspace
- `/keys`: every key the app answers to, grouped, with what it does; the
  rebindable ones show the key bound now. Type to filter, esc closes
- `/links`: every link this conversation carried, newest first
- `/quote`: cite a past turn by id
- `/copy`, `/copy-call`: put the last answer, or the last tool call and
  its result, on the clipboard
- `/reload`: read settings, rules, skills, sub-agents, models,
  instructions and keys from disk again
- `/resume`: pick a session of this project, newest first, and carry it
  into the next task; typing filters by name and first line, the one in use
  is marked, and the chat shows the chosen session's turns; `/resume <name
  or id>` takes that session straight away
- `/new`: start fresh, carrying nothing from the last session
- `/compact`: shrink the old tool results the next turn carries
- `/undo`, `/undo N`: put back the files the last turn, or the last N
  turns, changed
- `/cron`, `/loop`, `/goal`: scheduled and repeated prompts
- `/quit`: leave tofu

`/compact` replaces every old tool result in the history the next turn
carries with a short note and an artifact handle. It is the same shrink tofu
runs on its own when a request overflows the model's window, and it needs no
model call. The result is stored whole, and the model reads any part of it
again with `artifact_fetch`. The last step's results stay whole.

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

What `/undo` puts back is a private git folder per session,
`sessions/<session>/undo.git`, written at the start and the end of each turn,
with the list of turns in `undo.jsonl` beside it. The project's own `.git` is
never read for this and never written.

## Change it

Between turns, type:

    /compact

The chat says how many results were shrunk and how large the history was
before and after, in tokens:

    compacted 2 old tool result(s) to an artifact handle each: the history
    went from about 1599 tokens to 695, and the next turn carries it as
    session 6eae6fe1-0bc5-473f-bd01-ad4361d330c5

While a turn runs, `/compact` does nothing and says to stop the turn first
with ctrl+c. With no history yet, or nothing left to shrink, it says so and
changes nothing.

Between turns, `/undo 2` undoes the last two turns, and a second `/undo`
goes one turn further back. From a terminal:

    tofu undo --dry-run
    tofu undo 2 --force

`--dry-run` lists what would change and writes nothing, and `--force` puts
back a file changed after the turn too.

## Check it

    tofu session list

lists the new session at the head, beside the one it continues.

    tofu session info <old session>

shows the fork, its kind and the counts:
`fork  into 6eae6fe1-... · compact · 1599 → 695 tokens`.

    tofu context

after the next turn shows how full the history is now.

    git status --short

after `/undo` matches what it printed before the turn.

## Undo it

`/compact` does not change the old session. To carry its history whole
again:

    tofu session resume <old session>
