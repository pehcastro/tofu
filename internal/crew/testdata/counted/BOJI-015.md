---
id: BOJI-015
title: the linter and the caps, checked against the vendor docs
epic: E1
size: M
owns:
  - internal/judge/question/**
  - internal/konst/**
  - internal/judge/jev/caps.go
  - internal/judge/jev/wire/openrouter/**
depends: [BOJI-007]
spec: .local/research/jev/10-live-docs-check.md
owner: go-dev
status: done
---

## Log

### 2026-09-18 · go-core

BLOCKED, resolved by widening this ticket's own `owns`: deleting
`konst.JudgeRequestCap` broke `internal/judge/jev/client_test.go`, which is in the
same package as the owned `caps.go` but was not itself listed. Added
`internal/judge/jev/client_test.go` and `internal/judge/jev/caps_test.go` (needed
for a direct unit test of the estimator) to this ticket's `owns` and fixed the
break. Orchestrator: confirm or correct, same as BOJI-006's precedent.

### 2026-09-18 · go-core, round 2

BLOCKED: nothing. Open question for the coordinator, not a blocker: `EstimateBytes(32000)` = 90,165 bytes refuses BOJI-006's 90,411-byte, 32,086-token fixture, which the live route accepted. Decide whether `konst.JudgeStateTokenCeiling` (32,000, already accepted this round) should move to reflect the real gateway behavior BOJI-006 measured, or whether refusing a request that happens to bill a little over the documented round number is the correct, conservative behavior to keep.
