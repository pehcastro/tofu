# Where the search corpus comes from

**Recorded, 23 of 23, and nothing is written.** Every row is a `search` tool call a model actually issued against this tree, read out of `.tofu/sessions` at run time by `Load` in `corpus.go`. There is no `testdata` file here to hand-tune: the corpus is the recording, and `TestEveryRowCameOutOfARecordingRatherThanOffThisKeyboard` fails if a row arrives without the id of the turn it came from.

110 recorded turns hold 23 search calls. One entry in the directory, `HEAD`, is not a turn and is skipped by name.

**Written was not an option for this one, which is the opposite of `bench/tools`.** The thing being measured is how often a pattern a model wrote comes back empty. A pattern I write to test that is a pattern I already know the answer to, and the number it produces is a number about me.

## What is not measured here

**No label is written down at all.** A row is a pattern and a path; the outcome is whatever the tree holds today. So the failure TOFU-446 found, a question naming a component of its own answer, has no place to occur: there is no answer field.

The leakage that can occur is the mirror of it, and it did occur. A pattern is run over the whole tree, and this bench is in that tree, so a recorded pattern can be answered by the bench that replays it. `TestNoRecordedRowIsAnsweredByTheBenchsOwnSource` compiles every row and runs it over every file under `bench/search`.

**Count: 4 found across two rounds, then 0.** The cost measurement first named two recorded patterns as literals in its own source, one of them a deleted policy loader. The replay of that row went from absent to found because this bench had written the name down. Then the first draft of this file quoted all three patterns in prose and the check caught it again, which is why nothing below is quoted. Every pattern the cost measurement uses is now either taken from the corpus at run time or composed from pieces, so no whole pattern appears as text anywhere in this directory. The check is 0 of 23 and runs on every suite run.

`internal/secret.LeaksIn` runs over every row as it loads and refuses the load on a hit: 0 of 23 carry an identity or a credential. `bench/corpus.WalkSessions` scrubs before that, so the check is scrub then verify rather than scrub alone.

## What a reader should not conclude

**The replay runs against today's tree, not the tree each search was recorded against.** The recordings are from 2026-09-19 to 2026-09-23 and the tree moved under them: two of the package paths they search are gone, and so is the policy loader four of the rows look for. That drift is most of the empty results. It inflates the empty rate and it does not affect the rescue rate, which is the number the design turns on.

3 of 23 rows cannot be replayed at all because the recorded path is not in the tree. They are counted and named in the test log, not dropped quietly.

**23 rows is small.** Nothing here separates two arms at any confidence. It is a floor and a description, not a comparison.
