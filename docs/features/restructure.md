---
title: Restructuring a project
description: /restructure reads what a project really is, designs its tree from that alone, checks it against the real repository, and proposes one clean target tree with release and deployment, in text, moving nothing.
order: 25
updated: 2026-11-16
---

A repository's structure is an architectural decision. `/restructure`
proposes one: every feature and module in its own place under a plain name,
dependencies running one way, and a straight path from source to a released,
deployed artifact. It moves no file.

## How it works

- It writes a product spec first, before it lists the tree: what the product
  is, every part a person relies on, who uses each part, what is planned, and
  the stack. The spec names no folder, so today's layout does not steer the
  design.
- A fresh sub-agent gets only that spec and designs the tree without reading
  any file. That tree is the starting design.
- It then reads the real repository: the tracked tree, what it builds, how it
  is released and deployed, who uses each part, and the owners' conventions,
  with the line or command behind every claim.
- It chooses the architecture, names the pattern, and says for every part of
  today's tree why it moves or why it already sits in the best place.

## What you get

| Part | What it holds |
| --- | --- |
| Modules table | each feature and module, where it is now, and the problem |
| Architecture | the pattern chosen and its trade-off |
| Target tree | one line per folder: what it holds and why it sits there |
| Rules | which modules may import which, and where new things go |
| Release | the version, changelog, builds, CI and packaging for that tree |
| Moves | from, to, every dependent as `file:line`, and steps that keep the build green |
| Open directions | what it could not settle, each with a recommendation |

You reshape the tree in your own words and it shows it again, until you say
yes. A yes plans the migration: an epic with one ticket per slice on a board,
or one markdown plan when there is no board. It still moves nothing.

## Commands

| Command | What it does |
| --- | --- |
| `/restructure` | propose a target tree for the project, in text |
| `tofu run --dir . --command restructure` | the same turn, headless |
