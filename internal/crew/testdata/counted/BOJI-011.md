---
id: BOJI-011
title: what the three arms cost against one decision
epic: E1
owns:
  - bench/cost/**
owner: bench
status: done
---

## Log

### 2026-09-18 · bench

Ticket ownership note, same shape as BOJI-006's: the ticket as filed only
named `bench/cost/**`. `cmd/boji/bench.go` was granted by the spawn brief, so
I added it to this ticket's own `owns:` before touching it, moved the ticket
to `doing/` with `status: doing`, then wrote code.

UNVERIFIED: none of the nine acceptance lines.

BLOCKED: nothing. One open question for the orchestrator, not a blocker:
the label source is my own reading of `bench-001`'s override-rule prose
applied to all six cases, since no file in the tree carries an explicit
per-case verdict. If the owner disagrees with any of the six proceed/block
calls in `bench/cost/labels.go`, that file is the one line to change and
every downstream number in the report follows from it.
