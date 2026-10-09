---
topic: memory
title: Memory
summary: what tofu remembers for you across sessions, in four scopes from you everywhere to the team in one repository, and how to add, list and forget it
verbs: memory
---

## What it is

Memory is a short list of things you told tofu to keep, so you do not
have to say them again. Each entry is one line, like `never run cargo
with more than 2 jobs`, with your own words beside it. It opens each session as its own message
after the system message, kept through a fork and a resume; a new entry
joins next session, and a sub-agent gets only what the lead cites as `[memory#id]`.

A kind says what it is about: `person` (how you work), `project` (a fact
the code does not say) or `reference` (where something outside lives).
Memory is not for what a rule, `AGENTS.md` or `CLAUDE.md` already says,
nor for the task in front of you. The conversation is kept too, as episodes: your words, the lead's
answers and sub-agent reports, never tool output; their view follows memory, sized to the room the context
leaves, through every fork, and the lead opens it with `zoom` and `recall`. `tofu settings set episodes off` keeps none.

## Where it lives

Global is your home, local is the repository's `.tofu` folder:

- `user-global`, `~/.tofu/memory/user/`: you, in every project
- `project-global`, `~/.tofu/projects/<project>/memory/`: this project, as this machine knows it
- `user-local`, `<repo>/.tofu/memory/user/<author>/`: you, in this repository
- `project-local`, `<repo>/.tofu/memory/project/`: the team, travelling
  with the repository

Each scope keeps `entries.jsonl`, one json line per write, never edited:
a removal or a replace is a new line. The repository holds only those and
the salt, so committing `<repo>/.tofu` needs no `.gitignore`; the summary
tree the lead reads, its view and its lock live in the home. A
repository id carries a tag, `m3-1f81`, so two homes never write the same.

Where two entries disagree, the first of `user-local`, `project-local`,
`project-global`, `user-global` wins. `library/memory/scopes@1.yaml`
sets each scope's precedence, the view budget the lead reads and where an
entry is promoted next. A scope past its budget is never refused: old
entries go coarse into one-line summaries that `tofu memory zoom` opens.

## Who wrote it

Every entry carries an `author`: scrypt of your identity with the salt in
`<repo>/.tofu/memory/salt` (the home has its own). The identity is your
`gh` login's numeric id, else `git config user.email`, and is never
written into the repository. Without either the author is `unknown`,
said once. Your `user-local` entries always apply to you, another
author's only with `tofu settings set memoryFromAllUsers on`, and
`project-local` applies to everyone.

## Change it

    tofu memory add --scope user-local "tickets before code"
    tofu memory add "this project uses gpui-ce, not gpui"

Without `--scope` a `person` entry goes to `user-global`, the others to
`project-global`; without `--kind` a user scope takes `person` and a
project scope `project`. `--said "<your words>"` keeps the words,
`--replace <id>` writes a new version, `--dir <project>` names another
project.

In the app, `/remember <what>` keeps a `user-global` entry at once and
prints the undo. A message you type is never offered as memory.

The lead offers one rule with its `remember` tool, only with words you
typed in this conversation: one line, at most 160 bytes, naming no one.
Its card, `[&orchestrator] wants to add a project-global memory`, takes
`1` yes, `2` no, `3` always, `esc` no, and `tab` switches to
`user-global`. Nothing is written before you pick. Always turns
`autoMemory` on, and later rules are kept at once in the scope the lead
named, except `project-local`, which always asks. `tofu settings set
memory false` sends no memory and removes the tool.
`tofu learn apply <n> --scope <scope>` keeps a finding where you say.

## Check it

    tofu memory

prints each scope by precedence with its bytes against its view budget,
its promotion and its folder, then each entry's id, kind, date, `you` or
`another`, text and your words. `/memory` in the app lists them; enter
removes one or puts it in the composer to edit.

    tofu memory tree log.jsonl --budget 8192

builds a summary tree over a log of `kind` and `text` json lines and
prints its view: recent items whole, older ones as summaries of at most
512 bytes, written by the `memoryModel` subscription model
(`claude-sub/claude-haiku-4-5-20251001`), never the OpenRouter key. The
tree and view are saved, so a second run calls no model. Its last line
says how many nodes were summarized and how many model calls that took:
a line over 512 bytes is asked for once more.

The episode summaries written between turns are memory-model calls too.
`tofu session trace <name>` lists them under `memory model`, with why
each was asked, its tokens and its cost. They are not steps: `tofu
session list` counts only the session's own model, and the trace never
names a memory call as a cache break.
`tofu memory zoom log.jsonl <id> <n>` opens line `id+n` into its halves,
down to the item at `n` 1, and `tofu memory recall log.jsonl <regex>`
searches the items. A scope's own `log.jsonl` takes all three: beside its
entries in the home, under `~/.tofu/projects/<project>/local/` for a
local one. The episodes log is `~/.tofu/projects/<project>/episodes/log.jsonl`.

## Moving from the old shelves

Up to 0.5.8 memory was `<id>.yaml` files in your home. The first use
after the update copies them and says which entry went where, on stderr
and in the app's chat: the global shelf and the project shelf's
`person` entries to `user-global`, the rest to `project-global`, none
to a repository. The yaml stays for 0.5.8, and `copied-shelves` beside
it lists what was copied, so nothing is copied twice or brought back
after you remove it. A later 0.5.8 entry is copied next time; a 0.5.8
edit or removal of a copied one is not. A home an earlier build moved
says once that the new store is the only copy.

## Undo it

    tofu memory remove m3
    tofu memory remove --scope user-local m3-1f81

writes the removal and prints the `tofu memory add` that puts it back.
Without `--scope` the id has to be in one scope only. Every add prints its
own undo line.
