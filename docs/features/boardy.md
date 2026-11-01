---
title: Boardy
description: Your own Jira or Linear, kept as markdown files on your disk, with numbered tickets and sub-tickets, statuses, a thread of what happened on each, and a lint that catches two tickets claiming one file.
order: 18
updated: 2026-10-31
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

**A move to doing checks the callers.** When a ticket's title, scope or
acceptance names an exported Go or Rust item declared in a file it owns,
`tofu boardy move <id> doing` reads the project's other Go and Rust files
for a use of it. A use outside the owns refuses the move and names each
file and line, with the command that adds them to the owns. To move it
anyway, give a reason starting `not-owned-on-purpose`, and the files go on
the ticket's record.

**Your words.** Set `boardFlow` to `jira` (ticket, board, epic, sprint,
points) or `linear` (issue, team, project, cycle, estimate). The files are
the same either way.

## How to

Make a board and three tickets:

```
tofu boardy init --key DEMO
tofu boardy new --priority P1 --owns "internal/a/**" --before "tofu ignores the config" --driven "tofu config show" --still-works "tofu run reads its flags" --error-case "an empty config prints the defaults" --acceptance "tofu reads every key" parse the config
tofu boardy new --type bug --before "the list shows 9 of 10 rows" --driven "tofu boardy list" --still-works "list --json" --error-case "an empty board says so" the list drops a row
tofu boardy new --owns docs/x.md --before "no page" --driven "open docs/x.md" --still-works "the index links it" --error-case "a missing image is named" write the page
```

A ticket is made with four sections a person drives: **Before**, how it
works now; **Driven path**, the steps that show the change; **Still works**,
a line for each neighbour the change touches (repeat `--still-works`); and
**Empty or error case**, what an empty or failing input does. `new` without
one of them is refused, naming each one missing:

```
refused: "a plain ticket" has no Before, Driven path, Still works, Empty or error case, and a ticket is made with each of them.
```

A ticket asking for a visual or platform effect, such as a blur or a
transparency, also needs **Feasibility** (`--feasibility`): what the platform
gives, what it does not, and the source. Jev decides whether the ask is one,
with the library question `ticket_feasibility@1`, and each answer is a row in
the project's decision ledger:

```
refused: Jev judged that "blur behind the desk sidebar" asks for a platform or visual effect (ticket_feasibility@1, platform at 0.97 against 0.50), and such a ticket is made with a Feasibility section
```

When Jev cannot answer, an agent's create waits for the person, and the
person's own create goes through with one line saying it was not judged.
With no classifier bound that line names the fix:

```
nudge ticket_feasibility@1: "list sorts by priority" went ahead unjudged: no classifier is bound, so Jev did not answer: tofu models bind classifier <source/model>
```

Every way a ticket is made runs the same check: `new`, `ticket_create`,
`ticket_request`, a triage accept, a hand to another board, a hook's
`create` effect, a move out of triage and the `boardy.create` call of
`tofu serve`. A hook whose follow up lacks the sections prints one line on the
command that fired it, and the same line goes on the ticket's thread:

```
nudge follow: create follow up DRV-2 did not run: refused: "follow up DRV-2" has no Before, Driven path, ...
```

An epic runs one small version before it grows. A new epic takes its first
ticket only after a research note, what two rivals do and the options, and a
design naming the smallest first version that runs:

```
tofu boardy epic new THEME themes
tofu boardy epic research THEME zed reads one json file, helix one toml file
tofu boardy epic design THEME one theme file read at start --first the chat colour from the file
```

It takes a second ticket only once one of its tickets is done with a Driven
path, and the refusal names the ticket waiting. Dropped tickets, triage
requests and sub-tickets do not count, and an epic made before this step
takes tickets as it always did.

`tofu boardy set <id> --before text --driven text --still-works line
--error-case text --feasibility text` fills the sections of a ticket made
before they existed, the Log records each one, and the receipt names the
sections it set. Given a triage request, `set T1 --board DRV ...` fills the
request the same way, so it can be accepted rather than dropped.

Work one through review:

```
tofu boardy move DEMO-1 doing
tofu boardy move DEMO-1 review
tofu boardy log DEMO-1 parsed every key, the test passes
tofu boardy show DEMO-1
```

A ticket reaches done only when nothing it asked for is open. A move to
done is refused while any acceptance line in force has no evidence entry
naming it, a line left for the lead included, and the refusal names the line:

```
DEMO-3 refused: 1 of 2 acceptance lines in force have no evidence entry naming them: line 2, the dark theme shows the priority colour
```

A move to done needs a before capture too: how things work before the
change, on the same steps as the Driven path, logged before the ticket's
first commit. The Log shows it as `before capture:` beside the evidence:

```
tofu boardy log DEMO-3 --before "tofu boardy list printed no priority column"
```

A capture logged after the ticket's first evidence entry is refused as late,
and so is one logged after a commit touching the ticket's owns, made since it
entered doing, naming the commit. A ticket made with
`--labels docs-only` closes without one, and so does a ticket made before
2026-11-01 05:00 UTC; either way its done reason says why no capture was
asked.

The reason is read too. Jev asks `done_reason@1` whether it names a part
still open, and a reason such as `all done except the dark theme, follow up
later` is refused, quoting the part:

```
DEMO-3 refused: its reason says "all done except the dark theme" is still open (Jev, done_reason@1, open at 0.99 against 0.50)
```

Make the open part its own ticket and keep the first open, or close it with
a reason that names nothing open. A move with no reason asks nothing, and
when Jev cannot answer, an agent's move waits for the person and the
person's own move goes through with the same `nudge done_reason@1` line.

Add an acceptance line to a ticket that has none, or one more to a ticket
that does. `--reason` is required, and the change and its reason go on the
ticket's Log:

```
tofu boardy set DEMO-2 --acceptance "the list shows every row" --reason "the bug had no acceptance"
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
where it was once nothing is left to answer. The answered question stays on
the ticket under Questions with an `Answer:` line beneath it, and catchup,
the audit and the board's rows count only the questions still open. A move to `needs-decision`
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

The thread reads as the team talking, and on a recorded run it is 78%
shorter than before, with every acceptance line still carrying its evidence.
A spawn is one handoff line from the lead naming the engineer and its
mission, with the full brief kept in its
`brief` field. An engineer says what it decided, found or needs in at most
three sentences, and logs proof apart with `tofu boardy log --evidence` (or
`ticket_log` kind evidence), with `--line n` (or `line`) naming the acceptance
line it proves, which `show --thread` folds into one count and the Log keeps
for review. A move to review names the lines left for the lead in `leave`,
and the `evidence_at_review` hook refuses it while a line is unmet. The report
is one entry: the engineer's last words in at most two sentences, a `verdict`
(done, done with concerns, blocked or needs you), and each acceptance line as
met, unmet or not mine to prove. Empty moves stay out of the thread.

A sub-agent's ask and the lead's answer read as two people. The ask prints
its question, then `why`, the reasoning behind it, and `default`, what the
engineer will do without an answer. When the lead's model gives no answer in
time, the thread says `unanswered, went with the default:` and the reason, so
an assumption is never mistaken for a decision. `tofu session trace` lists the
side call that answered it.

Every voice says who it is. Each event's `author` carries a `role` (`pm`,
`engineer`, `person` or `hook`), the `harness` it ran in (`tofu`,
`claude-code`, `codex` or `other`), the `model` that spoke, and for an
engineer its agent type and number, such as `go-dev` and `2`. A
`tofu boardy` command run from Claude Code or Codex is credited to that
harness and its session.

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

## Before

## Driven path

## Still works

## Empty or error case

## Feasibility

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
| `ticket_create` | managers | creates a numbered ticket, or a sub-ticket with `parent`; asks for `before`, `driven`, `still_works`, `error_case` and, for a visual or platform effect, `feasibility` |
| `ticket_assign` | managers | assigns a ticket |
| `ticket_list` | managers | lists a board's tickets by status, oldest first with the time since each last changed, up to ten and how many more |

With the setting `off`, the default, nothing changes: no board file is
written, no board rule reaches a prompt, and a spawn's owns are the paths
the lead gives it.

## API

| Command | Does |
|---|---|
| `tofu boardy init --key KEY [--name text]` | makes a board |
| `tofu boardy new [--board KEY \| --parent ID] [--type t] [--priority P0..P4] [--status s] [--points n] [--size S\|M\|L] [--spec path] [--owns a,b] [--problem text] [--scope text] --before text --driven text --still-works line... --error-case text [--feasibility text] [--acceptance text] [--as name] <title>` | makes a ticket with the next number |
| `tofu boardy list [--board KEY] [--status s] [--all] [--fields id,status,assignee,owns]` | lists live tickets, or only the named front matter fields |
| `tofu boardy held <path>` | names the ticket holding a path on any board, and the glob, or says it is not held |
| `tofu boardy show <ticket> [--thread]` | prints one ticket; `--thread` adds its Activity in order |
| `tofu boardy move <ticket> <status> [--reason text]` | changes its status; done needs evidence on every line, a before capture unless it is labelled docs-only, and a reason naming nothing open |
| `tofu boardy log <ticket> <text> [--evidence [--line n]]` | appends a dated line to its Log; `--evidence` marks it as proof, and `--line` names the acceptance line it proves |
| `tofu boardy log <ticket> --before <text>` | records how things work before the change, refused once a commit touched the ticket's owns |
| `tofu boardy ask <ticket> --tldr t --eli5 e --options "a \| b" --recommend n --why w` | puts a question on a ticket |
| `tofu boardy catchup [--board KEY] [--answers]` | lists open questions and writes `catchup.md`; `--answers` applies what the person wrote |
| `tofu boardy lint [--board KEY]` | checks every ticket, exits 1 on a finding |
| `tofu boardy boards` | lists the project's boards |

Every subcommand takes `--json`. Settings: `projectManagement` (`off` or
`boardy`) and `boardFlow` (`jira` or `linear`).
