# Where standing.jsonl came from

**Written**, by hand, in commit `4d783c78` on 2026-11-01, not recorded from a session. The shape says so: forty sentences, exactly twenty labelled `standing: true` and twenty `false`, each carrying a `shape` field (`order with a listed word`, `report with a listed word`, `approval` and so on) that names the case it was written to cover. No session records a label or a shape.

The sentences are generic coding requests a person might type. None is quoted from a real session, and none names a person, a project, a path or a credential.

One file, 40 lines.

## Leakage

Checked by `TestNoSentenceSharesARunOfWordsWithTheQuestion` in `bench/suites/context/recall/standing`: no sentence shares a run of four words with the shipped `standing_order` question's instructions or answer wording, and the check is shown to catch a planted copy before it runs over the corpus.

## What a number over it means

A precision or recall over this file measures the arm against cases chosen to separate a word list from a reading of intent. It is not a rate over real traffic.
