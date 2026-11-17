---
topic: code-review
title: Reviewing a change
summary: how /code-review asks the lead to review one change for what it breaks now and later, inside and outside the diff, and report each finding with path:line, severity and the mechanism
verbs: run
---

## What it is

`/code-review [target]` in the app starts a turn that reviews one change: what
it breaks now, and what it will break later, inside the diff and in the code
outside it that still relies on the old behaviour, such as a caller reading
the old return shape. It changes no file.

The target is the working tree, the staged changes, a range such as
`HEAD~1..HEAD`, or a branch against its merge-base. With none, it is the
branch against its upstream merge-base, or the working tree against HEAD when
there is no upstream.

## Why it works this way

The lead gets the review as a plain request, with its usual tools and no
pipeline. On three sealed cases a plain request matched or beat a staged
pipeline of reviewer and verifier sub-agents, and was up to 4.6 times faster.
The turn spawns no sub-agent.

## What you get

- the conclusion first
- each finding with its path:line, its severity, and the mechanism by which
  it breaks

## Where it lives

The request is the `code-review` skill, in
`library/dev/skills/code-review.md`. A skill named `code-review` in the
project's `.tofu/skills/` is used instead.

## Change it

Type `/code-review` in the composer, with a target or without, or run it
headless:

    tofu run --dir . --command 'code-review HEAD~1..HEAD'

## Check it

    git status

shows the tree unchanged after the review.

## Undo it

A review changes no file, so there is nothing of it to undo: the tree after
it is the tree before it, and the findings stay in the chat as a reply.

A fix you ask for after reading the findings is a turn of its own, and
`/undo` puts back every file that turn changed; see `tofu docs commands`.

    /undo
