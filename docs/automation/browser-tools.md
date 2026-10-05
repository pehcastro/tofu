---
title: Browser tools
description: The tools that read and act in Chrome tabs, the three drivers that decide who picks each step, and checking a page in one call.
order: 2
updated: 2026-10-04
---

Six tools, tofu's own Go client to the [extension](/docs/automation/browser-extension):

| Tool | What it does |
|---|---|
| `browser_tabs` | the tabs tofu can reach |
| `browser_observe` | a tab as an accessibility tree, a `ref` per element |
| `browser_act` | up to 10 actions on refs or on targets by role and name, with guards |
| `browser_read` | a tab's visible text and a numbered table of controls |
| `browser_do` | a whole goal, or a list of steps with checks |
| `browser_motion` | records an animation, see [Browser debug](/docs/automation/browser-debug) |

The `browserDriver` setting decides who uses them:

| Driver | Who picks each step | Tools |
|---|---|---|
| `subagent`, the default | the `browser` sub-agent, on `browserModel`, in a tab of tofu's own | observe, act |
| `steps` | the turn's own model | observe, act |
| `goal` | a classifier, one step at a time | read, do |

Every driver also gets `browser_tabs` and `browser_motion`.

## Who drives, and checking in one call

Browsing fills context fast, so by default the lead hands it to a sub-agent
on a cheaper model. On one Airbnb task, a Sonnet sub-agent scored 12 of 12,
like Opus driving itself, and cut Opus's tokens from 1,604,780 to 118,401.

Checking a page is not a goal to reason about but a list of steps. A
`browser_do` with `steps` runs them in one call with no model in between,
and each step can carry a `check`: focus, URL, text, an element's name,
value and attributes, or its computed style. A failed check never stops the
list. Through this batch, a defect check took a median 18.8 s against 30.2 s
for headless Playwright, both 24 of 24 correct.

The `goal` driver asks a classifier for each step: 7 decisions per finished
action, down from 48.

![One browser_do call checks a page: 2 checks held, 0 failed](./media/tui-browser-check.png)

## Settings

Set the driver and its budget under **ctrl+k**, `browser`:

- `browserDriver`: `subagent`, `steps` or `goal`.
- `browserModel`: the model for the sub-agent and `browser_do`, as
  `source/model`. Empty takes the `dumb` tier, then `worker`, then the turn's
  own model.
- `browserSteps`: actions per goal, 30 by default, 60 at most.

## Running a check yourself

A check runs from the command line as the same steps, from a file or `-`:

```bash
tofu browser batch check.json
```

```json check.json
[{"action": "navigate", "value": "http://127.0.0.1:5311/"},
 {"action": "click", "target": {"role": "button", "name": "Keep it"},
  "check": {"focus": {"role": "button", "name": "Delete Rice"}}}]
```

Each check answers `check held` or `check failed` with what it read, and the
last line counts them, in the form `ran 3 of 3, checks 1 held and 1 failed`.
It exits 1 when a check failed, and closes the tab it opened.
