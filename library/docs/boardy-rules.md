---
topic: boardy-rules
title: Boardy managers, triage, hand and board hooks
summary: who may change a ticket on a boardy board, how other sessions ask through triage, how a ticket moves boards, and hooks that check, nudge or act before and after any board event
---

## What it is

A board's managers are listed in its `board.toml`, each a session id or the
person's name. A manager creates, assigns, accepts, widens, hands and
closes; a board with no managers is run by the person. A lead session acts
for the person. A sub-agent, `<session>/<ticket>`, and every other session
is an agent: it moves its own ticket from doing to review with evidence in
the Log, and its `tofu boardy new` becomes a triage request. Inside a tofu
session `TOFU_SESSION` names the actor; outside one the command line is the
person; `--as orchestrator` acts as the lead and any other `--as` name as
that agent, never as the person. Every move checks the role, an Acceptance, a Log
for review, owns disjoint from every doing ticket, references, and then the
board's before hooks. A move to the status a ticket has says it is already
there and exits 0, and an acceptance typed without a dash is stored one line
per check.

A hook is one YAML file:

    id: epic-required
    when: before
    on: ticket.move
    do: field epic
    then: refuse
    text: a ticket starts with an epic
    if:
      to: doing

- `when`: `before` or `after`. A before hook can refuse, ask or set; an after hook cannot refuse or ask.
- `on`: `ticket.create|move|assign|log|widen|close`, `epic.create`, `sprint.open|close`, `milestone.reach`, `triage.request|accept`, `question.ask|answer`, `agent.spawn|report`, `lead.attach`, or `schedule` with `every: 1h` (one minute at least) or `daily: 09:00`. A schedule hook is after and runs over every ticket.
- `if`, all must hold: `from`, `to`, `type`, `priority`, `labels` (comma lists), `actor` (`person`, `lead`, `sub-agent`, `hook`), `epic` and `sprint` (a name, `set` or `empty`), `owns` (a glob), `in_status_for` (a duration).
- `do`, a check: `command <cmd>` (non-zero exit fails; `{ticket}` is the ticket's file, and `TOFU_BOARD_EVENT`, `TOFU_TICKET`, `TOFU_TICKET_ID`, `TOFU_FROM`, `TOFU_TO`, `TOFU_ACTOR` are set), `field <name>` (fails when empty), `person` (fails unless the person acts), `jev <set>` (fails closed, and asks before an event). `then` says what a failure does: `refuse`, `nudge` (a note to the actor, printed as `nudge <id>: ...`), `ask` (waits for the person, a `waiting` line) or `record`.
- `do`, an effect: `create <title>`, `move <status>`, `assign <name>`, `log <text>`, `notify <text>`, `set <field> <value>` (epic, sprint, assignee, priority, component, spec or size). `{ticket}` and `{title}` expand. Only `set` runs before; an effect fires hooks again up to depth 3.

## Where it lives

- Hooks, in the order they fire and by file name within each: the board `boards/<KEY>/rules/`, the project `boards/rules/`, the user `~/.tofu/boardy/rules/`. `tofu boardy init` writes four starter hooks into the board's folder.
- A rule file in the library shape with `on:` and the `command` or `person_approves` checker loads as a before hook; in shadow mode it records.
- `boards/<KEY>/board.toml`: the managers, `holds`, `log_to_review` and `scope_cites`.
- `boards/<KEY>/triage/T<n>.json`: one waiting request each.
- `boards/<KEY>/events.jsonl`: a `rule` line for every hook that fires (actor `hook:<id>` for an effect), a `hook` line for every add, edit and remove, and under serve a `hook-failed` line naming a file that does not load.
- `boards/<KEY>/hooks-ticked.json`: each schedule hook's last run.

The `boards` folder is under the project's folder in the tofu home.

## Change it

    tofu boardy rule add <id> [--scope board|project|user] [--file hook.yaml] --when W --on E [--every D|--daily HH:MM] --do "verb arg" [--then T] [--text t] [--from s] [--to s] [--epic E|set|empty] ...
    tofu boardy rule edit <id> [any add flag]
    tofu boardy rule remove <id>

`edit` keeps the hook's scope and changes only the fields named; an empty
value removes one. A manager adds, edits and removes.

    tofu boardy manager add|remove|list [name] [--board KEY]
    tofu boardy triage list|show|accept|drop [request] [--reason text]
    tofu boardy assign <ticket> <session or name>
    tofu boardy widen <ticket> <glob,glob>
    tofu boardy hand <ticket> <board> [--project dir]
    tofu boardy set <ticket> [--epic E] [--sprint S]
    tofu boardy tick [--board KEY]
    tofu boardy fire agent.spawn|agent.report|lead.attach [ticket] [--when before|after]

`hand DEMO-3 OPS` creates the next OPS ticket with DEMO-3's Log and drops
DEMO-3 as `handed to OPS-1`. `tick` runs the due schedule hooks once;
`tofu serve`, and so the desk app, ticks every minute while it is open.
`fire` fires the events no board command makes. `triage show T1` prints
who asked, when, the spec and owns, then the requested ticket's sections.

Serve: `boardy.rules`, `boardy.rule.get` and `boardy.rule.remove` take
`board`, `id` and an optional `scope`, and answer the rows `--json` prints.
`boardy.rule.put` takes `board`, `scope` and `source`, the hook's YAML, and
refuses an invalid one naming the file. `boardy.rule.test` takes an `id` or
an unsaved `source`, a `ticket` and an optional `to`.

A move to `needs-decision` needs a question with a tldr, an eli5, two to
four options and the pick with why. `tofu boardy ask` asks it,
`tofu boardy catchup` writes `boards/<KEY>/catchup.md` for the answers, and
`catchup --answers`, run by a manager, applies them. An answered question
stays on the ticket under Questions with an `Answer:` line, and catchup,
audit and the board's rows count only the open ones.

## Check it

    tofu boardy rule list [--board KEY]
    tofu boardy rule show <id> [--scope S]
    tofu boardy rule test <id> [ticket] [--to status]
    tofu boardy check create|move|widen|close <ticket> [--to status]

`list` shows each hook's scope, file, trigger and last fired, and warns on
a file that does not load: schedules skip it and every before gate refuses
until it is fixed. `test` runs one hook against a ticket, a command check
included, says whether its conditions hold and what it would do, and
writes nothing to the board. `check` runs every check the command runs,
before hooks included, writes nothing, and exits 1 with the same refusal.

## Undo it

`tofu boardy rule remove <id>` stops a hook, and its `hook` line keeps the
file it removed. `tofu boardy manager remove <name>` takes a manager off;
with none left the person runs the board again. `tofu boardy triage drop T1`
drops a request. A handed ticket stays dropped on its old board with a link
to the new one.
