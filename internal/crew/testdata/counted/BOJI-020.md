---
id: BOJI-020
title: the shipped catalog layer resolves a bare name
epic: E1
owns:
  - catalog/questions/**
owner: bench
status: done
---

## Log

### 2026-09-18 · bench

UNVERIFIED: none of the seven acceptance lines.

BLOCKED: nothing in this ticket's own scope. Open question for the
coordinator: the general fix to `question.Resolve`/`pick` (refuse an
ambiguous bare name tree-wide, not just for the shipped catalog layer) is
still undone, lives in `internal/judge/question/load.go`, and needs its own
ticket or an explicit grant if it should happen now rather than later.
