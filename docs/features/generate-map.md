---
title: The project map
description: /generate-map drafts MAP.md from the real tree, one row per top-level folder, or shows what changed in the one you have, and writes nothing until you say yes.
order: 24
updated: 2026-11-15
---

`MAP.md` at the root of a project says what each top-level folder holds and
what it never holds. Agents read it before they place a file, so a new
folder is planned instead of appearing because some piece of work needed a
path.

## How it works

- `/generate-map` starts a turn that reads the tree: the top-level folders,
  their READMEs and files, who reads each one, and where the docs live.
- It drafts `MAP.md`: one row per top-level folder with Holds and Never
  holds, a table of where a new thing goes, the docs folder, and the folders
  not in git.
- It lists drift beside the draft: a cache or a build kept in the tree, the
  same files copied twice, a top-level folder only one other folder reads.
  It moves nothing.
- When `MAP.md` exists, it shows the changes against it instead.
- It writes nothing until you say yes. Then it offers the matching edit to
  the instruction file the project already has, `AGENTS.md` or `CLAUDE.md`,
  and a short `AGENTS.md` only when there is neither.

## The rules that keep it true

| Rule | What the lead does |
| --- | --- |
| `layout_map` | with no `MAP.md`, offers `/generate-map` once, on the first work that adds or moves files |
| `layout_plan` | gives a new top-level folder its `MAP.md` row in the same change, keeps tool output out of the tree, keeps one copy of a file, groups folders by the thing, and puts docs in `docs/` unless the map says otherwise |
| `instruction_file` | edits the `AGENTS.md` or `CLAUDE.md` that exists, never creates a second one beside it, and keeps it pointing at `MAP.md` and the docs |

## Commands

| Command | What it does |
| --- | --- |
| `/generate-map` | draft `MAP.md`, or the changes to it, and ask before writing |
| `tofu rules index "add a parser folder" --role orchestrator` | which of the rules fire for a task |
| `tofu rules off layout_map --reason "..."` | stop the offer in this project |
