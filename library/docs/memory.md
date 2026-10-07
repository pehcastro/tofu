---
topic: memory
title: Memory
summary: what tofu remembers for you across sessions, for every project or for one, and how to add, list and forget it
verbs: memory
---

## What it is

Memory is a short list of things you told tofu to keep, so you do not
have to say them again in the next session. Each entry is one line, like
`never run cargo with more than 2 jobs`, with your own words kept beside
it.

Every entry is sent to the lead in its system message, on every request
of every session in its scope, and it survives a fork. Sub-agents never
get it: the lead writes what matters into their brief.

An entry has a kind:

- `person`: how you work and what you want from the lead
- `project`: a fact or a decision about this project the code does not say
- `reference`: where something outside the project lives

Memory is not for what a rule, `AGENTS.md` or `CLAUDE.md` already says,
and not for the task in front of you.

## Where it lives

- `~/.tofu/memory/`: global, read in every project
- `~/.tofu/projects/<project>/memory/`: this project only, beside its
  sessions, never inside the project

Each entry is one file, `<id>.yaml`, with `id`, `kind`, `text`, `said`,
`session`, `at` and `by` lines. A hand edit is one small file.

Both scopes are sent, global first. Where two entries disagree, the
project one wins, and the block says so.

Each scope holds at most 4096 bytes, about 1,000 tokens, counted as the
lines the lead reads. A write past it is refused, with the entries listed
and the command that removes one. Nothing is dropped on its own.

## Change it

Add an entry for this project, or for every project with `--global`:

    tofu memory add "this project uses gpui-ce, not gpui"
    tofu memory add --global "cargo runs with at most 2 jobs"

`--kind person|project|reference` sets the kind; the default is `person`
with `--global` and `project` without. `--said "<your words>"` keeps the
words it came from. `--replace <id>` rewrites an entry in place, and
`--dir <project>` names another project.

In the app, a sentence that starts with `remember`, such as `also
remember that the build is cargo build -j 2`, gets an offer card: `1`
yes, `2` no, `3` always. `tab` switches between every project and this
project. Nothing is written until you pick, and `/remember <what>` opens
the same card. The lead can offer one too, with its `remember` tool, but
only with words you typed in this conversation; it asks the same three.

Your first 10 answers are counted. Once 7 of them kept the entry, or you
answer always, the `autoMemory` setting turns on and an entry is kept at
once, with the undo on the same line. `tofu settings set memory false`
sends no memory and offers nothing.

Every write is a row in the chat, `[memory#m3]`, and the lead is told in
the same turn that it is saved.

## Check it

    tofu memory

prints each scope with its size against the limit and its folder, then
every entry with its id, kind, date and text, and your words under it.
`/memory` in the app lists them; enter on one removes it or puts it in
the composer to edit. `tofu session trace <session>` lists each write
under notices with its id, scope and words, and
`tofu run --show-prompt --dir . "<task>"` prints the block at the end of
the system message, each line with its `[memory#id]`.

## Undo it

    tofu memory remove m3
    tofu memory remove --global m3

deletes the entry's file and prints the `tofu memory add` that puts it
back. Every add prints its own undo line.
