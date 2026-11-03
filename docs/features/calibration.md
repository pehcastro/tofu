---
title: Calibration
description: A decision point leaves shadow only on a lock that tofu calibrate fitted on labelled cases and checked on a held-out half and a rerun.
order: 23
updated: 2026-11-03
---

A decision point that declares `mode: enforced` still runs in shadow until
a lock file says its thresholds were fitted on recorded cases and held up.
`tofu calibrate` writes that lock, at
`~/.tofu/projects/<project>/calibration/<rule>@<version>.lock`.

## Why a lock is a measurement

**A cut that only fits its own data is refused.** The verb fits
`risk_ask_at` on one half of the labelled cases and scores it on the other.
If the two agree to within 0.02, it goes on.

**A cut must beat switching the gate off.** On the held-out half, the
blocks it catches against the proceeds it wrongly blocks must pass a sign
test at p under 0.05. A corpus too thin in its rarer label to ever pass
that is refused before any call.

**A cut must be steadier than the classifier is noisy.** Every case is
asked a second time, and the verdicts must agree at least as often as the
fit agrees with the labels.

**The lock says what was measured.** The rule and question versions, the
jev build, the map (`identity`), both agreements, the rerun agreement, the
calibration error over the held-out half, the case counts and a sha256 of
the corpus. The resolver still holds the point in shadow when the build,
the question version or the sample size does not match, and `tofu doctor`
names the lock and the reason on the point's row.

## Calibrating a point

- **Count the calls**: `tofu calibrate tool_gate@6 --cases cases.jsonl --split split.json --dry-run`.
- **Fit and write the lock**: the same command without `--dry-run`.
- **See the result**: `tofu doctor`, under `rules` and `state`.
- **See what it cost**: `tofu usage`, under `classifier`.
- **Undo it**: delete the lock file.

## Commands

```sh
tofu calibrate tool_gate@6 --cases cases.jsonl --split split.json
```

```text
✓ tool_gate@6 calibrated
  ~/.tofu/projects/demo/calibration/tool_gate@6.lock
cases        16 fit, 16 held out, 64 jev calls on jev-1.14.0
risk_ask_at  1, fitted
agreement    1.0000 fit, 1.0000 held out, 0.5000 with the gate off
against off  p 0.0039
rerun        1.0000 of verdicts the same on a second ask
calibration  error 0.0006 on the held-out half, 10 bins
```

Each case is one JSON line with `id`, `label` (`proceed` or `block`),
`origin` and `state`; the split file lists the held-out ids under
`heldout`. The verb takes `--json`.
