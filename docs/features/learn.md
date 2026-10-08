---
title: Learn
description: tofu reads your own sessions for what keeps going wrong, tells tofu's bugs from your own rules, and proposes the fix with your words behind it.
order: 15
updated: 2026-10-08
---

`tofu learn` reads your sessions when you run it and finds what you had
to say more than once. It opens with a short summary, what keeps going
wrong, what it cost you, and what to run, then at most five findings,
and changes nothing until you apply one.

## Why learn works the way it does

**Grouped by meaning, then checked.** With `tofu settings set learn true`
your own lead model groups your messages by what they are about, in one
request on your subscription. tofu then checks every answer: a rule that
copies your words, names someone or carries filler is refused, and so is
a version or a setting that does not exist. `--local` sends nothing and
groups only on several shared words, and says it is the weaker mode.

**Tofu's bugs are not your rules.** Each finding is a tofu defect, a tofu
library rule, your own rule, a setting, or about your project, with the
reason. A defect becomes a draft for tofu, never a memory entry, and a
remark about your project is listed and never proposed.

**It knows what was fixed since.** Each session is placed on the tofu
version that recorded it, from its day and the release dates in tofu's
changelog. A finding a later version fixed is listed as fixed in that
version, not proposed again.

**It checks whose fault it was.** For every correction that came back,
learn opens the request the model was answering and looks for your
earlier words in it. Missing means tofu lost them.

**Two sessions or it is not a pattern.** One session is watched, never
proposed. Confidence rises with the number of sessions.

**Drafts describe, they do not quote.** An upstream draft says what
happened in general terms, with counts, the tofu version and platform,
and a mark only `tofu learn` can make. It never carries your words, a
session name or a path, and it is never sent.

## Using learn

- **Scan a conversation**: `tofu learn scan --chain <name>` reads every
  generation of that conversation; `--local` keeps it on your machine.
- **Read the evidence**: `tofu learn show <n>`.
- **Apply one**: `tofu learn apply <n>`; `--project` keeps a memory entry
  to this project.
- **Say no**: `tofu learn reject <n> --reason "<why>"`; it stays quiet
  until more sessions support it.
- **Draft for tofu**: `tofu learn upstream <n>`, then
  `tofu learn upstream --list`.

## Commands

```
tofu learn scan [--chain <name>|--last N|--global] [--local] [--all] [--dir path] [--json]
tofu learn show <n>
tofu learn apply <n> [--project] [--dir path]
tofu learn reject <n> --reason "<why>"
tofu learn upstream <n>
tofu learn upstream --list
```
