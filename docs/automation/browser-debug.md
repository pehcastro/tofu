---
title: Browser debug
description: Recording an animation frame by frame, the site recipes tofu learns, and driving a tab yourself from the command line.
order: 3
updated: 2026-10-04
---

Three things for when a page doesn't behave:

- **`browser_motion`**, a tool that records an animation in a tab tofu
  opened and reads it back frame by frame.
- **Recipes**, files tofu writes after a successful browsing task: the URLs
  the run reached, each value as `{name}`, in
  `~/.tofu/browser/recipes/<host>.md`.
- **`tofu browser` step commands**, the model's own steps, which you run on
  one tab.

## Why record, and why keep recipes

A `style` check reads one moment, so it can't say when a menu closed or
whether it flickered. `browser_motion` records every frame around a trigger,
with each watched element's box, opacity, visibility and the attributes and
styles you name, and answers when each value changed, in ms from the trigger.

A recipe lets the next run on a site start from URLs that worked instead of
exploring. With one, an Airbnb search took 103 s against 342 s cold. Two
failures in a row set a recipe aside until a success learns a new one.

The step commands show you exactly what the model sees and does, so a
failure can be replayed by hand.

## Using motion, recipes and steps

- **Motion.** The model writes a scenario file: a URL, what to wait for, a
  `click`, `hover` or `press` trigger, and the elements to watch. `capture`
  records takes into `~/.tofu/motion/<take id>`, `inspect` reads one, and
  `compare` sets a `before` and an `after` label side by side.
- **Recipes.** Edit or delete the file to change or forget one.
- **Steps.** Every step command needs `--tab`, from `tofu browser`.

## Commands

```bash
tofu browser recipes
```

```text
  ● the-internet.herokuapp.com  in use  1 uses  0 failed in a row
    https://the-internet.herokuapp.com/dynamic_loading/{id}
```

The first line, above these rows, names the count and the folder.

| Command | What it does |
|---|---|
| `tofu browser observe --tab <id>` | the tree with its refs; `--all` for every node |
| `tofu browser click <ref> --tab <id>` | also `fill`, `select`, `press`, `scroll`, `back` |
| `tofu browser batch <steps.json>` | a whole check |
| `tofu browser motion capture <scenario.json>` | `--takes 3`, `--label`, `--tab` |

An action prints what changed and the new tree. Every command takes `--json`.
