---
title: Checking its own work
description: done, claims and the review agent, which check a turn's work before the person reads the report, so a turn ends on what its calls showed rather than on the model's word.
order: 13
updated: 2026-11-06
---

These tools check the model's work before you read about it. `done` reads
each acceptance line of the task against the call that checked it, and a
line that reads no keeps the turn going for one more turn. `claims` checks each sentence of a
report against the calls the session made. `review` is a read-only
sub-agent that reads the turn's changes in a fresh context and reports what
is wrong with them. `done` and `claims` are tofu's own Go code and ask Jev;
`review` is a library agent.

## Why they exist

A model ends a turn when it believes the work is finished, and its report
says what it believes. A test it never ran, a line it read as met from a
command that could not check it, or a file the task never asked for all
reach you as finished work. These tools put a recorded call behind each
line of the report, or name the line that has none.

## done

`done` answers whether the model can stop. It takes the task's acceptance
lines, each with `call`, the call whose command checked it: the
`[tool#abc12]` reference at the top of that call's result, or its call id.
Each line reads one of three ways:

| Reads | When |
|---|---|
| `yes` | the call exited 0, or a `test` call ran and passed, and Jev reads that call as checking that line |
| `no` | the call failed |
| `not_checked` | no call, a reference no earlier call has, a call that ran no command, or a passing call that does not check what the line says |

A plan item that carries a `check` is read as a line too, against the call
its `done` named. See [plan](/docs/tools/work#plan).

Once the model calls `done`, the turn does not end while a line reads no.
The model gets one more turn to fix it, run the check again and call `done`
with the new call; after that turn it ends, with a warning naming the line.
When a task states acceptance lines, the lead is told to
call `done` with every line before it ends the turn, including a line that
fails.

In this turn on a Go module, the model ran two checks, and the second
package does not compile:

```text
step 1: tool_call tool=bash command="go test -run TestTotal ." sub_agent="" exit_code=0 gate= error=""
step 1: tool_call tool=bash command="go test ./broken" sub_agent="" exit_code=1 gate= error=""
step 2: tool_call tool=done command="done" sub_agent="" exit_code=<nil> gate= error=""
```

`done` answered:

```text
done: 2 lines, 1 yes, 1 no, 0 not_checked
  yes         cassette_1_1 exit 0, jev reads 0.93 that it checks this line, against 0.50: the total test passes
  no          cassette_1_2 exit 1: the broken package passes its tests
the turn does not end while a line reads no: fix it, run its check again, and call done with the new call id
```

The model then said `both lines pass` and tried to end. The turn went on:

```text
step 3: warning done read a line as no, so the turn did not end and was given one grace turn: the broken package passes its tests (cassette_1_2 exit 1)
```

It ran the check again, which failed again, and called `done` with the new
call. The turn ended with the warning on record:

```text
turn turn-18e59b8d51aed954 warning the turn ended after its one grace turn with done still reading no: the broken package passes its tests (cassette_4_1 exit 1)
```

A line never reads yes on an exit code alone. Here the model cited a
passing test for a line about which files changed, a reference no call
has, and nothing at all:

```text
done: 3 lines, 0 yes, 0 no, 3 not_checked
  not_checked 0 earlier calls have the id or reference "[tool#abcde]": no file under broken/ is edited
  not_checked cassette_1_1 exit 0, jev reads 0.01 that it checks this line, against 0.50: nothing under broken/ is edited
  not_checked no call: the docs read well
```

When Jev cannot be reached or no classifier is bound, a line that would
read yes reads `not_checked` and says why. A line whose call failed still
reads no. Each Jev answer is a row in the decision ledger.

`done` reads the calls of the turn it runs in, so `tofu tool done` by hand
has none to read:

```bash
tofu tool done '{"lines":[{"text":"the total test passes","call":"cassette_1_1"}]}' --dir .
```

```text
done: 1 lines, 0 yes, 0 no, 1 not_checked
  not_checked 0 earlier calls have the id or reference "cassette_1_1": the total test passes
```

A call with no lines and no plan check says `done: 0 lines, since none was
given and no plan item carries a check`.

## claims

`claims` answers whether what the model is about to report is true. It
takes the report, splits it into sentences, and Jev reads each one: a
sentence that states a fact a tool call could show is a claim, and a
sentence that says what it will do next is not. Each claim reads one of
three ways:

| Reads | When |
|---|---|
| `shown` | a call shows it, named by its id |
| `not_shown` | no call shows it |
| `contradicted` | a call's output says it is false, named by its id |

The first 20 sentences are checked, against the latest 40 calls of the
session, and the answer says how many past 20 were left unchecked. A call
that contradicts a claim stops counting once the same call runs again
later and passes.

In this session the model edited `Discounted` in `order.go` and ran one
test:

```bash
tofu tool claims '{"report":"I changed Discounted in order.go. TestTotal passes. All tests pass."}' --dir . --session head
```

```text
3 claims in 3 parts of the report, checked against 2 calls of session wise-lilac-otter#q0vvx.1
  shown cassette_1_1: I changed Discounted in order.go
  shown cassette_2_1: TestTotal passes
  not_shown none: All tests pass
```

In another, the one test run failed:

```bash
tofu tool claims '{"report":"All tests pass."}' --dir . --session head
```

```text
1 claims in 1 parts of the report, checked against 1 calls of session zesty-slate-wren#bvgg9.1
  contradicted cassette_1_1: All tests pass
```

A report with no sentence says `0 claims: the report holds no sentence`.
When Jev cannot be reached, nothing is checked and the call fails saying
so. Each check is a row in the decision ledger.

## review

`review` is a sub-agent the lead spawns before you see the work. It is
given the task and the turn's changes and nothing else, so it judges the
change by what it does rather than by the author's reasoning. It calls the
`changes` tool on the turn that spawned it, reads every changed file whole,
searches for the callers, and reports only what this change introduced.

Its tools are read-only. `tofu tool list --agent review` prints them:

```text
read
glob
search
symbols
changes
scratch_path
scratch_share
scratch_list
scratch_read
skill
ask
artifact_fetch
reference
```

Each finding names a `path:line` range and one kind:

| Kind | What it is |
|---|---|
| `bug` | a wrong result, an unhandled empty value, a dropped error, a check on one path missing from its twin |
| `unused` | a function, type, field or import the change adds that nothing calls |
| `untested` | behaviour the change adds that no test or driven run exercises |
| `off_task` | a file or behaviour the task did not ask for |
| `comment` | a comment the change adds |

In this recorded session the lead added a helper nothing calls and a file
the task never named, then spawned `review` on the task `make Discounted
take 100 off the total`. Two of its findings:

```text
R1
path: order.go:18-20
kind: unused
note: `func unusedHelper() int { return 7 }` was added and nothing calls it. A search for `unusedHelper\(` finds only its own declaration at order.go:18. The task did not ask for it either.

R2
path: notes.txt:1
kind: off_task
note: The turn created notes.txt containing "hello". The task "make Discounted take 100 off the total" does not need this file.
```

The report ends with `findings: N` and the files it read. With nothing
wrong it is `findings: 0`. `review` runs only as a sub-agent inside a turn.

## Parameters

The model calls these tools. `tofu tool done` and `tofu tool claims` run one
call by hand; see [One tool call by hand](/docs/cli/tool).

| Tool | Parameter | What it does |
|---|---|---|
| `done` | `lines` | the acceptance lines, each an object |
| `done` | `lines[].text` | the line as the task states it; required |
| `done` | `lines[].call` | the `[tool#abc12]` reference or call id of the call that checked it |
| `claims` | `report` | the report to check; required |
| `claims` | `session` | another recorded session by name, id or `head`; left out, the one the model runs in |
