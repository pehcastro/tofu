# bench sift: 2026-09-22, the rerun spread is a point and the move was twenty seven

Machine: DESKTOP-AHUN9RO, go1.27.1 windows/amd64, 2026-09-22. Credential kind: key. Wire: openrouter. Build the response reported: `typesafe/jev-1.13-20260917`, on every one of the 891 calls.

Cost unit: money. A money figure is dollars that left the account behind the credential named above, read from the response of the call it names and never from a rate card.

This report needs the network. It is gated behind `TOFU_LIVE=1` and skipped otherwise. No first call was discarded as a warm up: every call made is counted.

Nothing was changed to produce it. The threshold is still the shipped `keep_at 0.50`, the corpus is still the same 34 rows, and the needle rule and the bytes measure are the ones `bench/sift.Read` has carried since TOFU-412.

## The answer

**The rerun spread is one needle and 0.4 points of bytes saved, so it cannot explain a move of three needles and 27.5 points, and the cause is on the model's side under an unchanged build id.** Three runs of the judged arm in one sitting, 92 seconds start to finish, over the same 34 rows at the same `keep_at 0.50`, kept 30, 31 and 30 needles and saved 26.4, 26.3 and 26.7 percent of bytes. The 2026-09-21 figure was 27 of 34 at 54.0 percent.

**Today's three runs reproduce the 2026-09-22 figure and not the 2026-09-21 one.** The shift happened between the two days and has held across every run since.

The one thing three runs cannot separate is a change in the model behind the id from a change in which replica the route picked, because both look identical from this side and both moved under a build id that did not. What three runs do settle is that it is not sampling noise.

## The spread, pasted from the instrument

`TOFU_LIVE=1 go test ./bench/sift/... -run TestTheJudgedArmThreeTimesInOneSitting -v -count=1 -timeout 900s`, DESKTOP-AHUN9RO, 2026-09-22:

```
=== RUN   TestTheJudgedArmThreeTimesInOneSitting
    spread_test.go:63: 34 rows scored, 0 skipped for a missing field, keep_at 0.50, no call discarded as a warm up
    spread_test.go:96: run 1 of 3: 30 of 34 needles kept, 111834 bytes to 82324, 26.4% saved, 297 calls, 0 failed, $0.00909, mean still_needed 0.439, builds map[typesafe/jev-1.13-20260917:297]
    spread_test.go:96: run 2 of 3: 31 of 34 needles kept, 111834 bytes to 82448, 26.3% saved, 297 calls, 0 failed, $0.00909, mean still_needed 0.439, builds map[typesafe/jev-1.13-20260917:297]
    spread_test.go:96: run 3 of 3: 30 of 34 needles kept, 111834 bytes to 81969, 26.7% saved, 297 calls, 0 failed, $0.00909, mean still_needed 0.440, builds map[typesafe/jev-1.13-20260917:297]
    spread_test.go:114: spread over 3 runs in one sitting: needles kept 30 to 31 of 34, bytes saved 26.3% to 26.7%, $0.02726 spent
    spread_test.go:116: every run asked the same 34 states, first digest e0e1355dfc59
    spread_test.go:138: per unit: 297 answered in all 3 runs, 18 gave the identical score every time, 13 straddle keep_at 0.50, mean spread 0.027, widest 0.10 at row 6 unit 21, 0 answered in fewer than 3 runs
--- PASS: TestTheJudgedArmThreeTimesInOneSitting (92.01s)
PASS
ok  	tofu/bench/sift	93.093s
```

**The 111834 bytes before is the same number the free arm reproduced on 2026-09-21**, so the input did not move. The test also refuses to report a spread it cannot vouch for: it hashes every state it puts on the wire and fails if run 1 and any later run asked different questions.

## What the spread looks like one unit at a time

**Jev does not give the same number twice and the wobble is small.** Only 18 of 297 units scored identically in all three runs. The mean spread over three runs is 0.027 and the widest is 0.10. **13 of 297 units, 4.4 percent, have a spread that crosses `keep_at 0.50`**, and those 13 are the whole source of the run-to-run movement in both the needle count and the byte count.

That is the number that decides the ticket. A per-unit wobble of 0.027 cannot move a corpus-wide byte saving by 27.5 points. To get from 54.0 percent to 26.5 the model has to be answering `still_needed` higher across the whole corpus, not noisier around the same level.

The mean `still_needed` today is 0.439, 0.439 and 0.440 across the three runs. **Yesterday's mean is not recoverable**, and that is the gap named further down.

## The build id

Identical everywhere, which is the problem.

Today: `typesafe/jev-1.13-20260917`, reported on all 297 calls of each of the three runs, 891 in total. No call reported anything else.

The ledger, read rather than remembered, by `ReadLedgerBuilds` over `.tofu/log`:

```
=== RUN   TestTheLedgerBuildOnTheDayTheOldFigureWasRecorded
    spread_test.go:25: 2026-09-18: map[typesafe/jev-1.13-20260917:42]
    spread_test.go:25: 2026-09-19: map[typesafe/jev-1.13-20260917:696]
    spread_test.go:25: 2026-09-20: map[typesafe/jev-1.13-20260917:1253]
    spread_test.go:25: 2026-09-21: map[typesafe/jev-1.13-20260917:764]
--- PASS: TestTheLedgerBuildOnTheDayTheOldFigureWasRecorded (0.10s)
```

**764 recorded decisions on 2026-09-21 and every one of them names the same build as today's 891.** So the id is not a version that moved. Either the artifact behind it was replaced without renaming, or the route serves more than one thing under one name.

## Money

**Said before: about $0.027**, three runs of 297 calls at TOFU-412's measured $0.00909 a run.

**Spent after: $0.05452, twice the estimate.** $0.02726 is the sitting reported above. The other $0.02726 is an identical first sitting whose output was filtered away by the rtk proxy on the way to the terminal and could not be recovered, so it was rerun. The mechanism that stops it recurring: a paid run is invoked through `rtk proxy` with its output redirected to a file, and the file is read afterwards.

$0.00909 for 297 calls is $0.0000306 a call.

## Every dated figure this moves

**`bench/sift/report-2026-09-21.md`.** Its Jev row, 27 of 34 and 26 of 34 at 54.0 and 54.2 percent, is the figure that did not reproduce. Today: 30 to 31 of 34 at 26.3 to 26.7 percent. Needles move up by 3 to 5, bytes saved moves down by 27.3 to 27.9 points.

That report also carries a sentence that is now false rather than stale: "Eight to nine more needles kept, both runs, same direction, at bytes saved the free arm already matches." **The free arm saves 54.7 percent and Jev now saves 26.5**, so Jev is no longer winning at equal bytes. It buys 12 to 13 more needles for half the saving. Whether that is still a win is a threshold question and this ticket was forbidden to touch the threshold.

**`bench/sift/report-2026-09-22.md`**, the cost report. Its money survives and its bytes do not.

- Median $0.00054 a session, p95 $0.005508, worst $0.011070: **they survive.** Today's price of one judged sift is 8.7 candidate units times $0.0000306, which is $0.000266, against the $0.00027 that report borrowed from the TOFU-217 log. That is under one percent and it moves no session figure at the precision quoted.
- 54.0 percent of a shell result not sent: **moves to 26.5 percent**, the mean of today's three runs, down 27.5 points.
- 13.7 percent of every byte the model reads from a tool once: **moves to 6.7 percent**, because it is 54.0 times the measured 25.4 percent shell share and the shell share did not move.
- 13.2 percent counting every resend: **moves to 6.5 percent**, the same arithmetic against the 24.4 percent resend-weighted share.
- 79.4 percent needles kept, quoted in its closing section: **moves to 88.2 to 91.2 percent.**
- The 838 ms added per shell tool call was not remeasured here and nothing in this report affects it.

**`bench/sift/report-2026-09-22-rtk.md`.** Its Jev-alone row, 30 of 34 at 26.7 percent for $0.00909, reproduces inside today's spread on all three numbers. Its section titled "A number that moved and I do not know why" is answered by this report and nothing else in it changes. Its rtk and rtk-then-Jev rows were not rerun.

**`bench/report/answers.json`**, line 130, the 79.4 percent, and line 133, whose evidence string is the 2026-09-21 table row. **`bench/report/INDEX.md`**, line 48, which reads Jev 79.4 percent against the regex 52.9 at a margin of 26.5 points and 1.50x: the margin becomes 35.3 to 38.3 points at 1.67x to 1.72x, and the parity-of-bytes claim behind it is gone. Line 71, which marks `report-2026-09-22.md` as standing. Regenerating those is the orchestrator's.

**No dated report was edited.** A dated report is a record of what was measured on its date, and rewriting one to a later number destroys the only evidence that the number moved.

## The hook that would have settled this outright

**The sift bench makes 891 paid decisions and writes no ledger row**, which `bench/sift/report-2026-09-22.md` already found from the other side: 2,999 ledger rows and not one `shell_sift`. Yesterday's 594 scores are therefore gone, and the difference between the model shifting its level and the model being replaced would be readable straight off them if they had been kept.

What is needed and where: a way for a bench arm to append to `internal/judge/ledger` without going through `internal/turn`, so a `bench/sift` run leaves rows carrying the point, the state hash, the build and the answers. It would cost nothing and it would have made this ticket a diff rather than a rerun. That is a `go-dev` change in `internal/judge/ledger` and outside this package's `owns`.

## What was skipped and why

- 0 of the 34 corpus rows were skipped for a missing field. Every row carries a session, a task, a command and an output, and the instrument counts and names any that does not.
- 5 tests skip offline in `bench/sift`: `TestTheJudgedArmAgainstTheFreeArmOverEveryCapturedOutput`, `TestTheSameChunkUnderTwoTasks`, `TestFourMethodsOverTheSameThirtyFourShellResults` and `TestTheJudgedArmThreeTimesInOneSitting` want `TOFU_LIVE=1` and the network, and `TestRewriteTheCorpusThroughTheScrub` wants `TOFU_REWRITE_CORPUS=1`. One more skips in `bench/api`: `TestLiveTheSameBatteryThroughBothWires` wants `TOFU_LIVE_COMPARE=1`.
- 0 of the 891 calls failed.

## The red run behind the build id claim

`TestTheLedgerBuildOnTheDayTheOldFigureWasRecorded` is worthless if it passes on a ledger that says anything. Shown failing first with `recordedBuild` set to `typesafe/jev-1.13-20260918` and then set back:

```
=== RUN   TestTheLedgerBuildOnTheDayTheOldFigureWasRecorded
    spread_test.go:29: 2026-09-21 carries map[typesafe/jev-1.13-20260917:764], and the reports of that day all name typesafe/jev-1.13-20260918 alone
--- FAIL: TestTheLedgerBuildOnTheDayTheOldFigureWasRecorded (0.02s)
FAIL
```
