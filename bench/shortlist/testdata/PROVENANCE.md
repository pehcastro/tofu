# Where shortlist-corpus.jsonl came from

**Recorded**, with **hand labels**. Four rows, 28,278 bytes, added 2026-09-20. The `task` is the user's own prompt, verbatim, the `files` are the tree as it stood, whole, and the `label` is the set of paths the turn actually changed, read from its own write and edit calls rather than decided afterwards.

**All four rows come from one turn**, `turn-18d6a7961d933ee8`: three numbered items of its prompt taken separately and the whole prompt as a fourth. One session, one project, one afternoon.

## The corpus is four rows, and that is the finding

`report-2026-09-21.md` opens on it. The ticket asked for at least thirty file-localisation questions. `.tofu/sessions` carried 131 recorded turns, 20 of which ever call `edit` or `write`, and 12 of those 20 create a file that did not exist, which is not localisation. **Four survived.** Nothing measured on four rows separates two arms, and the report says so rather than printing a ranking.

## Leakage: three of four rows leak, and they are not repaired

**Method, run on 2026-09-23**, the same rule `bench/tools/corpus/discipline_test.go` applies: split every path component and file stem of a row's own label into words of three characters or more, split on non-alphanumerics and on camel case, lowercase both sides, and look for any of them in the content words of the task.

| row | label | words of the answer that appear in the question |
|---|---|---|
| `turn-18d6a7961d933ee8#item3` | `src/store.ts` | none |
| `turn-18d6a7961d933ee8#item6` | `README.md` | `readme` |
| `turn-18d6a7961d933ee8#item7` | `package.json` | `package`, `json` |
| `turn-18d6a7961d933ee8#whole` | six paths | `src`, `package`, `json`, `readme` |

**Three of four.** The task says "Update README.md so it lists the routes" and the answer is `README.md`. The task says "Add a check script to package.json" and the answer is `package.json`. A regular expression that pulls a filename out of the prompt gets both, and no ranking arm scored on this file can be told apart from that regular expression.

**Not repaired.** The rows are real recordings and the prompts are the user's own words; editing either would make the corpus written rather than recorded, which is a worse trade than admitting the leak. The real repair is more rows, and `report-2026-09-21.md` already says the population to draw them from does not exist yet. This needs its own ticket.

**Read every `file_shortlist` number from this corpus as measuring the corpus.** One clean row is not a measurement.

## Parked on 2026-09-23 by TOFU-449, and what un-parks it

`answerwords_test.go` is the refusal, and it skips rather than failing: the three rows stay on disk exactly as recorded, and the skip names them every run. `live_test.go` skips before it reads the credential, so no `TOFU_LIVE=1` run prints an arm table off these four rows.

**The population was recounted, not repeated.** `population_test.go` reads `.tofu/sessions` through `bench/corpus.WalkSessions` and counts a turn as able to yield a shortlist question when it carries a non-empty task, when a `write` or an `edit` in it names a path, and when at least one changed path was already there before the turn, read off the turn's own calls: the first call touching it is a `read` or an `edit`, never a `write`. Turns are grouped by the exact task text, because a rerun of one prompt is one question asked twice. On 2026-09-23: 110 entries, 109 turns read, 1 unreadable, 0 with no task, 65 changing no file, 37 creating only files that did not exist, **7 recordable, carrying 2 distinct tasks.** Six of the seven are reruns of the prompt this corpus already comes from. The other is the Hono build turn, which `report-2026-09-21.md` rejected for building from nothing.

So the corpus cannot be repaired by adding rows either. `population_test.go` fails, rather than passing, once 30 distinct recordable turns exist, and that is the signal to rebuild and lift the park.

## What it does not carry

Every row goes through `corpus.LeaksIn` on load, in `corpus.go`, and `leak_test.go` proves the refusal fires on a planted home path and on a planted credential shape. Scanned again on 2026-09-23: no hit.
