---
id: flakiness
domain: qa
source: .local/sources/qa-skills/qaskills.sh/Pramod/flaky-test-quarantine/SKILL.md
---

# Flakiness

A flaky test produces different outcomes on the same code under the same conditions. It is the one property of a suite that is arithmetic rather than judgment: run the package N times and compare.

## Detection comes before anything else

One failure is not flakiness, it is a candidate bug. A test is flaky when it disagrees with itself across runs of unchanged code. Until that is measured, nothing else in this file applies.

`bench/testquality/flakerun <runs> <package>...` is the measurement here. It runs one package at a time, parses `go test -json`, and reports every test whose outcomes across runs are not all identical.

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

A real flake was found here before any of this existed: a test in `bench/sift` wrote the committed corpus that `bench/corpus` read, so the second package disagreed with itself depending on which package ran first. That is the shared-state category, and it was found by a person rather than by a measurement.
