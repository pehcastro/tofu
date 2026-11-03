# Where readworth-corpus.jsonl came from

**Recorded**, in the text, with **hand labels**. 70 rows, 115,028 bytes, added 2026-09-20 for the read-worth measurement.

Every paragraph is a real paragraph this harness wrote, drawn from `.tofu/sessions` through `bench/corpus.WalkSessions` rather than by reading a session file directly. The `task` is the user's own request from the session the paragraph came out of. `position` (`opening`, `middle`, `closing`) and `already_said`, the earlier paragraphs of the same reply, are carried through unchanged, because the question set reads both.

**The `keep` label is written, and by one person.** 35 keep, 35 drop, labelled against the `read_worth@1` criteria: does the paragraph carry the answer, a finding, a measurement or a caveat the reader has to act on, versus preamble, recap, hedging, jargon or selling with nothing under it. No second reviewer was in the loop. That is a limitation of every accuracy figure this corpus produces and it is named in `report-2026-09-21.md` as well.

## What a reader should not conclude

25 distinct tasks are represented, but the tree exposes roughly 246 candidate paragraphs at production split granularity. **70 rows is a sample of that ceiling, not the whole of it.** It is also one person, one repository and a narrow band of tasks.

The corpus could not exist before `bench/corpus/reader.go` learned to carry `AssistantText`: both session schemas silently dropped reply text, so `WalkSessions` reported zero paragraphs. Any measurement dated before that change did not see this data.

## Leakage, checked on 2026-09-23, none that separates a label

**Method.** The answer is a boolean, `keep`, so leakage is a row whose paragraph names its own verdict. Two passes over all 70 rows, 35 keep and 35 drop.

- the label vocabulary searched as whole words in `paragraph` and `already_said`: `keep`, `drop`, `worth`, `useful`, `useless`, `preamble`, `filler`, `padding`, `recap`, `noise`. **13 rows hit, and the hits do not carry a label.** `worth` appears in 11, 5 drop and 6 keep, which is the 50/50 split of the corpus itself. `useful` appears in 2, both drop. `keep` and `drop` appear in none. **Zero rows where the word predicts the verdict.**
- the label distribution checked against the two fields an arm should not be able to win on alone, `position` and the paragraph's own length, to see whether the label is recoverable without reading the text. It partly is: the length floor arm beat the judged arm in `report-2026-09-21.md`. **That is a finding about the arms and about this corpus, and it is already the headline of that report rather than something this file discovers.**

The second point is the honest caveat. A corpus where a free length rule wins is either a corpus of easy cases or a signal that the decision does not need a model, and the report chose the second reading. Nothing here separates them.

## What it does not carry

Every row goes through `corpus.LeaksIn` on load, in `corpus.go`, and `leak_test.go` proves the refusal fires on a planted home path and on a planted credential shape. Scanned again on 2026-09-23: no hit.
