---
topic: boardy
title: Boardy, a ticket board in files
summary: boards of tickets kept as markdown files under the tofu home, numbered under a lock, moved through statuses and checked by lint
---

## What it is

Boardy keeps a project's tickets as plain markdown files, the way a Jira or
Linear board keeps them on a server. A project holds as many boards as it
needs, each with a short key such as `DEMO` or `OPS`, and each board numbers
its own tickets from 1: `DEMO-1`, `DEMO-2`, `OPS-1`. On a board of 1,500 tickets the `boardy` verbs and the `boardy.*` calls of `tofu serve` answer in about half the time they took in 0.5.14, with the same output.

A ticket has front matter (type, status, assignee, priority P0 to P4,
points, owns, depends, blocks, relates, duplicates, epic, sprint, labels,
component, spec, size S, M or L) and four sections: Problem, Scope, Acceptance and Log. A ticket
that goes back from review to doing gets a numbered acceptance revision,
`Acceptance 2`, so every round keeps its own list.

Status is a field: triage, backlog, todo, doing, review, done, dropped,
needs-decision with a question, or
blocked with a reason. Numbers come from the board's counter under a file
lock, and every ticket write takes that ticket's lock, so two processes
never get one number or write one file at the same time.

The setting `boardFlow` picks the words: `jira` (the default) says ticket,
board, epic, sprint and points; `linear` says issue, team, project, cycle
and estimate. Only the words change. The setting `projectManagement` is
`off` by default; `boardy` turns the board on for the rest of tofu.

## Where it lives

    ~/.tofu/projects/<project>/boards/<KEY>/
      board.toml          key, name, managers, holds, created
      counter             the last number handed out
      tickets/<KEY>-<n>.md
      tickets/<KEY>-<n>/<KEY>-<n>-<m>.md   a sub-ticket, beside its own counter
      epics/ milestones/ sprints/
      events.jsonl        every create, move, log line, question, answer,
                          spawn and report, appended
      locks/              held only while a write runs

The board folder counts as the project's own tickets when a tool writes
there, not as a file outside the project.

## Change it

    tofu boardy init --key DEMO
    tofu boardy new --priority P1 --owns "internal/a/**" --acceptance "tofu reads every key" parse the config
    tofu boardy move DEMO-1 doing
    tofu boardy move DEMO-1 blocked --reason "waits on DEMO-2"
    tofu boardy log DEMO-1 parsed every key, the test passes
    tofu settings set boardFlow linear

With more than one board, `new` and `list` take `--board KEY`. Every
subcommand takes `--json`. Sub-tickets, `new --parent DEMO-1`, are in
`tofu docs boardy-views`.

## Agents on the board

With `projectManagement` set to `boardy`, every agent gets `ticket_read`,
`ticket_log`, `ticket_ask`, `ticket_request` and `ticket_move`; a session
that manages a board also gets `ticket_create` and `ticket_assign`. An
agent's `ticket_move` offers only review, and only for its own ticket.

A spawn names a ticket by id. Its owns become the sub-agent's paths, the
ticket is assigned to the sub-agent and moves to doing, and the ticket id
rides on the sub-agent's row, so `--continue` keeps it. The contract reads
the ticket by that id and its last acceptance revision, never the first
ticket-shaped word in the brief. The spawn and the report, with the files
the sub-agent wrote, land on the ticket's thread, and so does a
`ticket_ask`. `ticket_create` takes `parent` to make a sub-ticket.

The lead's session must manage the board: `tofu boardy manager add
<session>`. A session's `tofu boardy` calls act as the session named in
`TOFU_SESSION`.

The rules `board_lead` and `board_sub_agent` ship off and switch on with
the setting in a project that has a board. With it `off`, or with no board
yet, no board rule reaches a prompt: `tofu rules list` says `off until the
project has a board: tofu boardy init`.

## Check it

    tofu boardy boards
    tofu boardy list
    tofu boardy show DEMO-1
    tofu boardy show DEMO-1 --thread
    tofu boardy lint
    tofu boardy held cmd/tofu/main.go --json

`list` shows the live tickets; `--all` adds done and dropped, and
`--status review` shows one status. `--fields id,status,assignee,owns`
prints only those front matter fields, read without the ticket bodies.
`show --thread` adds an Activity section: every event on the ticket in
order, one line each. `held <path>` names the ticket holding a path, on any board of the project,
and the glob that matched, or says it is not held; a hook asks it before a
write. `lint` reads every ticket on every
board and exits 1 on a front matter error, a reference to a ticket no board
holds, a blocked ticket with no reason, a counter behind its tickets, a
sub-ticket owning outside its parent, or two
doing tickets whose owns overlap, naming both: `x/*.go` and `x/*_test.go`
overlap, because both hold `x/a_test.go`.

A ticket holds its owns only while it is doing, where an agent writes.
Every other ticket keeps its owns as a record, so two review tickets may
own the same paths. `holds = ["doing", "review"]` in `board.toml` makes
more statuses hold. An owns entry may be an absolute path outside the
project, such as `F:/work/other-repo/**` or `/home/me/other-repo/**`; it
is checked the same way, case-folded on Windows and macOS.

A ticket waiting on the person's call is `needs-decision`; `tofu boardy
ask` and `tofu boardy catchup` are in `tofu docs boardy-rules`.

## Undo it

Move a ticket back with `tofu boardy move DEMO-1 todo`, or drop it with
`tofu boardy move DEMO-1 dropped`. A ticket file is plain markdown: edit it
in any editor and run `tofu boardy lint`. Deleting a board's folder removes
the board; nothing else refers to it.
