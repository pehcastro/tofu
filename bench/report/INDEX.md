# What bench has measured

27 dated reports under `bench/`, over 21 benches. 7 benches carry none. 2 are withdrawn whole or in part, 1 is stale, and 10 name no conclusion a reader can find.

This file is generated. Run `go run ./bench/report/gen` to rewrite it, or open `bench/report/index.html`, which is the same data with a viewer over it. A withdrawal declared from outside a report lives in `bench/report/withdrawals.json`; every other state below is declared by the report's own first lines.

## Every dated report, newest first

| Bench | Date | Report | Headline | What it found | Sample | State |
|---|---|---|---|---|---|---|
| forkcache | 2026-09-22 | `bench/forkcache/report-2026-09-22.txt` | zero cache write | not quoted here: this report is withdrawn in part, and the note says by whom | 2 recorded context forks | withdrawn in part |
| harness | 2026-09-22 | `bench/harness/report-2026-09-22.md` | 36 runs | unparsed: no heading in this file names its own answer | 4 recorded rows, 3 of them carrying a turn count and a wall clock, plus a 3-repeat fixture that fixes the output shape | stands |
| picker | 2026-09-22 | `bench/picker/report-2026-09-22.txt` | 0 recorded window readings | unparsed: no heading in this file names its own answer | 404 files and 18,401 rows read, 0 readings over 0 distinct accounts | stands |
| wrongpath | 2026-09-22 | `bench/wrongpath/report-2026-09-22.md` | 1.01 per session | The measure cannot separate two arms and this line of work stops here. | 109 recorded sessions, 51 reverts and 48 contradictions | stands |
| ask | 2026-09-21 | `bench/ask/report-2026-09-21.md` | One correct call out of four | The free arm's three are exactly the three widenings. That is ask.md's own claim holding: the ownership boundary already knows the grant case, exactly and for nothing. | 7 real moments taken from the ticket logs | stands |
| promote | 2026-09-21 | `bench/promote/report-2026-09-21.md` | 0 rows | unparsed: no heading in this file names its own answer | 0 rows against a floor of 78 | stands |
| readworth | 2026-09-21 | `bench/readworth/report-2026-09-21.md` | 87.1% | The length-15 free arm, and read_worth should be unwired in favor of it. At the point where jev's accuracy sits, the length floor saves more bytes for the same accuracy, on both runs, with no live call and no cost. | 70 hand-labelled paragraphs, 35 keep and 35 drop, over 25 tasks, judged twice | stands |
| shortlist | 2026-09-21 | `bench/shortlist/report-2026-09-21.md` | The corpus holds four. | The ticket asked for at least thirty file-localisation questions drawn from real recorded turns in .tofu/sessions. The corpus holds four. | 4 questions, drawn from 131 recorded turns of which 20 ever write | stands |
| sift | 2026-09-21 | `bench/sift/report-2026-09-21.md` | 27 of 34 | The judgment, on needles kept at equal bytes saved, and it is not wired. catalog/policy/shell_sift@1.yaml still reads mode: shadow, so the model is given every shell result whole; TOFU-217 measured the win and left the lock for a later ticket to pull. | 34 needles, two full live runs, 594 live decisions | stands |
| stopcheck | 2026-09-21 | `bench/stopcheck/report-2026-09-21.md` | 71 agreements against 68 | the typed arm won, 71 agreements against 68 out of 78 labelled steps, which on a corpus this size is one labeller's judgement on a handful of steps rather than a result. | 78 labelled steps of 79 recorded | stands |
| testquality | 2026-09-21 | `bench/testquality/report-2026-09-21.md` | off 53, on 53 | Dead-test count over the nine comparable tasks: off 53, on 53. Net zero. | 9 comparable tasks, 10 runs with the rules on and 10 with them off | stands |
| thrift | 2026-09-21 | `bench/thrift/report-2026-09-21.md` | 15.5 percent | The volume case does not hold. A recorded session runs a median of 7 tool calls and a worst case of 55, not 9,000. | 107 recorded sessions, 1,401 tool calls, 419 of them bash | stands |
| tokens | 2026-09-21 | `bench/tokens/report-2026-09-21-pinned.md` | 207 to 1,222 | unparsed: no heading in this file names its own answer | 136 glob calls replayed against a tree of 1,223 tracked files | stands |
| tokens | 2026-09-21 | `bench/tokens/report-2026-09-21.md` | 88.7% | Yes. glob is 88.7% of every result byte the model read, 48,186,030 of 54,295,422 over 109 recorded turns, and the cap cuts it to 3.2% and the total to 6,313,245 bytes, 88.4% less. | 109 recorded turns read through bench/corpus | stands |
| turn | 2026-09-21 | `bench/turn/report-2026-09-21.md` | 16513 ms | unparsed: no heading in this file names its own answer | 102 replayed turns | stands |
| websift | 2026-09-21 | `bench/websift/report-2026-09-21.md` | 21/21 | The readability rule, a free arm, and the judgment is not wired. catalog/policy/page_sift@1.yaml still reads mode: shadow, so the model is given every fetched page whole. | 24 pages: 21 focused and 3 reference pages up to 390KB | stands |
| harness | 2026-09-20 | `bench/harness/report-2026-09-20.md` | 10 write calls | unparsed: no heading in this file names its own answer | 1 recorded turn, 10 write calls | stands |
| mutate | 2026-09-20 | `bench/mutate/report-2026-09-20.md` | 84.62% | unparsed: no heading in this file names its own answer | one recorded gremlins run: 88 killed, 16 lived, 5 not covered | stands |
| recall | 2026-09-20 | `bench/recall/report-2026-09-20.md` | 20 real forks | From the TOFU-246 log, dated 2026-09-21, taken from a ticket log rather than a rerun: the free arm, and gap 11 is struck rather than built. | 20 real forks | stands |
| stopcheck | 2026-09-20 | `bench/stopcheck/report-2026-09-20.md` | 71 agreements against 68 | the typed arm won, 71 agreements against 68 out of 78 labelled steps, which on a corpus this size is one labeller's judgement on a handful of steps rather than a result. | 78 labelled steps of 79 recorded | stands |
| transform | 2026-09-20 | `bench/transform/report-2026-09-20.md` | 3 of 14 | unparsed: no heading in this file names its own answer | 17 recorded turns, 14 write calls | stands |
| cost | 2026-09-19 | `bench/cost/report-2026-09-19.md` | $1.584970 | not quoted here: this report is withdrawn in part, and the note says by whom | 89 cases, 5 arms | withdrawn in part |
| stopcheck | 2026-09-19 | `bench/stopcheck/report-2026-09-19.md` | 37 agreements against 36 | the typed arm won, 37 agreements against 36 out of 41 labelled steps, which on a corpus this size is one labeller's judgement on a handful of steps rather than a result. | 41 labelled steps | stale |
| api | 2026-09-18 | `bench/api/report-2026-09-18.md` | 598 ms | No first call was discarded as a warm-up. Latency by state size, 3 runs each, median/tokens: 300 tokens 305 ms/920, 1k 320 ms/1982, 4k 428 ms/6748, 12k 486 ms/19446, 28k 598 ms/32086. | 5 state sizes at 3 runs each, plus a 6 case gate battery | stands |
| cost | 2026-09-18 | `bench/cost/report-2026-09-18.md` | $0.000041 | Jev decided for $0.000041 per correct decision and opus for $0.005351, 131 times Jev. On this date both sides were money on the same credential kind, so the ratio was defined. | 6 cases, 4 arms | stands |
| turn | 2026-09-18 | `bench/turn/report-2026-09-18.md` | 3 of 24 runs failed | unparsed: no heading in this file names its own answer | 4 tasks, 3 reps, 2 arms, 24 turns | stands |
| wording | 2026-09-18 | `bench/wording/report-2026-09-18.md` | 1 of 6 | Keep tool_gate@1. No case's verdict changed between the two wordings, every margin sits inside or explained beyond the 0.00 to 0.05 rerun spread BOJI-006 measured on this corpus (1 of 6 cases (case-4-rm-rf.json) had a per-question margin wider than that spread, see above), and v2 costs more for the identical decision on every case. | 6 cases, two wordings of the same question set | stands |

## Not standing

- `bench/forkcache/report-2026-09-22.txt` is withdrawn in part, said by bench/report/withdrawals.json: the estimator section only, withdrawn by the board entry of 2026-09-22 in .local/boji/tickets/BOARD.md and not by the file. Two of its rows compare numbers taken from two different conversations, so that section does not measure the estimator against the provider. Every other section of the file stands.
- `bench/cost/report-2026-09-19.md` is withdrawn in part, said by the file's own text: # PART OF THIS REPORT IS WITHDRAWN, 2026-09-19, BOJI-133
- `bench/stopcheck/report-2026-09-19.md` is stale, said by the file's own text: > STALE. Every agreement figure below is measured against a corpus smaller than the one in the tree.

## Benches with no dated report

- `bench/cmd`, runner: the bench command itself, package main. It runs the other benches and measures nothing of its own.
- `bench/corpus`, library: the shared reader over recorded sessions and the six gate cases. Every bench that reads .tofu/sessions reads it through here, so its work shows up in the reports that use it.
- `bench/prompts`, measurement with no dated report: a correlation over recorded prompts, owned by TOFU-380 and not yet run to a dated report.
- `bench/report`, runner: this package: the reader over every dated report, the viewer and the data it reads. It reads other packages' reports and measures nothing itself.
- `bench/stat`, library: median and percentile over a slice of floats, called by every bench that reports a spread.
- `bench/tools`, measurement with no dated report: four search arms over a 24 KB question corpus: ripgrep, git grep, bm25 and jevgrep. It has a corpus and it has arms, so it owes a dated report.
- `bench/tui`, measurement with no dated report: Go benchmarks over the session view, run by go test -bench. Never written up to a dated report, so no figure from it is quotable.

## Where these reports came from

0 of 27 are built by running the package's own code again. 27 are built from the markdown's own text, which is weaker evidence, and every one of them says so.
