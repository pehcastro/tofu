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
