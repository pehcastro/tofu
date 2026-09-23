# bench sift: 2026-09-22, rtk as a method, alone and in front of jev

Machine: DESKTOP-AHUN9RO, go1.27.1 windows/amd64, 2026-09-22. Credential kind: key. Wire: openrouter. Build the response reported: `typesafe/jev-1.13-20260917`. Proxy: `rtk 0.43.0`, at `C:\Users\Luiz\.local\bin\rtk`.

Cost unit: money. A money figure is dollars that left the account behind the credential named above, read from the response of the call it names and never from a rate card. A millisecond figure is wall clock on the machine named above.

This report needs the network. It is gated behind `TOFU_LIVE=1` and skipped otherwise. No first call was discarded: every Jev call made is counted.

## The answer

**rtk in front of Jev buys a 29 percent cheaper Jev call and pays 5 of 34 needles for it, and neither rtk arm beats Jev alone.** rtk alone keeps 26 of 34 needles at 36.9 percent of bytes removed. rtk then Jev keeps 25 of 34 at 48.3 percent for $0.00644, against Jev alone at 30 of 34 for $0.00909.

**The reason rtk cannot win here is structural, not tuning: rtk has no filter for 21 of the 34 recorded commands.** It is a per-command proxy with seventeen named filters and the corpus is compound shell pipelines. On those 21 rows rtk is a passthrough that removes nothing.

## The four methods

Pasted from `TestFourMethodsOverTheSameThirtyFourShellResults`, run 2 of 2, `keep_at 0.50`:

| method | needles kept | bytes saved | jev calls | jev input bytes | money | rtk ms |
|---|---|---|---|---|---|---|
| regex on error lines | 18 of 34 | 54.7% | 0 | none | none | 0s |
| jev | 30 of 34 | 26.7% | 297 | 175548 | $0.00909 | 0s |
| rtk | 26 of 34 | 36.9% | 0 | none | none | 300ms |
| rtk then jev | 25 of 34 | 48.3% | 218 | 116688 | $0.00644 | 300ms |

All four score the same 34 recorded shell results with one needle planted in each by `bench/sift.Plant`, and all four go through one function, `bench/sift.Read`. It is handed the message the model would receive and asks two things of it: does it still carry the needle line, and how many bytes is it. The regex and Jev arms hand it `sift.Message`, rtk hands it what rtk printed, rtk then Jev hands it `sift.Message` over rtk's output. **The needle rule reads the message and not the marks, which is what lets a rewriting filter be scored beside a selecting one.**

## What rtk costs

**300 to 342 ms for all 34 rows, and no money.** That is 13 subprocess calls, the only rows with a filter: mean 35 ms, p50 33 ms, p95 43 ms, worst 43 ms per call. Against the 838 ms Jev adds to a shell tool call, rtk is a rounding error and its own tail is flat.

## What rtk saves Jev

**Jev after rtk sees 116,688 of 175,548 state bytes, 66.5 percent, over 218 of 297 calls, 73.4 percent.** The money follows: $0.00644 against $0.00909, **70.9 percent of it, a 29.1 percent saving.** The figure was identical in both runs to five decimal places, because the input is deterministic and the token count with it.

The saving comes from two places at once: rtk removes bytes, and the smaller output splits into fewer units, so there are fewer calls as well as smaller ones.

## The 21 rows rtk has no filter for

`rtk pipe` names seventeen filters: cargo-test, pytest, go-test, go-build, tsc, vitest, grep, rg, find, fd, git-log, git-diff, git-status, log, mypy, ruff-check, ruff-format, prettier. The filter is chosen from the leading command of the recorded pipeline, which is what a person driving rtk does and what rtk's own hook does.

Filters chosen: git-log 5, git-status 3, grep 2, find 2, git-diff 1, none 21.

The 21 with no filter, by leading command: `git ls-files` 9 rows (7, 10, 16, 19, 22, 25, 28, 30, 32), `ls` 6 rows (3, 9, 13, 15, 18, 20), `head` 4 rows (5, 6, 11, 12), `for` 2 rows (27, 31).

**`git ls-files` is the largest group and rtk passes it through untouched.** Confirmed directly: `rtk git ls-files bench` prints byte for byte what `git ls-files bench` prints, and `rtk git --help` lists diff, log, status, show, add, commit, push, pull, branch, fetch, stash and worktree, with no ls-files among them.

`ls`, `head` and `for` have a different reason. rtk has an `ls` subcommand and a `read` subcommand, but both run the command themselves rather than filtering a stream, so neither can be put over a recorded output. **A recorded shell result is not replayable**: the tree moved after 2026-09-20 and rerunning the command would produce different bytes with no needle in them. Only `rtk pipe` can be scored on the record, and it is the only path used here.

**These 21 are scored, not dropped.** They are counted as rtk passing the output through whole, which is what rtk actually does with them: the needle survives and nothing is saved. Over the 13 rows a filter does exist for, rtk alone keeps 5 of 13 needles at 71.4 percent of bytes removed, so where rtk fires it is aggressive and it loses the needle two times in three.

## Rerun agreement

Two full runs, back to back, same process:

| | run 1 | run 2 |
|---|---|---|
| regex | 18 of 34, 54.7% | 18 of 34, 54.7% |
| jev | 30 of 34, 26.7% | 30 of 34, 26.7% |
| rtk | 26 of 34, 36.9% | 26 of 34, 36.9% |
| rtk then jev | 25 of 34, 48.4% | 25 of 34, 48.3% |

The needle counts are identical across the two runs and the rtk-then-Jev bytes move by 0.1 percent. An earlier single run of the same instrument, an hour before and counted in the money below, read jev 31 of 34 at 25.6 percent and rtk then jev 24 of 34 at 48.9 percent, so the spread over three runs is 30 to 31 for jev and 24 to 25 for the chain.

Jev latency, jev alone: p50 366 ms, p95 505 ms, worst 715 ms in run 1; p50 367 ms, p95 517 ms, worst 1,863 ms in run 2. Jev after rtk: p50 365 ms, p95 515 ms, worst 1,168 ms in run 1; p50 364 ms, p95 517 ms, worst 646 ms in run 2. **The p50 does not move and the worst call moves by a factor of three between two runs a minute apart**, which is the same shape the ledger already showed and the reason the tail sets the timeout.

## A number that moved and I do not know why

**Jev's bytes saved fell from 54.0 percent to 26.7 percent since 2026-09-21, and its needles rose from 27 of 34 to 30 of 34.** Same 34 rows, same `keep_at 0.50`, same build id `typesafe/jev-1.13-20260917`, same wire.

It is not the splitter and it is not the scoring rewrite in this ticket. The regex arm reproduces its old figure to the byte, 18 of 34 and 111,834 bytes to 50,696 and 54.7 percent, which it could only do if the units, the needle placement and the bytes measure are all unchanged. So the model is answering `still_needed` higher than it did the day before under an unchanged build id.

**That makes the 54.0 percent quoted in `bench/sift/report-2026-09-22.md` stale, and with it every downstream figure built on it**, including the median $0.00054 a session and the 13.7 percent of tool bytes. This ticket did not rewrite that report: the cause is outside `bench/sift/**` and the decision is not the bench agent's.

## What was skipped and why

- 4 tests skipped in the offline suite: `TestTheJudgedArmAgainstTheFreeArmOverEveryCapturedOutput`, `TestTheSameChunkUnderTwoTasks` and `TestFourMethodsOverTheSameThirtyFourShellResults` want `TOFU_LIVE=1` and a network, `TestRewriteTheCorpusThroughTheScrub` wants `TOFU_REWRITE_CORPUS=1` and rewrites a fixture.
- `rtkOnPath` skips both rtk tests and the four method test when rtk is not on PATH. It did not fire here.
- No corpus row was skipped. All 34 were scored by all four methods.

## What this report does not do

It does not switch the policy on. `library/tools/shell/rules/shell_sift@1.yaml` still reads `mode: shadow`.

It does not quote `bench/thrift/report-2026-09-21.md`. That report's 1.5 percent is rtk as a lossless filter over every tool result, a different question with a different answer, and it does not belong on this row.

It does not measure rtk on the commands rtk is good at. Every filter rtk names for a test runner, a type checker or a linter is unexercised, because the recorded corpus is one person exploring one repository with `git`, `ls`, `head` and `find`. **On a corpus of `go test` or `eslint` output rtk would very likely win, and this corpus cannot say so.**

## The money

Estimated before the run: about $0.018 for two Jev arms over 34 rows, and about $0.031 once a second run was added for rerun agreement.

**Spent: $0.04659.** $0.01553 on a first single run and $0.03106 on the two run pass, $0.00909 for the Jev arm and $0.00644 for the chained arm each time. Under two cents a run, as the ticket predicted.

## The run

`TOFU_LIVE=1 go test ./bench/sift/... -run TestFourMethodsOverTheSameThirtyFourShellResults -count=1 -timeout 1800s -v`, DESKTOP-AHUN9RO, 2026-09-22:

```
=== RUN   TestFourMethodsOverTheSameThirtyFourShellResults
    rtkjev_test.go:80: run 1 of 2
    rtkjev_test.go:122: | method | needles kept | bytes saved | jev calls | jev input bytes | money | rtk ms |
    rtkjev_test.go:123: |---|---|---|---|---|---|---|
    rtkjev_test.go:125: | regex on error lines | 18 of 34 | 54.7% | 0 | none | none | 0s |
    rtkjev_test.go:125: | jev | 30 of 34 | 26.7% | 297 | 175548 | $0.00909 | 0s |
    rtkjev_test.go:125: | rtk | 26 of 34 | 36.9% | 0 | none | none | 333ms |
    rtkjev_test.go:125: | rtk then jev | 25 of 34 | 48.4% | 218 | 116688 | $0.00644 | 333ms |
    rtkjev_test.go:128: jev after rtk sees 116688 of 175548 state bytes, 66.5%, over 218 of 297 calls, 73.4%
    rtkjev_test.go:131: money: jev alone $0.00909, rtk then jev $0.00644, 70.9% of it, $0.01553 this run
    rtkjev_test.go:133: rtk then jev lost the needle on row 0 filter "git-log", row 1 filter "git-log", row 4 filter "git-log", row 8 filter "grep", row 11 filter "", row 21 filter "git-log", row 24 filter "grep", row 26 filter "git-log", row 33 filter "git-diff"
    rtkjev_test.go:134: jev alone latency: p50 366ms, p95 505ms, worst 715ms, build typesafe/jev-1.13-20260917
    rtkjev_test.go:135: jev after rtk latency: p50 365ms, p95 515ms, worst 1.168s, build typesafe/jev-1.13-20260917
    rtkjev_test.go:136: keep_at 0.50, 34 rows, no call discarded as a warm up
    rtkjev_test.go:80: run 2 of 2
    rtkjev_test.go:122: | method | needles kept | bytes saved | jev calls | jev input bytes | money | rtk ms |
    rtkjev_test.go:123: |---|---|---|---|---|---|---|
    rtkjev_test.go:125: | regex on error lines | 18 of 34 | 54.7% | 0 | none | none | 0s |
    rtkjev_test.go:125: | jev | 30 of 34 | 26.7% | 297 | 175548 | $0.00909 | 0s |
    rtkjev_test.go:125: | rtk | 26 of 34 | 36.9% | 0 | none | none | 300ms |
    rtkjev_test.go:125: | rtk then jev | 25 of 34 | 48.3% | 218 | 116688 | $0.00644 | 300ms |
    rtkjev_test.go:128: jev after rtk sees 116688 of 175548 state bytes, 66.5%, over 218 of 297 calls, 73.4%
    rtkjev_test.go:131: money: jev alone $0.00909, rtk then jev $0.00644, 70.9% of it, $0.01553 this run
    rtkjev_test.go:133: rtk then jev lost the needle on row 0 filter "git-log", row 1 filter "git-log", row 4 filter "git-log", row 8 filter "grep", row 11 filter "", row 21 filter "git-log", row 24 filter "grep", row 26 filter "git-log", row 33 filter "git-diff"
    rtkjev_test.go:134: jev alone latency: p50 367ms, p95 517ms, worst 1.863s, build typesafe/jev-1.13-20260917
    rtkjev_test.go:135: jev after rtk latency: p50 364ms, p95 517ms, worst 646ms, build typesafe/jev-1.13-20260917
    rtkjev_test.go:136: keep_at 0.50, 34 rows, no call discarded as a warm up
--- PASS: TestFourMethodsOverTheSameThirtyFourShellResults (111.02s)
PASS
ok  	tofu/bench/sift	111.909s
```

The rtk arm on its own, no network, `go test ./bench/sift/... -run TestRtk -count=1 -timeout 300s -v`:

```
    rtk_test.go:37: rtk --version printed "rtk 0.43.0" and rtk gain answered
    rtk_test.go:88: rtk arm, all 34: 26 of 34 needles kept, 111834 bytes to 70582, 36.9% saved, lost at row 0 unit 0, row 1 unit 1, row 4 unit 4, row 8 unit 8, row 21 unit 0, row 24 unit 1, row 26 unit 0, row 33 unit 1
    rtk_test.go:95: rtk arm, the rows a filter exists for: 5 of 13 needles kept, 57765 bytes to 16534, 71.4% saved
    rtk_test.go:102: filters chosen:  21, find 2, git-diff 1, git-log 5, git-status 3, grep 2
    rtk_test.go:103: rows with no rtk filter: 21 of 34, row 3 ls, row 5 head, row 6 head, row 7 git, row 9 ls, row 10 git, row 11 head, row 12 head, row 13 ls, row 15 ls, row 16 git, row 18 ls, row 19 git, row 20 ls, row 22 git, row 25 git, row 27 for, row 28 git, row 30 git, row 31 for, row 32 git
    rtk_test.go:113: rtk pipe over 13 rows: mean 35ms, p50 33ms, p95 43ms, worst 43ms, no money
```

## The red run behind the central claim

**Every number above rests on one scoring function reading one needle rule out of the message the model would get.** If that rule is wrong, all four columns are wrong together and nothing would show it. `TestTheNeedleRuleReadsTheMessageEveryArmHandsTheModel` was shown failing first, with `Read` scoring the needle by whether the first line of its unit survived, which is the tempting rule and the one that cannot score a rewriting filter:

```
=== RUN   TestTheNeedleRuleReadsTheMessageEveryArmHandsTheModel
    rtk_test.go:143: the needle removed and nothing else: needle kept true, want false
    rtk_test.go:143: rewritten around the needle: needle kept false, want true
    rtk_test.go:143: a sibling line of its unit survives and it does not: needle kept true, want false
--- FAIL: TestTheNeedleRuleReadsTheMessageEveryArmHandsTheModel (0.00s)
FAIL
```

All three directions caught. Changed back, the test passes and the regex arm reproduces its 2026-09-21 figure to the byte, which is the second check on the rewrite: 18 of 34, 111,834 bytes to 50,696, 54.7 percent saved, lost at the same sixteen rows and units.

## One line number in the pasted log

The minify pass after the run added a `strconv` import to `rtkjev_test.go`, so the `run %d of 2` line the log names as 80 is now 81. Every other line number in the pasted output still points where it says. The output itself is unedited.
