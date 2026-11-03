# bench tokencount: 2026-09-24

Machine: DESKTOP-AHUN9RO.

`go test ./bench/tokencount/... -count=1` passes, 11 test functions.

## Headline

Split by whether the billed tokens could physically come from the step's own visible bytes: on 348 steps where they could, bytes over four still misses by a median of 47.1%, 94.0% out by more than a tenth. On 287 steps where they could not, because completion_tokens exceeds the visible byte count outright, no tokenizer could have produced that count from what is on record, and the median gap there is 87.2%. Four is not good enough on either half; the earlier single aggregate of both halves together, a 71.2% median, buried that this is two different failures rather than one.

## What was compared

The corpus carries a receipt per step, prompt_tokens and completion_tokens, but not the bytes that made up the prompt: the prompt for step N is the whole conversation to that point, the system prompt and the tool definitions, none of which is stored per step. What is fully recovered from a single step is what the model generated in it: assistant_text, or the Args of every tool call. That is compared against completion_tokens, the one receipt that covers exactly that step's own output and nothing before it. This is a completion-side measurement; the prompt side of the estimate is not checked here, and the gap is named rather than papered over.

A step that carries both assistant text and a tool call is excluded from the shape comparison: the receipt covers both together and cannot be split. A step with neither, or with a tool call whose Args serialise to zero bytes, is excluded and counted as skipped.

Args bytes count only the call's own JSON arguments, not the function name or the wire's own call envelope, so this estimate undercounts by a template-dependent constant the same way `crates/pi-natives/src/utok/jev.rs:19` undercounts by excluding the request frame.

## The split: can the visible bytes account for the billed tokens at all

A token is at least one byte under any real tokenizer, so a step whose completion_tokens exceeds its own visible byte count cannot have been produced from that content alone, at any rate, sane or not. That is a hard floor, not a guess about a typical ratio. Splitting on it:

key                                                                                               n   median %    worst %   share >10%
bytes < billed tokens, no tokenizer can produce more tokens than there are bytes                287      87.2%     100.0%       100.0%
bytes >= billed tokens, a tokenizer could produce this count from what's on record              348      47.1%      75.2%        94.0%

The unaccountable group is almost entirely one shape: 286 of its 287 steps are tool-call arguments, 1 is prose. Whatever produces completion_tokens on those steps is not visible in this corpus at all, which is consistent with billed content the step never writes down, a reasoning or thinking pass among the candidates, but this report does not have the data to confirm that; see the next section.

## Does four hold up on the steps where it is even possible

Restricting to the 348 accountable steps, where the byte count at least allows the token count, four still misses: prose is a median 33.9% off over 48 samples, 56.2% of them out by more than a tenth; tool-call arguments are a median 49.2% off over 300 samples, 100.0% out by more than a tenth. Both are one-sided: the estimate undershoots almost every accountable tool-call sample, 300 of 300, because short JSON arguments tokenize more densely than four bytes per token, a separate and smaller effect from the unaccountable group above. Four is not good enough for either shape even on the half of the corpus where the comparison is fair.

## The second lever: does any recorded step carry its own thinking

No. 1841 assistant messages were scanned across 48 turns stored in the header-plus-jsonl schema, the only schema that carries a message stream at all, and none of them carries a non-empty `thinking` field or a `reasoning` object, even though TOFU-540 and TOFU-544 landed a typed field for both on 2026-09-23. Either no session in this corpus ran at a raised reasoning effort since those tickets shipped, or the sessions that did are not the ones stored here. This report cannot compare the error on steps with a captured thinking block against steps without one, because the corpus holds zero of the first kind: the theory in the section above stays a theory, named rather than confirmed.

## Per wire

key                                                                                               n   median %    worst %   share >10%
anthropic                                                                                       268      82.5%     100.0%       100.0%
codex                                                                                           367      50.0%      98.6%        94.3%

## Per content shape

key                                                                                               n   median %    worst %   share >10%
prose, the assistant's own text                                                                  49      34.6%      78.4%        57.1%
tool-call arguments, json shaped by the tool's schema                                           586      74.1%     100.0%       100.0%

## The tool schema block itself

The two shapes above are what a step generates, not the tool definitions sent with every request. bench/schemas/report-2026-09-23.md already measured that block at 16574 wire bytes for the 18 tools a plain run offers, an estimate of 4143 tokens at bytes over four. That figure is cited rather than remeasured here, and it cannot be checked against a receipt the way the shapes above can: it never arrives in prompt_tokens on its own, only folded into the whole request, and bench/schemas found it is written to cache once and read back on every later step, so no step's usage numbers isolate it either. Whether four is a fair estimate for that specific block is therefore still unmeasured, named as a gap rather than assumed.

## Corpus

112 entries under ..\..\..\.tofu\sessions, 118 read as turns, 1 skipped as not a turn.
  skipped: HEAD: not a .json file
58 turns of 118 carry no wire field and are excluded from every wire, not from every shape.
1033 steps read, 635 usable, 398 skipped.
  1 skipped: step carries neither assistant text nor a tool call
  374 skipped: turn carries no wire field, recorded before it existed
  23 skipped: step mixes assistant text and tool calls; the completion receipt covers both and cannot be split by shape

## Is four good enough

No, for what four is actually asked to estimate on this corpus. Even on the 348 steps where the billed tokens could in principle come from the visible bytes, prose misses by a median of 33.9% and tool-call arguments by 49.2%, both one-sided on tool-call json, so a byte budget built on four will under-provision structured output specifically, not just occasionally miss. The other 287 steps, almost all tool-call shaped, bill more tokens than their visible bytes could ever encode; no per-shape constant fixes that, because the content it would need to count is not in the corpus at all. Replacing four with a real vocabulary costs a merge table and a merge loop this project has no dependency for. The cheaper next step is two separate fixes: a measured per-shape correction factor for the accountable half, and, before anything about the other half can be estimated, making the harness actually capture the thinking or reasoning field TOFU-540 and TOFU-544 already carry end to end but this corpus has never once recorded populated.
