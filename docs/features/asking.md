---
title: Asking you
description: When only you can settle something, the lead asks with options and a recommended one, and the work keeps going while you decide. Approvals for a tool call are a separate card with five answers.
order: 16
updated: 2026-10-13
---

When a choice is yours, such as which of two libraries to use, a matter of
taste, spending money or something that can't be undone, the lead asks you
with the `ask_person` tool. You get a form above the composer: the question,
up to four short options with a description each, the one the lead
recommends, and a last row, **other**, for an answer in your own words.

A tool call the gate is unsure about is a different card, an approval. The
two never mix: a question is never a request to run something.

## Why the work does not stop

**In auto, a question never stops the work.** The lead hears at once that
it asked and keeps going on whatever doesn't depend on the answer. With no
answer in `askPersonWaitSeconds`, 120 by default, the lead is told none
came and picks nothing for you, and the form stays open and says no answer
yet: an answer you give later still reaches the lead.

**An open request is restated before work starts.** When a message you
type leaves the work to the lead, such as `make the desk faster`, Jev's
`request_open` question says so, and the lead's first reply restates the
request with what is in and out and waits for your go. A message you type
while the lead works is asked the same question, so a new open request
mid-turn is restated too, and a correction to the running task is not. Jev
reads your message beside the end of the lead's last reply and the task in
hand, so `ok lets finish the tickets` after a ticket list, a `yes` to a
proposal or a go on a plan starts the work with no playback. A precise
request starts at once too, however many steps it has: a named rename, or
a search, an edit to a named function and a reply in a given form. When
Jev can't be reached nothing is restated, and the `request_open` row says
why. On 59 mocked conversations Jev restated none of the 24 continuations
and approvals, 1 of 15 precise requests and 19 of 20 open requests on each
of two passes.

**A reversible choice doesn't reach you at all in auto.** When the lead
marks every question `reversible`, meaning its recommended option can be
undone later at little cost, and the ask judge says proceed on each, the
recommended options are taken without showing the form. Those rows are
written with mode `enforced`, since their verdict decides, and the outcome
reads `taken_in_auto`.

**A recommendation is always there.** Every question but a free-text one
carries the option the lead would pick. When you press `Esc`, when the turn
stops, or when nobody can be asked, the lead takes that option, says which,
and carries on. A timeout is not one of these: it is no answer.

**It is offered only where someone can answer.** `ask_person` doesn't exist
in `tofu run`, in a turn a cron job started, or in a sub-agent. A
sub-agent asks the lead instead, and the lead may put the question to you
with the sub-agent named.

**Every question is a ledger row** at the decision point `ask`, so `tofu
why` shows each question the lead put to you.

## Answering a question

A call carries one to four questions, each `choice`, `multi`, `text` or
`yesno`. In the form:

- `Up` and `Down` move; a digit or `Enter` chooses.
- `Space` toggles an option of a `multi` question.
- `Tab` goes to the next question.
- On **other**, type the answer in the chat and press `Enter`.
- `Esc` dismisses the form, and the recommended options stand.

Options that carry a preview show side by side with the focused option's preview, and stack one under another when the screen is too narrow. A note typed in the chat goes with the option chosen.

To make the lead wait for every answer, with no timeout:

```sh
tofu settings set gatePrompt ask
```

To give yourself longer in auto:

```sh
tofu settings set askPersonWaitSeconds 300
```

## Answering an approval

Under `gatePrompt ask`, a call Jev would ask about waits for you. Under
`auto`, the default, it runs and is recorded, and only a change to tofu's
own settings, hooks or hook trust still waits. That means the settings and
hook files at the project's root and in your home: reading one, a `--help`,
an output sent to `/dev/null`, or a fixture's `.tofu/settings.json` in a
folder below the root runs without asking. The card takes five answers:

| Key | Answer |
|---|---|
| `1` | allow once |
| `2` | deny |
| `3` | always here: allow it on this target in this session |
| `4` | never here: refuse it on this target in this session |
| `5` | cancel: refuse it and stop the turn |

A standing answer from `3` or `4` decides each later call on that target
unasked and writes a `standing` row to the ledger. It is kept beside the
session and read back after a restart or `tofu --continue`, and it never
applies in another session. When Jev can't answer at all, the
call fails closed: refused with the reason in auto, put to you in ask.

**A sub-agent's ask goes to the lead, not to you,** with the call, Jev's
verdict and its risk. Under `auto`, a call whose every target sits inside
the sub-agent's owns or its scratch folder, such as removing its own build
folder, runs without asking the lead; anything outside, such as a delete
outside the project, still asks. The lead can answer `allow_here`, which stands for
that kind of call from that sub-agent until its run ends, and a lead turn
that only answers asks ends without a chat message. On a replayed session
where a sub-agent made three calls of one kind, the lead was asked once and
wrote nothing in the chat, where answering each ask costs three asks and
three replies.

## Checking what was asked

```sh
tofu why --last 1 --point ask
```

prints the last question's row. `tofu session trace <name>` shows each
approval and who answered it: `allowed by the person`, `allowed by the
orchestrator`, or `allowed by gatePrompt auto`.

A program driving tofu over `tofu serve` answers questions as
`tofu/askPerson` requests and approvals as `tofu/requestApproval`; see
[Serving tofu to another program](/docs/cli/serve).
