# The frozen stop check corpus

Nineteen turn files, copied out of `.boji/sessions` on 2026-09-19 for BOJI-111. They are the turns the hand labels in `labels.go` were written against, for BOJI-079 and BOJI-098, and nothing else.

They live here because `.boji` is gitignored, so the set the measurement rests on was never in the repository, and because `.boji/sessions` is where this repository's own live runs write: every turn anybody ran here joined the corpus and the test then demanded a hand label for it.

A turn enters this set by being copied into this directory on purpose. `session_test.go` asserts the file count, the step count and the labelled count, so an arrival or a departure fails rather than passing quietly.

Counts, from the test output on 2026-09-23: 19 files, 124,397 bytes. Fourteen readable turns, none of them with no step. Five files are skipped by `ReadSessions`, each because the turn carries no step and no wall clock the shared reader can bind to. 79 steps, 78 with a hand label, 1 listed in `Unlabelled`.

**This is the only copy.** Until 2026-09-23 seventeen of these files were also committed under `bench/transform/testdata/sessions`, byte identical, with nothing checking that the two stayed so. TOFU-456 removed that copy because this one is the superset, the hand labels in `labels.go` are keyed to these turns, and `session_test.go` already fails on an arrival or a departure. `bench/transform` now reads seventeen of these nineteen from here, and says in `bench/transform/testdata/PROVENANCE-sessions.md` which two it leaves out and why. `unique_test.go` refuses a second copy of any recording under `bench/`.

The ceiling is 256 KiB, checked by the test. These files carry whole transcripts, so the set grows by being pruned or by a deliberate decision to spend the bytes, never by accident.

Nothing was relabelled or edited. The bytes are identical to the recorded files, and every one of them is a fixed point of `bench/corpus.Scrub`, which is the check that no path or name from the recording machine is in here.
