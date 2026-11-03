---
topic: calibration
title: Calibration
summary: how tofu calibrate fits a decision point's cut on labelled cases, checks it on a held-out half and a rerun, and writes the lock that lets the point leave shadow
verbs: calibrate, doctor
---

## What it is

A decision point declared `mode: enforced` stays in shadow until a lock
file says its thresholds were fitted and checked on labelled cases.
`tofu calibrate` writes that lock. It asks jev about every case, fits
`risk_ask_at` on the fit half, and checks the cut three ways before it
writes anything:

- the held-out half agrees within 0.02 of the fit half
- on the held-out half the cut beats switching the gate off, by a sign
  test at p under 0.05
- a second ask of every case gives the same verdict at least as often as
  the fit agrees with the labels

Before it asks anything, it refuses a corpus whose rarer label is too thin
in either half for that sign test ever to pass, and a corpus whose cases
all came from one origin. A refusal writes no lock and names the check
that failed and the calls it spent.

## Where it lives

The lock is `~/.tofu/projects/<project>/calibration/<rule>@<version>.lock`.
It records:

- the rule and its version, the question set and its version
- the jev build that answered every ask
- `map: identity`, since the cut is fitted on the raw score
- when the cut was fitted and when it was verified
- the case count and the agreement of each half, and the rerun agreement
- `calibration_error`, the expected calibration error over the held-out
  half in ten bins, where a case's block probability is the risk answer's
  mass above the fitted `risk_ask_at`
- a sha256 of the cases and the split together
- the five thresholds it pins

Each case is one JSON line with `id`, `label` (`proceed` or `block`),
`origin` and `state`. The split file lists the held-out ids under
`heldout`, and every other case is the fit half. Every ask is a ledger row.

## Change it

Count the calls first; nothing is asked:

    tofu calibrate tool_gate@6 --cases cases.jsonl --split split.json --dry-run

Then fit, check and write the lock:

    tofu calibrate tool_gate@6 --cases cases.jsonl --split split.json

It prints the lock path, the fitted cut, the agreement of each half beside
the agreement with the gate off, the sign test and the rerun agreement.
Calibrating again replaces the lock.

## Check it

    tofu doctor

names the points that have a lock under `state`. Under `rules`, a point
with a lock names it, the cut it fits, and why the point is still in
shadow: declared shadow, a lock for another build or question version, or
a sample under the rule's `sample_floor`. The shipped gate rules, tool_gate
among them, set that floor to 300, so the smaller of the fit half and the held-out half needs at
least 300 cases before the lock can lift a point out of shadow.
`tofu calibrate --json` prints the fit as one JSON document.

    tofu usage

ends with a `classifier` section: the jev calls of the last 24 hours and
what they cost, charged and at list price apart, since the two do not add.

## Undo it

Delete the lock file. The point goes back to shadow, and `tofu doctor`
says `no lock file` again.
