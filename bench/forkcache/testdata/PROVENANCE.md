# Where forks.jsonl came from

**Recorded.** 21 fork pairs, 6,515 bytes, read out of `.tofu/sessions` and pinned here on 2026-09-22 for TOFU-373. The sessions themselves were recorded on 2026-09-19 and 2026-09-20 on the anthropic wire against a subscription credential, model `claude-opus-5`, and `.tofu/sessions` was read only throughout. The ledger at `.tofu/log`, 2,999 rows, was not written to.

It is pinned here because `.tofu` is gitignored, so without this file the measurement rests on bytes that were never in the repository.

A row is a parent turn's last request and the child turn's first request: `input_tokens`, `cache_read`, `cache_write` on each side, plus the wire, the model, the day and the fork kind. `report-2026-09-22.txt` beside the package names the exact upstream field each number comes from and the line that parses it.

## What it cannot answer, and the reader should not conclude it does

**There is no account fork in this file.** `TestAnAccountForkIsNotInTheRecordedCorpus` skips for that reason and the skip is counted in the report. A session header as recorded carries no credential identity, so no row here could tell an account fork from a context fork even if one had happened. That a cache is scoped to an account is an expectation in this project, not a measurement, and this corpus does not make it one.

Two of the 21 pairs are on a wire that reports a cache write at all. The other 19 constrain less than they appear to.

## Leakage

**Recorded, so the written-corpus rule does not apply**, and it was checked anyway on 2026-09-23. Method: the outcome each row is scored for is `Fate()`, computed from `child_first.cache_read` and `child_first.cache_write`, so leakage would mean a non-numeric field naming that outcome. The four fate words, `read`, `written`, `read and written` and `neither`, were searched as whole words across every string field of every row: `parent`, `child`, `wire`, `model`, `day`, `fork_kind`. **Count: zero of 21.** The rows carry no free text.

## What it does not carry

Every row goes through `corpus.LeaksIn` on load, in `fork.go`, so a row that skipped the scrub fails the measurement rather than passing quietly. Scanned again on 2026-09-23: no hit.
