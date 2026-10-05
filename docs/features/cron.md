---
title: Cron, loops and goals
description: One scheduler posts a prompt into your session on a timer or after each turn, and every change to a job is kept as a version with its reason.
order: 11
updated: 2026-10-05
---

A cron job is a prompt tofu posts into your session by itself: on a timer,
once at a time you name, or after every turn until a check passes. `/loop`
and `/goal` are two short ways to make one. They are not separate systems:
a loop is a job that fires every few minutes, and a goal is a job that fires
after each turn and stops when its check command exits 0.

Each job has an id like `c1`, a schedule, a prompt, an expiry and, for a
goal, a check. It lives in the session, so `tofu --continue` brings it back.

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
The chat shows one line naming the job, and the model reads the prompt as
the schedule's, never as yours.

## Guards

- **An expiry on every job.** A job without one expires after 24 hours.
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
- **Pause, resume or delete**: `/cron pause c1`, `/cron resume c1`, `/cron delete c1`.

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
