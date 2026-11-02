---
title: Boardy managers and hooks
description: Who may change a ticket on a boardy board, how other sessions ask through triage, and board hooks that check, nudge or act before and after any board event.
order: 19
updated: 2026-11-02
---

A boardy board has managers. A manager creates, assigns, accepts and closes
tickets; every other session asks, and its `new` becomes a triage request
that a manager accepts or drops. Board hooks are how a board runs itself:
each one says when it fires, on which event, under which conditions, and
what it does.

## Why

**Two sessions never write one ticket at once.** Numbers come only from a
manager's create or accept, under the board's lock.

**An agent works like an engineer.** It moves its own ticket from doing to
review, with its evidence in the Log. It never changes owns and never
closes. Owns are checked on every create, widen and move to doing, against
every doing ticket on every board of the project.

**The board's habits are data, not memory.** A check a lead runs by hand on
every ticket becomes a hook, so it runs the same way for every session,
and the person can read, test and remove it. The lead writes these itself
with its `board_rule` tool when you correct how the board runs twice, and
says so in one line.

## A hook

One YAML file in a `rules/` folder:

```yaml
id: epic-required
when: before
on: ticket.move
do: field epic
then: refuse
text: a ticket starts with an epic
if:
  to: doing
```

- `when`: `before` or `after`. A before hook can refuse, ask or set; an after hook cannot stop what already happened.
- `on`: one event. `ticket.create`, `ticket.move`, `ticket.assign`, `ticket.log`, `ticket.widen`, `ticket.close`, `epic.create`, `sprint.open`, `sprint.close`, `milestone.reach`, `triage.request`, `triage.accept`, `question.ask`, `question.answer`, `agent.spawn`, `agent.report`, `lead.attach`, or `schedule` with `every: 1h` or `daily: 09:00`.
- `if`: every condition must hold. `from`, `to`, `type`, `priority` and `labels` take a comma list; `actor` is `person`, `lead`, `sub-agent` or `hook`; `epic` and `sprint` take a name, `set` or `empty`; `owns` takes a glob; `in_status_for` takes a duration such as `2h`.
- `do`: a check or an effect. The checks are `command <cmd>`, `field <name>`, `person` and `jev <set>`; a failed check does what `then` says: `refuse`, `nudge` (a note to the actor), `ask` (waits for the person) or `record`. The effects are `create <title>`, `move <status>`, `assign <name>`, `log <text>`, `notify <text>` and `set <field> <value>`, and `{ticket}` and `{title}` expand in them.

A hook lives in one of three scopes, and they fire in this order: the board
(`boards/<KEY>/rules/`), the project (`boards/rules/`), then the user
(`~/.tofu/boardy/rules/`), and by file name within each. An effect fires
hooks again, up to depth 3. Every hook that fires is a line in the
board's `events.jsonl`, and so is every add, edit and remove. A `jev`
check fails closed: before an event it asks. A hook file that does not
load refuses every before gate and is listed as not loaded; the other
schedule hooks keep running, and `tofu serve` writes a `hook-failed` line
naming the file.

## The hooks a board starts with

`tofu boardy init` with no `--mode` gives a board 23 hooks. The five
starter hooks are copied into the board's `rules/` folder, so each is a
file you can edit. The eighteen library hooks ship in the binary, run
below the three scopes, and show as scope `library` in
`tofu boardy rule list`. Only `callers_owned` takes a bypass.

| Hook | Scope | Fires | Does | Bypass |
|---|---|---|---|---|
| `epic_suggest` | board | before `ticket.create` with no epic | Jev's `board_epic@1` picks an epic | none |
| `epic_at_create` | board | before `ticket.create` | refuses a ticket with no epic | none |
| `evidence_at_review` | board | before a move to review by the lead, a sub-agent or a hook | refuses until every acceptance line has evidence or is left for the lead | none |
| `report_to_review` | board | after `agent.report` from doing | moves the ticket to review | none |
| `review_waits` | board | every hour | notifies about a ticket in review over two hours | none |
| `may_move` | library | before `ticket.move`, `ticket.close` | refuses a move the actor may not make | none |
| `not_possible_hold` | library | before `ticket.move`, `ticket.close` | holds a not-possible ticket for the person | none |
| `acceptance` | library | before a move to doing, review or done | refuses a ticket with no Acceptance | none |
| `log_entry` | library | before a move to review | refuses an empty Log | none |
| `evidence_chars` | library | before a move to review | refuses less evidence than `log_to_review` | none |
| `decidable` | library | before a move to needs-decision | refuses a question without tldr, eli5, options and a pick | none |
| `done_stamp` | library | before a close to done | refuses a flows-area ticket without a stamped tree | none |
| `evidenced` | library | before a close to done | refuses while an acceptance line has no evidence | none |
| `before_captured` | library | before a close to done | refuses a ticket with no before capture, except docs-only | none |
| `sub_tickets_done` | library | before a move to review or done | refuses while a sub-ticket is open | none |
| `scope_cites` | library | before `ticket.create`, `ticket.widen` | refuses a Scope that names none of `scope_cites` | none |
| `epic_ready` | library | before `ticket.create`, `ticket.set` of an epic | refuses a gated epic that is not researched and designed | none |
| `owns_free` | library | before `ticket.create`, `ticket.move`, `ticket.close`, `ticket.widen`, `ticket.set` | refuses owns another held ticket holds | none |
| `drafted` | library | before `ticket.create`, `triage.request`, `ticket.move`, `ticket.close` | refuses a ticket missing a drafted section | none |
| `feasibility` | library | before `ticket.create`, `triage.request`, `ticket.move`, `ticket.close` | refuses a platform effect with no Feasibility | none |
| `done_reason` | library | before a close to done | refuses a reason Jev reads as naming an open part | none |
| `callers_owned` | library | before a move to doing | refuses while a caller outside the owns is not owned | the person or the lead, accepted by nobody |
| `before_on_time` | library | before `ticket.log` | refuses evidence logged ahead of the before capture | none |

A bypass of `callers_owned` is
`tofu boardy move DEMO-7 doing --bypass callers_owned --why "the caller is generated"`:
the move passes, and the bypass is a line in the board's `events.jsonl`
that `tofu boardy catchup` counts.

`tofu boardy init --mode empty` starts a board with none of them, and
`--mode custom --hooks may_move,drafted` with only the named ones.
`tofu boardy rule remove <id>` deletes a starter hook's file; for a library
hook it turns the hook off on that board alone. A board started with
`--mode empty` or `--mode custom` remembers the choice, so `tofu doctor`
does not offer it the starter hooks again.

## How to

```
$ tofu boardy rule add epic-required --board TOFU --when before --on ticket.move --to doing --do "field epic" --then refuse --text "a ticket starts with an epic"
$ tofu boardy rule add stale-review --when after --on schedule --every 1m --to review --do "notify {ticket} sits in review"
$ tofu boardy rule add owns-nudge --scope user --when before --on ticket.assign --do "field owns" --then nudge
$ tofu boardy rule edit epic-required --then nudge
$ tofu boardy rule remove owns-nudge
```

`add` and `edit` take every field as a flag, or a whole file with
`--file hook.yaml`; an empty flag value removes the field. A manager
changes hooks. `test` runs one against a ticket and writes nothing to the
board:

```
$ tofu boardy rule test epic-required TOFU-2
test epic-required on TOFU-2 to doing      ✗ the check fails, so it would refuse
  before ticket.move if to doing · field epic, then refuse
  a ticket starts with an epic
nothing was written
```

`--to` names the status a move test goes to; it defaults to the hook's
own `to`. A command check runs during a test.

## Schedules and events nothing else fires

A schedule hook runs over every ticket when it is due. `tofu boardy tick`
runs the due ones once, and `tofu serve`, so the desk app too, ticks every
minute while it is open. One host per project ticks, however many windows
are open, so an `every` hook runs once in each slot, and a hook that fails
is told once, not on every tick. `boards/<KEY>/hooks-ticked.json` keeps the last
run of each. `tofu boardy fire agent.spawn|agent.report|lead.attach
[ticket]` fires the three events no board command makes, and
`tofu boardy set <ticket> --epic E --sprint S` plans a ticket.

## API

```
$ tofu boardy rule list --board TOFU
TOFU hooks · 4 hooks
  hook           scope    last fired           file
○ epic-required  board    2026-10-16 21:41:25  epic-required.yaml
  before ticket.move if to doing · field epic, then refuse
○ stale-review   board    never                stale-review.yaml
  after schedule every 1m if to review · notify {ticket} sits in review
```

- `tofu boardy rule list|show|add|edit|remove|test`, each with `--json`.
- `tofu serve` methods: `boardy.rules`, `boardy.rule.get`, `boardy.rule.put` (the hook's YAML as `source`), `boardy.rule.remove` and `boardy.rule.test` (a saved `id`, or an unsaved `source`).
- `tofu boardy tick`, `tofu boardy fire`, `tofu boardy set`.
- Managers and triage: `tofu boardy manager add|remove|list`, `tofu boardy triage list|show|accept|drop`, `tofu boardy assign`, `widen` and `hand`. `triage show T1` prints who asked and when, the spec and owns, then the requested ticket's sections, so a manager reads it before accepting.
- A board rule file in the library rule shape, with `on:` and the `command` or `person_approves` checker, still loads as a before hook.
- `board.toml` sets two checks with no file: `log_to_review = 200` refuses a move to review with a shorter Log, and `scope_cites = ["docs/research/"]` refuses a create or widen whose Scope names none of them.
- `tofu boardy check move DEMO-7 --to doing` runs every check a move runs, hooks included, and writes nothing.
- Built in, with no hook file: a create needs Before, Driven path, Still works and Empty or error case, plus Feasibility when Jev's `ticket_feasibility@1` judges the ask a visual or platform effect. A move to done needs an evidence entry naming every acceptance line in force, a line left for the lead included, and a reason that Jev's `done_reason@1` judges names nothing still open. A ticket owning `internal/turn`, `internal/host`, `internal/subagent`, `internal/session`, `interface/tui`, `cmd/tofu` or `apps/desk` reaches done only on a HEAD tree stamped by `./bench/flows`. Each Jev answer is a decision ledger row; when Jev cannot answer, an agent's create or move waits for the person.
- Inside a session `TOFU_SESSION` names the actor. The lead acts as the lead, never as the person, and manages a board with no managers; a sub-agent, `<session>/<ticket>`, never does. Outside a session the command line is the person; `--as orchestrator` acts as the lead and any other name given with `--as` acts as that agent, never as the person. A move to the status a ticket already has prints `DEMO-7 is already review`, exits 0 and writes nothing. An `--acceptance` typed without a leading dash is stored one line per check, so every hook counts it.
