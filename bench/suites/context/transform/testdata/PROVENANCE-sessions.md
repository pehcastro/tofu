# Which recorded turns this bench measures, and where they live

**The recordings are not here.** They live in `bench/stopcheck/corpus`, the one committed copy, and this bench reads them from there. Until 2026-09-23 the same 17 files, 58,183 bytes, were also committed under `bench/transform/testdata/sessions`, byte identical, with nothing checking that the two copies stayed so. TOFU-456 removed this copy and left the superset.

**The census is 17 of the 19 turns in that corpus.** Two are measured elsewhere or carry nothing to measure, and `measure_test.go` names both with the reason:

- `turn-18d6a27c7dfb1644.json` is the v2 maintenance session. It is measured on its own, against `testdata/v2-before`, by `TestBothArmsOverTheV2MaintenanceSession`, and counting it here too would report its 10 write calls twice.
- `turn-18d6a5df2caeac68.json` was recorded after this census was frozen and carries no write call, so it changes no figure either way.

The count is asserted: a turn arriving in or leaving the shared corpus fails `load` rather than moving every figure quietly.

`transform.Load` reads every `write` call out of the 17 and pairs each one with the file's prior state from `before`. The census that comes out is 14 writes across 17 turns: 11 create a file that did not exist and 3 change an existing one.

## What a reader should not conclude

**A typed edit cannot help on 11 of the 14 writes, by construction**, because there is nothing to edit. The three that could were on files of 9 and 11 lines. The 45-fold saving in `report-2026-09-20.md`, 4,284 output tokens against 96, is one line changed in the largest recorded file here, and "largest" means eleven lines. The saving is real and this corpus has almost no instance of it.

## Leakage

**Recorded, so the written-corpus rule does not apply**, and it was checked anyway on 2026-09-23. Method: the outcome scored from each turn is a token count computed from the write call's own content, so there is no label a transcript could name. The turns carry no verdict field, no grade and no annotation of any kind: they are session files. **Count: zero eligible rows, zero leaks.**

## What it does not carry

Every file is a fixed point of `bench/corpus.Scrub`. Scanned again on 2026-09-23 for every identity and credential pattern in it: no hit.
