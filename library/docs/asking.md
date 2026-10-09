---
topic: asking
title: When the lead asks you
summary: how the lead puts a decision to you with options and a recommended one, what auto and ask do with it, and when it cannot ask
---

## What it is

When only you can settle something, such as which of two libraries to use,
a matter of taste, spending money or something that cannot be undone, the
lead asks with the `ask_person` tool instead of ending its turn on a
question.

A call carries 1 to 4 questions. Each has:

- a header of at most 12 characters, and one sentence
- a type: `choice`, `multi`, `text` or `yesno`
- 2 to 4 options of 1 to 5 words, each with a description and an
  optional preview, for `choice` and `multi`
- the option the lead recommends, for every type but `text`

It is never a request to run a tool. Approval belongs to the gate, and the
two are kept apart.

## Where it lives

The question appears where an approval does. The answer goes back to the
lead as the tool's result under `ask`, and as a message under `auto`:

    {"outcome": "submitted", "answers": [
      {"id": "lib", "status": "answered", "chosen": ["net/http"],
       "note": "the person accepted the recommended option"}]}

- outcome: `submitted`; `cancelled`, the turn stopped while it waited;
  `timed_out`, no answer in time under auto; `undelivered`, nobody could
  be asked
- status: `answered`; `skipped`, you declined the recommended option and
  named no other; `unanswered`

On anything but `submitted` the lead takes each recommended option, says
which, and carries on.

The tool is not offered where nobody can answer: in `tofu run`, in a turn
a cron job started, and in a sub-agent. A sub-agent asks the lead
instead, and the lead may put the question to you naming it.

## Change it

The `gatePrompt` setting, the asking mode, decides how long the lead
waits:

- `auto`: the work never stops on a question. The lead hears at once that
  it asked and keeps working on what does not depend on the answer. Your
  answer reaches it as a message. With none in 2 minutes the recommended
  option is taken, marked auto-selected after timeout, and the question
  stays open: an answer you give later still arrives, so it can correct
  course. A turn that ends with a question open ends its report with it
  as a poll.
- `ask`: the lead waits until you answer, with no timeout.

    tofu settings set gatePrompt ask

## Check it

Every question is a row in the decision ledger at the point `ask`, in
shadow: tofu records what the typed decision would have done, and the
code still decides by asking.

    tofu why --last 1 --point ask

A session's record lists the tools its turns were offered: `ask_person`
is among them in a turn you started, and absent in `tofu run` or a cron
turn.

## Undo it

A question cannot be taken back once the lead has read its answer. Say
what you meant instead: a message mid-turn reaches the lead before its
next step.

    tofu settings set gatePrompt auto
