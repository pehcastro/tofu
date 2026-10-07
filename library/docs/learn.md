---
topic: learn
title: Learn
summary: read your own sessions for what keeps going wrong, and get at most five proposals with the evidence behind each
verbs: learn
---

## What it is

`tofu learn` reads your sessions when you run it and looks for the same
correction said in two or more sessions. It prints at most five
proposals, each with your words from every session it came from, and
writes nothing until you apply one.

It reads the lead's sessions only, never a sub-agent's. Your words are
taken from each message you typed, with the environment, the instruction
files and the rule notes tofu put around them cut away. A prompt sent
again and again on a schedule, such as a cron fire, is set aside and
counted.

For each correction that came back, it opens the request the model was
answering when the mistake came back and looks for your earlier words in
it. Absent means tofu lost them, usually at a fork, and that becomes an
upstream draft. Present means the model had them and did it anyway.

A turn that ended asking you, and your next message corrected it, is its
own finding: the lead ended its turn handing you a choice.

Each proposal is one of:

- `apply`: a memory entry, or a rule when a memory entry was needed
  again in two later sessions, which also removes the entry
- `upstream`: the cause is tofu itself; a draft is written, never sent

Corrections seen in one session only are listed as watching. Remarks
about your project rather than about how tofu behaved are listed as seen
and never proposed.

## Where it lives

- `~/.tofu/learn/runs/<run>.json`: what each scan read and proposed
- `~/.tofu/learn/decisions.jsonl`: every apply, reject and draft, so a
  rejected proposal stays quiet until more sessions support it
- `~/.tofu/learn/upstream/<key>.md`: the drafts
- `~/.tofu/learn/mark.key`: the secret behind each draft's mark

A draft is a versioned file, `format: tofu-learn-upstream` and
`format_version: 1`, with a `kind` (learn writes `failure` today, and the
format also takes `bug` and `library`), the
tofu version, commit and platform, the counts, and a `mark`: an
HMAC-SHA256 over every other line, keyed by `mark.key`, so a draft edited
by hand no longer verifies. It describes what happened in general terms
and carries no quote, no session name, no path and no project name.

## Change it

    tofu learn scan --local
    tofu learn scan --chain sample-one --local
    tofu learn scan --last 40
    tofu learn scan --global

`--local` runs only the code stages and sends nothing anywhere. Without
it, and with `tofu settings set learn true`, each window around a message you
typed is also sent to Jev for four labels, after the window count and
bytes are printed. The labels are shown and logged, and decide nothing
until a calibration backs them. Each memory statement is then written by
your lead model, on the account the app uses and never on the OpenRouter
key, as one sentence that states the rule and names no one. With `--local`, or
when the model refuses, names someone or answers with anything else, the statement is a
template built from the clause of your words that states the instruction,
and the scan says which. Your words stay under it as evidence. `--global` reads every project's last
sessions and counts a correction only across two projects. `--all` lists
every watched and seen finding.

    tofu learn apply 3
    tofu learn apply 3 --project
    tofu learn reject 2 --reason "fixed since"
    tofu learn upstream 4

`apply` writes the memory entry or the rule and prints the undo.
`--project` keeps a memory entry to this project instead of every
project. `upstream` writes the draft and prints it whole.

## Check it

    tofu learn show 3

prints the proposal, your words from each session with its time, and
for each time it came back whether your earlier words were in the
request, with that request's id.

    tofu learn upstream --list

lists every draft with its kind, title, session count and whether its
mark verifies. `tofu memory` and `tofu rules` list what an apply wrote.

## Undo it

`apply` prints its own undo, `tofu memory remove <id>` for an entry.
Deleting a file under `~/.tofu/learn/upstream/` deletes that draft. A
reject keeps the proposal quiet; a scan with more sessions behind it
proposes it again.
