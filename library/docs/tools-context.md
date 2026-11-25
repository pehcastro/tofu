---
topic: tools-context
title: Tools for the agent's own context and its own work
summary: what context, budget, forget, note, digest, done, claims and the review agent give the model
verbs: tool
---

## What it is

These tools let the model look after its own window and check its own
work before you read about it.

- `context` reports what fills the model's own window: tokens used
  against the ceiling as the last request measured them, the window by
  kind, the biggest results with their call ids, and every file read that
  changed on disk since.
- `budget` reports calls, weighted tokens, USD and model and wall time for
  the turn, the session, the lead and each sub-agent, counted as `tofu
  session drain` counts them.
- `estimate_time` says how long a piece of work will take in all, with no
  model call: the total, the done-by promise, and the agent, test, build
  and verify parts, each with its basis. It reads the 10 newest finished
  runs of the same agent (the lead, or the named sub-agent) on the same
  model here; with none it says so and widens to the agent on other
  models, then other projects, then every agent on the model. A brief
  within the length of those runs' briefs is matched to the nearest half.
  Named with the id of a running sub-agent, it says the elapsed time and
  what remains, from the runs that lasted that long. The runs are read
  from the project's `settled-runs.jsonl`, one line per finished run with
  its agent, scope and split, built once from the sessions when missing.
- `forget` drops results the model names by call id, each left as one
  line, and says the tokens freed and the cache cost. Only a result after
  the last cache breakpoint can go. When a request passes 70% of the
  context ceiling, the lead is told the largest result `forget` can still
  drop, once each time the line is crossed.
- `note` keeps one line of at most 500 bytes word for word through every
  fork and compaction, up to 20 notes. `digest` answers one question about
  one recorded result with the model `modelTier.dumb` names.
- `forget` and `note` run only inside a turn. `history` in mode `lookup`
  brings back whole a result `forget` dropped.
- `done` takes the task's acceptance lines, each with `call`, the call
  that checked it. A line reads `yes` when that call exited 0, or a `test`
  call passed, and Jev reads the call as checking that line; `no` when the
  call failed; and `not_checked` otherwise. The turn does not end while a
  line reads no: it gets one grace turn, then ends with a warning naming
  the line. With Jev unreachable, a line that would read yes reads
  `not_checked` and says why.
- `claims` takes a report and Jev reads each sentence as `shown` with the
  call id, `not_shown`, or `contradicted` with the call whose output says
  it is false. The first 20 sentences are checked against the latest 40
  calls.
- `review` is a read-only sub-agent the lead spawns with the task. It reads
  the turn's changes through `changes`, in a fresh context, and returns
  findings with a `path:line` range and a kind: `bug`, `unused`,
  `untested`, `off_task` or `comment`, ending with `findings: N`.

```text
done: 2 lines, 1 yes, 1 no, 0 not_checked
  yes         cassette_1_1 exit 0, jev reads 0.93 that it checks this line, against 0.50: the total test passes
  no          cassette_1_2 exit 1: the broken package passes its tests
the turn does not end while a line reads no: fix it, run its check again, and call done with the new call id
```

## Where it lives

Each answer reads the recorded session, and each Jev answer `done` and
`claims` get is a row in the decision ledger; see `tofu docs sessions`.

## Change it

`digest` needs a cheap model bound, and is refused naming the setting
without one:

    tofu settings set modelTier.dumb <source/model>

## Check it

    tofu tool context '{}' --dir . --session head
    tofu tool budget '{}' --dir . --session head
    tofu tool claims '{"report":"All tests pass."}' --dir . --session head
    tofu tool list --agent review

## Undo it

A dropped result is not lost: `history` in mode `lookup` brings it back.
Clear the binding to turn `digest` off again:

    tofu settings set modelTier.dumb ""
