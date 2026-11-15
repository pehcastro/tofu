---
topic: generate-map
title: The project map
summary: how /generate-map drafts MAP.md from the tree or shows what changed in it, and the rules that keep the layout and the instruction file in step
verbs: run
---

## What it is

`MAP.md` at the root of a project says what each top-level folder holds and
what it never holds, where docs live and where a new thing goes. Agents read
it before they place a file, so a new folder is planned rather than born from
whatever path the work happened to need.

`/generate-map` in the app starts a turn that reads the real tree and drafts
the map: one row for every top-level folder, a table of where new things go,
the docs folder, the folders not in git, and drift, such as a cache kept in
the tree or the same files copied twice. When `MAP.md` already exists it
shows the changes against it instead of a fresh draft. Nothing is written
until you say yes.

After the map, it offers the matching change to the instruction file: the
`AGENTS.md` or `CLAUDE.md` the project already has, or a short `AGENTS.md`
when it has neither, pointing at `MAP.md` and the docs rather than copying
them.

## The rules behind it

Three rules reach the lead, from `library/general/rules/`:

- `layout_map`: in a project with no `MAP.md`, the lead offers
  `/generate-map` once, on the first work that adds or moves files.
- `layout_plan`: a new top-level folder gets its `MAP.md` row in the same
  change; tool output goes to scratch or an ignored path; a file exists once;
  folders group by the thing, not by role or variant; docs go in `docs/`
  unless `MAP.md` names another place.
- `instruction_file`: the lead edits the instruction file that exists, never
  creates a second one beside it, and creates `AGENTS.md` only when there is
  none.

## Where it lives

- The map is `MAP.md` at the project root. `/generate-map` writes it, and
  the instruction file edit it offers, only after your yes.
- The steps the turn follows are the `generate-map` skill, in
  `library/dev/skills/generate-map.md`.
- The three rules are `layout_map`, `layout_plan` and `instruction_file`.
  `layout_map` turns off by itself once `MAP.md` exists at the root.

## Change it

Type `/generate-map` in the composer, read the draft, and answer yes to
write it, or say what to change first.

Turn a rule off for one project:

    tofu rules off layout_map --reason "this project keeps its map in the README"

## Check it

    tofu rules index "add a parser folder" --role orchestrator

lists each rule and whether it fires for that task.

## Undo it

Delete `MAP.md`, or put it back from git. Turn a rule on again with
`tofu rules restore layout_map`.
