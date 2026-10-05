---
topic: start
title: Start here
summary: what tofu is, the first commands to type, and where to read more
verbs: docs, doctor, login, models, settings
---

## What it is

tofu is a coding agent that runs in your terminal. You type a task, a model
plans and writes, and tofu runs the tools, with jev judging each call first.

These pages answer one question each. Type `tofu docs` to see every question
with the command that answers it, `tofu docs <topic>` for a page, or
`tofu docs "a few words"` to find the closest answers.

Topics:

- start: this page
- files: every file tofu reads and writes
- settings: every setting, its default, and how to change it
- rules: add your own rules, switch one off, and see which rules run
- agents: add your own sub-agents, choose the model each one runs, and remove them
- models: sign in, name a model, and choose the model a task runs
- skills: where skills are found, and how to add or turn them off
- instructions: which AGENTS.md and CLAUDE.md files are sent with every task
- gate: what jev judges before a tool call, and the OpenRouter key
- sessions: continue a session, find an old one, and running shells
- commands: every slash command in the app, and /compact
- browser: share a Chrome tab so tofu can read it or act in it
- doctor: what tofu doctor prints, and what to do about each line

## Where it lives

tofu is one program. It keeps its own files in `~/.tofu` in your home
directory and in `.tofu` inside a project. `tofu docs files` lists them all.

## Change it

Open the app in a project:

    cd my-project
    tofu

Continue the session you last worked in:

    tofu --continue

The first time, the app opens on a setup screen with two steps, and the
chat opens once both are done:

1. language model: press 1 for the Claude subscription or 2 for the Codex
   one, which sign in through the browser, or 3 to paste a Meta API key.
2. classifier, jev, required: press 1 for an OpenRouter key or 2 for a
   TypeSafe key. jev judges every tool call, so a model alone is not
   enough.

A key field shows the key's shape while you paste it, such as
`sk-or-v1-…3f9a  73 chars  prefix ok`, never the key. Enter checks it
with its provider and stores it; a refused key stays in the field with
the reason, and esc goes back. A step already set, by a login, the
environment or the project's `.env`, shows as done. Esc or q leaves.
The same two steps from a terminal:

    tofu login llm claude-sub
    tofu login classifier openrouter

Work a task without the app, printing as it goes:

    tofu run --dir . "fix the failing test"

## Check it

    tofu doctor

says whether tofu can run here and what is wrong if it cannot.

    tofu models

lists the models your subscriptions serve.

    tofu version

prints the version you are running, and `tofu changelog` what changed.
Every verb that reports state takes `--json` and prints one JSON
document, `{tofu, verb, ok, at, data, problems}`, for a script to read.

## Undo it

Nothing on this page changes a file except `tofu login`, which stores a
credential in `~/.tofu`. `tofu login --status` lists what is stored, and
`tofu login --disable <number>` sets one aside.
