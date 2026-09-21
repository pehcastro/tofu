---
id: metrics
domain: qa
source: .local/sources/qa-skills/skills.sh/petrkindlmann/qa-skills/skills/qa-metrics/SKILL.md
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
