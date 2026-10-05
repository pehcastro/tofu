---
title: Decision points
description: Each place tofu can ask a classifier, what it decides, whether it is live or in shadow, and why.
order: 2
updated: 2026-10-04
---

A decision point is one choice in the loop that a classifier can make. One
file, `library/decisions/methods@1.yaml`, names the method for each point:
`judged` (the classifier decides), `cheap` (the free arm decides), or
`unwired` (nothing is asked). The rule beside each point says whether a
judged point is live or in shadow.

| Point | Decides | Method | Mode |
|---|---|---|---|
| Shell sift | which chunks of a command's output the model still needs | judged | live |
| Browser step | the next action toward a goal in a tab | judged | live under `browserDriver goal` |
| Stop check | whether a turn should stop or take another step | judged | shadow |
| Tool gate | whether a tool call runs, asks or is refused | unwired by default | shadow, live under `gatePrompt ask` |
| Read worth | which paragraphs of a message are worth reading | cheap | free arm |
| Page sift | which parts of a fetched page to keep | unwired | off |
| Ask gate | whether a sub-agent asks or proceeds at a moment of doubt | unwired | shadow |
| Instruction trust | whether outside text reads as an instruction | unwired | off |

## Why each point runs the way it does

Each point runs the way its measurements support.

- **Shell sift** kept 30 of 34 planted facts where a fixed cut of error lines
  and the ends of the output kept 18, so the line that mattered reaches the
  model. Standard error, the exit line and the first and last chunk are never
  cut. With Jev it costs a median 0.00054 USD a session.
- **Browser step** picked the right action on 8 of 8 recorded pages at
  316 ms a decision, and later fixes took it from 48 decisions per finished
  action to 7. By default the model drives the tab; the classifier picks the
  steps under `browserDriver goal`.
- **Stop check** agreed with 71 of 78 hand labels against the cheap check's
  68. It runs in shadow: it records its verdict and never ends a turn.
- **Tool gate** records every verdict and acts only when you set `gatePrompt`
  to `ask`, so you choose when a judgment can stop a call.
- **Read worth**: a 15 word floor decides, with no call.
- **Page sift**: the fetch step already strips what it would remove.
- **Ask gate** runs in shadow and changes nothing a sub-agent does.
- **Instruction trust** is off.

The `search` tool has no classifier: code tries the word, case and Go
symbol forms of a pattern to tell an absence from a wrong pattern.

## Turning points on and off

- Turn the gate on: `/settings`, **Interaction**, `gatePrompt` `ask`.
- Use the browser step: `/settings`, **Browser**, `browserDriver` `goal`,
  and `browserSteps` for its budget.
- Run without a point for one task: `--no-gate`, `--gate off|shadow|enforce`,
  `--sift free|judged`, `--done-review off|cheap|screen|typed`.

## What tofu doctor shows

```
tofu doctor
tofu run --dir . --no-gate --sift free "<task>"
tofu replay --point tool_gate --set risk_ask_at=1.5 --since 7d
```

The `rules` section of `tofu doctor` lists the points and the mode each runs
in:

```
rules
  ✓ library       project · 9 of 9 points
  ○ 8 points      shadow · thresholds from the rule
    gate tool_gate@3
  ⚠ shell_sift@1  shadow · no lock file for shell_sift@1 at build
    typesafe/jev-1.13-20260917
```

The `⚠` line is a known disagreement: the shell sift's rule asks to be live
and the sieve cuts, while the doctor reports it as shadow for lack of a lock
file.
