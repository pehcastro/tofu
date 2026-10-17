---
topic: boardy-views
title: Boardy views, reports, epics and sprints
summary: saved views over a board's ticket files, a report built from tickets and events, epics, milestones and sprints as files, and the boardy methods on tofu serve
---

## What it is

A view is a filter over a board's tickets that never changes them. The
default view shows the live tickets of the active sprint, grouped by
status. The built-in views are `default`, `review`, `epic` (live tickets
by epic), `blocked`, `mine` (live tickets assigned to the name in `--as`)
and `all`. A board's own views live in `views.toml` beside `board.toml`,
one `name = {json}` per line; a saved view with a built-in name replaces it.

    p0 = {"filter": {"statuses": ["todo", "doing"], "label": "p0"}, "group_by": "assignee"}

A filter takes `statuses`, `live`, `sprint` (an id, or `active`), `epic`,
`assignee` (a name, or `me`), `label` and `text`. `group_by` is `epic`,
`status` or `assignee`.

Epics, milestones and sprints are files in the board's `epics/`,
`milestones/` and `sprints/` folders, with front matter and free text. A
sprint is planned, active or closed, and one sprint is active at a time. A
ticket names its epic and sprint in its own front matter.

A report sums a board from its ticket files and `events.jsonl`: tickets
and points per status, per epic and in the active sprint, then moves,
finished tickets, rounds (moves into review) and time in doing within the
window, and the oldest tickets in review and blocked, five of each.

## Where it lives

- `boards/<KEY>/views.toml`: the board's saved views.
- `boards/<KEY>/epics/`, `milestones/` and `sprints/`: one markdown file each.
- `boards/<KEY>/events.jsonl`: what a report counts moves and rounds from.

The `boards` folder is under the project's folder in the tofu home.

## Change it

    tofu boardy epic new E1 Accounts
    tofu boardy epic milestone M1 Public beta --due YYYY-MM-DD
    tofu boardy epic new E2 Billing --milestone M1
    tofu boardy epic done E1
    tofu boardy sprint new S1 First cut --start YYYY-MM-DD --end YYYY-MM-DD
    tofu boardy sprint start S1
    tofu boardy sprint close S1

A due, start or end is a day, written `YYYY-MM-DD` or `Nd` from today, and
read back as that same day in any time zone; a sprint started with no start
takes today where you are.

Starting a sprint closes the one that was active. Only a manager of the
board changes its epics, milestones and sprints.

    tofu boardy new --parent DEMO-1 --owns internal/a/parse.go --acceptance "UTF-8 parses" the parser half

makes the sub-ticket `DEMO-1-1`, typed `subtask`, as Linear's sub-issues
and Jira's subtasks, in the folder `tickets/DEMO-1/`. The parent's owns are
the ceiling: an entry outside them is refused, naming the parent. A parent
in doing does not block its own sub-tickets, two sub-tickets in doing may
not overlap, and a parent reaches review or done only once every sub-ticket
is done or dropped. There is one level: `DEMO-1-1-1` is not an id.

## Check it

    tofu boardy view
    tofu boardy view review
    tofu boardy view epic --label p0
    tofu boardy report --since 7d
    tofu boardy epic list
    tofu boardy sprint list
    tofu boardy audit
    tofu boardy set DEMO-1 --epic E --sprint S

`audit` names what is wrong with a board, with ticket ids: review age,
reports nobody acted on, tickets with no epic or sprint, likely duplicates,
blocked tickets with no question, stale todo, and how long the lead takes
from a report to a move. Serve: `boardy.audit {board}`. Every subcommand
takes `--board KEY` and `--json`.

Over `tofu serve`, capability `boardy`: `boardy.boards`, `boardy.list
{board, view, filter}`, `boardy.get {id}` with the ticket's thread (a
spawn or report event carries session, agent, call and files),
`boardy.events {board, since}`, `boardy.report {board, since}`, `boardy.create`, `boardy.move`,
`boardy.assign`, `boardy.log`, `boardy.hand` and `boardy.triage {action}`.
`boardy.questions {board}` lists the open questions from the ticket files,
every board when `board` is left out, with an answer already typed in
`catchup.md`; it never rewrites that file. `boardy.answer {ticket, id,
answer}` applies one answer the way `tofu boardy catchup --answers` does:
it logs it, drops the question, and moves the ticket out of
needs-decision once none are left. `boardy.plan {board}` returns epics,
milestones, sprints and view names, and `boardy.init {key, name}` makes a
board as `tofu boardy init` does. Every answer carries `words`, the
ticket, board, epic, sprint and points names for the board's flow
(`boardFlow`). A watcher sends `boardy.changed {board, paths}` within a second of any
ticket file changing on disk, whoever wrote it.

## Undo it

Delete a line from `views.toml`, or the file, to get the built-in views
back. An epic, milestone or sprint is one markdown file: edit it or delete
it. A view or a report never writes anything.
