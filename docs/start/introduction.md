---
title: tofu
description: A coding harness for the terminal, with the workflow built in, that runs on your subscriptions and your own keys.
order: 1
updated: 2026-10-04
---

tofu is a coding harness for your terminal, one Go binary. You open it in a
project and talk to one model, the lead. The lead plans, hands the writing to
sub-agents that work in the background, checks what they return, and answers
you. Models run on the subscriptions you sign in with, Claude and Codex, or
on keys you bring from enabled providers.

![The tofu start screen: the chat tab, the composer, and the model in the status bar](./media/tui-start.png)

## The workflow comes built in

Claude Code and Codex are general-purpose agents that can code. They are made
for you to tweak: you add skills, change the obvious things, and work out
your own workflow, such as what the lead should delegate and what a sub-agent
must prove before it is done. tofu is specialized for coding and brings that
workflow by default:

- **Orchestrator and sub-agent rules.** The lead plans and checks; sub-agents
  write, each in the paths it owns. Five sub-agents running at once took a
  task's first turn to 4 min 58 s, where it took 21 min 18 s when none
  overlapped.
- **Language agents and their gate.** `go-dev`, `ts-dev`, `py-dev` and
  `rust-dev` are sent back until their language's checks ran after the last
  edit.
- **A library per language and framework.** With it, the same build passed
  Vue undo 4/4 against 0/4, a React shared URL 2/2 against 0/2, a Svelte
  keyed list 3/3 against 1/3, and a Rust deep-recursion test 3/3 against 1/6.
- **Warm checks.** Typecheck and test stay running between edits: a 33-file
  TypeScript rename went from 675 s to 94 s, and a first search from 42.8 s
  to 187 ms.
- **Less for the model to read.** Capped listings cut the tool output the
  model read by 88%, and a context fork took a session from 66k to 18k
  tokens.
- **Classifiers at the decision points**, where other harnesses use a prose
  rule or a guess from the big model, each kept only where it beats the free
  alternative. See [What a classifier is](/docs/classifiers/what-a-classifier-is).

## Getting started

[Install tofu](/docs/start/install), sign in as in [Setup](/docs/start/setup),
then type `tofu` in a project. `tofu --continue` reopens the session you last
worked in. `tofu help` lists every verb; the [Reference](/docs/cli/overview) groups them.
