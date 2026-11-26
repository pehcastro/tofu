---
topic: skills
title: Skills
summary: where tofu finds skills, how to add one for every project or for one, how a skill loads on your own words, and how to turn skills off
verbs: run, settings
---

## What it is

A skill is a folder holding a `SKILL.md`: instructions for one kind of job, like writing a release note or reviewing a migration, with any files it needs beside them. The model sees each skill's name and description, and loads the whole skill only when the task matches it, so a skill costs little until it is used.

tofu reads the same skill folders Claude Code and other agents use, so a skill you already have works here too.

## Where it lives

A skill is `<folder>/<name>/SKILL.md`, read from these places, in order:

- `.tofu/skills/` in the working directory, and in every folder above it up to the git root; outside a git repository, the working directory alone
- `.agents/skills/` in the same folders
- `.claude/skills/` in the same folders
- `~/.tofu/skills/`: your own skills, in every project

From your home, only `~/.tofu/skills` is read: not `~/.claude/skills`, not `~/.agents/skills`. When two skills share a name, the first one found is offered and the other is skipped with a notice.

tofu ships skills of its own, `debug`, `hunt`, `code-review`, `simplify`, `generate-map`, `restructure`, `flake-triage` and `test-plan`, and lists them beside yours for the lead and for every library sub-agent, each of which carries the skill tool. A skill of yours with the same name is offered instead of the shipped one. The lead also lists the library's general references, such as `contract`, and reads one whole with the `reference` tool, as a library sub-agent reads its own. An agent that loads a skill, by its definition or by the model's choice, can open that skill's references with the same tool, and a sub-agent is offered the references its stack reaches.

`SKILL.md` opens with front matter:

    ---
    name: release-note
    description: writes the release note for a version from the changelog
    ---

The name defaults to the folder's name. A skill with no description is not offered, and tofu says so. `hide: true` or `disable-model-invocation: true` keeps a skill out of the list the model reads, while a sub-agent can still load it by name.

A sub-agent file can name skills in its own front matter, as `skills: release-note, changelog`, and those are loaded into that sub-agent's prompt when it starts. A sub-agent without the skill tool is told which skills were loaded for it, and a brief that names one it cannot load gets a note to work without it.

## Load a skill when you say its words

Right below the description, `trigger` and `keywords` make a skill load by itself when you say one of its phrases:

    ---
    name: socratic-tutor
    description: teaches a topic by asking questions
    trigger: auto
    keywords: teach me, tutor me, quiz me
    ---

`trigger` is `auto` or `manual`, in any case: `auto`, `Auto` and `AUTO` are the same. With no `trigger` a skill is `manual` and loads only through the skill tool or a sub-agent's `skills` list. `keywords` is a comma separated list of phrases and is ignored unless `trigger` is `auto`. Any other `trigger` value keeps the skill out of the list, and tofu names the file and the allowed values once per session, not at every message.

A phrase matches in any case, as whole words, across any run of spaces between its words: `teach me` fires on "can you teach me goroutines" and on "TEACH   ME", and not on "teach meaning". It is read only in a message you write to the lead, or in a message the lead sends to a running sub-agent. A sub-agent's brief, a report and the model's own words never fire it.

When a phrase matches, tofu puts the skill's body in the conversation once, and the chat shows one row:

    · skill socratic-tutor loaded: you said "teach me"

While the body is still in the conversation the skill is active, and a later match adds nothing. Once compaction drops the body, the skill is no longer active and the next match loads it again. A skill the model loaded with the skill tool counts as active too.

Turn an active skill off by typing, to the lead:

    /skill off socratic-tutor

tofu tells the model to stop following it, and it stays off until you say one of its phrases again. A skill that is not active is left as it is, and tofu says the command changed nothing.

Every load, every match skipped because the skill is active, and every off writes a row to the session trace, with who said what:

    tofu session trace <session> --kind notice --grep skill

A skill that never matches adds nothing to a request.

## Change it

Add a skill for every project: make the folder and write its `SKILL.md`.

    ~/.tofu/skills/release-note/SKILL.md

Add one to a single project the same way, under `.tofu/skills/` in the project.

Turn skills off, so the model is neither shown the list nor offered the skill tool:

    tofu settings set skills off

tofu looks for skills again at the start of every task, so a skill you add, change or delete is seen by the next task without a restart.

## Check it

    tofu run --dir . --show-prompt "anything"

prints the prompt the next task would send, part by part, and sends nothing. The `skills` part lists every skill offered, and a skill left out is named on a `tofu:` line with the reason.

    tofu settings get skills

prints `on` or `off`. The host's `query.skills` answers with `skills`, every skill offered, `active`, each skill active in the lead's conversation with what loaded it and when, and `warnings`, each skill left out and why.

## Undo it

Delete the skill's folder. Turn skills back on with:

    tofu settings set skills on

A skill in a project's `.claude/skills` belongs to that project. Deleting it changes Claude Code too, so move it out of the way instead when you only want tofu to stop offering it.
