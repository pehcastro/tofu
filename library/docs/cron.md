---
topic: cron
title: Cron, loops and goals
summary: jobs that post a prompt into the session on a timer or after each turn, how to change one, and the history of every change
---

## What it is

A cron job is a prompt tofu posts into the session by itself. A loop is a
job that fires every few minutes. A goal is a job that fires after every
turn until its check command exits 0. Both are cron jobs, made by shorter
commands, and they share one list, one history and one set of limits.

A fire during a turn reaches the model at its next step, like a line you
type. A fire while tofu is idle starts a turn. The chat shows one row,
`cron c1 fired [expand]`, never a message from you; `[expand]` or `ctrl+o`
opens the prompt in the sub-agents tab. The model reads the prompt as the
schedule's, never as yours, so it approves nothing and is never carried or
learned as your words.
A job the model makes with its `cron` tool fires on time, and a job keeps
firing while one long turn runs.

The orchestrator has a `cron` tool. Under the rule `cron_edits` it may fix
a schedule that misfires, stretch an interval when fires change nothing,
or pause a job. It may not set a check command, and it may not push an
expiry more than one step past yours. Each change it makes is one line in
the chat, recorded as by agent.

## While you are away

A job can carry `away`. The orchestrator sets it with its `cron` tool, on
create or update, when you say you are leaving, going to sleep or away from
the computer and want the work to go on; there is no command for it. The
crons view, the history and the fire line show `away: decides what can be
undone`.

In a turn an away job starts, a sub-agent that reports its ticket not
possible does not stop the work: tofu takes the recommended option, moves
the ticket back, and writes `decided while the person was away`, the option
and why into the ticket's Log and its question. Jev reads the recommended
option first with the `away_decision` question `irreversible`; an option it
reads as one that cannot be undone (a push to a shared remote, a deletion
nobody can restore, a publish, a deploy, money spent, a message sent) is
held in `needs-decision` instead, and its question says why. When Jev
cannot answer or no classifier is bound, the question is held too. The gate
does not relax: a call that would ask you is refused, as in any job's turn.

`tofu boardy catchup` prints every decision taken while away, with the
ticket, the option and why, and each held question with its reason;
`--json` carries the same under `away`, and the catch-up file has a
`Decided while you were away` section. Tell the orchestrator you are back
and it sets `away` to false.

## Where it lives

Each session keeps its jobs in `cron.json` beside its record, under
`~/.tofu/projects/<project>/sessions/<id>/`. Every version of every job is
in that file with its time, who made it and why. Closing the session's
last window, or stopping the project's host, ends every job in it: each
says in that file who closed it and why, and the trace has a `close` row
naming the jobs it ended. `tofu --continue` and `/resume` reopen the
session with no job firing; `/new` starts with none. A job in a session
another window still has open keeps firing there.

Limits: 10 open jobs a session, 50 fires a job, once a minute at most, an
expiry after 24 hours unless you name one, and a check that runs at most
60 seconds. Three fires in a row with the same answer end the job. A turn a
job started cannot run `git push`. A job runs once, in the host of its
session, whichever window made it; `tofu --continue` in another window
joins a session that holds only a job before it first fires, and a fire
past midnight sees the new date.

## Change it

Repeat a prompt every ten minutes:

    /loop 10m check the build

Work until a command passes; the first turn starts at once:

    /goal make the test pass --until "go test ./..."

Fire on a cron line, a macro or a time:

    /cron "0 9 * * 1-5" summarise yesterday's commits
    /cron "@hourly" check the queue
    /cron "once at 17:30" remind me to push

Change a job; a reason is required and becomes the version's reason:

    /cron edit c1 --schedule "every 30m" --reason "the build is stable"

`--prompt "<text>"`, `--expires 48h` and `--until "<cmd>"` change the
rest. A schedule is a 5-field line (minute hour day month weekday),
`@hourly`, `@daily`, `@weekly`, `@monthly`, `every <duration>`,
`once at <15:04>` or `after each turn`. An expired job comes back with
`--expires 24h`; a job that ended any other way does not.

`/cron pause c1` stops a job without losing it, and `/cron resume c1`
starts it again.

## Check it

`/cron` opens the crons view: each job with its schedule, version, next
fire, expiry, fires so far and last result. The status bar shows `cron 1`
while any job is active.

`/cron history c1` shows every version, oldest first:

    v1 · Oct 5 05:14:43 · by person · made by /loop
       every 1m · expires Oct 6 05:14:43 · check the build
    v2 · Oct 5 05:15:44 · by agent · the build is unchanged, so every 10m
       every 10m · expires Oct 6 05:14:43 · check the build

A goal's fire line says why it fired again, such as `not met: go test ./...
exited 1`, and its end says `goal c1 ended: check passed`.

## Undo it

    /cron delete c1

removes a job and its history. A change is undone by another edit, which
adds a version rather than removing one. Deleting `cron.json` from a
session's folder removes every job in it.
