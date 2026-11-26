---
title: Work
description: plan, settings, artifact_fetch, history, ask_person, heads_up, skill and the tofu verbs, tools that organize the work rather than change files.
order: 6
updated: 2026-11-25
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
| `recall` | a regular expression, with an optional `store`, or a reference such as `[tool#abc12]` | every raw item that matches, word for word, or that one call |
| `lookup` | a call id a fork's carry names, with an optional `session` | that call and its result whole, from that session, this one, or any earlier session of the line |

`quote` and `lookup` look through every session of the chain, so an item
from before a fork or a `--continue` is found. An id that matches no item,
or two, is an error, never the nearest item. A paraphrase of what was said
is a new claim. A quote is a citation.

A summary line is a pointer, not the thing, so the lead zooms until it has
the part it needs whole before it acts on it. `zoom` and `recall` are
offered while `episodes` is on. See [Memory](/docs/features/memory).
Parameters: `mode`, `anchor`, `store`, `session`.

A call by an old name, `quote`, `recall`, `zoom` or `lookup`, is refused
with the call to make instead:

```text
tofu tool: quote is a mode of history now: call history with mode quote and what you asked for as its anchor
```

`history` in mode `lookup` also brings back whole a result the model
dropped with `forget`. See [The agent's own context](/docs/tools/own-context).

## ask_person

`ask_person` puts one to four questions to you, each with up to four
options, the one the lead recommends, and room for your own words. In auto
the lead keeps working while you decide. With no answer in
`askPersonWaitSeconds` it is told none came and picks nothing, and the form
stays open for a later answer. It exists only in a turn you started, never
in `tofu run`, a cron turn or a sub-agent. See [Asking
you](/docs/features/asking).

### Options you can look at

An option may carry a `preview`: what taking it looks like, as code, a
unified diff or a text screen. When the options of a question carry one,
the form shows them side by side, each option over its own preview, so you
compare two designs rather than two labels. A diff preview is colored as a
diff, and a preview shows up to 14 rows. On a screen too narrow for columns
of 24 cells, the options stack and the focused one shows its preview under
it.

What the form showed at 110 columns, with two options that each carry a
diff:

```text
  ? Discount fix  1 of 1
  answer whenever: the lead keeps working, and in 4m 59s hears you have not answered yet
  the full question is in the chat above
  › 1. Round at the end (recommended)                 │   2. Round each line
    - return sum * (100 - rate) / 100                 │   - total += line * (100 - rate) / 100
    + return round(sum * (100 - rate), 100)           │   + total += round(line * (100 - rate), 100)
    3. other  type it in the chat, then enter
    4. return  close the question without choosing anything
  a note typed in the chat goes with the option enter chooses
  ↑↓ move   enter choose   tab next question
```

The form is the short selector: its header, the numbered labels, other,
return and any previews. Its header counts the questions, and the asks
still waiting behind it, such as `1 of 2 · 1 more ask waits`. The question
and its context are in the chat row above it, so nothing is shown twice;
an option's description is not drawn. A long line wraps, it is never cut,
and a form taller than its share of the screen scrolls with the focused
option and says which rows it shows, such as `rows 2-15 of 24`.
**esc** does nothing on the form, so a stray key never answers for you. To
close the question without choosing, pick **return**: nothing is chosen,
the lead is told so, and the question stays open.

While the question is open, its chat row holds the header, the question, a
**Question context** card when the lead gave a `context`, and `waiting for
your answer in the form below`:

```text
? Stale lock
Which way should Resolve treat a stale lock?
waiting for your answer in the form below
```

Once you answer, the row folds to one line, such as `? Discount fix: you
chose Round at the end`, or `? Discount fix: you closed it: nothing chosen,
and the question stays open` when you picked return. A reopened session
shows the same row.

### A note with your choice

On a question whose options carry a preview, type a line in the chat and
press **enter**: the focused option is chosen and your line goes with it as
a note. You pick a design and say what to change about it in one answer.
The chat row shows both on its one line:

```text
? Discount fix: you chose Round at the end, and wrote: keep it in cents
```

and the lead reads the note beside the choice:

```text
the answer to your ask_person question: {"outcome":"submitted","answers":[{"id":"fix","status":"answered","chosen":["Round at the end"],"note":"keep it in cents"}],"next":"on any outcome but submitted, take each recommended option, say which you took, and continue. on submitted, follow what was chosen; a skipped question is yours to decide."}
```

### Parameters

`questions` holds 1 to 4 questions. Each question takes:

| Field | What it does |
|---|---|
| `id` | names the question in the answer |
| `header` | 1 to 4 words, the form's title |
| `question` | one sentence, at most 200 characters |
| `context` | up to 1,500 characters, drawn as the Question context card in the chat |
| `type` | `choice`, `multi`, `text` or `yesno` |
| `options` | 2 to 4, for `choice` and `multi`; each has a `label` of 1 to 5 words, a one-line `description` of at most 200 characters and an optional `preview` |
| `recommended` | the index of the option the lead would take, for every type but `text` |
| `reversible` | `true` only when the recommended option can be undone later at little cost |
| `needs_yes` | `true` when nothing it asks may happen without your own answer: it is never taken in auto, by the ask judge, by a timeout or by a dismissal |

While you are away, no form opens: the call answers `person_away` and the
recommended option stands. A `needs_yes` question takes nothing: the lead is
told to do nothing it asks and to say it waits on you.

A question, a description or a context over its cap is refused back to the
lead, naming the question and the cap. tofu adds the **other** row to every
choice, so an option never says "other". Run by hand, `tofu tool ask_person` is refused, because nobody is
there to answer:

```text
error: ask_person refused: nobody is here to answer this turn: tofu run has no person and a cron job runs unattended, so take the recommended option where it can be undone, or end the turn on the question. it stays listed so the tool list matches the one this session's cache was written with
```

## heads_up

`heads_up` puts one line on your screen while the lead keeps working. It
never waits for an answer and never stops the turn.

The lead tells you a thing worth knowing now, such as a slow step starting,
a risk it found, or a result worth seeing before the turn ends, without
turning it into a question you have to answer. To ask and wait, the lead
uses `ask_person`.

```bash
tofu tool heads_up '{"level":"warn","line":"order_test.go fails before any change"}' --dir .
```

```text
tofu: heads up, warning: order_test.go fails before any change
shown to the person; the turn goes on
```

The first line is what you see; the second is what the lead reads. In the
chat the line sits in the lead's turn:

```text
    · heads up, warning: order_test.go fails before any change
```

At `info` the line starts `heads up:`. A line with a line break, an empty
line or another level is refused and nothing is shown:

```text
error: heads_up: line holds more than one line, so nothing was shown; say it in one line
```

| Parameter | What it does |
|---|---|
| `level` | `info`, or `warn` for something that may need you |
| `line` | the one line to show |

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
