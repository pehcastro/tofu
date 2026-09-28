---
id: failure-triage
domain: qa
document: QA Agent for Claude Code, a skill describing a seven step explore to report loop
found: published on qaskills.sh, copied into a local archive and not committed here
related: a second skill, qa-testing-strategy, published on skills.sh, which says the same thing about weakening a test
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

## The passage this was drawn from

Quoted from *QA Agent for Claude Code*, its triage step and its guardrails. The action column of the original table used an em dash where a colon stands here, and the table is written as a list because the rows are long.

> ## Step 5 - Triage failures
>
> For each failure, classify before acting:
>
> - Real bug. Signal: App behaves wrong vs. the requirement. Action: **Stop and report to the user** with repro + trace: do NOT "fix" the test to pass
> - Stale locator. Signal: Element moved/renamed. Action: Self-heal (step 6)
> - Bad test. Signal: Wrong assertion/expectation. Action: Fix the test
> - Flaky. Signal: Passes on retry, timing-related. Action: Remove the race (waits/data), not add a sleep
>
> The cardinal rule: **never make a failing test pass by weakening it to hide a real defect.**

> - **Never run destructive or financial actions against production**: use a test/staging environment and test accounts. Refuse if only prod is available.
> - Escalate real bugs; never silently rewrite a test to green.

The rest of that document is a browser crawling loop, a Page Object Model and self-healing locators, which belong to a web suite and not to this tree.
