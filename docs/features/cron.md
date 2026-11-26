---
title: Cron, loops and goals
description: One scheduler posts a prompt into your session on a timer or after each turn, and every change to a job is kept as a version with its reason.
order: 11
updated: 2026-11-25
---

A cron job is a prompt tofu posts into your session by itself: on a timer,
once at a time you name, or after every turn until a check passes. `/loop`
and `/goal` are two short ways to make one. They are not separate systems:
a loop is a job that fires every few minutes, and a goal is a job that fires
after each turn and stops when its check command exits 0.

Each job has an id like `c1`, a schedule, a prompt, an expiry and, for a
goal, a check. It lives in the session and ends when the session closes:
closing the last window that has it open ends every job, so
`tofu --continue` reopens the session without a job firing.

## Why one scheduler

**One place to look.** Loops, goals and timed jobs share one list, one
history and one set of limits, so the crons view shows everything that will
start a turn without you.

**Every change is a version.** Changing when a job fires, its prompt or its
expiry writes a new version with who made it, `person` or `agent`, and why.
The history shows each one, so a schedule that drifted can be read back.

**The orchestrator can tune it, inside a rule.** The model has a `cron` tool.
The rule `cron_edits` lets it fix a schedule that misfires, stretch an
interval when the last fires changed nothing, or pause a job that no longer
fits. Each change is one line in the chat and is recorded as `by agent`.

**A fire never interrupts you.** During a turn, the prompt reaches the model
at its next step, like a line you type. When tofu is idle, it starts a turn.
The chat shows one row, `cron c1 fired [expand]`, never a message from
you; `[expand]` or `ctrl+o` opens the full prompt under the orchestrator in
the sub-agents tab. The model reads the prompt as
the schedule's, never as yours: it is not carried, remembered or learned as
something you said. A job the orchestrator makes with its `cron` tool fires
on time too, and a job keeps firing while one long turn runs.

## While you are away

Tell the orchestrator you are leaving, going to sleep or away from the
computer. It sets the session away with its `cron` tool, action `away` and
the reason, and makes a job that brings the work back while you are gone if
you want it to go on. There is no command to type and no switch to
remember. The chat says `the person is away, set by agent: <reason>`,
`tofu session trace` has a `cron` row with the reason, and the session keeps
the state in its `cron.json`, so a resumed session stays away. A job that
fires while you are away says `away: decides what can be undone` on its
fire line.

Away covers the whole session, not one job. Every turn while you are away
does not wait for you, whatever started it: a cron fire, a sub-agent's
report, a shell notice or a check-in.

- **No form opens.** A call to `ask_person` answers `person_away`, the
  chat shows `answered as person_away`, and the lead takes the option it
  recommended. A question marked `needs_yes` is the exception: nothing is
  chosen, and it waits on your answer when you are back.
- **A sub-agent's question is decided.** When a sub-agent reports its ticket
  not possible, tofu takes the option it recommended and the ticket goes back
  to work. The ticket's Log and its question say it was `decided while the
  person was away`, which option, and why.
- **What cannot be undone still waits.** Jev reads the recommended option
  with the `away_decision` question `irreversible`: pushing to a shared
  remote, deleting what nobody can restore, publishing, deploying, spending
  or sending a message. At or above `irreversible_at` the question stays in
  `needs-decision` and says why it was held, with Jev's reading. When Jev
  cannot answer, or no classifier is bound, the question waits too.
- **Catch up lists both.** `tofu boardy catchup` prints every decision taken
  while you were away, with the ticket, the option and why, and each held
  question with the reason it was held. `--json` carries the same under
  `away`, and the catch-up file has a `Decided while you were away` section.
- **The gate does not relax.** A tool call that would ask you is refused,
  saying the person is away until they say they are back, and `git push`
  stays blocked. A memory that would ask you where to keep it is not kept.
- **The footer says so.** The status line shows `away` beside the cron count
  while the session is away.

Tell the orchestrator you are back and it turns away off with action
`back`: the chat says `the person is back, set by agent: <reason>`, the
trace has the `cron` row, and the next `ask_person` opens a form again.

## Guards

- **An expiry on every job.** A job without one expires after 24 hours.
- **One process fires it.** A job runs in the project's `tofu host`,
  whichever window made it, even a window that has sent nothing yet, and
  fires once however many windows follow, and `tofu --continue` in another
  window joins a session that holds only a job before it first fires. Past
  midnight a fire sees the new date.
- **A close ends every job.** Closing the last window on a session, or
  stopping the project's host, ends each job with who closed it and why, and
  the trace's `close` row names the jobs it ended. Closing one of two windows
  leaves the jobs firing in the other.
- **A missed fire runs once.** After an hour busy or asleep, a 1-minute loop
  fires once, not sixty times.
- **No overlap.** A job does not fire again while the turn it started runs.
- **A stop when nothing changes.** Three fires in a row with the same answer
  end the job.
- **Caps.** At most 10 open jobs a session and 50 fires a job, and nothing
  more often than once a minute.
- **No push.** A turn a job started cannot run `git push`.
- **The check is yours.** A check command runs without the gate, so only you
  set one. The tool refuses it from the model, and refuses an expiry pushed
  more than one step past yours.
- **A check that cannot run ends the job** and says why, rather than counting
  as passed.

## Making and changing jobs

- **Repeat a prompt**: `/loop 10m check the build`.
- **Work until a check passes**: `/goal make the test pass --until "go test ./..."`.
  The first turn starts at once, and the check runs after every turn.
- **A cron line or a time**: `/cron "0 9 * * 1-5" summarise yesterday's commits`,
  `/cron "once at 17:30" remind me to push`, or `/cron "@hourly" check the queue`.
- **Change a job**: `/cron edit c1 --schedule "every 30m" --reason "the build is stable"`.
  `--prompt`, `--expires 48h` and `--until "<cmd>"` change the rest. A reason is required.
  An expired job comes back with `--expires 24h`; a job that ended any other way does not.
- **Pause, resume or delete**: `/cron pause c1`, `/cron resume c1`, `/cron delete c1`.

Jobs live in the session's `cron.json`, written only when a job fires, ends, expires or is changed, so a quiet session never rewrites it.

## Commands

```text
/cron
```

```text
Crons                                                       esc

cron c1 · every 1m · v1 · next Oct 5 05:15:43 (in 1m) · expires Oct 6 05:14:43 · fired 0
```

`/cron` and `/cron list` open the crons view: each job with its schedule,
version, next fire, expiry, fires and last result. The status bar shows
`cron 1` while any job is active.

```text
/cron history c1
```

```text
History of cron c1                                          esc

v1 · Oct 5 05:14:43 · by person · made by /loop
   every 1m · expires Oct 6 05:14:43 · check the build
v2 · Oct 5 05:15:44 · by person · the build keeps passing, so check less often
   every 2m · expires Oct 6 05:14:43 · check the build
```

A fired job shows in the chat as one line, `cron c1 fired · every 1m · check
the build`, and a goal's line adds why it fired again, `not met: go test ./...
exited 1`. A job that ends says so: `goal c1 ended: check passed`.
