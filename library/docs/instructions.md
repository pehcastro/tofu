---
topic: instructions
title: Instruction files
summary: which AGENTS.md and CLAUDE.md files tofu sends with every task, in what order, and how to change that
verbs: run, settings
---

## What it is

An instruction file is a plain Markdown file of house rules for a project,
like how to run the tests or which folders never to touch. tofu sends it
to the model with every task. It reads `AGENTS.md`, the name most coding
agents share, and `CLAUDE.md`, the name Claude Code uses, so a project set
up for either one works here.

## Where it lives

tofu looks from the working directory up to the git root, and stops
there. Outside a git repository it looks in the working directory alone.

- It takes the nearest `AGENTS.md` on that way up, and the nearest
  `CLAUDE.md`.
- When both sit in the same folder, only the one named first in the
  `instructionSources` setting is sent, `AGENTS.md` by default, and tofu
  says which one it skipped.
- When they sit in different folders, both are sent.
- An empty file is not sent.

From your home, only `~/.tofu/AGENTS.md` is read. It is sent first, in
every project. `~/.claude/CLAUDE.md` and any other file in your home are
never read.

The project's files come after yours, and the model is told that a later
file outranks an earlier one where they disagree, so the project wins.

Together they are cut at `projectInstructionsCap` bytes, 32768 by default.
When a file does not fit, the part that did not fit is dropped and tofu
names it.

## Change it

Write house rules for every project in `~/.tofu/AGENTS.md`, and for one
project in its `AGENTS.md` or `CLAUDE.md`.

Read only `CLAUDE.md`, or put it first:

    tofu settings set instructionSources CLAUDE.md
    tofu settings set instructionSources CLAUDE.md,AGENTS.md

Send more of a long file each task, up to 262144 bytes:

    tofu settings set projectInstructionsCap 65536

Change either one for a single project with `--scope project`:

    tofu settings set --scope project instructionSources CLAUDE.md

Work one task with no instruction file at all:

    tofu run --dir . --no-instructions "fix the failing test"

## Check it

    tofu run --dir . --show-prompt "anything"

prints the prompt the next task would send and sends nothing. Its
`instruction files:` line says how many bytes of instructions went in, or
that none was found, and the first user message shows each file under
the name it came from. A file that was cut or skipped is named on a
`tofu:` line.

    tofu settings get instructionSources
    tofu settings get projectInstructionsCap

print the two settings.

## Undo it

Delete the file you wrote, or the line you added to it. Set a setting
back with its default:

    tofu settings set instructionSources AGENTS.md,CLAUDE.md
    tofu settings set projectInstructionsCap 32768

or delete its line from `settings.json`.
