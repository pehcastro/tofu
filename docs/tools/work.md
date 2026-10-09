---
title: Work
description: plan, settings, artifact_fetch, quote, ask_person, zoom, recall, skill and the tofu verbs, tools that organize the work rather than change files.
order: 6
updated: 2026-10-09
---

Tools that change no project file. All are tofu's own Go code, except the
`tofu_*` verbs, which run the tofu binary itself.

## plan

`plan` is an ordered list held in memory and shown in the chat. `set`
writes it once, then `start`, `done` and `drop` move one item, named by its
own words.

You see what the model intends before it acts, and one item runs at
a time, so the list can't drift from the work. Parameters: `op`, `items`,
`item`.

## settings

`settings` is a request to you to raise `subAgentsPerTurn` (10) or
`subAgentDepth` (2), and nothing else.

A limit you set shouldn't move without your yes. On yes, your global
settings file changes and the next spawn reads it. Parameters: `key`, `value`.

## artifact_fetch

`artifact_fetch` reads a byte range of a result tofu stored whole.

A result over 32 KB isn't cut and lost: the model sees the first and
last part and a handle, and reads the middle only if it needs it.
Parameters: `handle`, `offset`, `length`.

## quote

`quote` returns the words of an earlier message you referenced as
`[message#9c2d40]`, the id the chat shows beside it, with the names of the
tools it ran, never their output, capped at 32 KB. It looks through every
session of the chain, so a message from before a fork or a `--continue`
is found. An id it cannot find is an error, never the nearest message.

A paraphrase of what was said is a new claim. A quote is a citation.
Parameter: `id`.

## ask_person

`ask_person` puts one to four questions to you, each with up to four
options, the one the lead recommends, and room for your own words. In auto
the lead keeps working while you decide, and the recommended option stands
after `askPersonWaitSeconds`. It exists only in a turn you started, never
in `tofu run`, a cron turn or a sub-agent. See [Asking
you](/docs/features/asking). Parameter: `questions`.

## zoom and recall

`zoom` opens one line of a memory view into the two lines it sums up, down
to the original message, word for word. `recall` searches the raw items of
a store with a regular expression. The store is `episodes` for the
conversation, or one of the four memory scopes.

A summary line is a pointer, not the thing, so the lead zooms until it has
the part it needs whole before it acts on it. See
[Memory](/docs/features/memory). Parameters: `store`, `id`, `n` for `zoom`;
`regex` and an optional `store` for `recall`.

## skill

`skill` loads a listed skill's `SKILL.md`, and any file in its
folder. Registered only when a skill exists.

The system prompt carries each skill's name and description, and the
body loads only when the task needs it. With the list, the model loaded the
right skill in 2 of 3 tasks against 0 of 3 without, for 247 cached tokens a
request. Parameters: `name`, `path`.

## tofu verbs

The verbs are six tools that run a tofu command in the working directory
and return its output and exit code, killed after 120 s.

| Tool | Runs |
|---|---|
| `tofu_docs` | `tofu docs [topic]` |
| `tofu_why` | `tofu why <id>` |
| `tofu_replay` | `tofu replay --point <point>` |
| `tofu_judge` | `tofu judge` |
| `tofu_lint_comments` | `tofu lint comments [path]` |
| `tofu_rules_check` | `tofu rules check [path]` |

The model asks tofu about itself the way you would, instead of
guessing: `tofu_docs` before changing tofu's settings or agents, `tofu_why`
for the recorded reason behind a gate decision.

## Your part

The model calls these tools. You answer `settings` requests with yes or no,
and reference an earlier message with its `[message#...]` id.

The verbs are the same commands you run:

```bash
tofu docs
```

It opens with `Docs`, the number of asks and topics, then each topic with the
commands that answer it.
