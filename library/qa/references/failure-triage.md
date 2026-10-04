---
id: failure-triage
domain: qa
document: a seven step loop to explore a failure, reproduce it and report it
---

# Triage before acting on a failing test

A failing test is one of four things, and the four take opposite actions. Classifying before touching anything is the whole discipline.

| Class | What it looks like | What to do |
|---|---|---|
| A real defect | the code behaves wrong against the requirement | stop, report it with a reproduction, change nothing in the test |
| A wrong expectation | the assertion encodes something that was never true | fix the test |
| A stale reference | the thing being addressed moved or was renamed | re-address it by the most stable signal available |
| Flakiness | it passes on a rerun | remove the race, never add a wait |

The rule that carries the rest: never make a failing test pass by weakening it. A test edited until it is green hides the defect it was written to find, and the edit is invisible in a diff that only shows a changed expected value.

## A rerun that passes is not a resolution

It is unresolved flakiness debt. Count it as such and send it to `references/flakiness.md`.

## Choose the smallest scope that reproduces it

One failing case is triaged at its own layer first. Widening to the full suite as the first response wastes the run and tells you less, because a failure that reproduces in one package and not alone is itself the finding.

## Escalate rather than absorb

A real defect goes back to the person or the agent that owns the code, with the reproduction and the evidence, not into a quiet test edit.

## Never reproduce against production

A destructive or financial action is reproduced in a test or staging environment with test accounts. When production is the only place it can run, stop and say so.
