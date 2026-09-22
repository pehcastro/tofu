# tofu

Tofu is a personal Go coding-agent harness where the code owns the
loop, a language model plans and writes, and a typed decision model
classifies state at the points where other harnesses use a prose rule
or a regular expression.

It is personal and not released. There is no support, no release, no
package and no promise that anything keeps working. It is here so a
friend can read it and run it.

## The thesis

Every decision point gets a typed judgment instead of a prose rule,
and every judgment gets an arm that turns it off and does the same
job for free. The arm is the thing the judgment actually replaces: a
plain regular expression where a regular expression could do the job,
never an agent turn against a tool call.

The honest part is that several of those judgments lost to their free
arm and are switched off:

- The ask gate, which decides whether an agent stops to ask or
  defers the question: 4 of 7 recorded moments right against the free
  arm's 3 of 7, and the free arm's three were exactly the three the
  ownership boundary already answers for nothing. Four items is a
  coin flip that landed once. It stays in shadow.
- The stop check, which decides whether the loop runs another model
  step: 71 of 78 labelled steps against the cheap arm's 68. On the
  15 steps labelled stop, the class that matters, the cheap arm
  caught 14 and the typed arm caught 11. Neither is fit to end a
  turn, so the point stays in shadow.
- The test-quality rules, which are meant to make the model write
  fewer dead tests: 53 dead tests with the rules on and 53 with them
  off, over nine comparable tasks. Net zero, so nothing blocks on
  them.

One that won, measured the same way: picking a search pattern by
judgment finds the right line in 36 of 100 questions against plain
ripgrep's 28, and the whole gap is in the band of questions written
to share no words with the code, 13 of 40 against 5 of 40.

## The money rule

The API key pays for the decision model and nothing else. Every
language model call rides a subscription you already pay for, by
speaking the vendor's own protocol with a credential tofu mints
itself. No model runs on an API key, including in a benchmark, where
the comparison is the vendor's own command line driven the way a
person drives it.

So a model slug names what pays, not who built the model.
`claude-sub/claude-opus-5` is a subscription and
`anthropic/claude-opus-5` is a key, and they are different money.

## Running it

Go 1.25.8 or later, which is what `go.mod` asks for. No cgo, no
daemon, one binary.

```
go build -o tofu ./cmd/tofu
```

Ask it whether it can run here, and what is wrong if it cannot:

```
./tofu doctor
```

The version, the commit it was built from and the Go version:

```
./tofu version
```

Every verb, with one line each:

```
./tofu help
```

What the rule library holds in this project:

```
./tofu rules list
```

What changed since the version you last read:

```
./tofu changelog
```

Running `tofu` with no verb starts the terminal app in the current
directory, and `tofu --continue` starts it on the session you last
worked in. Both want a subscription credential, which `tofu login`
mints.

## Where the numbers are

`bench/` is the reason to believe any of the above. Each package
there measures one mechanism against the arm that turns it off and
writes a dated report beside the code, so `bench/ask`,
`bench/stopcheck` and `bench/testquality` hold the runs the numbers
above come from, with their corpus, their skips and their cost.
`bench/tools` is the exception and has no dated report yet: its
numbers are in `.local/boji/tools_benchmark.md` instead.

A report says which arm won and what it cost. When a judgment loses,
the report says so and the judgment stays off.

## What changed

`CHANGELOG.md`, kept by hand. It says what changed for someone
driving the binary: the verbs, their flags, the exit codes, and the
library, ledger and policy schemas. It never goes to 1.0.0, so a
minor bump can break any of that.
