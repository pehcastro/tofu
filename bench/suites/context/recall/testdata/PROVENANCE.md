# Where forks.jsonl and recorded-turn.json came from

Two files, 110,552 bytes, both **recorded**, added 2026-09-20.

## forks.jsonl

20 real context forks read out of `.tofu/sessions` across three origin lineages. A row is the fork point: the step, the task, the token counts either side, how long the fork blocked in microseconds, and the carry text and last word as the harness actually produced them.

**One of the three lineages is real work**, `turn-18d6f8d9e45f8efc`, task "did the benchmark had any results?", **and it is one row of the twenty.** The other 19 come from two lineages of a synthetic stress task run at a deliberately lowered context ceiling to make forking happen at all: 16 rows from `turn-18d6d295...` and 3 from `turn-18d6d2c8...`. Nothing on the rows themselves says this, which is why it is here. 19 of 20 forks exist because someone wanted a fork, not because a session needed one.

## recorded-turn.json

One real recorded turn, `turn-18d6a7961d933ee8`, the Hono task-list change. It is the fixture for the replay and ceiling tests and **it is not part of the fork corpus**. No fork number is computed from it.

## Leakage, checked on 2026-09-23

**Method.** The question this corpus decides is what a fork should carry forward, and the answer of the winning arm is the raw `last_word`. Leakage would be the question side, `task`, naming the last word before it is produced. Each row's `last_word` was searched as a substring of `task`, all 20 rows. **Count: zero.**

`last_word` appears inside `carry_text` in all 20 rows, and that is construction rather than leakage: the carry is built from the last word, so the field is downstream of the answer and not an input to it. It is stated because a check that did not separate the two would report 20 leaks and mean nothing by it.

**The stronger result, and it is not a leakage result.** In all 20 rows the raw last word is a bare tool-call invocation and never assistant prose, because the fork check runs before the model has had a turn to react to the tool results. **There is no prose at the fork point for a distilled arm or a judged arm to summarise.** That is why the free arm won and why gap 11 was struck rather than built: not because the judged arm was bad, but because the corpus contains nothing for it to work on. A reader should treat the recall result as "the input is empty" rather than "the model lost".

## What it does not carry

Every row goes through `corpus.LeaksIn` on load, in `forkcorpus.go`. Scanned again on 2026-09-23 for every identity and credential pattern in `bench/corpus.Scrub`: no hit in either file.
