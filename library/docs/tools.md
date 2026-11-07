---
topic: tools
title: What the tools return
summary: what each tool gives the model, grouped by job, which page says more about each group, and how to see a result for yourself
verbs: tool
---

## What it is

The model calls tools; tofu runs each call and hands back a result. Each
result is shaped to be the answer rather than a log: counts and
`path:line` for a check, one line for a file the model already read, the
date a page states. `tofu tool <name> '<json>' --dir .` runs any of them
by hand and prints the exact text the model would read.

Each group has a page of its own:

- `tofu docs tools-checks`: `test`, `typecheck`, `read`, `write`, `edit`,
  `bash` and `shell`, which run the project's checks, read and change its
  files, and run its commands.
- `tofu docs tools-code`: `symbols`, `map` and `api`, which say where a
  name is declared and who calls it, what a change reaches, which files
  matter for a task, and what a dependency looks like at its pinned
  version.
- `tofu docs tools-debug`: `debug`, which runs a Go or Python program or
  test under `dlv` or `debugpy`, stops it at a line, and prints the values
  there, or lists every call of one Go function.
- `tofu docs tools-work`: `plan`, `history`, `fetch`, `web_search`, `env`,
  `http`, `record`, `profile`, `shape`, `db`, `spawn`, `race`,
  `ask_person` and `heads_up`, which organize the work, read the web and
  the machine, and reach sub-agents and the person.
- `tofu docs tools-context`: `context`, `budget`, `forget`, `note`,
  `digest`, `done`, `claims` and the review agent, which let the model
  look after its own window and check its own work.
- `tofu docs turn-history`: `changes`, `trail`, `bisect`, `checkpoint` and
  `whatif`, which say what each turn changed, find the turn that broke a
  check, and put back one turn's edits while keeping the rest.

What the model reads is what it pays for, so every result is shaped before
the model sees it: keys are redacted, a `bash` result is sifted when a
classifier is set up, and a result over 32 KB is stored whole, the model
getting a handle and the first and last part to read further with
`artifact_fetch`.

## Where it lives

Every call runs in the working directory and is recorded in the session,
under `~/.tofu/projects/<project>/`. Walking tools skip `node_modules`,
`.git`, `.tofu` and what `.gitignore` excludes.

## Change it

You don't call tools; you change what the model gets:

- `readBeforeEdit`, on by default, refuses `write` and `edit` on a file
  the session hasn't read.
- `browser`, `drive`, `read` or `off`, decides the browser tools.
- An agent file's `tools:` line limits a sub-agent to the tools it names.

    tofu settings set readBeforeEdit false

## Check it

    tofu tool list --dir .
    tofu agents

The first names every tool a run offers the lead; the second prints each
sub-agent's tools on a line of its own. `tofu docs tool` says how `tofu
tool` itself works.

## Undo it

    tofu settings set readBeforeEdit true

`/undo` puts back the files the last turn changed, whichever
tool changed them.
