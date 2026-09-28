---
topic: skills
title: Skills
summary: where tofu finds skills, how to add one for every project or for one, and how to turn skills off
verbs: run, settings
---

## What it is

A skill is a folder holding a `SKILL.md`: instructions for one kind of
job, like writing a release note or reviewing a migration, with any files
it needs beside them. The model sees each skill's name and description,
and loads the whole skill only when the task matches it, so a skill costs
little until it is used.

tofu reads the same skill folders Claude Code and other agents use, so a
skill you already have works here too.

## Where it lives

A skill is `<folder>/<name>/SKILL.md`, read from these places, in order:

- `.tofu/skills/` in the working directory, and in every folder above it
  up to the git root; outside a git repository, the working directory alone
- `.agents/skills/` in the same folders
- `.claude/skills/` in the same folders
- `~/.tofu/skills/`: your own skills, in every project

From your home, only `~/.tofu/skills` is read: not `~/.claude/skills`,
not `~/.agents/skills`. When two skills share a name, the first one found
is offered and the other is skipped with a notice.

`SKILL.md` opens with front matter:

    ---
    name: release-note
    description: writes the release note for a version from the changelog
    ---

The name defaults to the folder's name. A skill with no description is
not offered, and tofu says so. `hide: true` or
`disable-model-invocation: true` keeps a skill out of the list the model
reads, while a sub-agent can still load it by name.

A sub-agent file can name skills in its own front matter, as
`skills: release-note, changelog`, and those are loaded into that
sub-agent's prompt when it starts.

## Change it

Add a skill for every project: make the folder and write its `SKILL.md`.

    ~/.tofu/skills/release-note/SKILL.md

Add one to a single project the same way, under `.tofu/skills/` in the
project.

Turn skills off, so the model is neither shown the list nor offered the
skill tool:

    tofu settings set skills off

tofu looks for skills again at the start of every task, so a skill you
add, change or delete is seen by the next task without a restart.

## Check it

    tofu run --dir . --show-prompt "anything"

prints the prompt the next task would send, part by part, and sends
nothing. The `skills` part lists every skill offered, and a skill left
out is named on a `tofu:` line with the reason.

    tofu settings get skills

prints `on` or `off`.

## Undo it

Delete the skill's folder. Turn skills back on with:

    tofu settings set skills on

A skill in a project's `.claude/skills` belongs to that project. Deleting
it changes Claude Code too, so move it out of the way instead when you
only want tofu to stop offering it.
