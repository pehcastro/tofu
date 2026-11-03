# Where shortlist-corpus.jsonl came from

**Recorded**, with **hand labels**. One row, added 2026-09-20, three more added the same day and dropped 2026-09-23 by TOFU-501 for leaking. The `task` is the user's own prompt, verbatim, the `files` are the tree as it stood, whole, and the `label` is the set of paths the turn actually changed, read from its own write and edit calls rather than decided afterwards.

**The row comes from one turn**, `turn-18d6a7961d933ee8`, one numbered item of its prompt. One session, one project, one afternoon.

## The corpus started at four rows; TOFU-501 cut it to one

`report-2026-09-21.md` opens on the four. The ticket asked for at least thirty file-localisation questions. `.tofu/sessions` carried 131 recorded turns, 20 of which ever call `edit` or `write`, and 12 of those 20 create a file that did not exist, which is not localisation. **Four survived.** Nothing measured on four rows separates two arms, and the report says so rather than printing a ranking.

## Leakage: three of four rows leaked, and TOFU-501 dropped them

**Method**, the same rule `bench/tools/corpus/discipline_test.go` applies: split every path component and file stem of a row's own label into words of three characters or more, split on non-alphanumerics and on camel case, lowercase both sides, and look for any of them in the content words of the task.

| row | label | words of the answer that appeared in the question | disposition |
|---|---|---|---|
| `turn-18d6a7961d933ee8#item3` | `src/store.ts` | none | kept |
| `turn-18d6a7961d933ee8#item6` | `README.md` | `readme` | dropped |
| `turn-18d6a7961d933ee8#item7` | `package.json` | `package`, `json` | dropped |
| `turn-18d6a7961d933ee8#whole` | six paths | `src`, `package`, `json`, `readme` | dropped |

The task says "Update README.md so it lists the routes" and the answer is `README.md`. The task says "Add a check script to package.json" and the answer is `package.json`. A regular expression that pulls a filename out of the prompt gets both, and no ranking arm scored on those rows could be told apart from that regular expression. **`item6`, `item7` and `whole` are removed from `shortlist-corpus.jsonl`.** They stay recorded here, in this file and in `report-2026-09-21.md`, which is not edited. `answerwords_test.go` now asserts zero leaks over what remains rather than skipping.

## Parked on 2026-09-23 by TOFU-449, sized and closed on 2026-09-23 by TOFU-501

`population_test.go` reads `.tofu/sessions` through `bench/corpus.WalkSessions` and counts a turn as able to yield a shortlist question when it carries a non-empty task, when a `write` or an `edit` in it names a path, and when at least one changed path was already there before the turn, read off the turn's own calls: the first call touching it is a `read` or an `edit`, never a `write`. Turns are grouped by the exact task text, because a rerun of one prompt is one question asked twice.

TOFU-501 computed the number of questions the observed gap needs, `size.go`, `NeededQuestions`: Laplace-smooth the four-question result (4/4 judged, 1/4 BM25) to 5/6 and 2/6 to avoid a zero-variance 100% estimate, then apply the two-proportion sample size formula at 95% confidence and 80% power: n = (1.96+0.84)^2 × (0.833×0.167 + 0.333×0.667) / (0.833−0.333)^2 = 7.84 × 0.361 / 0.25 ≈ 11.3, rounds up to **12**.

On 2026-09-23: 111 entries, 110 turns read, 1 unreadable, 0 with no task, 65 changing no file, 37 creating only files that did not exist, **8 recordable, carrying 2 distinct tasks.** Six of the eight are reruns of the prompt this corpus already comes from; the other two are reruns of the Hono build turn, which `report-2026-09-21.md` rejected for building from nothing. **2 against a needed 12, and the count has not moved in the direction that matters**: entries grew by one, the distinct-task count did not. `population_test.go` fails, rather than passing, once 12 distinct recordable turns exist, and that is the signal to rebuild.

## What it does not carry

Every row goes through `corpus.LeaksIn` on load, in `corpus.go`, and `leak_test.go` proves the refusal fires on a planted home path and on a planted credential shape. Scanned again on 2026-09-23: no hit.
