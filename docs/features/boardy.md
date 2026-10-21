---
title: Boardy
description: Your own Jira or Linear, kept as markdown files on your disk, with numbered tickets and sub-tickets, statuses, a thread of what happened on each, and a lint that catches two tickets claiming one file.
order: 18
updated: 2026-10-13
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
`tofu boardy lint` refuses two doing tickets whose owns overlap and names
both, globs included: `x/*.go` and `x/*_test.go` both hold `x/a_test.go`.
It also names a live ticket assigned to an agent type that does not exist,
since a spawn for it is refused.
A ticket holds its owns only while it is doing; review, todo and done
tickets keep theirs as a record. An owns entry may be an absolute path in
another repository, such as `F:/work/other-repo/**`.

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

Ask what a hook asks, fast, without reading ticket bodies:

```
tofu boardy held cmd/tofu/main.go --json
tofu boardy list --fields id,status,assignee,owns --json
```

`held` answers `{"held": true, "ticket": "OPS-1", "status": "doing", "glob": "cmd/tofu/**"}`
from any board of the project, or `{"held": false}`.

Ask the person for a decision they can make in a minute:

```
tofu boardy ask DEMO-1 --tldr "One catch up file per board, or one per ticket?" --eli5 "Like one shared notepad for the team, or a sticky note on each card." --options "One file per board | One file per ticket" --recommend 1 --why "one place to read"
tofu boardy move DEMO-1 needs-decision
tofu boardy catchup
```

`catchup` prints each open question with its tldr, eli5, options and the
recommended pick, and writes them to `catchup.md` beside the tickets with an
empty `answer:` line under each. Write `1`, `2, but keep it short`, or your
own words there. `tofu boardy catchup --answers` logs every answer on its
ticket, closes the question, clears the entry, and moves the ticket back to
where it was once nothing is left to answer. A move to `needs-decision`
without a question carrying all of that is refused.

A second board in the same project, `tofu boardy init --key OPS`, counts
from `OPS-1`. With two boards, `new` and `list` take `--board KEY`.

Split a ticket into sub-tickets, the way Linear has sub-issues and Jira has
subtasks:

```
tofu boardy new --parent DEMO-1 --owns internal/a/parse.go --acceptance "UTF-8 parses" the parser half
```

That makes `DEMO-1-1`, then `DEMO-1-2`, numbered under the parent and typed
`subtask`. The parent's owns are the ceiling: an owns entry outside them is
refused, naming the parent. A parent in doing does not block its own
sub-tickets, two sub-tickets in doing may not overlap each other, and a
parent reaches review or done only once every sub-ticket is done or
dropped. Sub-tickets go one level down: `DEMO-1-1-1` is not an id.

See everything that happened on a ticket, in order:

```
tofu boardy show DEMO-1 --thread
```

The Activity section lists each create, move, log line, question and answer,
each sub-agent spawned on the ticket with its call, and each report it gave
back with the files it wrote.

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

Status is one of triage, backlog, todo, doing, review, done, dropped,
needs-decision, or
blocked with a reason (`--reason`). A ticket moved from review back to
doing gets an `Acceptance 2` section, a copy of the last list, so each round
keeps its own acceptance. `events.jsonl` beside the tickets records every
create, move and log line. A sub-ticket's file sits in its parent's folder,
`tickets/DEMO-1/DEMO-1-1.md`.

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
- The spawn and the report land on the ticket's thread, and a sub-agent's
  `ticket_ask` lands there as a question.
- Two board rules, `board_lead` and `board_sub_agent`, reach the prompt only
  while the setting is `boardy`.
- `board_lead` has the lead review every ticket in review each turn, oldest
  first and whoever spawned it; its timed check-in lists them, up to ten,
  with the time since each last changed. It judges from the Log, the code and the
  cited commit, asks for a live drive only for a line none of them proves,
  and batches drives per wave. A ticket whose work is already in, that
  repeats another or that a later one replaced moves to done or dropped
  with a reason naming the commit or ticket that holds the work.

| Tool | Who gets it | Does |
|---|---|---|
| `ticket_read` | every agent | reads any ticket of the project by id |
| `ticket_log` | every agent | appends evidence to its own ticket's Log |
| `ticket_ask` | every agent | writes a question for a manager into its own ticket's Log |
| `ticket_request` | every agent | writes a request into a board's triage |
| `ticket_move` | every agent | an agent moves its own ticket from doing to review; a manager moves any ticket |
| `ticket_create` | managers | creates a numbered ticket, or a sub-ticket with `parent` |
| `ticket_assign` | managers | assigns a ticket |

With the setting `off`, the default, nothing changes: no board file is
written, no board rule reaches a prompt, and a spawn's owns are the paths
the lead gives it.

## API

| Command | Does |
|---|---|
| `tofu boardy init --key KEY [--name text]` | makes a board |
| `tofu boardy new [--board KEY \| --parent ID] [--type t] [--priority P0..P4] [--status s] [--points n] [--size S\|M\|L] [--spec path] [--owns a,b] [--scope text] [--acceptance text] [--as name] <title>` | makes a ticket with the next number |
| `tofu boardy list [--board KEY] [--status s] [--all] [--fields id,status,assignee,owns]` | lists live tickets, or only the named front matter fields |
| `tofu boardy held <path>` | names the ticket holding a path on any board, and the glob, or says it is not held |
| `tofu boardy show <ticket> [--thread]` | prints one ticket; `--thread` adds its Activity in order |
| `tofu boardy move <ticket> <status> [--reason text]` | changes its status |
| `tofu boardy log <ticket> <text>` | appends a dated line to its Log |
| `tofu boardy ask <ticket> --tldr t --eli5 e --options "a \| b" --recommend n --why w` | puts a question on a ticket |
| `tofu boardy catchup [--board KEY] [--answers]` | lists open questions and writes `catchup.md`; `--answers` applies what the person wrote |
| `tofu boardy lint [--board KEY]` | checks every ticket, exits 1 on a finding |
| `tofu boardy boards` | lists the project's boards |

Every subcommand takes `--json`. Settings: `projectManagement` (`off` or
`boardy`) and `boardFlow` (`jira` or `linear`).
