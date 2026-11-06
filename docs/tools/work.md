---
title: Work
description: plan, settings, artifact_fetch, history, ask_person, skill and the tofu verbs, tools that organize the work rather than change files.
order: 6
updated: 2026-11-06
---

Tools that change no project file. All are tofu's own Go code, except the
`tofu_*` verbs, which run the tofu binary itself.

## plan

`plan` is an ordered list held in memory and shown in the chat. `set`
writes it once and starts the first item. `done` finishes the running item
and starts the next, so a plan worked in order takes `set` and `done` only.
`start` takes an item out of order and `drop` abandons one, each named by
its own words.

You see what the model intends before it acts, and one item runs at
a time, so the list can't drift from the work. The model writes a plan only
for a task of several steps across more than one file, and sends a move
beside its next real tool call rather than in a request of its own.
Parameters: `op`, `items`, `item`, `call`.

An item may carry a `check`: a command or an acceptance line that proves
it. The plan shows the check under its item:

```bash
tofu tool plan '{"op":"set","items":[{"text":"fix the discount","check":"go test -run TestDiscounted ."}]}' --dir .
```

```text
plan, 1 items
  running  fix the discount
    check  go test -run TestDiscounted .
```

`done` on an item with a check needs `call`, the id of the earlier tool call
in the session that ran the check. Without one, `done` is refused and says
to run the check and send `done` again with that id. An id no earlier call
has is refused too, so an item is finished only by a check that ran. A plan
without checks works as before.

Once the lead holds a plan, every later request carries it as it stands, as
the last message, after the part the prompt cache reads. The plan is never
kept in the conversation, so the cache reads exactly what it would without
it. A sub-agent keeps a plan of its own and sees that one before each of its
steps, never the lead's. `tofu settings set planEveryStep false` stops that,
and each agent then sees its plan only in the `plan` tool's own answer.

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

## history

`history` answers from the record of the work. One tool with a `mode`
replaces four close ones, so the model picks the mode rather than guessing
between tools. Each mode takes one `anchor`:

| Mode | Anchor | What it returns |
|---|---|---|
| `quote` | a reference you wrote, such as `[quote#3f2a]`, or the short id inside it | that recorded turn or tool call, word for word |
| `zoom` | a memory view line's `id+n`, with `store` | the two lines it sums up, down to the original item |
| `recall` | a regular expression, with an optional `store` | every raw item that matches, word for word |
| `lookup` | a call id a fork's carry names | that call and its result whole, from any earlier session of the line |

`quote` and `lookup` look through every session of the chain, so an item
from before a fork or a `--continue` is found. An id that matches no item,
or two, is an error, never the nearest item. A paraphrase of what was said
is a new claim. A quote is a citation.

A summary line is a pointer, not the thing, so the lead zooms until it has
the part it needs whole before it acts on it. `zoom` and `recall` are
offered while `episodes` is on. See [Memory](/docs/features/memory).
Parameters: `mode`, `anchor`, `store`.

## ask_person

`ask_person` puts one to four questions to you, each with up to four
options, the one the lead recommends, and room for your own words. In auto
the lead keeps working while you decide, and the recommended option stands
after `askPersonWaitSeconds`. It exists only in a turn you started, never
in `tofu run`, a cron turn or a sub-agent. See [Asking
you](/docs/features/asking). Parameter: `questions`.

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
and reference an earlier message with its `[quote#...]` id.

The verbs are the same commands you run:

```bash
tofu docs
```

It opens with `Docs`, the number of asks and topics, then each topic with the
commands that answer it.
