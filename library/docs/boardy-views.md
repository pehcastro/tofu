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

Starting a sprint closes the one that was active. Only a manager of the
board changes its epics, milestones and sprints.

## Check it

    tofu boardy view
    tofu boardy view review
    tofu boardy view epic --label p0
    tofu boardy report --since 7d
    tofu boardy epic list
    tofu boardy sprint list

Every subcommand takes `--board KEY` and `--json`.

Over `tofu serve`, capability `boardy`: `boardy.boards`, `boardy.list
{board, view, filter}`, `boardy.get {id}`, `boardy.events {board,
since}`, `boardy.report {board, since}`, `boardy.create`, `boardy.move`,
`boardy.assign`, `boardy.log`, `boardy.hand` and `boardy.triage {action}`.
A watcher sends `boardy.changed {board, paths}` within a second of any
ticket file changing on disk, whoever wrote it.

## Undo it

Delete a line from `views.toml`, or the file, to get the built-in views
back. An epic, milestone or sprint is one markdown file: edit it or delete
it. A view or a report never writes anything.
