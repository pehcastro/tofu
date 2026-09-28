---
topic: start
title: Start here
summary: what tofu is, the first commands to type, and where to read more
verbs: docs, doctor, login, models, settings
---

## What it is

tofu is a coding agent that runs in your terminal. You type a task, a model
plans and writes, and tofu runs the tools and asks you before anything risky.

These pages answer one question each. Type `tofu docs` to see every question
with the command that answers it, `tofu docs <topic>` for a page, or
`tofu docs "a few words"` to find the closest answers.

Topics:

- start: this page
- files: every file tofu reads and writes
- settings: every setting, its default, and how to change it

## Where it lives

tofu is one program. It keeps its own files in `~/.tofu` in your home
directory and in `.tofu` inside a project. `tofu docs files` lists them all.

## Change it

Open the app in a project:

    cd my-project
    tofu

Continue the session you last worked in:

    tofu --continue

Sign in with a subscription the first time, so tofu has a model to use:

    tofu login claude-sub
    tofu login codex-sub

Work a task without the app, printing as it goes:

    tofu run --dir . "fix the failing test"

## Check it

    tofu doctor

says whether tofu can run here and what is wrong if it cannot.

    tofu models

lists the models your subscriptions serve.

    tofu version

prints the version you are running, and `tofu changelog` what changed.

## Undo it

Nothing on this page changes a file except `tofu login`, which stores a
credential in `~/.tofu`. `tofu login --status` lists what is stored, and
`tofu login --disable <number>` sets one aside.
