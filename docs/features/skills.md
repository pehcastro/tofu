---
title: Skills
description: A skill loads when you say one of its phrases, once while it is active, and stays out of every request until then.
order: 27
updated: 2026-11-25
---

A skill is a folder holding a `SKILL.md`: instructions for one kind of job.
tofu reads the same skill folders Claude Code uses, `.tofu/skills`,
`.agents/skills` and `.claude/skills` in the project and `~/.tofu/skills` in
your home. The model sees each skill's name and description and loads the
whole skill when the task matches it.

A skill can also load on your own words, so you never have to remember to ask
for it.

## Why it works the way it does

**Your words, not a guess.** A skill with `trigger: auto` loads when a message
you write holds one of its `keywords`, matched in any case, as whole words,
across any run of spaces between them. No model decides it, so "teach me" and
"TEACH   ME" fire and "teach meaning" does not.

**Once while it is active.** The body goes into the conversation once. A later
match while it is still there adds nothing, so a long session does not carry
the same skill twice. When compaction drops the body, the next match loads it
again.

**Free until it fires.** A skill that never matches adds 0 bytes to a request:
the same three turns sent 113,729, 114,518 and 115,298 bytes with the skill set
to `auto` and to `manual`.

**You stay in charge.** `/skill off <name>` turns an active skill off and tells
the model to stop following it. A sub-agent loads one only from a message the
lead sends it, never from its brief.

## How to

Add `trigger` and `keywords` right below the description:

    ---
    name: socratic-tutor
    description: teaches a topic by asking questions
    trigger: auto
    keywords: teach me, tutor me, quiz me
    ---

Type a message that holds a phrase. The chat shows:

    · skill socratic-tutor loaded: you said "teach me"

Turn it off:

    /skill off socratic-tutor

See every load, skip and off, with who said what:

    tofu session trace <session> --kind notice --grep skill

## Reference

| Field | Values | Default |
|---|---|---|
| `trigger` | `auto`, `manual`, in any case | `manual` |
| `keywords` | comma separated phrases, read only when `trigger` is `auto` | none |

A `manual` skill loads only through the skill tool or a sub-agent's `skills`
list. Any other `trigger` value keeps the skill out of the list, and tofu
names the file and the allowed values once per session.

`/skill off` works in the lead's chat, and on a skill that is not active it
changes nothing and says so.

The host's `query.skills` answers with `skills`, every skill offered,
`active`, each skill active in the lead's conversation with what loaded it and
when, and `warnings`, each skill left out and why.
