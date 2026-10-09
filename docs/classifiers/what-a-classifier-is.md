---
title: What a classifier is
description: A small model that answers typed questions at a decision point, how its answers become allow, ask or deny, and why most points stay in shadow.
order: 1
updated: 2026-10-09
---

A classifier is a small model that answers a fixed set of typed questions
about a state and returns numbers, not prose. tofu asks one at a **decision
point**, a place in the loop where it must choose, such as whether a tool
call may run. Each point is three files in the library: the **state** it
reads, the **questions** it asks, and a **rule** with thresholds and a mode.

## How answers become a verdict

The answers are typed: a `score` on a scale, a `noul` (a yes or no as a
number from 0 to 1), or a `choice` from named options. For the tool gate the
rule turns them into a verdict:

1. The `risk` score sets the base: above 1.5 is ask, above 2.5 is deny,
   otherwise allow. Within 0.06 of a cut it asks, because a reading that
   close could fall either way.
2. A `user_requested` clear of 0.85, or an `approval` clear below 0.15,
   relaxes the verdict one step: deny to ask, ask to allow. "Clear" means
   past the same 0.06 band.
3. A `from_untrusted` near 0.5 or above, an instruction planted in a page or
   a file, blocks any relaxing.

## Why a classifier, and why most stay in shadow

Other harnesses decide these points with a prose rule in the prompt, a
regular expression, or a guess from the big model. A classifier is cheaper
and faster than the big model: on six tool calls Jev cost 0.000041 USD per
correct decision in 299 to 931 ms, where Opus cost 0.005351 USD in 2.5 to
4.8 s, both 6 of 6. Every point also has a **free arm** that does the same
job with no call, and a classifier acts only where it beats that arm.

So a point runs in one of two modes:

- **Shadow**: asked and recorded, and nothing changes.
- **Live**: the verdict acts. A rule that asks to be live stays in shadow
  until a calibration lock pins the classifier build it was fitted on, with
  enough samples; a new build drops it back to shadow.

## Acting on verdicts

- Make the tool gate act: in `/settings`, **Interaction**, set `gatePrompt`
  to `ask`. Under `ask` you answer **1** to allow once, **2** to deny, **3**
  always here, **4** never here, or **5** to cancel the turn. Always here
  and never here stand for that session, also after a restart or
  `tofu --continue`. See [Asking you](/docs/features/asking).
- Turn a point off for one run: `--no-gate`, `--sift free`.
- Correct a verdict: `tofu label --last allow`.
- Try other thresholds on past verdicts, with no network: `tofu replay`.

## Reading the ledger

```
tofu why <id> | --last [n] [--point name] [--state | --json]
tofu label <id> allow|ask|deny | tofu label --last allow|ask|deny
tofu replay --point name [--set threshold=value]... [--since 7d]
tofu check "<command>"
```

`tofu why --last --point tool_gate`:

```
tool_gate · 2026-09-26-61ea7fbe65152513c4e7c5298c7ec713 · 8d 6h ago      ✓ ALLOW

  approval        ▓░░░░░░░░░░░  10%
  from_untrusted  ░░░░░░░░░░░░   2%
  risk            chose 0
    0             ▓▓▓▓▓▓▓▓▓▓▓▓ 100%
  user_requested  ▓░░░░░░░░░░░   7%

  threshold    risk 0.00 vs risk_ask_at 1.50 · tool_gate@3
  mode         shadow
  build        typesafe/jev-1.13-20260917
```
