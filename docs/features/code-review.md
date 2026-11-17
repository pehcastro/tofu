---
title: Reviewing a change
description: /code-review reviews one change for what it breaks now and what it will break later, inside the diff and in the code outside it, and reports each finding with path:line, severity and the mechanism.
order: 26
updated: 2026-11-17
---

`/code-review` reviews one change: a commit range, a branch, the staged
changes or the working tree. It looks for what the change breaks now and for
what it will break later, inside the diff and in the code outside it that
still relies on the old behaviour, such as a caller reading the old return
shape. It changes no file.

## How it works

The lead gets the review as a plain request and answers it with its usual
tools. It spawns no sub-agent. On three sealed cases this matched or beat a
staged pipeline of reviewer and verifier sub-agents, and was up to 4.6 times
faster.

With no target, the review covers the branch against its upstream
merge-base, or the working tree against HEAD when there is no upstream.

## What you get

| Part | What it holds |
| --- | --- |
| Conclusion | what the change breaks, first |
| Findings | path:line, severity, and the mechanism by which it breaks |

## Commands

| Command | What it does |
| --- | --- |
| `/code-review` | review the branch against its upstream merge-base, else the working tree |
| `/code-review HEAD~1..HEAD` | review one range |
| `tofu run --dir . --command 'code-review HEAD~1..HEAD'` | the same turn, headless |

## Undo

A review changes no file, so there is nothing of it to undo: `git status`
shows the tree as it was, and the findings stay in the chat. A fix you ask
for afterwards is a turn of its own, and `/undo` puts back every file that
turn changed.
