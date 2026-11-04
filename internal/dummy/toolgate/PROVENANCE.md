# Where the tool gate cases came from

Moved here from `bench/corpus/gate/` and `bench/suites/judge/cost/answers/` on 2026-11-03, TOFU-2282, byte for byte, because `bench` and `internal/judge/state` both read them. `bench/kit/corpus` reads the cases and the split; `bench/suites/judge/cost` and `internal/judge/state` read the held-out answers.

The four corpus files are 348,428 bytes. **Recorded**, with six written rows named below.

## cases.jsonl

178 labelled tool-gate cases, added on 2026-09-19. Every row carries its own `origin` and a `recorded` flag, which is the thing this file gets right that the rest of the tree did not: the caveat travels with the row. 172 rows have `origin: claude-code-transcript` or a session origin and `recorded: true`; their `recorded_at` is the transcript event timestamp.

**Six rows are written**, `auth-case-1` to `auth-case-6`, and they are the six states in `bench/corpus/` that `provenance.json` there declares as authored fresh for BOJI-006. They are 3.4% of this file and they carry hand labels like the rest. See `bench/corpus/PROVENANCE.md` for what is guessed in them and for the leakage found in `auth-case-5`.

Labels are hand labels, `label_by: agent`, with a `label_note` per row saying the rule applied. One reviewer, no second opinion, which is the largest known weakness of the label set.

## cases-whole.jsonl and whole-provenance.json

33 rows recovered on 2026-09-19. The `command` field of these cases was cut at recording time and carried a truncation marker; each one was recovered from the Claude Code transcript of the session it was recorded in, matched on `recorded_at`, which is unique across the file. `whole-provenance.json` beside them lists the recorded and whole length of every one of the 33 and names the transcript file. Everything else in each row is byte identical to `cases.jsonl`, which is what every report still reads.

## split.json

The train and heldout split, created 2026-09-19, 89 and 89. The method is in the file: stratified on label, then alternating over the ids of each label class sorted by the hex sha256 of the id. **The rule reads nothing but the id and the label**, so it can be recomputed and could not have been fitted to any arm. A digest of the heldout set is stored with it.

## heldout-2026-09-19.jsonl

**Recorded**, on 2026-09-19. 229 rows, 57,805 bytes: the per-case output of arms run over the heldout half of `split.json`. Nothing here was authored to make a number look a way.

Each row carries its own `source` string saying where it came from and an `answers_known` flag saying whether the per-question answers survived. **That flag is the important field.** The rows sourced from the `report-2026-09-19` agreement table carry `answers_known: false` and a verdict only, because the paid run that produced them persisted no per-case answers. A reader should not compute a calibration figure over those rows: there is nothing in them to calibrate.

`live` says whether the row came off a paid call or an offline arm.

The rows carry no free text at all, only a case id, an arm name, a label, a verdict, a source string and four floats, so there is no prose for an answer to hide in. Two of the six written rows, `auth-case-1` and `auth-case-5`, are in the heldout half and therefore in this file.

## Leakage

**The written rows are the only ones the rule applies to**, and they are checked in `bench/corpus/PROVENANCE.md`: one of six leaks, `auth-case-5`, not repaired.

**All 178 rows were checked anyway**, on 2026-09-23: each row's own `label` searched as a whole word inside its `state` object, and separately the full verdict vocabulary searched the same way. **Rows whose state names its own label: zero.** Rows whose state contains some verdict word: 16, every one of them a real command or user message that happens to contain `block`, `safe` or `allow`, never the row's own answer. A recorded state cannot be written to name its verdict, so a hit there would be an accident rather than a corpus defect.

The held-out answers are recorded too, and were checked rather than waved through. Method, run on 2026-09-23: every row's `label` value searched as a whole word in the rest of that row, with `label` and `verdict` themselves excluded because those two are the scored pair and not input. **Count over 229 rows: zero.** The exclusion matters and is stated rather than buried: `verdict` equals `label` in 194 of the 229 rows, which is the agreement figure this file exists to produce. A check that did not exclude it would report 194 leaks and mean nothing by it.

## What was removed

Every row passed through `corpus.Scrub`. Scanned again on 2026-09-23 for every identity and credential pattern in `Scrub`: no hit in any of the five files. The recovered commands in `cases-whole.jsonl` carry the same em-dash-to-double-hyphen substitution the original recording made.
