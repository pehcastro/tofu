---
name: test-plan
domain: qa
description: Decide what a change needs tested before writing a test. Use when a feature is about to be verified, when a release needs a scope, or when somebody asks for a QA plan.
references:
  - test-planning
  - metrics
  - failure-triage
source: library/qa/references/test-planning.md
---

# Test plan

A plan is what a person decides before a test exists. It is short, it is specific, and a missing detail is written down as an assumption rather than guessed silently.

## Ask only for what is missing

What changed, what the acceptance is, what runs it, what the data needs are, and what is deliberately out of scope. Anything still unknown is listed as an assumption, and a risky one is marked as a risk.

## Say what is out of scope

An unwritten exclusion reads as an oversight later. The out of scope list decides as much as the in scope list and is usually shorter to write.

## Choose the smallest layer that can fail

The layer is chosen by where the defect would originate, not by habit.

| Layer | What it proves |
|---|---|
| unit | a rule, an invariant, a parser |
| component | one piece behaving in its real container |
| contract | two sides of a boundary still agree |
| integration | a real dependency behaves as assumed |
| end to end | one journey a person actually takes |
| property | an invariant over generated input |

An end to end test written where a unit test would do costs more on every run forever and fails for more reasons than the one it was written for.

## Order by risk, not by coverage

The paths that hurt when they break come first: authentication, money, data loss, anything irreversible. A plan that tries to cover everything covers the cheap things.

## Say what ends it

Entry: the build is deployed and the data exists. Exit: the high priority cases ran, no blocker is open, and the known issues are written down and accepted. Without an exit condition a QA pass ends when somebody gets tired.

## Never promise automation you will not write

An automation column filled in optimistically becomes a coverage number that is not true. Write the scope that will exist.
