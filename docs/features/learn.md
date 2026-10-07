---
title: Learn
description: tofu reads your own sessions for the correction you keep repeating, and proposes the fix with your words from every session behind it.
order: 15
updated: 2026-10-07
---

`tofu learn` reads your sessions when you run it and finds what you had
to say more than once. It prints at most five proposals, each with your
own words from every session it came from, and changes nothing until you
apply one.

## Why learn works the way it does

**Two sessions or it is not a pattern.** A correction counts only when
you said it in two or more sessions. One session is listed as watching,
never proposed.

**It checks whose fault it was.** For every correction that came back,
learn opens the request the model was answering at that moment and looks
for your earlier words in it. If they were missing, tofu lost them, and
that becomes a draft for tofu itself. If they were there, the model
ignored them, and a memory entry or a rule is the fix you can apply.

**A turn that ends asking you is a finding.** When the lead ended its
turn handing you a choice and your next message corrected it, learn
counts it across sessions.

**Code decides, and nothing leaves by default.** Finding the corrections,
the in-request check, the counting and the cap are code, with no model.
`--local` sends nothing. With `tofu settings set learn true`, each window is also
labelled by Jev after the count and bytes are printed, and those labels
are shown, not acted on, until a calibration backs them, and your own lead
model writes each memory statement as one rule that names no one, with your words kept
under it as evidence.

**Drafts describe, they do not quote.** An upstream draft says what
happened in general terms, with counts, the tofu version and platform,
and a mark only `tofu learn` can make. It never carries your words, a
session name or a path, and it is never sent.

## Using learn

- **Scan this project**: `tofu learn scan --local`, or `--chain <session>`
  for one chain of forks, or `--last 40`.
- **Read the evidence**: `tofu learn show <n>`.
- **Apply one**: `tofu learn apply <n>`; `--project` keeps a memory entry
  to this project.
- **Say no**: `tofu learn reject <n> --reason "<why>"`; it stays quiet
  until more sessions support it.
- **Draft for tofu**: `tofu learn upstream <n>`, then
  `tofu learn upstream --list`.

## Commands

```
tofu learn scan [--chain <session>|--last N|--global] [--local] [--all] [--dir path] [--json]
tofu learn show <n>
tofu learn apply <n> [--project] [--dir path]
tofu learn reject <n> --reason "<why>"
tofu learn upstream <n>
tofu learn upstream --list
```
