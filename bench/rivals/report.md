# bench rivals: tofu 0.5.15-rc7 against Claude Code, omp and codex on one task

**tldr: all 15 runs scored 10 of 10, so tofu's quality median is 10 on Sonnet 5.5 and 10 on gpt-5.6-sol, and no run left the code unchanged. On gpt-5.6-sol tofu now beats codex on wall time (66.1 s against 83.4 s), total input, memory and CPU. On Sonnet 5.5 it ties Claude Code and omp on wall time and input and uses the least memory, but loses on output tokens, requests against Claude Code, fresh input, and its own memory model.** The rivals also got 13% to 37% faster since 2026-10-13, so part of tofu's gain is upstream.

Fifteen live runs on 2026-10-17, 01:52 to 02:08 local time, one at a time behind the rivalslot, on the owner's subscriptions: tofu, Claude Code and omp on Sonnet 5.5, tofu and codex on gpt-5.6-sol, three runs each. Rep 1 of each arm was the price run and counts as one of the 15. Rows are in `runs/0.5.15-rc7`. **This benchmark needs the network and spends subscription quota**: `go run ./bench/rivals run -arm <arm> -rep <n> [-setting omitted] -batch 0.5.15-rc7 -slot <dir>`, then `go run ./bench/rivals report -batch 0.5.15-rc7`. The task, the hidden tests and the rubric are unchanged from 2026-10-13 (`testdata/prompt.txt`, `testdata/hidden`, `testdata/reference`). A difference inside the spread of three runs is called a tie.

## Price of one run per arm, rep 1

Source: `runs/0.5.15-rc7/*-1.json`, `tofu usage` at 01:33 and 02:10.

| arm | wall s | conversation requests | memory model calls | total in | out | quality of 10 |
|---|---|---|---|---|---|---|
| tofu-sonnet | 53.4 | 5 | 3 | 75,837 | 2,892 | 10 |
| claude-sonnet | 38.1 | 3 | 0 | 71,443 | 2,092 | 10 |
| omp-sonnet | 47.8 | 5 | 0 | 70,848 | 2,490 | 10 |
| tofu-gpt | 52.8 | 7 | 3 | 77,921 | 3,351 | 10 |
| codex-gpt | 83.4 | 7 | 0 | 124,597 | 4,238 | 10 |

| quota window | before, 01:33 | after the 15 runs and one discarded run, 02:10 |
|---|---|---|
| claude-sub account 1, 5h and 7d | 4%, 85% | 5%, 85% |
| claude-sub account 2, 5h and 7d | 8%, 44% | 16%, 47% |
| codex-sub, 7d | 54% | 55% |
| money | 0 | 0 |

## Parity, per pairing

Source: the run rows, the Claude Code and omp stdout of rep 1, the codex rollout `turn_context` of rep 1.

| arm | model the responses reported | effort, and how it was set | thinking summary, and what shows it |
|---|---|---|---|
| tofu-sonnet | claude-sonnet-5-5 | medium, `--effort medium` | none: `--thinking-summary omitted`, recorded body `thinking {"type":"adaptive"}` |
| claude-sonnet | claude-sonnet-5-5 | medium, `--effort medium` | none: 2.1.301 sends `display` only when `showThinkingSummaries` is set, it is not, and its thinking block came back with 0 characters of text |
| omp-sonnet | claude-sonnet-5-5 | medium, `--thinking medium` | none: `omitThinking: true` in `testdata/omp-overlay.yml`, and its 3 thinking blocks came back with 0 characters |
| tofu-gpt | gpt-5.6-sol | medium, `--effort medium` | none: `--thinking-summary omitted`, recorded body `reasoning {"context":"all_turns","effort":"medium"}` |
| codex-gpt | gpt-5.6-sol | medium, `-m gpt-5.6-sol -c model_reasoning_effort="medium"` | none: rollout `turn_context` says `summary: none` |

## Where tofu wins

Source: `runs/0.5.15-rc7` (2026-10-17) against `runs/*.json` (2026-10-13). Medians, spread in brackets.

| pairing | measure | tofu rc7 | rival rc7 | verdict | tofu 2026-10-13 | rival 2026-10-13 |
|---|---|---|---|---|---|---|
| GPT | wall s | 66.1 [52.8 to 66.1] | codex 83.4 [69.8 to 89.2] | tofu, 1.3x faster | 325.8 | 110.8 |
| GPT | total in | 78,985 [77,921 to 90,596] | codex 126,689 [124,597 to 131,509] | tofu, 1.6x fewer | 494,279 | 125,052 |
| GPT | tools s, timed per run | 11.0 [9.7 to 11.5] | codex 21.9 [21.8 to 26.8] | tofu, 2.0x less | 19.3 | 25.4 |
| GPT | CPU s | 29.3 [26.0 to 31.6] | codex 60.2 [59.5 to 70.7] | tofu, 2.1x less | 55.8 | 67.1 |
| GPT | peak tree RSS MiB | 304 [304 to 307] | codex 496 [462 to 528] | tofu, 1.6x less | 393 | 543 |
| Sonnet | peak tree RSS MiB | 281 [275 to 297] | Claude Code 592, omp 789 | tofu, 2.1x and 2.8x less | 393 | 616, 872 |
| Sonnet | CPU s | 30.4 [26.5 to 46.9] | Claude Code 48.5 [48.0 to 59.0] | tofu, 1.6x less | 53.0 | 57.5 |
| Sonnet | cache write | 5,675 [5,589 to 5,980] | Claude Code 7,616 [7,559 to 8,821] | tofu, 1.3x fewer | 13,686 | 11,783 |
| Sonnet | edit p50 ms | 6 | omp 116 | tofu | 1 to 3 | 22 to 127 |
| GPT | edit p50 ms | 4 | codex 850 | tofu | 1 to 3 | about 500 to 4,000 |

## Where tofu ties

Source: `runs/0.5.15-rc7` (2026-10-17) against `runs/*.json` (2026-10-13).

| pairing | measure | tofu rc7 | rivals rc7 | tofu 2026-10-13, verdict then |
|---|---|---|---|---|
| both | quality of 10 | 10 [10 to 10] on Sonnet, 10 [10 to 10] on GPT | 10 [10 to 10] on every arm | 10 [9 to 10] and 10 [4 to 10], tie and loss |
| Sonnet | wall s | 37.7 [31.0 to 53.4] | Claude Code 31.8 [30.6 to 38.1], omp 33.8 [31.6 to 47.8] | 63.1, loss to both |
| Sonnet | total in | 75,837 [61,308 to 76,306] | Claude Code 71,443 [71,244 to 95,937], omp 65,280 [54,782 to 70,848] | 234,241, loss to both |
| both | failed calls | 0 [0 to 1] and 0 [0 to 0] | 0 on every arm | 4 and 3, loss |
| Sonnet | requests against omp | 5 [4 to 5] | omp 5 [4 to 5] | 11 against 4, loss |
| GPT | requests | 7 [7 to 8] | codex 7 [7 to 7] | 27 against 7, loss |
| GPT | out | 3,488 [3,351 to 3,987] | codex 4,238 [3,333 to 5,082] | 8,401 against 4,447, loss |
| Sonnet | CPU s against omp | 30.4 [26.5 to 46.9] | omp 52.2 [45.2 to 55.0] | 53.0 against 55.5, tie |

## Where tofu loses

Source: `runs/0.5.15-rc7` (2026-10-17) against `runs/*.json` (2026-10-13).

| pairing | measure | tofu rc7 | rival rc7 | verdict | tofu 2026-10-13 | rival 2026-10-13 |
|---|---|---|---|---|---|---|
| Sonnet | out | 2,892 [2,758 to 2,997] | Claude Code 1,747 [1,733 to 2,092], omp 2,435 [1,308 to 2,490] | 1.7x and 1.2x more | 3,874 | 4,316, 1,854 |
| Sonnet | conversation requests | 5 [4 to 5] | Claude Code 3 [3 to 4] | 2 more at the median, spreads meet at 4 | 11 | 6 |
| both | memory model calls | 3 [3 to 4] per run, claude-haiku-4-5 | 0 on every rival | 3 more calls, and on the GPT pairing they spend claude-sub quota | not counted | not counted |
| Sonnet | fresh in | 2,784 [2,778 to 4,177] | Claude Code 6, omp 12 | over 200x | 1,194 | 12, 10 |
| GPT | fresh in | 28,897 [17,161 to 28,900] | codex 10,677 [10,337 to 12,213] | 2.7x more | 83,143 | 10,876 |
| GPT | to first token, summed per run s | 40.4 [28.0 to 40.8] | codex 25.0 [23.8 to 34.7], an upper bound | at least 1.6x longer | 197.5 | 38.3 |

## Runs that changed nothing, and runs set aside

Source: `runs/0.5.15-rc7` and `runs/0.5.15-rc7/discarded`.

| run | what happened | cause |
|---|---|---|
| none of the 15 | every run passed 5 of 5 hidden tests | |
| 2026-10-13 tofu-gpt rep 2, for reference | changed nothing, lead stopped on `board_lead` | the owner's global `projectManagement boardy`; not exercised here, since a fresh HOME resolves `projectManagement` to `off`, so this run does not show it fixed |
| tofu-sonnet rep 1, 01:34 | exit 2 in 0.1 s, no request | a fresh HOME has no `claude-sonnet-5-5` until `tofu models reload`; the bench now runs it in each fresh home |
| tofu-sonnet rep 1, 01:36, `discarded/` | 10 of 10 tests passed, rubric 4 of 5 for a stray `rtk/history.db` | bench defect: rtk writes its history into the working directory when `USERPROFILE` moves and `APPDATA` does not; the bench now moves `APPDATA` and `LOCALAPPDATA` too |

## What changed in the conditions since 2026-10-13

Source: `runs/0.5.15-rc7` rows and `runs/*.json` rows.

| condition | 2026-10-13 | 2026-10-17 |
|---|---|---|
| tofu build | 0.5.13-fix1+dev | 0.5.15-rc7, commit 9fa88559, the installed binary |
| tofu home | the owner's real home and his global settings | a fresh HOME per run, `agent.db` copied by `VACUUM INTO` only while every access token has 45 minutes left, then `tofu models reload` |
| tofu thinking summary | summarized on both wires | omitted on both wires, to match each rival |
| omp thinking summary | summarized, its default | omitted, `omitThinking: true` |
| tofu sub-agents per run | 1 | 0 in all six runs |
| Claude Code | 2.1.296 | 2.1.301 |
| omp, codex | 18.3.2, 0.155.1 | unchanged |
| rival wall time | Claude Code 50.5 s, omp 38.7 s, codex 110.8 s | 31.8 s, 33.8 s, 83.4 s |

## Conditions

Process tree: a Windows job object holds the whole process tree from its first instruction: CPU is the job's total user plus kernel time, peak tree RSS is the largest sum of every live member's working set sampled every 250 ms, peak commit is the job's PeakJobMemoryUsed.

| arm | tool version | asked for | every model the responses reported | credential | posture | machine | dates |
|---|---|---|---|---|---|---|---|
| tofu-sonnet | 0.5.15-rc7 commit 9fa88559 | claude-sub/claude-sonnet-5-5 | claude-sonnet-5-5, claude-haiku-4-5-20251001 | tofu's own claude-sub login, spend subscription windows 5h and 7d, no money | --wire anthropic --gate shadow --no-instructions, a fresh HOME per run holding a copy of the owner's claude-sub login, scratch project folder | windows/amd64, 32 logical CPUs | 2026-10-17 01:52 to 2026-10-17 02:05 |
| claude-sonnet | 2.1.301 (Claude Code) | claude-sonnet-5-5 | claude-sonnet-5-5 | apiKeySource none | --permission-mode bypassPermissions --safe-mode | windows/amd64, 32 logical CPUs | 2026-10-17 01:53 to 2026-10-17 02:05 |
| omp-sonnet | omp/18.3.2 | anthropic/claude-sonnet-5-5 | claude-sonnet-5-5 | provider anthropic through omp's own login, credential 3 | --auto-approve --no-rules --no-skills --no-extensions, foreign config providers off by --config overlay | windows/amd64, 32 logical CPUs | 2026-10-17 01:55 to 2026-10-17 02:06 |
| tofu-gpt | 0.5.15-rc7 commit 9fa88559 | codex-sub/gpt-5.6-sol | gpt-5.6-sol, claude-haiku-4-5-20251001 | tofu's own codex-sub login, spend subscription windows 5h and 7d, no money | --wire codex --gate shadow --no-instructions, a fresh HOME per run holding a copy of the owner's codex-sub login, scratch project folder | windows/amd64, 32 logical CPUs | 2026-10-17 01:56 to 2026-10-17 02:07 |
| codex-gpt | codex-cli 0.155.1 | gpt-5.6-sol | gpt-5.6-sol | Logged in using ChatGPT | --sandbox workspace-write with the Go cache added, --ignore-rules | windows/amd64, 32 logical CPUs | 2026-10-17 01:57 to 2026-10-17 02:08 |

## Every run

| arm | rep | exit | wall s | CPU s | peak tree RSS MiB | peak commit MiB | processes | conversation requests | fresh in | cache read | cache write | out | calls | failed | retried | hidden of 5 | rubric of 5 | lines +/- | notes |
|---|---|---|---|---|---|---|---|---|---|---|---|---|---|---|---|---|---|---|---|
| tofu-sonnet | 1 | 0 | 53.4 | 46.9 | 275 | 338 | 382 | 5 | 2778 | 67079 | 5980 | 2892 | 12 | 1 | 1 | 5 | 5 | +41/-6 | setting: thinkingSummary resolves to summarized in the fresh home, -setting omitted; turn outcome stopped |
| tofu-sonnet | 2 | 0 | 37.7 | 30.4 | 297 | 333 | 280 | 4 | 4177 | 51542 | 5589 | 2997 | 11 | 0 | 0 | 5 | 5 | +85/-6 | setting: thinkingSummary resolves to summarized in the fresh home, -setting omitted; turn outcome stopped |
| tofu-sonnet | 3 | 0 | 31.0 | 26.5 | 281 | 333 | 284 | 5 | 2784 | 67847 | 5675 | 2758 | 11 | 0 | 0 | 5 | 5 | +85/-6 | setting: thinkingSummary resolves to summarized in the fresh home, -setting omitted; turn outcome stopped |
| claude-sonnet | 1 | 0 | 38.1 | 59.0 | 592 | 1028 | 599 | 3 | 6 | 62616 | 8821 | 2092 | 2 | 0 | 0 | 5 | 5 | +85/-6 |  |
| claude-sonnet | 2 | 0 | 30.6 | 48.0 | 624 | 1034 | 516 | 3 | 6 | 63622 | 7616 | 1747 | 2 | 0 | 0 | 5 | 5 | +82/-6 |  |
| claude-sonnet | 3 | 0 | 31.8 | 48.5 | 577 | 911 | 521 | 4 | 8 | 88370 | 7559 | 1733 | 3 | 0 | 0 | 5 | 5 | +82/-6 |  |
| omp-sonnet | 1 | 0 | 47.8 | 52.2 | 826 | 1447 | 298 | 5 | 12 | 54459 | 16377 | 2490 | 10 | 0 | 0 | 5 | 5 | +85/-6 |  |
| omp-sonnet | 2 | 0 | 33.8 | 55.0 | 789 | 1437 | 390 | 5 | 12 | 62364 | 2904 | 1308 | 4 | 0 | 0 | 5 | 5 | +41/-6 |  |
| omp-sonnet | 3 | 0 | 31.6 | 45.2 | 722 | 1357 | 293 | 4 | 10 | 49692 | 5080 | 2435 | 10 | 0 | 0 | 5 | 5 | +85/-6 |  |
| tofu-gpt | 1 | 0 | 52.8 | 26.0 | 304 | 333 | 288 | 7 | 28897 | 49024 | 0 | 3351 | 19 | 0 | 0 | 5 | 5 | +103/-6 | setting: thinkingSummary resolves to summarized in the fresh home, -setting omitted; turn outcome stopped |
| tofu-gpt | 2 | 0 | 66.1 | 31.6 | 307 | 334 | 288 | 8 | 28900 | 61696 | 0 | 3488 | 20 | 0 | 0 | 5 | 5 | +100/-6 | setting: thinkingSummary resolves to summarized in the fresh home, -setting omitted; turn outcome stopped |
| tofu-gpt | 3 | 0 | 66.1 | 29.3 | 304 | 331 | 288 | 7 | 17161 | 61824 | 0 | 3987 | 19 | 0 | 0 | 5 | 5 | +104/-6 | setting: thinkingSummary resolves to summarized in the fresh home, -setting omitted; turn outcome stopped |
| codex-gpt | 1 | 0 | 83.4 | 70.7 | 496 | 593 | 510 | 7 | 10677 | 113920 | 0 | 4238 | 6 | 0 | 0 | 5 | 5 | +130/-8 |  |
| codex-gpt | 2 | 0 | 89.2 | 59.5 | 462 | 589 | 499 | 7 | 12213 | 119296 | 0 | 5082 | 7 | 0 | 0 | 5 | 5 | +109/-6 |  |
| codex-gpt | 3 | 0 | 69.8 | 60.2 | 528 | 594 | 485 | 7 | 10337 | 116352 | 0 | 3333 | 6 | 0 | 0 | 5 | 5 | +93/-7 |  |

## Turn against turn

Median of the runs, with the lowest and highest in brackets. Total in is fresh plus cache read plus cache write.

| arm | runs | quality of 10 | wall s | CPU s | peak tree RSS MiB | conversation requests | total in | fresh in | out | calls | failed | retried | lines changed |
|---|---|---|---|---|---|---|---|---|---|---|---|---|---|
| tofu-sonnet | 3 | 10 [10 to 10] | 37.7 [31.0 to 53.4] | 30.4 [26.5 to 46.9] | 280.7 [275.0 to 297.3] | 5 [4 to 5] | 75837 [61308 to 76306] | 2784 [2778 to 4177] | 2892 [2758 to 2997] | 11 [11 to 12] | 0 [0 to 1] | 0 [0 to 1] | 91 [47 to 91] |
| claude-sonnet | 3 | 10 [10 to 10] | 31.8 [30.6 to 38.1] | 48.5 [48.0 to 59.0] | 592.2 [577.1 to 624.3] | 3 [3 to 4] | 71443 [71244 to 95937] | 6 [6 to 8] | 1747 [1733 to 2092] | 2 [2 to 3] | 0 [0 to 0] | 0 [0 to 0] | 88 [88 to 91] |
| omp-sonnet | 3 | 10 [10 to 10] | 33.8 [31.6 to 47.8] | 52.2 [45.2 to 55.0] | 788.6 [722.2 to 825.6] | 5 [4 to 5] | 65280 [54782 to 70848] | 12 [10 to 12] | 2435 [1308 to 2490] | 10 [4 to 10] | 0 [0 to 0] | 0 [0 to 0] | 91 [47 to 91] |
| tofu-gpt | 3 | 10 [10 to 10] | 66.1 [52.8 to 66.1] | 29.3 [26.0 to 31.6] | 304.4 [303.6 to 307.3] | 7 [7 to 8] | 78985 [77921 to 90596] | 28897 [17161 to 28900] | 3488 [3351 to 3987] | 19 [19 to 20] | 0 [0 to 0] | 0 [0 to 0] | 109 [106 to 110] |
| codex-gpt | 3 | 10 [10 to 10] | 83.4 [69.8 to 89.2] | 60.2 [59.5 to 70.7] | 495.7 [462.4 to 528.0] | 7 [7 to 7] | 126689 [124597 to 131509] | 10677 [10337 to 12213] | 4238 [3333 to 5082] | 6 [6 to 7] | 0 [0 to 0] | 0 [0 to 0] | 115 [100 to 138] |

## Where the wall time goes

One row per run whose transcript times each model request: tofu from `tofu session request`, codex from its rollout file. Model wait is every request from sent to its last byte, split at the first token; for codex the first token is the first finished output item, so its share is an upper bound. Own work is the time between requests that no tool call covers; it goes below zero when a tool starts before its request finishes streaming. Outside is the wall clock before the first request and after the last.

| arm | rep | wall s | requests | model wait s | to first token s | after first token s | tools s | own work s | outside s |
|---|---|---|---|---|---|---|---|---|---|
| tofu-sonnet | 1 | 53.4 | 5 | 15.6 | 3.9 | 11.7 | 31.9 | 0.0 | 5.9 |
| tofu-sonnet | 2 | 37.7 | 4 | 17.7 | 6.5 | 11.1 | 11.7 | 0.0 | 8.3 |
| tofu-sonnet | 3 | 31.0 | 5 | 16.0 | 4.9 | 11.1 | 9.3 | -0.0 | 5.7 |
| tofu-gpt | 1 | 52.8 | 7 | 36.7 | 28.0 | 8.7 | 9.7 | 0.1 | 6.4 |
| tofu-gpt | 2 | 66.1 | 8 | 48.4 | 40.4 | 8.0 | 11.5 | 0.1 | 6.1 |
| tofu-gpt | 3 | 66.1 | 7 | 48.6 | 40.8 | 7.8 | 11.0 | 0.1 | 6.3 |
| codex-gpt | 1 | 83.4 | 7 | 49.8 | 23.8 | 26.0 | 26.8 | 2.9 | 4.0 |
| codex-gpt | 2 | 89.2 | 7 | 61.9 | 34.7 | 27.1 | 21.9 | 2.3 | 3.1 |
| codex-gpt | 3 | 69.8 | 7 | 42.8 | 25.0 | 17.8 | 21.8 | 2.1 | 3.0 |

Every timed request of an arm pooled, fitted as a fixed wait plus a time per output token, least squares.

| arm | requests | fixed s per request | ms per output token |
|---|---|---|---|
| tofu-sonnet | 14 | 1.2 | 4.4 |
| tofu-gpt | 22 | 1.7 | 10.0 |
| codex-gpt | 21 | 1.0 | 10.5 |

## Tool against tool

Every call of the three runs pooled. Calls per run is the median. Time is from the tool's own transcript: tofu reports each call's duration itself, the other three are timed here from the moment the call event reached stdout to the moment its result did. An untimed call is one whose start the transcript did not show.

| arm | kind | calls per run | p50 ms | p95 ms | max ms | failed | untimed |
|---|---|---|---|---|---|---|---|
| tofu-sonnet | read | 4 | 12 | 17 | 17 | 0 | 0 |
| tofu-sonnet | edit | 1 | 6 | 11 | 11 | 1 | 0 |
| tofu-sonnet | write | 3 | 2 | 4 | 4 | 0 | 0 |
| tofu-sonnet | search | 2 | 3 | 15 | 15 | 0 | 0 |
| tofu-sonnet | bash | 1 | 9280 | 29287 | 29287 | 0 | 0 |
| claude-sonnet | bash | 2 | 4283 | 17223 | 17223 | 0 | 0 |
| omp-sonnet | read | 3 | 32 | 75 | 75 | 0 | 0 |
| omp-sonnet | edit | 1 | 116 | 137 | 137 | 0 | 0 |
| omp-sonnet | write | 3 | 13 | 90 | 90 | 0 | 0 |
| omp-sonnet | search | 2 | 77 | 135 | 135 | 0 | 0 |
| omp-sonnet | bash | 1 | 11479 | 14812 | 14812 | 0 | 0 |
| tofu-gpt | read | 8 | 1 | 2 | 2 | 0 | 0 |
| tofu-gpt | edit | 1 | 4 | 10 | 10 | 0 | 0 |
| tofu-gpt | write | 3 | 1 | 2 | 2 | 0 | 0 |
| tofu-gpt | search | 3 | 2 | 16 | 16 | 0 | 0 |
| tofu-gpt | bash | 2 | 69 | 11413 | 11413 | 0 | 0 |
| tofu-gpt | other | 2 | 2 | 31 | 31 | 0 | 0 |
| codex-gpt | edit | 2 | 850 | 4876 | 4876 | 0 | 0 |
| codex-gpt | bash | 4 | 1752 | 13306 | 13306 | 0 | 0 |
