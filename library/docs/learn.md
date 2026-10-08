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

It reads the lead's sessions only, never a sub-agent's. Your words are
taken from each message you typed, with the environment, the instruction
files and the rule notes tofu put around them cut away. A prompt sent
again and again on a schedule, such as a cron fire, is set aside and
counted.

Every finding is one of five classes:

- `defect`: tofu itself misbehaved. The fix is an upstream bug draft.
- `library`: tofu's own behaviour or rules should change for everyone.
  The fix is an upstream library draft.
- `personal`: how you want the lead to work. The fix is a memory entry,
  or a rule when a memory entry was needed again in two later sessions,
  which also removes the entry.
- `setting`: a tofu setting would fix it. The fix is
  `tofu settings set`.
- `project`: about what you are building, not about tofu. Listed, never
  proposed.

For each correction that came back, it opens the request the model was
answering when the mistake came back and looks for your earlier words in
it. Missing words become a `defect`: tofu lost them, usually at a fork.

It knows which tofu recorded each session from the session's day and the
release dates in tofu's own changelog, as a range when several versions
came out that day. A finding a later version fixed is listed under
fixed, with the version, and never proposed; when the fix came out the
same day it was last seen, it says so.

A finding needs two sessions. One session is listed as watching. A
finding you already applied, rejected or drafted stays quiet until more
sessions than before say it. Confidence is high at three sessions or
more, medium at two, low at one, and one step lower with `--local`.

## Two ways to group

With `tofu settings set learn true`, your lead model groups the messages
by meaning in one request on the account the app uses, never on the
OpenRouter key. tofu checks every field it answers: message numbers,
class, fixed version, setting, and the rule, which must be one plain
line that names no one, carries no filler or picture tag, and copies no
five words of yours. A refused field is counted in the output. Jev also
labels each window; the labels decide nothing until calibrated.

With `--local`, or `learn` off, nothing is sent and messages group only
on three or more shared words that carry meaning, filler and swearing
not counted. It says it is the weaker mode, and classes come from code.

## Where it lives

- `~/.tofu/learn/runs/<run>.json`: what each scan read and found
- `~/.tofu/learn/decisions.jsonl`: every apply, reject and draft, so a
  rejected finding stays quiet until more sessions support it
- `~/.tofu/learn/upstream/<key>.md`: the drafts
- `~/.tofu/learn/mark.key`: the secret behind each draft's mark

A draft is a versioned file, `format: tofu-learn-upstream`, with a
`kind` (`bug`, `library`, or `failure` for a lost correction or a turn
that ended asking), the tofu version and platform, the counts, and a
`mark`, an HMAC-SHA256 keyed by `mark.key` that breaks on a hand edit.
It carries no quote, session name, path or project name.

## Change it

    tofu learn scan --local
    tofu learn scan --chain sample-family --local
    tofu learn scan --chain sample-family#ab3x9.12
    tofu learn scan --last 40
    tofu learn scan --global

`--chain` takes a family's name, which reads every generation, or one
generation's name or `name#tag.N`, which reads up to it. `--global`
reads every project's last sessions. `--all` lists every held, fixed,
watched and project finding. `--json` prints the same run.

    tofu learn apply 3
    tofu learn apply 3 --project
    tofu learn reject 2 --reason "fixed since"
    tofu learn upstream 4

`apply` writes the memory entry, the rule or the setting and prints the
undo. `--project` keeps a memory entry to this project instead of every
project. `upstream` writes the draft and prints it whole.

## Check it

    tofu learn show 3

prints the finding, every message of yours behind it with its time and
Jev labels, the tofu it was last seen on, and for each time it came back
whether your earlier words were in the request, with that request's id.

    tofu learn upstream --list

lists every draft with its kind, title, session count and whether its
mark verifies. `tofu memory`, `tofu rules` and `tofu settings` list what
an apply wrote.

## Undo it

`apply` prints its own undo. Deleting a file under
`~/.tofu/learn/upstream/` deletes that draft. A reject keeps the finding
quiet; a scan with more sessions behind it finds it again.
