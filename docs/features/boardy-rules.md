---
title: Boardy managers and rules
description: Who may create, move and close a ticket on a boardy board, how other sessions ask through triage, how a ticket moves to another board, and board rules that stop a move.
order: 19
updated: 2026-10-09
---

A boardy board has managers. A manager creates, assigns, accepts and closes
tickets. Every other session asks: its `new` becomes a request in the
board's triage, and a manager accepts it, which gives it a number, or drops
it. Board rules decide what a move needs, and when the person has to approve.

## Why

**Two sessions never write one ticket at once.** Numbers come only from a
manager's create or accept, under the board's lock, so two sessions cannot
both write `DEMO-12`.

**An agent works like an engineer.** It moves its own ticket from doing to
review, with its evidence in the Log, and appends to its own Log. It never
changes owns and never closes.

**Owns are checked on every change.** A create, a widen or a move to doing
is refused when its owns overlap a live ticket's owns on any board of the
project, and the refusal names that ticket.

**The person is asked only when a rule says so.** Without a rule, a manager
closes a ticket. With one, the close waits for the person.

## How to

Name the managers. A manager is a session id or the person's name:

```
tofu boardy manager add s-7f2c
tofu boardy manager add luiz --as luiz
tofu boardy manager list
```

A board with no managers is run by the person. Only the person or a manager
changes the list.

Accept or drop what other sessions asked for:

```
tofu boardy triage list
tofu boardy triage accept T1
tofu boardy triage drop T2 --reason duplicate of DEMO-4
```

Assign, widen owns, and hand a ticket to another board:

```
tofu boardy assign DEMO-3 s-7f2c
tofu boardy widen DEMO-3 "internal/rule/**"
tofu boardy hand DEMO-3 OPS
tofu boardy hand DEMO-3 OPS --project ../other-repo
```

`hand` gives the ticket a new number on the other board with its whole Log,
and `show DEMO-3` says `handed to OPS-1`.

## Board rules

A board rule is a rule file, the same shape as a library rule, in
`~/.tofu/projects/<project>/boards/<KEY>/rules/`. It fires on one event:
`create`, `move`, `widen` or `close`, and `to:` narrows a move or a close
to one status. A close is a move to done or dropped.

The person approves every done:

```yaml
id: person_closes
domain: general
kind: structural
concern: process_discipline
checker: person_approves
mode: enforced
text: the person approves every done
on: close
to: done
```

A command decides. A non-zero exit refuses the move with the rule's text:

```yaml
id: test_line
domain: general
kind: structural
concern: process_discipline
checker: command
command: grep -q "test:" {ticket}
mode: enforced
text: a ticket reaches review with a test line in its Log
on: move
to: review
```

`{ticket}` is the ticket's file. The command also gets `TOFU_BOARD_EVENT`,
`TOFU_TICKET`, `TOFU_TICKET_ID`, `TOFU_FROM`, `TOFU_TO` and `TOFU_ACTOR`. A
rule in `shadow` mode never stops a move; what it found goes in
`events.jsonl`.

An outside hook runs the same rules:

```
tofu boardy check move DEMO-7 --to review
```

It exits 1 when an enforced rule stops the move.

## Who is acting

Inside a tofu session, `TOFU_SESSION` names the session, and `--as` is
ignored, so a session cannot act as the person. Outside a session the
command line is the person, named by `--as`.

## Reference

- Roles: manager (named in `board.toml`), the person, and every other session as an agent.
- Moves an agent may make: its own ticket, doing to review, with a Log.
- A move to doing, review or done needs an Acceptance; review also needs a Log.
- Triage requests live in `boards/<KEY>/triage/T<n>.json` until accepted or dropped.
- Every accept, drop, hand, widen, assign and rule that fired is a line in `events.jsonl`.
