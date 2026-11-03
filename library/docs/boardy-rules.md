---
topic: boardy-rules
title: Boardy managers, triage, hand and board hooks
summary: who may change a ticket on a boardy board, how other sessions ask through triage, how a ticket moves boards, and hooks that check, nudge or act before and after any board event
---

## What it is

A board's managers are listed in its `board.toml`, each a session id or the
person's name. A manager creates, assigns, accepts, widens, hands and closes;
a board with no managers is run by the person, and a lead session acts for
the person. Every other session, a sub-agent `<session>/<ticket>` included,
is an agent: it moves its own ticket from doing to review with evidence in
the Log, and its `tofu boardy new` becomes a triage request.

- Inside a tofu session `TOFU_SESSION` names the actor; outside one the command line is the person. `--as orchestrator` acts as the lead and any other `--as` name as that agent, never as the person.
- Every move checks the role, an Acceptance, a Log for review, owns disjoint from every doing ticket, references, then the board's before hooks. A move to the status a ticket has says so and exits 0; an acceptance typed without a dash is stored one line per check.
- Built in, with no hook file: a create needs Before, Driven path, Still works and Empty or error case, plus Feasibility when Jev's `ticket_feasibility@1` judges the ask a visual or platform effect. A move to done needs an evidence entry naming every acceptance line in force, a line left for the lead included, and a reason Jev's `done_reason@1` judges names nothing still open. A ticket owning `internal/agent/loop`, `internal/tools/registry`, `internal/host`, `internal/agent/subagents`, `internal/store/session`, `internal/ui/tui`, `apps/cli` or `apps/desk` reaches done only on a HEAD tree with a passing flows stamp. Each Jev answer is a decision ledger row; when Jev cannot answer, an agent's create or move waits for the person.

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

- Hooks, in the order they fire and by file name within each: the board `boards/<KEY>/rules/`, the project `boards/rules/`, the user `~/.tofu/boardy/rules/`. `tofu boardy init` copies the five starter hooks into the board's folder, and the eighteen library hooks run from the binary below all three scopes.
- A rule file in the library shape with `on:` and the `command` or `person_approves` checker loads as a before hook; in shadow mode it records.
- `boards/<KEY>/board.toml`: the managers, `holds`, `log_to_review` and `scope_cites`.
- `boards/<KEY>/triage/T<n>.json`: one waiting request each.
- `boards/<KEY>/events.jsonl`: a `rule` line for every hook that fires (actor `hook:<id>` for an effect), a `hook` line for every add, edit and remove, and under serve a `hook-failed` line naming a file that does not load.
- `boards/<KEY>/hooks-ticked.json`: each schedule hook's last run.
- The `boards` folder is under the project's folder in the tofu home.

## The hooks a board starts with

`tofu boardy init` with no `--mode` gives a board these 23 hooks. Each runs before its events and refuses unless it says otherwise. Only `callers_owned` takes a bypass.

Starter hooks, copied into `boards/<KEY>/rules/`, scope `board`:

- `epic_suggest`, on `ticket.create` with no epic: Jev's `board_epic@1` picks an epic for the new ticket.
- `epic_at_create`, on `ticket.create`: refuses a ticket with no epic.
- `evidence_at_review`, on `ticket.move` to review by the lead, a sub-agent or a hook: refuses until every acceptance line has an evidence entry or is left for the lead.
- `report_to_review`, after `agent.report` from doing: moves the ticket to review.
- `review_waits`, a schedule every hour: notifies about a ticket that has waited in review over two hours.

Library hooks, read from the binary, scope `library`:

- `may_move`, Who moves a ticket, on `ticket.move` and `ticket.close`.
- `not_possible_hold`, A not-possible question is the person's, on `ticket.move` and `ticket.close`.
- `acceptance`, Work starts with an Acceptance, on a move to doing, review or done.
- `log_entry`, Review starts with a Log entry, on a move to review.
- `evidence_chars`, Enough evidence for review, on a move to review.
- `decidable`, A decision carries its question, on a move to needs-decision.
- `done_stamp`, Done needs a stamped tree, on a close to done.
- `evidenced`, Every line has evidence, on a close to done.
- `before_captured`, Done needs a before capture, on a close to done, except a docs-only ticket or one made before the gate; a late capture naming its pre-change build passes when the done reason says `late`, the word after the check name.
- `sub_tickets_done`, A parent waits for its sub-tickets, on a move to review or done.
- `scope_cites`, Scope names a source, on `ticket.create` and `ticket.widen`.
- `epic_ready`, An epic is researched and designed first, on `ticket.create` and on `ticket.set` of an epic.
- `owns_free`, Owns do not overlap, on `ticket.create`, `ticket.move`, `ticket.close`, `ticket.widen` and `ticket.set`.
- `drafted`, A ticket is drafted, on `ticket.create`, `triage.request`, `ticket.move` and `ticket.close`.
- `feasibility`, A platform effect states its feasibility, on `ticket.create`, `triage.request`, `ticket.move` and `ticket.close`.
- `done_reason`, Done says nothing is open, on a close to done.
- `callers_owned`, Callers are owned, on a move to doing. The person or the lead bypasses it with `tofu boardy move <ticket> doing --bypass callers_owned --why <reason>`; nobody accepts it, the move passes, and the bypass is a `rule` line in `events.jsonl`.
- `before_on_time`, A before is captured on time, on `ticket.log`, unless it is logged with `--late <commit or build>`.

`tofu boardy init --mode empty` starts a board with none of the 23, and `--mode custom --hooks may_move,drafted` with only the named ones. Such a board keeps a `rules/.mode` file, so `tofu doctor` does not offer it the starter hooks again.

## Change it

    tofu boardy rule add <id> [--scope board|project|user] [--file hook.yaml] --when W --on E [--every D|--daily HH:MM] --do "verb arg" [--then T] [--text t] [--from s] [--to s] [--epic E|set|empty] ...
    tofu boardy rule edit <id> [any add flag]
    tofu boardy manager add|remove|list [name] [--board KEY]
    tofu boardy triage list|show|accept|drop [request] [--reason text]
    tofu boardy assign <ticket> <session or name>
    tofu boardy widen <ticket> <glob,glob>
    tofu boardy hand <ticket> <board> [--project dir]
    tofu boardy set <ticket> [--epic E] [--sprint S]
    tofu boardy tick [--board KEY]
    tofu boardy fire agent.spawn|agent.report|lead.attach [ticket] [--when before|after]

- `edit` keeps the hook's scope and changes only the fields named; an empty value removes one. A manager adds, edits and removes.
- `hand DEMO-3 OPS` creates the next OPS ticket with DEMO-3's Log and drops DEMO-3 as `handed to OPS-1`. `triage show T1` prints who asked, when, the spec and owns, then the requested ticket's sections.
- `tick` runs the due schedule hooks once; `tofu serve`, and so the desk app, ticks every minute while open. One host per project ticks however many windows are open, so an `every` hook runs once per slot, and a failing hook is told once. `fire` fires the events no board command makes.
- Serve: `boardy.rules`, `boardy.rule.get` and `boardy.rule.remove` take `board`, `id` and an optional `scope`, and answer the rows `--json` prints. `boardy.rule.put` takes `board`, `scope` and `source`, the hook's YAML, and refuses an invalid one naming the file. `boardy.rule.test` takes an `id` or an unsaved `source`, a `ticket` and an optional `to`.
- A move to `needs-decision` needs a question with a tldr, an eli5, two to four options and the pick with why. `tofu boardy ask` asks it, `tofu boardy catchup` writes `boards/<KEY>/catchup.md` for the answers, and `catchup --answers`, run by a manager, applies them. An answered question stays under Questions with an `Answer:` line; catchup, audit and the board's rows count only the open ones.

## Check it

    tofu boardy rule list [--board KEY]
    tofu boardy rule show <id> [--scope S]
    tofu boardy rule test <id> [ticket] [--to status]
    tofu boardy check create|move|widen|close <ticket> [--to status]

`list` shows each hook's scope, file, trigger and last fired, and warns on a
file that does not load: schedules skip it and every before gate refuses
until it is fixed. `show` prints the hook's file. `test` runs one hook, a
command check included, says whether its conditions hold and what it would
do. `check` runs every check the command runs and exits 1 with the same
refusal. Neither writes to the board.

## Undo it

- `tofu boardy rule remove <id>` deletes a starter hook's file, and for a library hook writes `boards/<KEY>/rules/<id>.removed`, which turns it off on that board alone. Its `hook` line keeps the file it removed.
- `tofu boardy manager remove <name>` takes a manager off; with none left the person runs the board again.
- `tofu boardy triage drop T1` drops a request. A handed ticket stays dropped on its old board with a link to the new one.
