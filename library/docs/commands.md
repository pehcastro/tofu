---
topic: commands
title: Commands in the app
summary: every slash command the app's composer takes, how /compact shrinks the history the next turn carries, how /fork and /branch start a new line by hand, how /undo puts files back, and how /permissions lists and revokes a standing answer
verbs: context, session, undo
---

## What it is

Typing `/` at the start of the composer opens the command list. Typing more filters it, the arrow keys pick a row, tab completes, and enter runs it.
`/simplify`, `/debug`, `/hunt` and `/generate-map` start a model turn: the model gets the command's skill, and the chat shows what you typed. Every other command runs in tofu and never reaches the model. `tofu serve` answers the list as `query.commands`; a command that starts a turn says `turn` or `diff-turn`.

- `/help`: every command and what it does, one line each
- `/simplify`: the first message carries the simplify skill and the last diff, or the last commit on a clean tree; cuts wait for your approval
- `/debug <symptom>`: reproduce, narrow, fix the cause, show it go green
- `/hunt [where]`: spawns `hunter`, then `skeptic`, then `referee` after both report; only what can be shown now is kept, nothing is fixed
- `/generate-map`: drafts `MAP.md` from the tree, one row per top-level folder, or shows the changes to the one there, and writes nothing until you say yes; see `tofu docs generate-map`
- `/chat`, `/sub-agents`, `/file-edits`, `/shells`, `/settings`: open that screen
- `/models`: choose the model the next turn runs
- `/status`: each subscription's quota windows and when they reset
- `/attach`: reference a file in this workspace
- `/keys`: every key the app answers to, with the key bound now; type to filter
- `/links`: every link this conversation carried, newest first
- `/quote`: cite a past turn by id, as `[message#id]` typed does
- `/copy`, `/copy-call`: put the last answer, or the last tool call and its result, on the clipboard
- `/permissions`, `/permissions revoke <n>`: every always here and never here answer standing in this session, numbered, or take answer `n` back so its next call asks you again; see `tofu docs gate`
- `/reload`: read settings, rules, skills, sub-agents, models, instructions and keys again
- `/resume`: pick a session of this project, newest first, and carry it into the next task; `/resume <name or id>` takes that session straight away
- `/side`: a side chat on this session's cache, as `sideChatAccess` allows
- `/new`: start fresh, carrying nothing from the last session
- `/compact`, `/compact <focus>`: shrink the old tool results the next turn carries; with a focus, every result that names it stays whole
- `/fork`: start a fresh line now, carrying what `/compact` carries
- `/branch`, `/branch N`: list your messages, or restart from message N on a new line, the old line kept
- `/undo`, `/undo N`: put back the files the last turn, or the last N, changed
- `/cron`, `/loop`, `/goal`: scheduled and repeated prompts
- `/quit`: leave tofu
- `!git status`, or any `!` prompt: runs in the project as the `bash` tool does, sends nothing; the next prompt carries its output, keys redacted. `Esc` stops it, and a stopped one is not carried. Refused mid-turn

`/compact` keeps the head of the history byte for byte, so the next request still reads it from cache: the system prompt, the tools and every message before the newest 8,000 tokens. Jev reads each old tool result after it, `keep_result`, and keeps the ones the work still needs; each other one becomes a note and a handle `artifact_fetch` reads back. With no classifier bound, the age rule cuts them all. The last step's results stay whole.
`tofu session trace <name> --kind compaction` names each cut, with its handle, bytes, `judged` or `aged`, and its `keep_result` row; a cut read back with `artifact_fetch` labels that row `refetched`.

`/compact <focus>` keeps whole every old tool result whose text, tool name or arguments name the focus, matched as one phrase whatever its case: `/compact parser.go` keeps the reads of that file and shrinks the rest. The chat says how many results the focus kept, and the fork records the focus in its reason.

`/fork` opens a new line now and carries what `/compact` carries: the same warm head byte for byte, the same results shrunk. It forks even when nothing is left to shrink, so the next turn starts a fresh generation on the cache the last one warmed. The new line records the kind `fork`.

`/branch N` restarts from your Nth message, counted in what the next turn carries: the new line carries every message before it, and its words wait in the composer to send as they were or to change. The old line is kept open and whole, and `tofu session resume` takes it back. The new line records the kind `branch`. `/branch` alone lists your messages with their numbers. On a session with no turns yet, `/fork` and `/branch` say there is nothing to fork.

`/undo` puts back every file the last turn changed, whether by `edit`, `write`, `bash` or a sub-agent: a file it created is deleted and a file it deleted comes back, byte for byte. The conversation stays as it was. A file changed after the turn ended, by you or a background shell, is left alone and named. Ignored files and new files over 10 MiB are never touched. `tofu undo` does the same from a terminal, and needs git on the PATH.

## Where it lives

The shrunk history is written as a new session that continues the old one, in the same folder as every session: `~/.tofu/projects/<project>/sessions`. The head moves to it, so `tofu --continue` after a restart carries the shrunk history. The old session is kept whole and marked as forked into the new one. The stored results are in `~/.tofu/projects/<project>/artifacts`.

What `/undo` puts back is one private git folder per project, `~/.tofu/projects/<project>/undo.git`, beside `sessions/`, written at the start and the end of each turn. Each session keeps its list of turns in `sessions/<session>/undo-turns.jsonl`. A turn older than 7 days is pruned and can no longer be undone. The project's own `.git` is never read for this and never written.

## Change it

Between turns, type `/compact`. The chat says what it shrank and kept:

    compacted 2 old tool result(s) after the first 6 message(s), which stay
    as they were so the next request reads them from cache: the history went
    from about 21332 tokens to 11218, and the next turn carries it as
    session 84bbad34-9a69-4cb9-9aa2-f6d2d128587d

While the lead works, `/compact` does nothing and says to stop the turn first with ctrl+c. While the lead is idle but waits on its sub-agents, `/compact`, `/new` and `/undo` say so and run when the turn ends. With no history yet, or nothing left to shrink, it says so and changes nothing. In an app that joined the project's tofu host, `/compact` shrinks the session the host holds, and every member moves to the new session.

Between turns, `/undo 2` undoes the last two turns, and a second `/undo` goes one turn further back. From a terminal:

    tofu undo --dry-run
    tofu undo 2 --force

`--dry-run` lists what would change and writes nothing, and `--force` puts back a file changed after the turn too. `tofu undo --turn <id>` undoes that turn only while it is the newest recorded, and otherwise names the newest; two undos on one session run one at a time.

## Check it

    tofu session list

lists the new session at the head, beside the one it continues.

    tofu session info <old session>

shows the fork, its kind and the counts:
`fork  into 6eae6fe1-... · compact · 1599 → 695 tokens`.

    tofu session <name>

lists each generation with the kind that began it, `compact`, `fork` or `branch`, and `tofu session info <new line>` names the line its history came from.

    git status --short

after `/undo` matches what it printed before the turn.

## Undo it

`/compact`, `/fork` and `/branch` do not change the old session's history. To carry it whole again:

    tofu session resume <old session>
