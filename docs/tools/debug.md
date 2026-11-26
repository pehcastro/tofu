---
title: Debug
description: debug runs a Go or Python program or test under a real debugger and answers what the values are at a line, so the model never adds print lines it then has to take out.
order: 15
updated: 2026-11-25
---

`debug` runs a program or a test under a real debugger and prints the values
at the line where it stops. It wraps `dlv` for Go and `debugpy` for Python,
speaking the
[Debug Adapter Protocol](https://microsoft.github.io/debug-adapter-protocol/)
to each. The model sends a list of steps,
tofu runs them in order and returns one answer, and the session stays open
between calls until `stop`.

## Why it exists

When a value is wrong only on one call of many, or deep inside a running
test, the usual fix is a print line: edit it in, run the test, read the
output, edit it out. That is five calls and lines written into your
source. `debug` stops the program where the value is wrong and reads it
there, with nothing written to your files.

In Go, on a function that is wrong only on its third call, `debug` showed the
wrong call in 1 call with 0 print lines written. The print line procedure
took 5 calls and wrote 3 lines. In a real session, given only the task "this
returns a wrong value on the third call, find out why", the model chose
`debug` on its own, stopped on the third call with a condition, and named the
cause.

## How to use it

The examples run against a project with a `Till` whose `Ring` takes ten
percent off every third order, so the test sees 2425 where it wants 2500:

```go till.go
package orders

type Till struct {
	rung  int
	total int
}

func (t *Till) Ring(o Order) int {
	t.rung++
	cents := o.Total()
	if t.rung%3 == 0 {
		cents -= cents / 10
	}
	t.total += cents
	return cents
}
```

### Stop on one call with break --if

`break` takes `path:line` and, after `--if`, an expression in the program's
own language. `run` starts the program and stops at the first break whose
condition is true. Every stop prints where it is, the locals one line each,
and the top of the stack.

```bash
tofu tool debug '{"steps":["start test TestTill","break till.go:14 --if t.rung == 3","run","eval cents","stop"]}' --dir .
```

```text
> start test TestTill
test TestTill started under dlv, pid 30752: break where it should stop, then run
> break till.go:14 --if t.rung == 3
break till.go:14 if t.rung == 3: set
> run
stopped at till.go:14 in orders.(*Till).Ring, on breakpoint
locals:
  t = *fixture.test/orders.Till {rung: 3, total: 750} (*fixture.test/orders.Till)
  o = fixture.test/orders.Order {Quantity: 3, UnitCents: 250} (fixture.test/orders.Order)
  ~r0 = 0 (int)
  cents = 675 (int)
stack:
  till.go:14 in orders.(*Till).Ring
  till_test.go:8 in orders.TestTill
  C:/Users/you/go/pkg/mod/golang.org/toolchain@v0.0.1-go1.27.1.windows-amd64/src/testing/testing.go:2193 in testing.tRunner
  C:/Users/you/go/pkg/mod/golang.org/toolchain@v0.0.1-go1.27.1.windows-amd64/src/testing/testing.go:2258 in testing.(*T).Run.gowrap1
  C:/Users/you/go/pkg/mod/golang.org/toolchain@v0.0.1-go1.27.1.windows-amd64/src/runtime/asm_amd64.s:1264 in runtime.goexit
> eval cents
cents = 675 (int)
> stop
dlv and the program it ran have exited
```

The third order of 750 cents came out as 675. A break that never stops, or
whose condition is never true, is named when the program ends.

### Move one line with step

`step` moves to the next line, `step in` into the call on this line, and
`step out` back to the caller. `locals` prints the values at the stop again,
and a second `run` continues to the next break. The answer below is the
part after `run`:

```bash
tofu tool debug '{"steps":["start test TestTill","break till.go:10","run","step","locals","stop"]}' --dir .
```

```text
> step
stopped at till.go:11 in orders.(*Till).Ring, on step
locals:
  t = *fixture.test/orders.Till {rung: 1, total: 0} (*fixture.test/orders.Till)
  o = fixture.test/orders.Order {Quantity: 1, UnitCents: 250} (fixture.test/orders.Order)
  ~r0 = 0 (int)
  cents = 250 (int)
stack:
  till.go:11 in orders.(*Till).Ring
  till_test.go:8 in orders.TestTill
  C:/Users/you/go/pkg/mod/golang.org/toolchain@v0.0.1-go1.27.1.windows-amd64/src/testing/testing.go:2193 in testing.tRunner
  C:/Users/you/go/pkg/mod/golang.org/toolchain@v0.0.1-go1.27.1.windows-amd64/src/testing/testing.go:2258 in testing.(*T).Run.gowrap1
  C:/Users/you/go/pkg/mod/golang.org/toolchain@v0.0.1-go1.27.1.windows-amd64/src/runtime/asm_amd64.s:1264 in runtime.goexit
```

### See every call with trace

`trace` takes a Go function with its package, such as `orders.Order.Total`
or `orders.(*Till).Ring` for a pointer method, runs the program to its end,
and lists each call with its arguments, its result, the time it took under
the debugger and its caller. The first 20 calls are shown in full, with the
count of all of them. It runs in place of `break` and `run`, right after
`start`.

```bash
tofu tool debug '{"steps":["start test TestTill","trace orders.(*Till).Ring","stop"]}' --dir .
```

```text
> start test TestTill
test TestTill started under dlv, pid 37556: break where it should stop, then run
> trace orders.(*Till).Ring
trace orders.(*Till).Ring: 4 calls
  1. t = *fixture.test/orders.Till {rung: 0, total: 0} (*fixture.test/orders.Till), o = fixture.test/orders.Order {Quantity: 1, UnitCents: 250} (fixture.test/orders.Order), returned 250 (int), 2.50 ms under the debugger, from till_test.go:8 in orders.TestTill
  2. t = *fixture.test/orders.Till {rung: 1, total: 250} (*fixture.test/orders.Till), o = fixture.test/orders.Order {Quantity: 2, UnitCents: 250} (fixture.test/orders.Order), returned 500 (int), 3.50 ms under the debugger, from till_test.go:8 in orders.TestTill
  3. t = *fixture.test/orders.Till {rung: 2, total: 750} (*fixture.test/orders.Till), o = fixture.test/orders.Order {Quantity: 3, UnitCents: 250} (fixture.test/orders.Order), returned 675 (int), 2.00 ms under the debugger, from till_test.go:8 in orders.TestTill
  4. t = *fixture.test/orders.Till {rung: 3, total: 1425} (*fixture.test/orders.Till), o = fixture.test/orders.Order {Quantity: 4, UnitCents: 250} (fixture.test/orders.Order), returned 1000 (int), 1.50 ms under the debugger, from till_test.go:8 in orders.TestTill
the program ended with exit code 1
the program printed:
  Building tests of C:\work\shop\go
  --- FAIL: TestTill (0.07s)
      till_test.go:11: got 2425, want 2500
  FAIL
> stop
dlv and the program it ran have exited
```

The arguments are read as the call starts, so call 3 shows `rung: 2` before
`Ring` adds one. `trace` is Go only for now; in Python, break inside the
function and run instead.

### A line with no statement

A break on a line `dlv` cannot stop at, such as a blank line or a brace, is
refused before the program runs, and the error names the nearest lines that
hold a statement. Nothing has run, so the model moves the break and calls
`run`.

```bash
tofu tool debug '{"steps":["start test TestTill","break till.go:7","run"]}' --dir .
```

```text
debug: break till.go:7: dlv could not set it: could not find statement at C:/work/shop/go/till.go:7, please use a line with a statement. the nearest lines with a statement are till.go:8, and the program has not run
> start test TestTill
test TestTill started under dlv, pid 420: break where it should stop, then run
> break till.go:7
```

### A debugger that is not installed

tofu never installs a debugger. When one is missing, the error names the
command that installs it:

```bash
tofu tool debug '{"steps":["start test TestTill"]}' --dir .
```

```text
debug: start test TestTill: dlv dap is not installed, and tofu never installs it: install it with go install github.com/go-delve/delve/cmd/dlv@latest
> start test TestTill
```

For Python the command is `python -m pip install debugpy`.

## What it debugs

| Language | Debugger | `start test` | `start <path>` |
|---|---|---|---|
| Go | `dlv` | the tests of the package in the working directory, all or the ones named | a package directory or a `.go` file, with its arguments |
| Python | `debugpy` | `unittest` ids such as `test_order.OrderTest.test_discounted` | a `.py` file, with its arguments |

`start test` picks Go when the working directory has a `go.mod` and Python
when it has a `.py` file. A second `start` stops the open session first and
says so. A session left open is stopped after 5 minutes unused, and the next
call says it was. A `run` that neither stops nor ends within 2 minutes
returns an error. End a session
with `stop`, so the debugger and the program both exit.

## Steps

The model calls `debug` with one parameter, `steps`, a list of strings run
in order. A step that fails stops the list and returns the error with
everything the earlier steps printed.

| Step | What it does |
|---|---|
| `start test [names...]` | debugs the tests of the working directory, all of them or the ones named |
| `start <path> [args...]` | debugs a program: a Go package directory or file, or a Python file |
| `break <path:line> [--if <expression>]` | stops there, only when the expression is true |
| `run` | starts or continues until a break or the end |
| `step`, `step in`, `step out` | move one line, into a call, or out to the caller |
| `locals` | prints the values at the stop |
| `eval <expression>` | prints one value at the stop |
| `trace <package.Function>` | Go only: runs to the end and lists each call of the function |
| `stop` | ends the program and the debugger |

`tofu tool` runs one call by hand; see [One tool call by hand](/docs/cli/tool).
