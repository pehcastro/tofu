---
id: flakiness
domain: qa
document: Flaky Test Quarantine, a QA skill written for Playwright and Jest trees
found: published on qaskills.sh under a contributor account, copied into a local archive on 2026-09-21 and not committed here
---

# Flakiness

A flaky test produces different outcomes on the same code under the same conditions. It is the one property of a suite that is arithmetic rather than judgment: run the package N times and compare.

## Detection comes before anything else

One failure is not flakiness, it is a candidate bug. A test is flaky when it disagrees with itself across runs of unchanged code. Until that is measured, nothing else in this file applies.

the flakerun tool named in the `flake_disagreement` rule's `measurement` field is the measurement here. It runs one package at a time, parses `go test -json`, and reports every test whose outcomes across runs are not all identical.

## Four outcomes, not two

Pass and fail are the obvious pair. Two more matter:

- skip: a result, counted and named, never folded into pass. A suite with 500 passing and 150 skipped tests hides a coverage gap of 23 percent behind a green bar.
- absent: a test that ran in one pass and not in another. Absence is a disagreement, because a test that sometimes does not exist cannot be trusted when it does.

## The root cause decides the fix

- timing: the test waits on wall clock instead of on the condition. Wait for the condition.
- shared state: one test leaves behind what another reads. Give each test its own copy.
- an external dependency: the test reaches something it does not control. Move the boundary.
- ordering or parallelism: the test passes alone and fails in the suite. Reproduce it in the suite, not alone.

## What makes it worse

A retry, a longer timeout, or a rerun until green. Each hides the non-determinism and lengthens the run. Quarantine is not deletion either: a quarantined test keeps running somewhere and keeps being counted.

## What this repository already knew

A real flake was found here before any of this existed: a test in one package wrote the committed corpus that a second package read, so the second package disagreed with itself depending on which package ran first. That is the shared-state category, and it was found by a person rather than by a measurement.

## The passage this was drawn from

Quoted from *Flaky Test Quarantine*, its opening and four of its seven core principles. The numbering is the original's.

> Flaky tests are tests that produce different outcomes (pass or fail) when run against the same code under the same conditions. They erode confidence in the test suite, train developers to ignore failures, and slow down CI pipelines with unnecessary retries. A single flaky test in a suite of 500 can cause the entire pipeline to require re-runs, wasting developer time and compute resources.

> 1. **Detection Before Quarantine**: A test must be proven flaky through repeated execution before it is quarantined. A single failure does not make a test flaky; it might indicate a real bug. Multi-run analysis with statistical tracking separates genuine flakiness from legitimate failures.

> 2. **Quarantine Is Not Deletion**: Quarantined tests must remain in the codebase and continue running in a separate pipeline. Quarantine is a temporary holding pattern that prevents flaky tests from blocking the main pipeline while preserving the test and tracking its behavior.

> 3. **Root Cause Categorization Drives Fixes**: Different types of flakiness require different fixing strategies. Timing issues need explicit waits, state leakage needs proper cleanup, external dependencies need mocking, and race conditions need synchronization. Categorizing the root cause directs the fix.

> 6. **Fix the Root Cause, Not the Symptom**: Adding retries or increasing timeouts masks flakiness without fixing it. These approaches hide real issues and increase overall test execution time. Address the underlying non-determinism.

> 7. **Isolation Verification**: After fixing a flaky test, verify the fix by running the test in isolation and in the full suite multiple times. Some flakiness only manifests under specific ordering or parallel execution conditions.

The rest of that document is TypeScript for a quarantine registry, a Playwright fixture and two GitHub Actions workflows. None of it applies to a Go tree, and the flakerun tool is the measurement instead.

The fourth outcome above, absent, is ours and not the original's: it has pass, fail and skip.
