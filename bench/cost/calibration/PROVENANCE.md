# Where the calibration files came from

Three files, 59,529 bytes. **Recorded**, on 2026-09-19, added to the tree on 2026-09-20.

## fit-2026-09-19.jsonl and verify-2026-09-19.jsonl

89 rows each, the two halves of `bench/corpus/gate/split.json`, each case asked live on 2026-09-19 through `catalog/policy/tool_gate@1.yaml` at build `typesafe/jev-1.13-20260917`, with every question answer kept. Every row carries its own `source` string saying that, plus `live: true` and `answers_known: true`. These are the rows the earlier `answers` set could not be: the per-question floats survived the run.

The split was computed before these were asked, from the id and the label only, so no arm could have been fitted to it.

## tool_gate@1.json

Not a corpus row. It is the fitted operating point and the record of what the fit was worth, and its own `provenance` field carries the whole story at length: which fields BOJI-132 removed, which readers of them are gone, and that the cut was **not adopted**. The headline in it is that at 2.125 the gate agrees with 84 of 89 verify labels and catches 1 of 6 blocks, against always-proceed at 83 and none, which is worth one case on 178 labels against a sample floor of 300. `ece` is null: it was not computed.

## Leakage

**Method, run on 2026-09-23**: each row's `label` searched as a whole word in the rest of that row, with `label` and `verdict` excluded as the scored pair. **Count: zero of 89 in each file.** The rows carry a case id, an arm, a label, a verdict, five floats and a source string, and no free text.

The states these rows score live in `bench/corpus/gate/cases.jsonl`. Six of those 178 are written rather than recorded and one of the six leaks; both facts are in `bench/corpus/PROVENANCE.md`, and four of the six are in the fit half.

## What it does not carry

Scanned on 2026-09-23 for every identity and credential pattern in `bench/corpus.Scrub`: no hit in any of the three files.
