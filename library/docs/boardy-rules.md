---
topic: boardy-rules
title: Boardy managers, triage, hand and board rules
summary: who may change a ticket on a boardy board, how other sessions ask through triage, how a ticket moves boards, and rules that stop a move or wait for the person
---

## What it is

A board's managers are listed in its `board.toml`, each a session id or the
person's name. A manager creates, assigns, accepts, widens, hands and
closes. The person always changes the manager list and always closes; a
board with no managers is run by the person. A lead session acts for the
person, so it manages a board that lists no managers or lists `person`, and
spawns for a ticket without being added. A sub-agent, named
`<session>/<ticket>`, and every other session is an agent.

An agent moves its own ticket from doing to review, with evidence in the
Log, and appends to its own Log. Its `tofu boardy new` becomes a triage
request instead of a ticket, and the request takes no number until a
manager accepts it.

Inside a tofu session `TOFU_SESSION` names the actor and `--as` is ignored.
Outside one the command line is the person, named by `--as`.

Every move checks, in order:

- The actor's role allows it.
- A move to doing, review or done has an Acceptance; review has a Log.
- A create, a widen, or a move to doing has owns disjoint from every live ticket on every board of the project; the refusal names the ticket.
- The ticket's depends, blocks, relates and duplicates resolve.
- The board's rules for the event pass.

## Where it lives

- `boards/<KEY>/board.toml`: the managers, one per line of its list.
- `boards/<KEY>/triage/T<n>.json`: one waiting request each.
- `boards/<KEY>/rules/`: the board's rule files.
- `boards/<KEY>/events.jsonl`: one line per create, move, accept, hand and wait.

The `boards` folder is under the project's folder in the tofu home.

## Change it

    tofu boardy manager add|remove|list [name] [--board KEY]
    tofu boardy triage list|accept|drop [request] [--board KEY] [--reason text]
    tofu boardy assign <ticket> <session or name>
    tofu boardy widen <ticket> <glob,glob>
    tofu boardy hand <ticket> <board> [--project dir]

`triage accept T1` numbers the request on the board. `hand DEMO-3 OPS`
creates the next OPS ticket with DEMO-3's Log, drops DEMO-3 and sets its
reason to `handed to OPS-1`. When the actor does not manage the other
board, the ticket lands in that board's triage.

A board rule is a rule file in `boards/<KEY>/rules/`, the library's rule
shape, with `on:` set to `create`, `move`, `widen` or `close` and an
optional `to:` status. A close is a move to done or dropped. Two checkers:

- `person_approves`: an enforced rule makes a close by anyone but the person wait; the ticket stays where it is and `events.jsonl` gets a `waiting` line.
- `command`: runs `command:` with `{ticket}` replaced by the ticket's file and `TOFU_BOARD_EVENT`, `TOFU_TICKET`, `TOFU_TICKET_ID`, `TOFU_FROM`, `TOFU_TO`, `TOFU_ACTOR` set. A non-zero exit fails it, and the refusal carries the rule's `text:`.

Shadow rules never stop a move.

## Check it

    tofu boardy manager list
    tofu boardy triage list
    tofu boardy check create|move|widen|close <ticket> [--to status]

`tofu boardy check` runs the same rules a move runs, for an outside hook,
and exits 1 when one stops it. A refused move prints why and exits 1:

    x DEMO-4 refused: it is assigned to "s-other", and an agent moves only its own ticket

## Undo it

`tofu boardy manager remove <name>` takes a manager off; with none left the
person runs the board again. `tofu boardy triage drop T1` drops a request.
A handed ticket stays dropped on its old board with a link to the new one;
move the new one back with another `hand`. Delete a rule file to stop its
rule, or set its mode to shadow to keep it reporting without stopping a
move.
