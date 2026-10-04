---
id: test-planning
domain: qa
document: a test plan and the rules for filling it in
---

# Planning a test before writing one

A plan fixes the scope, the layer, the order and the end condition before a test exists, while each is cheaper to change on a page than in a suite.

## Missing detail becomes an assumption, never a guess

Ask only for what is missing: what changed, the acceptance, what runs it, the data it needs. Write anything still unknown as an assumption, and mark it a risk if being wrong would change the plan.

## Write the out of scope list

An exclusion nobody wrote down reads later as an oversight.

## Order by risk, not by coverage

Authentication, money, data loss and anything irreversible come first. A plan that covers everything covers the cheap things.

## Say what ends it

Entry: the build is deployed and the data exists. Exit: the high priority cases ran, no blocker is open, and the known issues are written down and accepted. Without an exit condition a pass ends when somebody gets tired.

## Never promise automation that will not be written

An optimistic automation column becomes a coverage number that is not true.

## Give every risk a layer

Name the layer that catches each risk: system integration, end to end, regression, compatibility or non functional. A high risk with no layer is a hole. Unit and code level integration tests belong to whoever writes the code and are a precondition, not part of the plan.

## A stage is a rule, an environment is where it runs

Assign the stage first, then where it can run. A test whose stage cannot run yet is blocked or deferred, never failed. One stage can use several environments if each can do what it needs.

## The contract is tested from outside

Whoever verifies the change checks an agreed API contract as a black box: paths, fields, status codes, errors, permissions, idempotency, compatibility. A contract suite the developer wrote is extra evidence, never the verdict.

## Name what must not regress

Before testing starts, list the capabilities and flows that must keep working, so the regression pass has a target.

## Concrete, never vague

A line such as test everything plans nothing. Name the case, the layer and the data.
