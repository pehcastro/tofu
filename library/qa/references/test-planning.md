---
id: test-planning
domain: qa
document: Test Plan Creator, a skill that produces a test plan template and the rules for filling it in
found: published on qaskills.sh under a contributor account, copied into a local archive on 2026-09-21 and not committed here
related: a second skill, qa-testing-strategy, published on skills.sh, which orders the layers by where a defect originates
---

# Planning a test before writing one

A plan is decided before a test exists. What it fixes is the scope, the layer, the order and the end condition, and every one of those is cheaper to change on a page than in a suite.

## Missing detail becomes an assumption, never a guess

Ask only for what is actually missing: what changed, what the acceptance is, what runs it, what data it needs. Anything still unknown is written down as an assumption, and an assumption that would change the plan if it were wrong is marked as a risk.

## The out of scope list decides as much as the in scope list

An exclusion nobody wrote down reads as an oversight later. It is usually the shorter list and the faster one to write.

## Order by risk, not by coverage

Authentication, money, data loss and anything irreversible come first. A plan that tries to cover everything covers the cheap things.

## Say what ends it

Entry: the build is deployed and the data exists. Exit: the high priority cases ran, no blocker is open, and the known issues are written down and accepted. Without an exit condition a QA pass ends when somebody gets tired.

## Never promise automation that will not be written

An automation column filled in optimistically becomes a coverage number that is not true.

## The passage this was drawn from

Quoted from *Test Plan Creator*, its inputs list, its entry and exit criteria, and its style rules.

> ## Inputs to ask for when missing
>
> Ask only for information that is required to create a useful test plan. If some details are missing, make reasonable assumptions and clearly list them.
>
> Useful inputs include:
>
> - Feature name or change summary
> - Requirements, acceptance criteria, user stories, or design links
> - Target platform, browser, device, OS, or API version
> - Environment details such as dev, QA, staging, or production
> - Release date or testing window
> - Known risks, dependencies, and integrations
> - Test data needs
> - Automation scope
> - Out-of-scope areas

> ## 8. Entry Criteria
>
> - Requirements are finalized enough for testing.
> - Build is deployed to the target environment.
> - Test data is available.
> - Critical dependencies are working.
>
> ## 9. Exit Criteria
>
> - All high-priority test cases executed.
> - No open blocker or critical defects.
> - Known issues are documented and accepted.
> - Test summary is shared with stakeholders.

> ## Style rules
>
> - Be specific and actionable.
> - Avoid vague phrases like "test everything."
> - Prioritize risk-based testing.
> - Use clear priority labels: `High`, `Medium`, `Low`.
> - Separate smoke, regression, and full test scope.
> - Include negative, edge, and integration scenarios.
> - Do not overpromise automation coverage.
> - Mention assumptions instead of pretending missing details are known.

The rest of that document is a twelve section plan template with a sign-off table, written for a team with a release owner and stakeholders. One of its example tables carries a placeholder email address, which is not copied here. The layer table in `library/qa/general/skills/test-plan.md` is ours, not the original's: it ships a list of testing types instead.
