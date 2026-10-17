---
topic: learn
title: Learn
summary: read your own sessions for what keeps going wrong, sorted into tofu's bugs, tofu's rules, your own rules, settings and your project, with at most five findings to act on
verbs: learn
---

## What it is

`tofu learn` reads your sessions when you run it and finds what you had
to say more than once. It prints a short summary first: what keeps going
wrong, what it cost you in repeats and the lead in calls, and the
commands that act on it. Then at most five findings, each with what
happened, how often, its class and why, the fix, a confidence, and your
words last as evidence. It writes nothing until you apply one.

It reads the lead's messages only, never a sub-agent's. Your words are
taken from each message you typed, with the environment, the instruction
files and the rule notes tofu put around them cut away. A prompt sent
again and again on a schedule, such as a cron fire, is set aside and
counted.

Every finding is one of five classes:

- `defect`: tofu itself misbehaved. The fix is an upstream bug draft.
- `library`: tofu's own behaviour or rules should change for everyone.
  The fix is an upstream library draft.
- `personal`: how you want the lead to work. The fix is a memory entry,
  or a rule when the entry was needed again in two later sessions.
- `setting`: a tofu setting would fix it, with `tofu settings set`.
- `project`: about what you are building. Listed, never proposed.

For each correction that came back, it opens the request the model was
answering and looks for your earlier words in it. Missing words become a
`defect`: tofu lost them, usually at a fork.

Each session is placed on the tofu that recorded it, from its day and
the release dates in tofu's changelog. A finding a later version fixed
is listed under fixed, with the version, and never proposed.

A finding needs two sessions; one session is watched. A finding you
applied, rejected or drafted stays quiet until more sessions say it.
Confidence is high at three sessions, medium at two, low at one, and one
step lower with `--local`.

## Repeated jobs

A job is a message you typed or a schedule fired, and everything done
until your next message; a steer stays in its job. Two jobs are the same
when the lead used the same tools in the same order, a tool repeated in
a row counting once, and the messages share three or more meaningful
words. A job seen twice or more is listed, dearest first, by its
shortest wording, with times seen, sessions, tools, and cost and tokens
each and in total, from every request in its window, a sub-agent's
included. A job with no tool call is not one. `--json` carries
`repeated_jobs`: `name`, `tools`, `times`, `sessions`, `tokens`,
`tokens_each`, `cost_usd`, `cost_usd_each`, and `runs` with `session`,
`at`, `text`, `calls`, `tokens` and `cost_usd`.

## Two ways to group

With `tofu settings set learn true`, your lead model groups the messages
by meaning in one request on the account the app uses, never on the
OpenRouter key. tofu checks every field: message numbers, class, fixed
version, setting, and the rule, one plain line that names no one,
carries no filler and copies no five words of yours. A refused field is
counted. Jev also labels each window; the labels decide nothing until
calibrated.

With `--local`, or `learn` off, nothing is sent and messages group only
on three or more shared words that carry meaning. It says it is the
weaker mode, and classes come from code. Corrections agree only within
neighbouring clauses, and "again" counts only when it speaks to the lead.

## Where it lives

- `~/.tofu/learn/runs/<run>.json`: what each scan read and found
- `~/.tofu/learn/decisions.jsonl`: every apply, reject and draft
- `~/.tofu/learn/upstream/<key>.md`: the drafts
- `~/.tofu/learn/mark.key`: the secret behind each draft's mark

A draft, `format: tofu-learn-upstream`, has a `kind` (`bug`, `library`
or `failure`), the tofu version and platform, the counts, and a `mark`,
an HMAC-SHA256 keyed by `mark.key` that breaks on a hand edit. It
carries no quote, session name, path or project name.

## Change it

    tofu learn scan --local
    tofu learn scan --chain sample-family#ab3x9.12
    tofu learn scan --last 40 --global

`--chain` takes a family's name, which reads every generation, or one
generation's name or `name#tag.N`, which reads up to it. `--global`
reads every project's last sessions. `--all` lists every held, fixed,
watched and project finding and every repeated job. `--json` prints the
same run.

    tofu learn apply 3 --project
    tofu learn reject 2 --reason "fixed since"
    tofu learn upstream 4

`apply` writes the memory entry, the rule or the setting and prints the
undo; `--project` keeps a memory entry to this project. `upstream`
writes the draft and prints it whole.

## Check it

`tofu learn show 3` prints the finding, every message behind it with its
time and Jev labels, the tofu it was last seen on, and whether your
earlier words were in each request where it came back.
`tofu learn upstream --list` lists every draft and whether its mark
verifies. `tofu memory`, `tofu rules` and `tofu settings` list what an
apply wrote.

## Undo it

`apply` prints its own undo. Deleting a file under
`~/.tofu/learn/upstream/` deletes that draft. A reject keeps the finding
quiet; a scan with more sessions behind it finds it again.
