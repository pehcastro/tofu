---
title: Boardy managers and hooks
description: Who may change a ticket on a boardy board, how other sessions ask through triage, and board hooks that check, nudge or act before and after any board event.
order: 19
updated: 2026-10-16
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
minute while it is open. `boards/<KEY>/hooks-ticked.json` keeps the last
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
- Managers and triage: `tofu boardy manager add|remove|list`, `tofu boardy triage list|accept|drop`, `tofu boardy assign`, `widen` and `hand`.
- A board rule file in the library rule shape, with `on:` and the `command` or `person_approves` checker, still loads as a before hook.
- `board.toml` sets two checks with no file: `log_to_review = 200` refuses a move to review with a shorter Log, and `scope_cites = ["docs/research/"]` refuses a create or widen whose Scope names none of them.
- `tofu boardy check move DEMO-7 --to doing` runs every check a move runs, hooks included, and writes nothing.
- Inside a session `TOFU_SESSION` names the actor. The lead acts as the lead, never as the person, and manages a board with no managers; a sub-agent, `<session>/<ticket>`, never does. Outside a session the command line is the person, named by `--as`.
