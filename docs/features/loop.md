---
title: The loop
description: tofu's own code runs the loop of steps, decides when a turn ends, and stops a turn that repeats itself.
order: 1
updated: 2026-10-04
---

The loop is the part of tofu that drives a turn. Each step, tofu sends the
conversation and the tool definitions to the model. The model answers with
text, which ends the turn, or with tool calls, which tofu runs, records and
appends to the conversation before the next step.

![A task running in the chat tab, handed to go-dev-1](./media/tui-task-working.png)

Read-only calls in one answer run side by side, up to 4 at a time. Writes,
edits and shell commands run one at a time, in order. Before each step, tofu
adds anything you typed and any sub-agent report from the inbox.

A turn ends on a final message, on the step cap, on the loop guard, or on an
error. On a cap or the guard, tofu asks the model once more for what it did,
what is left and what to do next.

## Why code decides when to stop

The model decides what to do; code decides when to stop. A model that is
asked to judge its own progress keeps going or quits early. A loop in code
can't do either: on a task that needs a second step, tofu's loop finished 3 of
3 where one model call finished 0 of 3.

**The loop guard** stops a turn when the same call returns the same result 3
times within 6 calls. A model stuck on a failing command burns steps and
money without learning anything new.

**The stop check** reads a sub-agent's steps when it says it's done, and a
classifier judges whether work remains. It agrees with 71 of 78 hand labels,
against 68 for a check that only looks for repeated commands. It runs when
you ask for it; by default the language gate does the sending back.

## Stopping and steering a turn

- **Stop a turn**: `Esc` with an empty composer, or `Ctrl+C`.
- **Steer a running turn**: type and send; the message reaches the model at
  its next step.
- **Cap the steps**: `tofu settings set decisionCap 50`. `0`, the default,
  means no cap. A sub-agent stops at 150 steps.
- **Turn the stop check on**: `tofu run --done-review typed` for one run.

## Commands

```sh
tofu why --last --point stop_check
```

```text
stop_check · 2026-09-25-7395f9499b3d5d43e0a547a0e1e545cd · 8d 21h ago    ✓ ALLOW

  budget_exhausted  ▓░░░░░░░░░░░   5%
  stalled           ▓▓▓▓▓░░░░░░░  45%
  stop_pressure     chose 0.63
    0               ▓▓▓▓▓▓░░░░░░  50%
    1               ▓▓▓▓▓░░░░░░░  42%
    2               ░░░░░░░░░░░░   2%
    3               ▓░░░░░░░░░░░   6%
  work_remains      ▓▓▓▓▓▓▓▓▓▓░░  87%

  threshold    stop_pressure 0.63 vs risk_ask_at 1.50 · stop_check@1
```

An `ALLOW` here sends the sub-agent back to work. `tofu run` takes
`--max-steps`, `--loop-guard-repeats` and `--loop-guard-window` for one run.
