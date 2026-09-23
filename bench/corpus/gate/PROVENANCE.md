# Where the gate corpus came from

Four files, 348,428 bytes. **Recorded**, with six written rows named below.

## cases.jsonl

178 labelled tool-gate cases, added on 2026-09-19. Every row carries its own `origin` and a `recorded` flag, which is the thing this file gets right that the rest of the tree did not: the caveat travels with the row. 172 rows have `origin: claude-code-transcript` or a session origin and `recorded: true`; their `recorded_at` is the transcript event timestamp.

**Six rows are written**, `auth-case-1` to `auth-case-6`, and they are the six states in `bench/corpus/` that `provenance.json` there declares as authored fresh for BOJI-006. They are 3.4% of this file and they carry hand labels like the rest. See `bench/corpus/PROVENANCE.md` for what is guessed in them and for the leakage found in `auth-case-5`.

Labels are hand labels, `label_by: agent`, with a `label_note` per row saying the rule applied. One reviewer, no second opinion, which is the largest known weakness of the label set.

## cases-whole.jsonl and whole-provenance.json

33 rows recovered on 2026-09-19. The `command` field of these cases was cut at recording time and carried a truncation marker; each one was recovered from the Claude Code transcript of the session it was recorded in, matched on `recorded_at`, which is unique across the file. `whole-provenance.json` beside them lists the recorded and whole length of every one of the 33 and names the transcript file. Everything else in each row is byte identical to `cases.jsonl`, which is what every report still reads.

## split.json

The train and heldout split, created 2026-09-19, 89 and 89. The method is in the file: stratified on label, then alternating over the ids of each label class sorted by the hex sha256 of the id. **The rule reads nothing but the id and the label**, so it can be recomputed and could not have been fitted to any arm. A digest of the heldout set is stored with it.

## Leakage

**The written rows are the only ones the rule applies to**, and they are checked in `bench/corpus/PROVENANCE.md`: one of six leaks, `auth-case-5`, not repaired.

**All 178 rows were checked anyway**, on 2026-09-23: each row's own `label` searched as a whole word inside its `state` object, and separately the full verdict vocabulary searched the same way. **Rows whose state names its own label: zero.** Rows whose state contains some verdict word: 16, every one of them a real command or user message that happens to contain `block`, `safe` or `allow`, never the row's own answer. A recorded state cannot be written to name its verdict, so a hit there would be an accident rather than a corpus defect.

## What was removed

Every row passed through `corpus.Scrub`. Scanned again on 2026-09-23 for every identity and credential pattern in `Scrub`: no hit in any of the four files. The recovered commands in `cases-whole.jsonl` carry the same em-dash-to-double-hyphen substitution the original recording made.
