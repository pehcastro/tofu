---
topic: turn-history
title: History and undo
summary: changes, trail, bisect, checkpoint and whatif, the tools that say what each turn changed, find the turn that broke a check, and put back one turn's edits while keeping the rest
verbs: tool
---

## What it is

tofu commits the project to a private store at the start and the end of
every turn, with the session, turn, calls and agent in each commit. Five
tools read it, so the agent tells its own edits from yours and names the
turn and the message behind each one.

- `changes` lists what a turn changed: each file with lines added and
  removed and the call ids that named it. Inside a turn it lists that
  turn; `turns` lists the last 1 to 20 ended turns of every session,
  newest first. A file you changed between turns is not listed. A turn
  holding `owns` ends with `outside owns:`, and `of` `parent` reads the
  turn that spawned this one.
- `trail` mode `find` takes an `anchor`, a quote, a message number, a
  time, a ticket, a commit or a path, and lists the turns it names, each
  change with its id as `turn:path`. Mode `restore` takes those `ids`: a
  file nothing changed since goes back whole, a text file later turns or
  you changed is merged three ways, keeping those changes, and a change
  to the same lines refuses the whole call naming the lines. Mode
  `owner_of` takes `line` as `path:number` and names the turn behind it.
- `bisect` takes a `check` in `whatif`'s shape and names the turn that
  broke it, replaying the project as each turn left it on copies. Without
  `since` the suspects are the newest session's turns.
- `checkpoint` `save` a `label`, then `diff` and `back` put back only the
  files this session changed since, keeping and naming every file
  somebody else changed. It answers only inside a turn.
- `whatif` applies `edits` in `edit`'s shape to copies and runs a
  `check`, `build`, `test` or `command`, and returns passed, the check and
  the diff. A check past 3 minutes times out.

```text
culprit: turn turn-18e59e28a5a7a41c, 3 of 4 since the anchor, session b47541bc-0af5-4472-8cc3-f1974627b3a3, agent lead
  order.go changed by calls cassette_1_1
```

## Where it lives

```
~/.tofu/projects/<project>/shadow                 the turn store, a git folder of its own
~/.tofu/projects/<project>/scratchpad/overlays/   the copies whatif and bisect check
```

The project's own `.git` is never read for this and never written, and a
project with no git works the same. The store keeps the last 1,000 turns
and at most 512 MiB, dropping the oldest first and saying so.

## Change it

The model calls these tools; you run one by hand with `tofu tool`:

```
tofu tool changes '{"turns":4}' --dir .
tofu tool trail '{"mode":"find","anchor":"handling fee"}' --dir .
tofu tool trail '{"mode":"owner_of","line":"order.go:11"}' --dir .
tofu tool bisect '{"check":"test","name":"TestTotal"}' --dir .
tofu tool whatif '{"edits":[{"path":"order.go","old_string":"- 100","new_string":"- 75"}],"check":"test"}' --dir .
```

## Check it

`trail` restore with `dry_run` true says what it would write and writes
nothing:

```text
dry run, nothing written: restore would put back turn-18e59e28a5a7a41c:order.go, keeping the later changes to it
```

`bisect` and `whatif` end with `the working tree was not changed`. A
`bisect` check that already fails before the first suspect stops and says
so, and one that passes after every suspect says `no culprit`.

## Undo it

A restore, a `checkpoint` back or anything else the lead writes in a turn
is part of that turn, so `/undo` puts the turn back whole; see `tofu docs
commands`. `trail` reaches any recorded turn and any one file in it. A
refused restore wrote nothing:

```text
error: trail restore: refused, and nothing was written: order.go from turn turn-18e59e34736e7794: the turn wrote line 11, and a later change rewrote it as lines 11-15 of the file now
```
