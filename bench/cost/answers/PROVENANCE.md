# Where heldout-2026-09-19.jsonl came from

**Recorded**, on 2026-09-19. 229 rows, 57,805 bytes: the per-case output of arms run over the heldout half of `bench/corpus/gate/split.json`. Nothing here was authored to make a number look a way.

Each row carries its own `source` string saying where it came from and an `answers_known` flag saying whether the per-question answers survived. **That flag is the important field.** The rows sourced from the `report-2026-09-19` agreement table carry `answers_known: false` and a verdict only, because the paid run that produced them persisted no per-case answers. A reader should not compute a calibration figure over those rows: there is nothing in them to calibrate.

`live` says whether the row came off a paid call or an offline arm.

## Leakage

**Recorded, so the rule about a written corpus naming its own answer does not apply**, and it was checked rather than waved through. Method, run on 2026-09-23: every row's `label` value searched as a whole word in the rest of that row, with `label` and `verdict` themselves excluded because those two are the scored pair and not input. **Count over 229 rows: zero.**

The exclusion matters and is stated rather than buried: `verdict` equals `label` in 194 of the 229 rows, which is the agreement figure this file exists to produce. A check that did not exclude it would report 194 leaks and mean nothing by it.

The rows carry no free text at all, only a case id, an arm name, a label, a verdict, a source string and four floats, so there is no prose for an answer to hide in.

The upstream states these rows score are in `bench/corpus/gate/cases.jsonl`, and the leakage found in six of those rows is recorded in `bench/corpus/PROVENANCE.md`. Two of the six, `auth-case-1` and `auth-case-5`, are in the heldout half and therefore in this file.

## What it does not carry

Scanned on 2026-09-23 for every identity and credential pattern in `bench/corpus.Scrub`: no hit.
