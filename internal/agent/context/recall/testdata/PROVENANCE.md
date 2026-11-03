# The pinned estimate corpus

`estimate-corpus.jsonl` holds 594 recorded requests, 188,899 bytes, taken out of `.tofu/sessions` on 2026-09-21 for TOFU-312. Three recording days are in it: 63 requests from 2026-09-19, 441 from 2026-09-20 and 90 from 2026-09-21. Two wires: 349 requests on codex and 245 on anthropic. The bound is measured over the 494 of them billed above 5,000 tokens.

It exists because `.tofu/sessions` is gitignored and grows on every run of this binary, so `TestOurEstimateOfAContextAgainstWhatTheProviderBilledForTheSameOne` was asserting a fixed bound over an input that nobody chose. A new recording crossing the bound made the estimator look broken on a day nobody had changed it.

A row carries no message text. `recall.Measure` reads only `len(text)`, so a row records the byte length of each conversation entry, the byte length of the instruction block the per-session prefix calibration produced, what the provider reported for that request, and the occupancy the build recorded at the time. That is everything the estimator consumes and nothing a person wrote, which is why no scrubbing pass is needed and why the file stays under 200 KB with whole transcripts behind it.

`instruction_bytes` is not a count of real system-prompt bytes. It is the calibrated prefix in tokens, written out at 2,310 bytes per thousand tokens, which is the ratio `data/elide.yaml` gives a wire it does not name. A reader that measures at some other ratio converts first, as `conversationOn` does in `estimate_test.go`, so the identity band holds the same token count whatever ratio is under test. Taking a new pin at a different default silently rewrites that unit.

`recorded_estimate` was short by the whole working set band until TOFU-386, because it is decoded into `recall.Occupancy`, which carried no JSON names, so the `working_set` key a step row writes matched nothing and landed as zero: on `turn-18d74adff1dccf60` step 6 the column read 28,480 where the build wrote 35,180. TOFU-386 gave the struct its names and rewrote the column from the same untouched sessions. 42 of the 594 rows moved, all of them on anthropic, in 8 sessions; every other field on every row is byte for byte what it was, and no row was added or removed. The column is used only by the log lines about the recording build.

`billed_fresh` is already corrected for the wire. On codex, `prompt_tokens` is the OpenAI Responses API's `input_tokens`, which counts cached tokens inside itself; on anthropic, `input_tokens` excludes them. The test adds the cache read only where the wire leaves it out.

To take a new pin, with the live sessions present:

```
TOFU_PIN_ESTIMATE_CORPUS=testdata/estimate-corpus.jsonl go test ./internal/recall/ -run TestThePinnedCorpusStillMatchesTheLiveSessionsItWasTakenFrom -count=1 -v
```

That is a deliberate act and it moves the bound's meaning, so the counts above and the numbers in the ticket report are rewritten in the same change. `TestThePinnedCorpusStillMatchesTheLiveSessionsItWasTakenFrom` fails when a pinned row still in the live store no longer rebuilds to the same numbers, and counts the live requests recorded since the pin without failing on them.
