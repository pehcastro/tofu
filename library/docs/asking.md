---
topic: asking
title: When the lead asks you
summary: how the lead puts a decision to you with options and a recommended one, what auto and ask do with it, and when it cannot ask
---

## What it is

When only you can settle a short choice, such as which of two libraries to
use, a name, a matter of taste, spending money or something that cannot be
undone, the lead asks with the `ask_person` tool. The form is for choices
whose options fit in a few words and that the chat already explained. A big
or explanatory decision, a trade-off that needs paragraphs, or a plan to
review is never a form: the lead writes it in its reply as prose with
numbered options, and you answer in words. A decision is asked once, and the
lead does not repeat a form's question or options in the chat.

A call carries 1 to 4 questions. Each has:

- a header of 1 to 4 words, and one sentence of at most 200 characters
- an optional context of at most 1500 characters, in markdown: why it is
  asked and what each way leads to, shown under the question
- a type: `choice`, `multi`, `text` or `yesno`
- 2 to 4 options of 1 to 5 words, each with a description of at most 200
  characters and an optional preview, for `choice` and `multi`
- the option the lead recommends, for every type but `text`
- `reversible`, set only when the recommended option can be undone later
  at little cost

A question over these limits is refused back to the lead with the limit
named, never cut. It is never a request to run a tool: approval belongs to
the gate, and the two are kept apart. When a sub-agent reports its ticket
not possible, tofu puts the decision to you once, as the same form: the
ticket's reason and the recommended way with why as its context, and each
way to go as a short label with what it means beside it. The lead is told
the question is already with you and does not ask it again.

## Where it lives

In the chat the question is a form above the composer: each option with its
description, the recommended one marked, and a row, other, for an answer in
your own words. Up and down move, a digit or enter chooses, space toggles an
option of a `multi`, and tab goes to the next question. Esc does nothing on
the form; the last row, return, closes it with nothing chosen. On other,
type the answer in the chat and press enter. When the options carry a
preview, they are shown side by side, each option over its preview, so you
compare what each one looks like. On a screen too narrow for columns of 24
cells they stack, and the focused option's preview shows under it. On such a
question, type a line in the chat and press enter: the focused option is
chosen and the line goes with it as a note.

The answer goes back to the lead as the tool's result under `ask`, and as a
message under `auto`:

    {"outcome": "submitted", "answers": [
      {"id": "fix", "status": "answered", "chosen": ["Round at the end"],
       "note": "keep it in cents"}]}

- outcome: `submitted`; `cancelled`, you chose return, so nothing is chosen
  and the question stays open, or the turn stopped, and the recommended
  option stands; `timed_out`, no answer in time under auto, nothing chosen;
  `undelivered`, nobody could be asked; `taken_in_auto`, the question never
  reached you, see below; `person_away`, the session is away, so no form
  opened, see `tofu docs cron`
- status: `answered`; `skipped`, you chose nothing; `unanswered`
- a client of `tofu serve` sees it as a `tofu/askPerson` request; see
  `tofu docs serve`

On `undelivered`, `person_away`, or a turn that stopped, the lead takes
each recommended option, says which, and carries on. On return or `timed_out` it picks
nothing for you, does nothing that depends on the answer, and does not ask
again. Once answered, the chat keeps the question, its context, every option
with its description, the recommended one and the one you chose. The tool
is not offered where nobody can answer: in `tofu run`, in a turn a cron job
started, and in a sub-agent. A sub-agent asks the lead instead, and the lead
may put the question to you naming it.

Each message you type, between turns or while the lead works, is put to
Jev's `request_open` question, beside the end of the lead's last reply and the
task in hand. When it judges the lead would have to guess what you want, the
task carries a note and the rule `restate_open` has the first reply restate
it, with what is in and out, and wait. A continuation, an approval, a go on a
plan the lead laid out or a precise request of several named steps is never
restated, and when Jev can't be reached nothing is restated. `tofu why --last
1 --point request_open` shows the row and, on a failure, the reason.

## Change it

The `gatePrompt` setting, the asking mode, decides how long the lead waits:

- `auto`: the work never stops on a question. The lead hears at once that it
  asked and keeps working on what does not depend on the answer. Your answer
  reaches it as a message. With none in `askPersonWaitSeconds`, 300 by
  default, the lead is told no answer came, no option is taken, and the form
  stays open and says no answer yet: an answer you give later still arrives.
  When every question is marked `reversible` and the ask judge says proceed
  on each, the recommended options are taken without showing you the form.
- `ask`: the lead waits until you answer, with no timeout.

    tofu settings set gatePrompt ask
    tofu settings set askPersonWaitSeconds 600

## Check it

Every question is a row in the decision ledger at the point `ask`, in
shadow: tofu records what the typed decision would have done, and the code
still decides by asking, except for a reversible recommendation under
`auto`, whose row is written `enforced` because its verdict decides. A
proceed takes the recommendation and the row's outcome reads
`taken_in_auto`. A session's record lists the tools its turns were offered:
`ask_person` is among them in a turn you started, and absent in `tofu run`
or a cron turn.

    tofu why --last 1 --point ask

## Undo it

A question cannot be taken back once the lead has read its answer. Say what
you meant instead: a message mid-turn reaches the lead before its next step.

    tofu settings set gatePrompt auto
