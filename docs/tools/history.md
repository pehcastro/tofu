---
title: History and undo
description: changes, trail, bisect, checkpoint and whatif, the tools that say what each turn changed, find the turn that broke a check, and put back one turn's edits while keeping the rest.
order: 14
updated: 2026-11-06
---

These tools read the record of what each turn changed and act on it.
`changes` lists the files a turn changed, with the calls that changed them.
`trail` finds the turn behind a quote, a message, a path or a line, and
puts back that turn's change to a file. `bisect` finds which turn broke a
check. `checkpoint` saves the project under a label and later puts back only
the files this session changed since. `whatif` runs a check on edits without
writing them. All five are tofu's own Go code.

## Why they exist

An agent's edits sit uncommitted beside your own. `git diff` shows both
mixed together, `git stash` takes your files with the agent's, and `git
bisect` needs a commit per step, which a session does not make. These tools
read a record tofu keeps of every turn, so they tell the agent's edits from
yours, name the turn and the message behind each one, and never touch your
working tree to answer.

The record is a private git store in your home, one per project:
`~/.tofu/projects/<project>/shadow`. tofu commits the project to it at the
start and the end of every turn, with the session, the turn, the calls and
the agent in each commit. The project's own `.git` is never read for this
and never written, and a project with no git works the same. The store
keeps the last 1,000 turns and at most 512 MiB, dropping the oldest first
and saying so. `/undo` and `tofu undo` put back the last turn whole; see
[Undo](/docs/features/undo). These tools reach any recorded turn and any
one file in it.

On eight recorded turns where one broke a test, in Go and in Python,
`bisect` named the turn in 1 call each time. `git bisect` over a commit per
turn named it in 4, and with the edits uncommitted, as an agent leaves them,
the git procedure found the file and never the turn. `trail` then put back
only the breaking turn exactly in 4 of 4, including where the turns before
and after it changed the same file, with your own edit kept every time.

## changes

`changes` lists what a turn changed: each file with lines added and
removed, and the ids of the calls that named it. Inside a turn it lists that
turn, still running. `turns` lists the last ended turns of every session
instead, newest first, each under its session. A file you changed between
turns is not listed under any turn.

In this session the lead ran four turns: a discount change, a notes file, a
handling fee in `Total`, and a label method.

```bash
tofu tool changes '{"turns":4}' --dir .
```

```text
turn turn-18e59e292223a450, session young-ivory-mink#xyu4g.1, agent lead, ended: 1 file
  order.go +4 -0 by cassette_1_1
turn turn-18e59e28a5a7a41c, session young-ivory-mink#xyu4g.1, agent lead, ended: 1 file
  order.go +1 -1 by cassette_1_1
turn turn-18e59e284cf69c38, session young-ivory-mink#xyu4g.1, agent lead, ended: 1 file
  NOTES.md +1 -0 by cassette_1_1
turn turn-18e59e27c7822130, session young-ivory-mink#xyu4g.1, agent lead, ended: 1 file
  order.go +1 -1 by cassette_1_1
```

A turn that holds an `owns` list, as a sub-agent's does, ends with
`outside owns:` and every file it changed outside that list. `of` set to
`parent` lists what the turn that spawned this one has changed so far,
which is how the review agent reads the lead's work; see
[Checking its own work](/docs/tools/check-work#review). With nothing
recorded yet it says `no turn has been recorded in the shadow store`.

## trail

`trail` has three modes.

**find** takes an `anchor` and lists the turns it names, each with every
file it changed, who changed it, the restore step, and every later change to
the same file. The anchor is a quote from your message, a message number in
the newest session, a time such as `2026-11-06 05:41`, a ticket such as
`SHOP-12`, a project commit, or a path.

```bash
tofu tool trail '{"mode":"find","anchor":"handling fee"}' --dir .
```

```text
trail quote "handling fee": 1 of the 4 ended turns
anchor: session young-ivory-mink#xyu4g.1, turn turn-18e59e28a5a7a41c, agent lead, ended 2026-11-06T22:12:05-03:00, message "add the handling fee to the total"
  change turn-18e59e28a5a7a41c:order.go: d9ae44c -> 1946c5e, calls cassette_1_1, commit none
restore, with mode restore and these ids:
  turn-18e59e28a5a7a41c:order.go: writes order.go back to d9ae44c
changed since, which restore merges, refusing only where the same lines changed:
  turn-18e59e28a5a7a41c:order.go: order.go changed again by turn turn-18e59e292223a450
```

`commit` names the project commit that holds the turn's result, or `none`
while it is uncommitted. A file the turn created is put back by deleting it:

```text
trail message 2 of young-ivory-mink#xyu4g.1: 1 of the 4 ended turns
anchor: session young-ivory-mink#xyu4g.1, turn turn-18e59e284cf69c38, agent lead, ended 2026-11-06T22:12:03-03:00, message "add a notes file"
  change turn-18e59e284cf69c38:NOTES.md: absent -> bfa6551, calls cassette_1_1, commit none
restore, with mode restore and these ids:
  turn-18e59e284cf69c38:NOTES.md: deletes NOTES.md, which the turn created
changed since, which restore merges, refusing only where the same lines changed:
  none
```

**restore** takes the change `ids` that find printed and undoes each one. A
file nothing changed since goes back whole. A text file that later turns or
you changed too is merged three ways, so only this turn's lines go back and
every later change stays. `dry_run` says what it would write and writes
nothing.

```bash
tofu tool trail '{"mode":"restore","ids":["turn-18e59e28a5a7a41c:order.go"],"dry_run":true}' --dir .
```

```text
dry run, nothing written: restore would put back turn-18e59e28a5a7a41c:order.go, keeping the later changes to it
```

```bash
tofu tool trail '{"mode":"restore","ids":["turn-18e59e28a5a7a41c:order.go"]}' --dir .
```

```text
restored turn-18e59e28a5a7a41c:order.go, keeping the later changes to it
```

`Total` lost its handling fee, the discount from the first turn and the
label method from the fourth stayed, and the tests pass again:

```text
test: go test -json -count=1 . in C:\work\shop\go, 1241 ms
go test: 2 passed, 0 failed, 1 skipped
skipped: TestShipping
```

When a later change rewrote the same lines, restore refuses the whole call,
names the file and the lines, and writes nothing. Here a fifth turn changed
the handling fee to two cents:

```text
error: trail restore: refused, and nothing was written: order.go from turn turn-18e59e34736e7794: the turn wrote line 11, and a later change rewrote it as lines 11-15 of the file now
```

**owner_of** takes `line` as `path:number` and names the session, turn,
calls and message behind that line of the file as it is now, or says it was
changed outside any recorded turn.

```bash
tofu tool trail '{"mode":"owner_of","line":"order.go:11"}' --dir .
```

```text
order.go:11 "\treturn pr.Total(o.Quantity, o.UnitCents) + 1"
written by session young-ivory-mink#xyu4g.1, turn turn-18e59e28a5a7a41c, agent lead, calls cassette_1_1, message "add the handling fee to the total"
```

trail lists ended turns only, at most 20 in one answer, newest kept.

## bisect

`bisect` finds which turn broke a check. It runs the check on the project as
the turns left it: before the first suspect, after the last, then halving
the range between. Each step runs on copies through `whatif`, so the working
tree never changes. Without `since` the suspects are every ended turn of the
newest session; `since` names the first suspect by the turn id trail
prints. The check takes `whatif`'s shape.

```bash
tofu tool bisect '{"check":"test","name":"TestTotal"}' --dir .
```

```text
culprit: turn turn-18e59e28a5a7a41c, 3 of 4 since the anchor, session b47541bc-0af5-4472-8cc3-f1974627b3a3, agent lead
  order.go changed by calls cassette_1_1
diff:
--- order.go
+++ order.go
@@ -8,7 +8,7 @@
 }
 
 func (o Order) Total() int {
-	return pr.Total(o.Quantity, o.UnitCents)
+	return pr.Total(o.Quantity, o.UnitCents) + 1
 }
 
 func (o Order) Discounted() int {

steps:
  at the anchor, before turn turn-18e59e27c7822130: passed
  after turn turn-18e59e292223a450, 4 of 4: the check failed
  after turn turn-18e59e284cf69c38, 2 of 4: passed
  after turn turn-18e59e28a5a7a41c, 3 of 4: the check failed
check: go test -overlay=C:\Users\you\.tofu\projects\go-a32c98bb\scratchpad\overlays\975676897\overlay.json -json -count=1 -run TestTotal ./. in C:\work\shop\go, 1655 ms: 0 passed, 1 failed
FAIL TestTotal order_test.go:9: got 751, want 750
check sha256: e17dcdebc4623612f329ee6de6bd55bef315ce4acac64f005e54b4a63d64fe96
the working tree was not changed
```

The culprit's change ids for trail are the turn and each file it names,
here `turn-18e59e28a5a7a41c:order.go`. A check that already fails before
the first suspect says so and stops:

```text
bisect stopped: the check already fails at the anchor, before turn turn-18e59e33a608b3f0, so no turn since broke it
```

A check that passes after every suspect says `no culprit`.

## checkpoint

`checkpoint` saves the project under a `label` and later puts back only the
files this session changed since. `save` snapshots every file into the
store, never a stash. `diff` shows what `back` would do: each changed file
with lines added and removed, the declarations the change touches, and
whether back restores it or keeps it. `back` restores each file this
session changed to its saved content, and keeps every file somebody else
changed, naming it. A file you edited after the agent last wrote it stays as
you left it.

checkpoint tells the agent's edits from everybody else's by the turn it
runs in, so it answers only inside a turn. By hand it says so:

```text
error: checkpoint: answers only inside a turn, because it tells this agent's edits from everybody else's by the turns the shadow store records, and this call runs in none
```

In this recorded session, turn 1 was `try a cache in the pricing`: the lead
saved `before-cache` and edited `order.go` and `pricing/pricing.go`. You
then added a line to `.env.example` by hand. Turn 2 was `that broke it, put
it back`. These are the three results the lead read:

```text
saved checkpoint before-cache: 6 files, 953 bytes, in the shadow store at C:\Users\you\.tofu\projects\go-49b3e299\shadow
back before-cache restores only the files this session changes from here, and diff before-cache shows them first
```

```text
checkpoint before-cache, saved in turn turn-18e59e3dec4488d0, against the project now: 3 changed
  .env.example +1 -0, symbols none: back keeps it, changed since the save, and not by this session
  order.go +1 -1, symbols Order.Discounted: back restores it
  pricing/pricing.go +1 -1, symbols Total: back restores it
```

```text
back to checkpoint before-cache, saved in turn turn-18e59e3dec4488d0
restored: order.go, pricing/pricing.go
kept as they are now: 1
  .env.example: changed since the save, and not by this session
```

Both files came back byte for byte and `.env.example` kept your line. A
second `save` under the same label replaces the first and says so.

## whatif

`whatif` applies edits to copies of the files and runs a check on them, so
the working tree never changes, a crash leaves nothing half changed, and two
agents can try rival fixes on one file. The edits take `edit`'s shape, and
an edit with an empty `old_string` on a path that does not exist creates
that file. A Go check runs on the real tree through Go's `-overlay`; any
other runs on a copy under the scratchpad.

| Check | What runs |
|---|---|
| `build` | `go build` and `go vet` on the Go package at `path`, or `cargo build` on the Rust crate at or above it |
| `test` | `go test` on that package or `cargo test` on that crate, `name` filtering the tests |
| `command` | `command`, a program and its arguments, in a copy of the project at `path` |

```bash
tofu tool whatif '{"edits":[{"path":"order.go","old_string":"return o.Total() - 100","new_string":"return o.Total() - 75"}],"check":"test","name":"TestDiscounted"}' --dir .
```

```text
passed: true; the working tree was not changed
check: go test -overlay=C:\Users\you\.tofu\projects\go-6e865ad5\scratchpad\overlays\3133638233\overlay.json -json -count=1 -run TestDiscounted ./. in C:\work\shop\go, 1267 ms: 1 passed, 0 failed
check sha256: f01b9b29648d96a814d64a776465d9ab2bf3fe7abdddf66b9d2f0c4ee4686e5a
diff:
--- order.go
+++ order.go
@@ -12,5 +12,5 @@
 }
 
 func (o Order) Discounted() int {
-	return o.Total() - 100
+	return o.Total() - 75
 }
```

A failing try reads `passed: false, the check failed` with the failure, and
a check that runs past 3 minutes says it timed out.

## Parameters

The model calls these tools. `tofu tool` runs one call by hand; see
[One tool call by hand](/docs/cli/tool).

| Tool | Parameter | What it does |
|---|---|---|
| `changes` | `turns` | the last 1 to 20 ended turns of every session, newest first; left out, the running turn, or the newest ended one by hand |
| `changes` | `of` | `parent`: the turn that spawned this one, while it runs |
| `trail` | `mode` | `find`, `restore` or `owner_of`; left out, `find` |
| `trail` | `anchor` | for find: a quote, a message number, a time, a ticket, a commit or a path |
| `trail` | `ids` | for restore: change ids as find printed them, `turn:path` |
| `trail` | `dry_run` | for restore: say what it would write and write nothing |
| `trail` | `line` | for owner_of: `path:number`, such as `order.go:11` |
| `bisect` | `check` | `build`, `test` or `command`; required |
| `bisect` | `since` | the first suspect turn, by the id trail prints |
| `bisect` | `path`, `name`, `command` | as for `whatif` |
| `checkpoint` | `action` | `save`, `back` or `diff`; required |
| `checkpoint` | `label` | letters, digits, `-` and `_`, such as `before-cache`; required |
| `whatif` | `edits` | edits as `edit` takes them, each with `path` and `new_string`; required |
| `whatif` | `check` | `build`, `test` or `command`; required |
| `whatif` | `path` | the package, crate or directory the check runs at |
| `whatif` | `name` | for test: the tests to run |
| `whatif` | `command` | for command: a program and its arguments |
