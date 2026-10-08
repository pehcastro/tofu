---
topic: status
title: Program status on the terminal
summary: tofu reports what the lead, each sub-agent and each kept shell is doing with OSC 7501, so a terminal or an agent inbox reads it instead of guessing
---

## What it is

The app tells the terminal it runs in what it is doing, with the Program
Status Protocol, OSC 7501
(https://www.superlogical.com/rex/docs/build/program-status). A terminal
that knows the sequence shows it, on a tab, in a session list or an inbox.
One that does not ignores it: the legacy Windows console and Windows
Terminal both drop it and print nothing.

Each report is one record with an id path, a state and a few optional
keys. The states are `idle`, `working`, `done` (finished, not yet seen),
`blocked` (waiting on you), `error`, and `clear`, which removes a record
and every record under it. A blocked record says what it waits for:
`permission`, `question` or `auth`.

The records tofu sends:

- The lead is the terminal's root record, with no id, `app=tofu` and the
  session's name as its title. Every other record takes `app=tofu` from it.
  It is `working` while a turn runs, `blocked:kind=permission` while a
  gated call waits for your answer, `error` when the turn failed, and
  `idle` at the prompt and after you stop a turn. A turn that ends while
  the terminal is not focused leaves `done`, which becomes `idle` the next
  time you focus the terminal, press a key or click.
- `agents/<name>`, one for each sub-agent: `working`, `blocked:kind=question`
  while it waits on an answer from the lead, `blocked:kind=permission` while
  one of its calls waits on you, `done` when it finished or sits in review,
  `idle` when parked, `error` when it failed. The message is what it is
  doing now.
- `shells/<name>`, one for each kept shell: `working` while it runs, `done`
  when it exited 0, `error` with the exit code otherwise. The message is the
  command. A shell you killed has no record.
- `cron/<id>` while a cron job's turn runs, with the lead's state, and
  until you have seen the turn it started.

## Where it lives

Nowhere on disk. The app works the records out from what it already
shows, after every change on the screen, and writes a report only when a
record changed. A `done` or `error` record for a sub-agent or a shell
clears once you open the sub-agents screen or the shells screen.

A name becomes a segment of the id: a byte outside letters, digits and
`_.+-` becomes `_`, and a segment stops at 32 bytes. The real name is in
the record's title. A title stops at 192 bytes and a message at 2048, and a
control character in either is sent as a space.

## Change it

There is no setting. The sequence is safe on every terminal, so tofu
always sends it, and what a terminal does with a record is the terminal's
choice: turn the indicator or the notification off there.

## Check it

Run tofu in a terminal that implements OSC 7501, such as Rex or one built
on libghostty, and start a turn: the tab shows it working, then done.

To read the bytes yourself, run tofu under a program that records its
pseudo-console output and look for `ESC ] 7501 ;`. A turn reads, in order:

    state=idle:app=tofu
    state=working:app=tofu:title=...
    state=done:app=tofu:title=...

## Undo it

Quitting tofu sends `state=clear`, which removes every record it sent. If
tofu was killed before it could, clear what it left from the same
terminal:

    printf '\e]7501;state=clear\a'
