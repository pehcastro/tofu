---
topic: restructure
title: Restructuring a project
summary: how /restructure reads what a project is, designs its tree from that alone, checks it against the real repository, and proposes one clean target tree with release and deployment, moving nothing
verbs: run
---

## What it is

`/restructure` in the app starts a turn that proposes a structure for the
project: one target tree in which every feature and module has its own place
under a plain name, dependencies run one way, and the path from source to a
released, deployed artifact reads straight off the tree.

The turn moves no file. It writes its proposal in the reply, you reshape it
in your own words, and on your yes it plans the migration rather than doing
it.

## Why it runs in this order

Today's folders pull a design toward themselves. So the turn:

1. Writes a product spec before it lists the tree: what the product is, every
   part a person receives or relies on, who uses each part, what is planned,
   and the stack. Research sub-agents read the README, docs, specs, manifests
   and ignored notes, and the spec names no folder and no path.
2. Hands only that spec to a fresh sub-agent, which designs the tree without
   reading any file. Its tree is the starting design.
3. Reads the real repository: the tracked tree, what it produces, how it is
   released and deployed, who uses each part, and the owners' conventions.
   Every claim carries the line or the command behind it.
4. Chooses the architecture, names the pattern it follows and why it fits the
   project's purpose, size and ecosystem, and says for every part of today's
   tree why it moves or why it already sits in the best place.

## What you get

The reply holds, in text:

- the features and modules table: name, what it is, where it is now, the
  problem
- the architecture in two or three sentences, with its trade-off
- one target tree, one line per folder saying what it holds and why
- the import rules, and where a new feature, module, test or doc goes
- release and deployment for that tree
- the move table, with every dependent as `file:line` and the count of files
  found, and the order of steps that each leave the build and tests passing
- the directions it could not settle, each with a recommendation

It never asks through a picker: a structure is read and reshaped, not picked
from a list.

## On yes

A yes approves the target, not a start on moving files. The turn plans the
migration: every file that moves and every file that changes because of it,
whether the stack allows each move, slices in an order that keeps the build
green, where to be careful and where to look. With a board it writes one epic
and one ticket per slice; with none it writes one markdown plan where the
project keeps plans.

## Where it lives

The steps the turn follows are the `restructure` skill, in
`library/dev/skills/restructure.md`. A skill named `restructure` in the
project's `.tofu/skills/` is used instead.

## Change it

Type `/restructure` in the composer, or run it headless:

    tofu run --dir . --command restructure

## Check it

    git status

shows the tree unchanged after the proposal.
