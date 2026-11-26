---
topic: tools-debug
title: The debug tool
summary: what debug gives the model: a Go or Python program or test run under dlv or debugpy, stopped at a line, with the values there, and every call of one Go function
verbs: tool
---

## What it is

`debug` runs a program or a test under a real debugger and prints the
values at the line where it stops, so the model never adds print lines it
then has to take out. It wraps `dlv` for Go and `debugpy` for Python,
speaking the Debug Adapter Protocol to each. The
model sends a list of steps, run in order, and the session stays open
between calls until `stop`.

- `start test [names...]` debugs the tests of the working directory, all
  of them or the ones named; `start <path> [args...]` debugs a program.
- `break <path:line> --if <expression>` stops there only when the
  expression is true, such as on the third call of many.
- `run` starts or continues; `step`, `step in` and `step out` move one
  line.
- `locals` and `eval <expression>` print the values at the stop.
- `trace <package.Function>`, Go only, runs to the end and lists each call
  with its arguments, result, time and caller, the first 20 in full.
- `stop` ends the program and the debugger.

In Go, on a function wrong only on its third call, `debug` showed that call
in 1 call with 0 print lines written; print lines took 5 calls and 3 lines.

    break till.go:14 if t.rung == 3: set
    > run
    stopped at till.go:14 in orders.(*Till).Ring, on breakpoint
    locals:
      t = *fixture.test/orders.Till {rung: 3, total: 750} (*fixture.test/orders.Till)
      o = fixture.test/orders.Order {Quantity: 3, UnitCents: 250} (fixture.test/orders.Order)
      ~r0 = 0 (int)
      cents = 675 (int)

A break on a line with no statement, such as a blank line, is refused
before the program runs, naming the nearest lines that hold one:

    debug: break till.go:7: dlv could not set it: ... the nearest lines with a statement are till.go:8, and the program has not run

## Where it lives

The debuggers are your own, found on `PATH`. tofu never installs one: a
missing one is named with its install command, `go install
github.com/go-delve/delve/cmd/dlv@latest` or `python -m pip install
debugpy`. A session left unused for 5 minutes is stopped, and the next call
says so.

## Change it

Install `dlv` or `debugpy` to give the model a debugger for that language.

## Check it

    tofu tool debug '{"steps":["start test TestTill","break till.go:14 --if t.rung == 3","run","eval cents","stop"]}' --dir .
    tofu tool debug '{"steps":["start test TestTill","trace orders.(*Till).Ring","stop"]}' --dir .

Each call from `tofu tool` is its own session, so end its steps with
`stop`.

## Undo it

`debug` changes none of your source files, and `stop` ends the debugger
and the program it ran.
