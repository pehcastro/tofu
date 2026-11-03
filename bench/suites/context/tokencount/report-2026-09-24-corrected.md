# bench tokencount, a correction factor over bytes over four: 2026-09-24

Machine: DESKTOP-AHUN9RO.

`go test ./bench/tokencount/... -count=1` passes, 16 test functions.

## Headline

Fitted per shape on the 348 accountable steps only, the residual median moves from 47.1% to 16.8%, worst 71.2%, 67.0% still out by more than a tenth. The factor is this corpus's factor: it is fitted and reported on the same steps, not validated on a held-out set, and it does nothing for the 287 unaccountable steps where the billed tokens exceed the visible bytes outright.

## The fitted factor per shape

Each factor is the median of bytes divided by billed tokens, taken sample by sample over that shape's accountable steps, replacing the fixed 4 in konst.SearchBytesPerToken with a shape-specific divisor. This is a proposal, not a change: konst is untouched.

shape                                                               n    bytes/token
prose, the assistant's own text                                    48           2.65
tool-call arguments, json shaped by the tool's schema             300           2.08

## Residual error after correction, per shape

key                                                                                               n   median %    worst %   share >10%
prose, the assistant's own text before correction                                                48      33.9%      73.2%        56.2%
prose, the assistant's own text after correction                                                 48      39.5%      65.2%        72.9%
tool-call arguments, json shaped by the tool's schema before correction                         300      49.2%      75.2%       100.0%
tool-call arguments, json shaped by the tool's schema after correction                          300      15.9%      71.2%        66.0%

The median of bytes over billed tokens is not the same divisor as the one that minimizes median error, and prose, the assistant's own text shows it: fitting it moved the median error from 33.9% to 39.5%, worse than the flat 4 it replaced. A per-shape median is not guaranteed to help, and this shape is the counterexample in this corpus.

## What the factor cannot fix

The 287 unaccountable steps, bytes < billed tokens, no tokenizer can produce more tokens than there are bytes, were not fitted on and cannot be: their own completion_tokens exceeds the bytes recorded for them, so no divisor, however small, reproduces the count from what is on record. Median error there stays 87.2%, unmoved by this ticket, because moving it needs the corpus to capture content it currently does not, named in TOFU-562 as most likely an uncaptured reasoning or thinking pass.

## Which figures move if the constant changes

konst.SearchBytesPerToken is unchanged by this ticket. If a later ticket replaces 4 with a shape-aware factor, these dated reports carry figures derived from it and would need to be recomputed and superseded, never edited in place:

- bench/schemas/report-2026-09-23.md: the tool schema block, 16574 bytes estimated as 4143 tokens
- bench/prefix/report-2026-09-23.md: the per-session rewrite table, 4135/3908/1852 bytes estimated as 1033/977/463 tokens
- bench/prefix/report-2026-09-23-corrected.md: the per-session rewrite table, 4492/4265/2209 bytes estimated as 1123/1066/552 tokens
- bench/tokens/report-2026-09-21.md: the per-tool tokens column over the whole corpus
- bench/tokens/report-2026-09-21-pinned.md: the same per-tool tokens column, replayed against the pinned tree
- bench/linenumbers/report-2026-09-23.md: 1016862 read-tool bytes estimated as about 254215 tokens
- bench/tokencount/report-2026-09-24.md and report-2026-09-24-accountable.md: the Estimate field of every sample, and every median and worst percentage derived from it

## Corpus

Same corpus as report-2026-09-24-accountable.md: 112 entries under ..\..\..\.tofu\sessions, 118 turns, 1033 steps read, 635 usable. The factor is fitted only on the 348 accountable of those 635 usable steps.

## Is a fitted constant good enough

A per-shape median is an improvement over the flat 4 on the half of the corpus it can reach, moving the overall accountable median from 47.1% to 16.8%, but 67.0% of corrected accountable steps are still out by more than a tenth, so a single fitted number per shape is not a substitute for measuring what a step actually generated. It also does not touch the 287 unaccountable steps, still off by a median of 87.2%. The finding is not that four was the wrong constant; it is that a constant, fitted or not, is the wrong shape of estimate for a distribution this wide, and the honest fix stays a real vocabulary or a captured receipt, not a better divisor.
