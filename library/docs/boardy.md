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
component, spec, size S, M or L) and its sections: Problem, Scope, Before, Driven path, Still works, Empty or error case, Feasibility, Acceptance and Log. A ticket
that goes back from review to doing gets a numbered acceptance revision,
`Acceptance 2`, so every round keeps its own list.

A ticket is made with Before (how it works now), Driven path (the steps a person performs to see the change), Still works (a line for each neighbour it touches) and Empty or error case (what an empty or failing input does); `new` without one is refused, naming each one missing. When Jev's `ticket_feasibility@1` judges the ask a visual or platform effect, such as a blur, Feasibility is required too: what the platform gives, what it does not, and the source.

An epic made with `tofu boardy epic new` takes its first ticket only after `tofu boardy epic research <ID> <note>` records what two rivals do and the options, and `tofu boardy epic design <ID> <design> --first <smallest first version>` names the smallest first version that runs; each refusal names the step missing. It takes a second ticket only once one of its tickets is done with a Driven path, and the refusal names the ticket waiting. Dropped tickets, triage requests and sub-tickets do not count. An epic made before this step carries no `gated: true` line and takes tickets as before.

A move to done is refused while an acceptance line in force has no evidence entry naming it, a line left for the lead included, and the refusal names the line. It is refused too while the ticket has no before capture: `tofu boardy log <id> --before "<what you ran and what it printed>"`, or `ticket_log` kind before, records how things work before the change, on the same steps as the Driven path, and the Log shows it as `before capture:` beside the evidence. A capture logged after the ticket's first evidence entry is refused as late, and so is one logged after a commit touching the ticket's owns, made since it entered doing, naming the commit. A ticket made with `new --labels docs-only` closes without one, and so does a ticket made before 2026-11-01 05:00 UTC; either way its done reason says why no capture was asked. Jev's `done_reason@1` reads the reason, and one that names a part still open, such as `all done except the dark theme`, is refused, quoting that part. A move with no reason asks nothing. Each Jev answer is a decision ledger row. When Jev cannot answer, an agent's create or move waits for the person, and the person's own goes through with one `nudge` line; with no classifier bound it names `tofu models bind classifier <source/model>`. Every way a ticket is made runs this check: `new`, `ticket_create`, `ticket_request`, a triage accept, a move out of triage, a hand, a hook's `create` effect and `boardy.create` in `tofu serve`; a hook follow up it refuses prints a `nudge` line on the command that fired it. `tofu boardy set <id> --before --driven --still-works --error-case --feasibility` fills the sections of an older ticket or, with `--board KEY`, of a triage request such as `T1`, and its receipt names the sections set. `new --problem` sets the Problem.

Status is a field: triage, backlog, todo, doing, review, done, dropped, needs-decision with a question, or blocked with a reason. Numbers come from the board's counter under a file lock, and every ticket write takes that ticket's lock, so two processes never get one number or write one file at the same time.

The setting `boardFlow` picks the words: `jira` (the default) says ticket, board, epic, sprint and points; `linear` says issue, team, project, cycle and estimate. Only the words change. The setting `projectManagement` is `off` by default; `boardy` turns the board on for the rest of tofu.

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
    tofu boardy new --priority P1 --owns "internal/a/**" --before "tofu ignores the config" --driven "tofu config show" --still-works "tofu run reads its flags" --error-case "an empty config prints the defaults" --acceptance "tofu reads every key" parse the config
    tofu boardy move DEMO-1 doing
    tofu boardy move DEMO-1 blocked --reason "waits on DEMO-2"
    tofu boardy log DEMO-1 parsed every key, the test passes
    tofu settings set boardFlow linear
    tofu boardy set DEMO-2 --acceptance "the list shows every row" --reason "the bug had no acceptance"

`set --acceptance` adds an `## Acceptance` section to a ticket with none, or a line to the one it has; `--reason` is required and is logged with the change. With more than one board, `new` and `list` take `--board KEY`. Every subcommand takes `--json`. Sub-tickets, `new --parent DEMO-1`, are in `tofu docs boardy-views`.

## Agents on the board

With `projectManagement` set to `boardy`, every agent gets `ticket_read`,
`ticket_log`, `ticket_ask`, `ticket_request` and `ticket_move`; a session
that manages a board also gets `ticket_create`, `ticket_assign` and `ticket_list`. An
agent's `ticket_move` offers only review, and only for its own ticket.

A spawn names a ticket by id. Its owns become the sub-agent's paths, the
ticket is assigned to the sub-agent and moves to doing, and the ticket id
rides on the sub-agent's row, so `--continue` keeps it. The contract reads
the ticket by that id and its last acceptance revision, never the first
ticket-shaped word in the brief. The spawn and the report, with the files
the sub-agent wrote, land on the ticket's thread, and so does a
`ticket_ask`. `ticket_create` takes `parent` to make a sub-ticket, and asks for `before`, `driven`, `still_works`, `error_case` and, for a visual or platform effect, `feasibility`.
Evidence names the line it proves with `line`, a move to review names lines left for the lead with `leave`, and `evidence_at_review` refuses review while a line is unmet. The report is one entry: the engineer's last words in two sentences at most, a `verdict` (done, done with concerns, blocked or needs you), and each line as met, unmet or not mine to prove.

With no managers the lead manages the board as the lead, never as you, with
`board_rule`, `epic`, `sprint`, `milestone`, `triage`, and `epic` and `sprint` on `ticket_create`.

The rules `board_lead` and `board_sub_agent` ship off and switch on with
the setting in a project that has a board, and `tofu rules list` says
`off until the project has a board: tofu boardy init` until then. With
them on the lead closes every review oldest first from its Log, code and
commit, drops or merges a dead ticket with a pointer, and batches drives.

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
`show --thread` adds the ticket's conversation, 78% shorter on a recorded run with every acceptance line still proven: a spawn as one handoff line, what each engineer said in at most three sentences, and reports with their verdict and each acceptance line; evidence logged with `log --evidence`, and `--line n` for the line it proves, folds into one count, kept in the Log for review. A sub-agent's ask and the lead's answer read as two people, the ask followed by `why` and `default`, and by `unanswered, went with the default:` when no answer came. Every voice is an `author` with its `role` (pm, engineer, person, hook), `harness` (tofu, claude-code, codex, other) and `model`. `held <path>` names the ticket holding a path, on any board of the project,
and the glob that matched, or says it is not held; a hook asks it before a
write. `lint` reads every ticket on every
board and exits 1 on a front matter error, a reference to a ticket no board
holds, an assignee that is no agent type, a blocked ticket with no reason, a counter behind its tickets, a
sub-ticket owning outside its parent, or two
doing tickets whose owns overlap, naming both: `x/*.go` and `x/*_test.go`
overlap, because both hold `x/a_test.go`.

A ticket holds its owns only while it is doing, where an agent writes.
Every other ticket keeps its owns as a record, so two review tickets may
own the same paths. `holds = ["doing", "review"]` in `board.toml` makes
more statuses hold. An owns entry may be an absolute path outside the
project, such as `F:/work/other-repo/**` or `/home/me/other-repo/**`; it
is checked the same way, case-folded on Windows and macOS. A move to doing reads the callers: when the ticket's title, scope or acceptance names an exported Go or Rust item declared in a file it owns, a use of that item in a file outside the owns refuses the move, naming each file and line and the `tofu boardy set --owns` that adds them. A reason starting `not-owned-on-purpose` moves it anyway and records the files.

A ticket waiting on the person's call is `needs-decision`; `tofu boardy
ask` and `tofu boardy catchup` are in `tofu docs boardy-rules`.

## Undo it

Move a ticket back with `tofu boardy move DEMO-1 todo`, or drop it with
`tofu boardy move DEMO-1 dropped`. A ticket file is plain markdown: edit it
in any editor and run `tofu boardy lint`. Deleting a board's folder removes
the board; nothing else refers to it.
