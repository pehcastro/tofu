---
title: Memory
description: Tell tofu once. Memory keeps what you asked it to remember in four scopes, and the episode log keeps the whole conversation of a project, summarized as a tree the lead can open line by line.
order: 14
updated: 2026-10-09
---

Memory keeps two things for you across sessions. **Entries** are the short
rules you told tofu to keep, like `never run cargo with more than 2 jobs`,
with your own words beside them. **Episodes** are the conversation itself:
your messages, the lead's replies and the sub-agents' reports, kept for the
whole life of a project. Both open each session, so you don't repeat
yourself and the lead doesn't lose what was said three forks ago.

## Four scopes, and the closest one wins

An entry lives in one of four scopes. A global scope sits in your home, a
local one in the repository's `.tofu` folder:

| Scope | Means | Kept in |
|---|---|---|
| `user-global` | you, in every project | `~/.tofu/memory/user/` |
| `project-global` | this project, as this machine knows it | `~/.tofu/projects/<project>/memory/` |
| `user-local` | you, in this repository | `<repo>/.tofu/memory/user/<author>/` |
| `project-local` | the team, traveling with the repository | `<repo>/.tofu/memory/project/` |

Where two entries disagree, the first of `user-local`, `project-local`,
`project-global` and `user-global` wins: your own wish in this repository
beats the team's convention, and the team's convention beats your habit
everywhere else.

**A team can share it.** Commit `<repo>/.tofu` and every teammate's tofu
reads the `project-local` entries. Nothing private goes in: every entry
carries an opaque author, a scrypt hash of your `gh` id or git email with
a salt kept in the repository, never the email itself. Another author's
`user-local` entries apply to you only with `memoryFromAllUsers` on.

**You decide what is kept.** Nothing you type is offered as memory, even a
message that says `remember`. The lead can offer one short rule, quoting
words you typed, on a card such as `[&orchestrator] wants to add a
project-global memory`. Jev suggests the scope on the card, `tab` changes
it, and nothing is written before you pick. A rule that names something of
yours is never offered for `project-local`, which every teammate reads.

**A scope never refuses a write.** Each scope has a view budget, 8 KB, or
12 KB for `project-global`. Past it, old entries go coarse into one-line
summaries that the lead can open again, instead of a full shelf turning
your next entry away.

**The cache stays warm.** Memory opens the session as its own message,
after the system prompt, so remembering something mid-session doesn't
rewrite the cached prompt. A sub-agent gets only the entries the lead cites
in its brief as `[memory#m3]`, copied word for word, never the whole list.

## The conversation is the memory

The episode log follows one design, OptChat, by Victor Taelin
([gist](https://gist.github.com/VictorTaelin/91837951a5ce5b38f341ec1ba1df6449)),
packaged for any agent as [OptMem](https://github.com/VictorTaelin/OptMem).
Its idea is that the chat itself is the memory: every message is logged
word for word, forever, and a cheap model folds the log into a binary tree
of one-line summaries. Each message is a line, two neighbouring lines merge
into one, two of those merge again, and so on. The model reads a **view**,
summary lines covering the whole log with recent lines whole and old ones
coarse, and when a line is too vague it **zooms**, opening that line into
the two lines it was made from, down to the original message.

tofu implements that tree as it is written: an append-only log, nodes of at
most 512 bytes named `id+n` (the first item and how many it covers), a view
saved to disk and never rebuilt on restart, and the model tools `zoom` and
`recall`, with OptChat's rule that the lead zooms until it has a line whole
before it acts on it. Where tofu departs:

- **Sessions still exist.** tofu keeps sessions and forks, and the episode
  view is carried at every fork, after memory and before the session. A
  fork needs no model call to write down where the work stands.
- **No tool output in the log.** Episodes hold your words, the lead's
  replies and sub-agent reports. A tool result keeps its handle, which
  `lookup` and `artifact_fetch` read back exactly.
- **The view fits the context target.** It takes at most half the room left
  under the target, capped at 32 KB, where OptChat sends 64 to 128 KB.
- **Your subscription writes the summaries.** The `memoryModel` setting,
  `claude-sub/claude-haiku-4-5-20251001` by default, compacts between
  turns, never on the classifier key.
- **Entries get the same tree.** Each of the four scopes is its own log,
  tree and view, so `zoom` and `recall` work on entries as well as
  episodes.

On a bench of 30 questions about earlier work across chains that forked
again and again, the episode view made 0 fork-summary calls per 100
messages against 12 for a model-written summary, and 95.3% of input was
read from cache against 93.2%. On a real session that forked four times,
the lead called `zoom` and quoted a reply from before the first fork word
for word.

## Keeping, reading and forgetting

- **Keep something for every project**: type `/remember never run cargo
  with more than 2 jobs` in the app.
- **Keep it in a scope**: `tofu memory add --scope user-local "tickets
  before code"`. Without `--scope`, a `person` entry goes to `user-global`
  and the rest to `project-global`.
- **See every entry**: `tofu memory`, or `/memory` in the app, where
  `Enter` on an entry removes it or puts it in the composer to edit.
- **Let the lead keep rules without asking**: answer **always** on a card,
  or `tofu settings set autoMemory true`. A `project-local` rule still asks.
- **Apply teammates' own entries**: `tofu settings set memoryFromAllUsers on`.
- **Keep no conversation**: `tofu settings set episodes off`. Entries still
  go; the episode log, its view, `zoom` and `recall` stop.
- **Turn memory off**: `tofu settings set memory false`.
- **Forget one**: `tofu memory remove m3`. Every add prints its own undo
  line, and every removal prints the add that puts it back.

A home with entries from an older tofu, one `<id>.yaml` file each, is copied
into the scopes on first use, and the yaml files stay where they were.

## Commands

```
/remember <what>
/memory
tofu memory [list] [--json]
tofu memory add [--scope user-global|project-global|user-local|project-local] [--kind person|project|reference] [--said "<words>"] [--replace <id>] [--dir path] "<statement>"
tofu memory remove [--scope <scope>] [--dir path] <id>
tofu memory tree <log.jsonl> [--budget bytes]
tofu memory zoom <log.jsonl> <id> <n>
tofu memory recall <log.jsonl> <regex>
```

`tofu memory` lists each scope by precedence, with its bytes against its
view budget and where an entry is promoted next, then each entry. In a
project at `C:\code\shop`:

```text
Memory · 2 entries · where two disagree the first wins: user-local,
  project-local, project-global, user-global
  user-local      0 · 0 bytes of a 8192 byte view · promotes to user-global
    ~/shop/.tofu/memory/user
  project-local   0 · 0 bytes of a 8192 byte view · promotes to none
    ~/shop/.tofu/memory/project
  project-global  1 · 55 bytes of a 12288 byte view · promotes to
    project-local
    ~/.tofu/projects/C--code-shop/memory
  user-global     1 · 40 bytes of a 8192 byte view · promotes to none
    ~/.tofu/memory/user

project-global
  m1       project   2026-10-09  you      this project uses pnpm, never npm

user-global
  m2       person    2026-10-09  you      tickets before code
```

`tofu memory tree` builds the tree over any log of `kind` and `text` JSON
lines and prints its view. A line that fits in 512 bytes is its own node,
with no model call:

```text
0+1|user: the dev server runs on port 4317
1+1|lead: noted, I will start it there
2+1|user: use pnpm, never npm
4 nodes built now: 0 model calls on claude-sub/claude-haiku-4-5-20251001, 4 free, 0 failed; the view is 3 lines, 112 bytes of a 8192 byte budget
```

`tofu memory recall log.jsonl port` prints every item that matches, word
for word. A project's episode log is
`~/.tofu/projects/<project>/episodes/log.jsonl`, and the three verbs read it
too.
