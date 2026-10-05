---
title: The ledger
description: Every classifier verdict is an appended row you can explain, correct, and replay against new thresholds without a model call.
order: 7
updated: 2026-10-04
---

The ledger is the record of every classifier decision, one folder per
project at `~/.tofu/projects/<project>/log/`. Each row holds the decision
point, the state that was judged, each answer, the verdict, the threshold
that decided it, the mode (shadow or enforced), the classifier build, the
latency and the cost. Rows go to one file per day, and only ever get
appended.

## Why every verdict is kept

**A verdict you can't explain is a verdict you can't trust.** `tofu why`
shows which answer crossed which threshold, and the earlier rows whose
answers were closest.

**Answers are kept, so a threshold change is a recount.** `tofu replay`
scores the recorded answers again against the value you name. It rescored
1,464 gate decisions with no model call and no cost, before anything was
changed.

**Labels sit beside the row.** `tofu label` writes what a verdict should have
been to a separate outcome file, so the original row is never edited.

A key in a judged state is written as `[key redacted]`.

## Explaining, labelling and replaying

- **Explain the last verdict**: `tofu why --last`, or one point with
  `--point stop_check`.
- **See the whole state**: `tofu why <id> --state`.
- **Correct a verdict**: `tofu label --last allow` or `tofu label <id> deny`.
- **Test a threshold**: `tofu replay --point tool_gate --set risk_ask_at=1.5 --since 30d`.

## Commands

```sh
tofu replay --point tool_gate --set risk_ask_at=1.5 --since 30d
```

```text
Replay · tool_gate · 1476 rows                              ✓ no verdict changes

  set          risk_ask_at=1.5
  rescored     1464
  no rule      11
  unavailable  1
```

Each verb takes `--json`.
