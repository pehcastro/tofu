# The pinned estimate corpus

`estimate-corpus.jsonl` holds 594 recorded requests, 188,899 bytes, taken out of `.tofu/sessions` on 2026-09-21 for TOFU-312. Three recording days are in it: 63 requests from 2026-09-19, 441 from 2026-09-20 and 90 from 2026-09-21. Two wires: 349 requests on codex and 245 on anthropic. The bound is measured over the 494 of them billed above 5,000 tokens.

It exists because `.tofu/sessions` is gitignored and grows on every run of this binary, so `TestOurEstimateOfAContextAgainstWhatTheProviderBilledForTheSameOne` was asserting a fixed bound over an input that nobody chose. A new recording crossing the bound made the estimator look broken on a day nobody had changed it.

A row carries no message text. `recall.Measure` reads only `len(text)`, so a row records the byte length of each conversation entry, the byte length of the instruction block the per-session prefix calibration produced, what the provider reported for that request, and the occupancy the build recorded at the time. That is everything the estimator consumes and nothing a person wrote, which is why no scrubbing pass is needed and why the file stays under 200 KB with whole transcripts behind it.

`billed_fresh` is already corrected for the wire. On codex, `prompt_tokens` is the OpenAI Responses API's `input_tokens`, which counts cached tokens inside itself; on anthropic, `input_tokens` excludes them. The test adds the cache read only where the wire leaves it out.

To take a new pin, with the live sessions present:

```
TOFU_PIN_ESTIMATE_CORPUS=testdata/estimate-corpus.jsonl go test ./internal/recall/ -run TestThePinnedCorpusStillMatchesTheLiveSessionsItWasTakenFrom -count=1 -v
```

That is a deliberate act and it moves the bound's meaning, so the counts above and the numbers in the ticket report are rewritten in the same change. `TestThePinnedCorpusStillMatchesTheLiveSessionsItWasTakenFrom` fails when a pinned row still in the live store no longer rebuilds to the same numbers, and counts the live requests recorded since the pin without failing on them.
