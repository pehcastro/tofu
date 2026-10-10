---
title: Boardy
description: Your own Jira or Linear, kept as markdown files on your disk, with numbered tickets, statuses and a lint that catches two tickets claiming one file.
order: 18
updated: 2026-10-09
---

Boardy is a ticket board that lives in files. Each project can hold several
boards, each board numbers its tickets from 1, and every ticket is one
markdown file you can open in any editor. `tofu boardy` makes, moves, logs
and checks them.

## Why files

**Nothing to host.** A board is a folder under `~/.tofu/projects/<project>/boards/`.
There is no server, no account and no sync.

**Numbers never collide.** The counter and each ticket file are written
under a lock that works on Linux, macOS and Windows, so twenty `tofu boardy
new` started at once get twenty different numbers.

**Ownership is checked, not hoped for.** A ticket names the paths it owns.
`tofu boardy lint` refuses two live tickets whose owns overlap and names
both.

**Your words.** Set `boardFlow` to `jira` (ticket, board, epic, sprint,
points) or `linear` (issue, team, project, cycle, estimate). The files are
the same either way.

## How to

Make a board and three tickets:

```
tofu boardy init --key DEMO
tofu boardy new --priority P1 --owns "internal/a/**" --acceptance "tofu reads every key" parse the config
tofu boardy new --type bug the list drops a row
tofu boardy new --owns docs/x.md write the page
```

Work one through review:

```
tofu boardy move DEMO-1 doing
tofu boardy move DEMO-1 review
tofu boardy log DEMO-1 parsed every key, the test passes
tofu boardy show DEMO-1
```

See the board and check it:

```
tofu boardy list
tofu boardy boards
tofu boardy lint
```

A second board in the same project, `tofu boardy init --key OPS`, counts
from `OPS-1`. With two boards, `new` and `list` take `--board KEY`.

## The ticket file

```
---
id: DEMO-1
title: parse the config
type: task
status: review
priority: P1
points: 0
owns: ["internal/a/**"]
depends: []
...
---

## Problem

## Scope

## Acceptance

## Log

- 2026-10-09 person: parsed every key, the test passes
```

Status is one of triage, backlog, todo, doing, review, done, dropped, or
blocked with a reason (`--reason`). A ticket moved from review back to
doing gets an `Acceptance 2` section, a copy of the last list, so each round
keeps its own acceptance. `events.jsonl` beside the tickets records every
create, move and log line.

## Agents work from the board

Set `projectManagement` to `boardy` and tofu's own lead and sub-agents work
from tickets, the way a team works from a board:

```
tofu settings set projectManagement boardy
tofu boardy manager add <lead session id>
```

- The lead spawns a sub-agent for a ticket by its id. The ticket's owns
  become the paths the sub-agent may write, the ticket is assigned to the
  sub-agent and moves to doing.
- The sub-agent reads the ticket, logs its evidence and moves its own ticket
  to review, and nothing further. A manager moves it to done or back to
  doing.
- The sub-agent's report is checked against the ticket by its exact id and
  its acceptance revision in force, so a brief that mentions `UTF-8` is
  still read as `DEMO-2`.
- A resumed session keeps the ticket on the sub-agent's row.
- Two board rules, `board_lead` and `board_sub_agent`, reach the prompt only
  while the setting is `boardy`.

| Tool | Who gets it | Does |
|---|---|---|
| `ticket_read` | every agent | reads any ticket of the project by id |
| `ticket_log` | every agent | appends evidence to its own ticket's Log |
| `ticket_ask` | every agent | writes a question for a manager into its own ticket's Log |
| `ticket_request` | every agent | writes a request into a board's triage |
| `ticket_move` | every agent | an agent moves its own ticket from doing to review; a manager moves any ticket |
| `ticket_create` | managers | creates a numbered ticket |
| `ticket_assign` | managers | assigns a ticket |

With the setting `off`, the default, nothing changes: no board file is
written, no board rule reaches a prompt, and a spawn's owns are the paths
the lead gives it.

## API

| Command | Does |
|---|---|
| `tofu boardy init --key KEY [--name text]` | makes a board |
| `tofu boardy new [--board KEY] [--type t] [--priority P0..P4] [--status s] [--points n] [--owns a,b] [--acceptance text] [--as name] <title>` | makes a ticket with the next number |
| `tofu boardy list [--board KEY] [--status s] [--all]` | lists live tickets |
| `tofu boardy show <ticket>` | prints one ticket |
| `tofu boardy move <ticket> <status> [--reason text]` | changes its status |
| `tofu boardy log <ticket> <text>` | appends a dated line to its Log |
| `tofu boardy lint [--board KEY]` | checks every ticket, exits 1 on a finding |
| `tofu boardy boards` | lists the project's boards |

Every subcommand takes `--json`. Settings: `projectManagement` (`off` or
`boardy`) and `boardFlow` (`jira` or `linear`).
