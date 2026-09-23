---
id: metrics
domain: qa
document: qa-metrics, a QA skill about what to count and what to do when a number goes red
found: published on skills.sh under a contributor account, copied into a local archive on 2026-09-21 and not committed here
---

# What is worth counting

A metric with no action attached is decoration. Before a number is tracked, three things are fixed: the threshold that triggers something, the thing that happens, and who does it.

## Leading and lagging

A lagging number tells you what already went wrong: a defect found in production, time to resolution. A leading number still lets you change the outcome: flakiness rate, skipped test count, coverage moving on a pull request. Attention goes to the leading ones because they are the only ones still actionable.

## A single number is nearly useless

Coverage at 72 percent means nothing. Coverage moving 68 to 72 over three sprints means something. Report the direction and the spread, never a bare middle.

## Coverage measures execution, not verification

Code can be executed by a test that asserts nothing. Coverage paired with a mutation score is a truer picture, and that is why `bench/testquality` counts dead tests rather than lines. A suite with high coverage and escaping defects has tests without teeth.

## Counting what is not there

Skipped, disabled and pending tests are invisible coverage gaps. Their count trends toward zero or it is a gap that no dashboard shows. In this project a skip is already a result that must be named, which is the same rule reached from the other side.

## Vanity

Test count, lines of test code, number of dashboards. None of them connect to whether a person hit a bug. Three numbers are enough to start with, and a fourth is added only when somebody can say what action it triggers.

## Anti-patterns worth naming

- comparing two components by the same target, when one is a payment path and the other an admin screen
- gaming a number with trivial tests, which coverage alone cannot detect and a mutation score can
- tracking twenty five metrics and acting on none

## The passage this was drawn from

Quoted from *qa-metrics*, three of its five core principles and the section on skipped tests. An em dash in the original heading of the second is written here as a colon, which is the only change.

> ### 1. Metrics Should Drive Action, Not Just Dashboards
> A metric without an action plan is decoration. For every metric, define: what threshold triggers action, what the action is, and who takes it. If flakiness crosses 5%, the on-call engineer investigates the top 3 flaky tests that week. No ambiguity.

> ### 2. Leading vs Lagging: Track Both, Act on Leading
> Defect escape rate and MTTR are **lagging**: you learn after users were hurt. Flakiness, coverage delta, and skipped-test count are **leading**: they predict escapes before they happen. Lagging metrics tell leadership whether quality is moving; leading metrics are where engineers spend their daily attention because they can still change the outcome.

> ### 3. Trend Over Snapshot
> A single number is nearly useless. Coverage at 72% means nothing; coverage trending 68% to 72% over three sprints tells a story. Display metrics as time series and evaluate direction, not absolute position.

> #### Disabled and Skipped Test Count
> Total tests marked `skip`, `disabled`, `pending`, `xit`, `xdescribe` or equivalent.
>
> **Target:** Trend toward zero. Skipped tests older than 2 sprints: fix or delete. Add a CI step that fails if skipped count exceeds 5% of total tests.
>
> **Why it matters:** Skipped tests are invisible coverage gaps. A suite with 500 passing and 150 skipped tests has a `150 / (500 + 150) = 23%` gap that dashboards hide.

The original's front matter carries an author handle and an MIT licence line. Neither is copied here.
