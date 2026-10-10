---
title: Boardy views and reports
description: See only the tickets that matter now. Saved views keep the default to the active sprint and live tickets, a report sums a board from its tickets and events, and the desk reads and writes every ticket field over tofu serve.
order: 19
updated: 2026-10-09
---

A board with sixty tickets is too many to read at once. `tofu boardy view`
shows a slice of it, and the default slice is the active sprint's live
tickets, grouped by status. `tofu boardy report` sums the whole board in
one screen, built from the ticket files and `events.jsonl` rather than a
page somebody keeps by hand.

## Why

**The default is short.** Done and dropped tickets, and tickets in other
sprints, stay out of the default view. A view is a filter over the files
and never changes them, the way a Linear view is a perspective on its
issues (https://linear.app/docs/conceptual-model).

**A report nobody writes.** Counts per status, progress per epic and
sprint in tickets and points, moves, rounds and time in doing, and the
oldest tickets waiting in review or blocked, all read from the files.

**One API for every screen.** The desk and the terminal ask `tofu serve`
the same `boardy.*` methods, and a file watcher sends `boardy.changed`
whenever a ticket file changes on disk, whoever wrote it: another tofu, an
editor, or git.

## How to

Plan the work:

```
tofu boardy epic new E1 Accounts
tofu boardy epic milestone M1 Public beta --due 2026-11-01
tofu boardy epic new E2 Billing --milestone M1
tofu boardy sprint new S1 First cut --start 2026-10-09 --end 2026-10-22
tofu boardy sprint start S1
```

A ticket names its epic and sprint in its front matter (`epic: E1`,
`sprint: S1`). A due, start or end is a day, `2026-11-01` in the file and
over `tofu serve`, so it reads as the same day in every time zone.

Look at it:

```
tofu boardy view
tofu boardy view review
tofu boardy view epic
tofu boardy view mine --as go-dev-1
tofu boardy report --since 7d
```

## Views

| View | Shows |
|---|---|
| `default` | live tickets in the active sprint, by status |
| `review` | tickets in review |
| `epic` | live tickets, by epic |
| `blocked` | blocked tickets |
| `mine` | your live tickets, by status |
| `all` | every ticket, by status |

Save your own in `views.toml` beside `board.toml`, one per line:

```
p0 = {"filter": {"statuses": ["todo", "doing"], "label": "p0"}, "group_by": "assignee"}
```

A filter takes `statuses`, `live`, `sprint` (an id, or `active`), `epic`,
`assignee` (a name, or `me`), `label` and `text`; `group_by` is `epic`,
`status` or `assignee`. A saved view with a built-in name replaces it.

## API

| Command | Does |
|---|---|
| `tofu boardy view [name] [--epic E] [--label l] [--assignee name]` | prints a view |
| `tofu boardy report [--since YYYY-MM-DD\|Nd]` | sums the board |
| `tofu boardy epic list\|new <ID> <title> [--milestone M]\|milestone <ID> <title> [--due date]\|done <ID>` | epics and milestones |
| `tofu boardy sprint list\|new <ID> <title> [--start date] [--end date]\|start <ID>\|close <ID>` | sprints; starting one closes the active one |

Every subcommand takes `--board KEY` and `--json`.

Over `tofu serve` (capability `boardy`): `boardy.boards`, `boardy.list
{board, view, filter}`, `boardy.get {id}` with the ticket's events and its
rounds and minutes in doing, `boardy.events {board, since}`,
`boardy.report {board, since}`, `boardy.create`, `boardy.move`,
`boardy.assign`, `boardy.log`, `boardy.hand`, `boardy.triage {action:
list|accept|drop}`, and the notification `boardy.changed {board, paths}`.
`tofu serve --schema` types each one.
