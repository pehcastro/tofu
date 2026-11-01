---
name: flake-triage
domain: qa
description: Prove a test disagrees with itself, categorise why, and fix the cause rather than the symptom. Use when a test passed on a rerun, when a suite is red without a code change, or when nobody trusts the pipeline.
references:
  - flakiness
  - failure-triage
---

# Flake triage

## 1. Prove it

Run the package N times with the cache off, each run its own process, and compare per test outcomes. Three runs finds the obvious case, ten finds the rest. One failure is not proof.

In Go, one bash call per run, since bash refuses a loop:

```
go test -count=1 -v -run '<pattern>' <package>
```

Read each run's `--- PASS`, `--- FAIL` and `--- SKIP` lines. Anything else runs its own test command once per call, and a vitest file reruns with the test tool. A test disagreed with itself when its outcomes are not the same in every run. Count a test skipped in every run separately, because a skip is a result and not an absence.

## 2. Read what disagreed

The outcome sequence is the first clue. `[pass fail fail]` is state left behind by the first run. `[pass fail pass]` is timing or ordering. `[pass absent pass]` means the test did not exist in one run, which is a build or a registration problem and not flakiness at all.

## 3. Categorise before fixing

Take the class from the failure-triage reference, then the cause from the flakiness reference: timing, shared state, an external dependency, or ordering. The category names the fix. Skipping this step is how a wait gets added.

## 4. Fix the cause

A longer timeout, a retry, or a rerun is not a fix. It hides the non-determinism and lengthens every future run.

## 5. Prove the fix the same way

Re-run the same measurement with the same N. A fix that was never measured is a claim. If the test only misbehaves inside the full suite, the proof has to run it inside the suite too.

## 6. Quarantine is a last resort with a clock on it

A test that cannot be fixed now keeps running somewhere and keeps being counted, and it carries the date it was quarantined. A quarantine with no date becomes a deletion nobody voted for.
