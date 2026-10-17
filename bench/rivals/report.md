# bench rivals: tofu 0.5.15-rc7 against Claude Code, omp and codex

- **tofu against Claude Code on claude-sonnet-5-5**: tofu ahead, 5 wins, 2 losses, 6 ties. Wall time is a tie, tofu 1.2x slower at the median (37.7 s against 31.8 s); biggest wins: peak commit 3.1x less, peak memory 2.1x less.
- **tofu against omp on claude-sonnet-5-5**: tofu ahead, 3 wins, 2 losses, 8 ties. Wall time is a tie, tofu 1.1x slower at the median (37.7 s against 33.8 s); biggest wins: peak commit 4.3x less, peak memory 2.8x less.
- **tofu against codex on gpt-5.6-sol**: tofu ahead, 8 wins, 1 loss, 4 ties. Wall time is a win, tofu 1.3x faster at the median (66.1 s against 83.4 s); biggest wins: CPU time 2.1x less, tool time 2.0x less.

15 live runs, 2026-10-17 01:52 to 02:08, from `runs/0.5.15-rc7`, against the runs of 2026-10-13 directly under `runs/`. A measure is a win or a loss only when the two spreads do not meet; otherwise it is a tie.

## Where tofu wins

| pairing | measure | tofu | rival | tofu is | difference | on 2026-10-13 |
|---|---|---|---|---|---|---|
| Claude Code, claude-sonnet-5-5 | CPU time, s | 30.4 [26.5 to 46.9] | 48.5 [48.0 to 59.0] | 1.6x less | -18.1 s | tie |
| Claude Code, claude-sonnet-5-5 | CPU use, cores | 0.85 [0.80 to 0.88] | 1.55 [1.52 to 1.57] | 1.8x less | -0.70 cores | win |
| Claude Code, claude-sonnet-5-5 | peak memory, MiB | 281 [275 to 297] | 592 [577 to 624] | 2.1x less | -311 MiB | win |
| Claude Code, claude-sonnet-5-5 | peak commit, MiB | 333 [333 to 338] | 1,028 [911 to 1,034] | 3.1x less | -695 MiB | win |
| Claude Code, claude-sonnet-5-5 | processes started | 284 [280 to 382] | 521 [516 to 599] | 1.8x fewer | -237 | win |
| omp, claude-sonnet-5-5 | CPU use, cores | 0.85 [0.80 to 0.88] | 1.43 [1.09 to 1.63] | 1.7x less | -0.58 cores | win |
| omp, claude-sonnet-5-5 | peak memory, MiB | 281 [275 to 297] | 789 [722 to 826] | 2.8x less | -508 MiB | win |
| omp, claude-sonnet-5-5 | peak commit, MiB | 333 [333 to 338] | 1,437 [1,357 to 1,447] | 4.3x less | -1,104 MiB | win |
| codex, gpt-5.6-sol | wall time, s | 66.1 [52.8 to 66.1] | 83.4 [69.8 to 89.2] | 1.3x faster | -17.3 s | tie |
| codex, gpt-5.6-sol | CPU time, s | 29.3 [26.0 to 31.6] | 60.2 [59.5 to 70.7] | 2.1x less | -30.9 s | win |
| codex, gpt-5.6-sol | CPU use, cores | 0.48 [0.44 to 0.49] | 0.85 [0.67 to 0.86] | 1.8x less | -0.37 cores | win |
| codex, gpt-5.6-sol | peak memory, MiB | 304 [304 to 307] | 496 [462 to 528] | 1.6x less | -192 MiB | win |
| codex, gpt-5.6-sol | peak commit, MiB | 333 [331 to 334] | 593 [589 to 594] | 1.8x less | -260 MiB | win |
| codex, gpt-5.6-sol | processes started | 288 | 499 [485 to 510] | 1.7x fewer | -211 | tie |
| codex, gpt-5.6-sol | total input | 78,985 [77,921 to 90,596] | 126,689 [124,597 to 131,509] | 1.6x fewer | -47,704 tokens | loss |
| codex, gpt-5.6-sol | tool time, s | 11.0 [9.7 to 11.5] | 21.9 [21.8 to 26.8] | 2.0x less | -10.9 s | win |

## Where tofu loses

| pairing | measure | tofu | rival | tofu is | difference | on 2026-10-13 |
|---|---|---|---|---|---|---|
| Claude Code, claude-sonnet-5-5 | fresh input | 2,784 [2,778 to 4,177] | 6 [6 to 8] | 464x more | +2,778 tokens | loss |
| Claude Code, claude-sonnet-5-5 | output | 2,892 [2,758 to 2,997] | 1,747 [1,733 to 2,092] | 1.7x more | +1,145 tokens | tie |
| omp, claude-sonnet-5-5 | fresh input | 2,784 [2,778 to 4,177] | 12 [10 to 12] | 232x more | +2,772 tokens | loss |
| omp, claude-sonnet-5-5 | output | 2,892 [2,758 to 2,997] | 2,435 [1,308 to 2,490] | 1.2x more | +457 tokens | loss |
| codex, gpt-5.6-sol | fresh input | 28,897 [17,161 to 28,900] | 10,677 [10,337 to 12,213] | 2.7x more | +18,220 tokens | tie |

## Where it is a tie

| pairing | measure | tofu | rival | tofu is | difference | on 2026-10-13 |
|---|---|---|---|---|---|---|
| Claude Code, claude-sonnet-5-5 | quality | 10 | 10 | level | +0 points | tie |
| Claude Code, claude-sonnet-5-5 | wall time, s | 37.7 [31.0 to 53.4] | 31.8 [30.6 to 38.1] | 1.2x slower | +5.9 s | loss |
| Claude Code, claude-sonnet-5-5 | model requests | 5 [4 to 5] | 3 [3 to 4] | 1.7x more | +2 | loss |
| Claude Code, claude-sonnet-5-5 | total input | 75,837 [61,308 to 76,306] | 71,443 [71,244 to 95,937] | 1.1x more | +4,394 tokens | loss |
| Claude Code, claude-sonnet-5-5 | failed tool calls | 0 [0 to 1] | 0 | level | +0 | loss |
| Claude Code, claude-sonnet-5-5 | tool time, s | 11.7 [9.3 to 31.9] | 17.4 [17.3 to 21.5] | 1.5x less | -5.7 s | tie |
| omp, claude-sonnet-5-5 | quality | 10 | 10 | level | +0 points | tie |
| omp, claude-sonnet-5-5 | wall time, s | 37.7 [31.0 to 53.4] | 33.8 [31.6 to 47.8] | 1.1x slower | +3.9 s | loss |
| omp, claude-sonnet-5-5 | CPU time, s | 30.4 [26.5 to 46.9] | 52.2 [45.2 to 55.0] | 1.7x less | -21.8 s | tie |
| omp, claude-sonnet-5-5 | processes started | 284 [280 to 382] | 298 [293 to 390] | 1.0x fewer | -14 | loss |
| omp, claude-sonnet-5-5 | model requests | 5 [4 to 5] | 5 [4 to 5] | level | +0 | loss |
| omp, claude-sonnet-5-5 | total input | 75,837 [61,308 to 76,306] | 65,280 [54,782 to 70,848] | 1.2x more | +10,557 tokens | loss |
| omp, claude-sonnet-5-5 | failed tool calls | 0 [0 to 1] | 0 | level | +0 | loss |
| omp, claude-sonnet-5-5 | tool time, s | 11.7 [9.3 to 31.9] | 14.5 [11.9 to 16.0] | 1.2x less | -2.8 s | loss |
| codex, gpt-5.6-sol | quality | 10 | 10 | level | +0 points | tie |
| codex, gpt-5.6-sol | model requests | 7 [7 to 8] | 7 | level | +0 | loss |
| codex, gpt-5.6-sol | output | 3,488 [3,351 to 3,987] | 4,238 [3,333 to 5,082] | 1.2x fewer | -750 tokens | tie |
| codex, gpt-5.6-sol | failed tool calls | 0 | 0 | level | +0 | tie |

## What tofu spends that the others do not

tofu's memory model runs beside the conversation and no rival has one, so it is not a row above; its tokens are inside tofu's token totals.

| arm | memory model calls per run | memory model |
|---|---|---|
| tofu-sonnet | 3 [3 to 4] | claude-haiku-4-5-20251001 |
| tofu-gpt | 3 | claude-haiku-4-5-20251001 |

## Change since 2026-10-13, medians

| measure | tofu-sonnet | claude-sonnet | omp-sonnet | tofu-gpt | codex-gpt |
|---|---|---|---|---|---|
| quality | 10 → 10, +0% | 10 → 10, +0% | 10 → 10, +0% | 10 → 10, +0% | 10 → 10, +0% |
| wall time, s | 63.1 → 37.7, -40% | 50.5 → 31.8, -37% | 38.7 → 33.8, -13% | 325.8 → 66.1, -80% | 110.8 → 83.4, -25% |
| CPU time, s | 53.0 → 30.4, -43% | 57.5 → 48.5, -16% | 55.5 → 52.2, -6% | 55.8 → 29.3, -47% | 67.1 → 60.2, -10% |
| CPU use, cores | 0.84 → 0.85, +2% | 1.07 → 1.55, +44% | 1.46 → 1.43, -2% | 0.18 → 0.48, +164% | 0.58 → 0.85, +46% |
| peak memory, MiB | 393 → 281, -28% | 616 → 592, -4% | 872 → 789, -10% | 393 → 304, -23% | 543 → 496, -9% |
| peak commit, MiB | 541 → 333, -38% | 1,017 → 1,028, +1% | 1,484 → 1,437, -3% | 535 → 333, -38% | 593 → 593, +0% |
| processes started | 427 → 284, -33% | 572 → 521, -9% | 377 → 298, -21% | 477 → 288, -40% | 482 → 499, +4% |
| model requests | 11 → 5, -55% | 6 → 3, -50% | 4 → 5, +25% | 27 → 7, -74% | 7 → 7, +0% |
| total input | 234,241 → 75,837, -68% | 151,458 → 71,443, -53% | 54,983 → 65,280, +19% | 494,279 → 78,985, -84% | 125,052 → 126,689, +1% |
| fresh input | 1,194 → 2,784, +133% | 12 → 6, -50% | 10 → 12, +20% | 83,143 → 28,897, -65% | 10,876 → 10,677, -2% |
| output | 3,874 → 2,892, -25% | 4,316 → 1,747, -60% | 1,854 → 2,435, +31% | 8,401 → 3,488, -58% | 4,447 → 4,238, -5% |
| failed tool calls | 4 → 0, -100% | 0 → 0 | 0 → 0 | 3 → 0, -100% | 0 → 0 |
| tool time, s | 21.2 → 11.7, -45% | 19.9 → 17.4, -12% | 16.8 → 14.5, -14% | 19.3 → 11.0, -43% | 25.4 → 21.9, -14% |

## Medians per arm, lowest to highest in brackets

| arm | runs | quality | wall time, s | CPU time, s | CPU use, cores | peak memory, MiB | peak commit, MiB | processes started | model requests | total input | fresh input | output | failed tool calls | tool time, s |
|---|---|---|---|---|---|---|---|---|---|---|---|---|---|---|
| tofu-sonnet | 3 | 10 | 37.7 [31.0 to 53.4] | 30.4 [26.5 to 46.9] | 0.85 [0.80 to 0.88] | 281 [275 to 297] | 333 [333 to 338] | 284 [280 to 382] | 5 [4 to 5] | 75,837 [61,308 to 76,306] | 2,784 [2,778 to 4,177] | 2,892 [2,758 to 2,997] | 0 [0 to 1] | 11.7 [9.3 to 31.9] |
| claude-sonnet | 3 | 10 | 31.8 [30.6 to 38.1] | 48.5 [48.0 to 59.0] | 1.55 [1.52 to 1.57] | 592 [577 to 624] | 1,028 [911 to 1,034] | 521 [516 to 599] | 3 [3 to 4] | 71,443 [71,244 to 95,937] | 6 [6 to 8] | 1,747 [1,733 to 2,092] | 0 | 17.4 [17.3 to 21.5] |
| omp-sonnet | 3 | 10 | 33.8 [31.6 to 47.8] | 52.2 [45.2 to 55.0] | 1.43 [1.09 to 1.63] | 789 [722 to 826] | 1,437 [1,357 to 1,447] | 298 [293 to 390] | 5 [4 to 5] | 65,280 [54,782 to 70,848] | 12 [10 to 12] | 2,435 [1,308 to 2,490] | 0 | 14.5 [11.9 to 16.0] |
| tofu-gpt | 3 | 10 | 66.1 [52.8 to 66.1] | 29.3 [26.0 to 31.6] | 0.48 [0.44 to 0.49] | 304 [304 to 307] | 333 [331 to 334] | 288 | 7 [7 to 8] | 78,985 [77,921 to 90,596] | 28,897 [17,161 to 28,900] | 3,488 [3,351 to 3,987] | 0 | 11.0 [9.7 to 11.5] |
| codex-gpt | 3 | 10 | 83.4 [69.8 to 89.2] | 60.2 [59.5 to 70.7] | 0.85 [0.67 to 0.86] | 496 [462 to 528] | 593 [589 to 594] | 499 [485 to 510] | 7 | 126,689 [124,597 to 131,509] | 10,677 [10,337 to 12,213] | 4,238 [3,333 to 5,082] | 0 | 21.9 [21.8 to 26.8] |

## Load: memory and CPU as sub-agents are added

From `bench/scale/results.jsonl`, live arm: one run per rung, each sub-agent reads one file and repeats its first line. MiB per sub-agent is the least squares slope over every rung.

| tool | version | dates | rungs | peak RSS at 0, MiB | peak RSS at the top rung, MiB | MiB per sub-agent, peak | MiB per sub-agent, steady | peak commit at the top rung, MiB | CPU at the top rung, s | cores at the top rung |
|---|---|---|---|---|---|---|---|---|---|---|
| tofu, claude-sonnet-5-5 | 0.5.14 75d2b58b | 2026-10-14 | 0 to 10 sub-agents | 35 | 47 | 0.7 | 0.7 | 103 | 1.4 | 0.07 |
| Claude Code, claude-sonnet-5-5 | not recorded | 2026-10-13 | 0 to 10 sub-agents | 227 | 292 | 4.3 | 4.7 | 395 | 4.8 | 0.24 |
| omp, claude-sonnet-5-5 | not recorded | 2026-10-13 | 0 to 10 sub-agents | 543 | 876 | 20.9 | 4.8 | 1609 | 11.0 | 0.57 |
| codex, gpt-5.6-sol | not recorded | 2026-10-13 | 0 to 10 sub-agents | 213 | 440 | 22.0 | 14.4 | 366 | 37.2 | 0.56 |

### tofu with 10 and with 450 sub-agents in one session

From `bench/longsession/results.jsonl`, arms head10 and head450, tofu 0.5.15-rc1 on anthropic messages from a local stub, no model, 2026-10-15: each cell is the median of the samples of that phase. Idle is the session open with nothing running; work is its sub-agents working.

| sub-agents | phase | samples | working set MiB | private MiB | cores |
|---|---|---|---|---|---|
| 10 | idle | 10 | 43 | 68 | 0.000 |
| 450 | idle | 10 | 112 | 139 | 0.000 |
| 10 | work | 10 | 74 | 95 | 0.057 |
| 450 | work | 10 | 286 | 310 | 0.072 |

From 10 to 450 sub-agents the working set grows 0.16 MiB per sub-agent idle and 0.48 MiB working.

### Memory per sub-agent on the subagents task

From `runs/subagents`: peak tree RSS of a run divided by the sub-agents it started, on a real task with a live model, so the lead's own memory is spread across them.

| arm | build | runs | sub-agents | peak memory, MiB | MiB per sub-agent |
|---|---|---|---|---|---|
| tofu-sonnet | 0.5.14 commit b0e2401d | 3 | 6 | 363 [354 to 368] | 60 [59 to 61] |
| omp-sonnet | omp/18.3.2 | 3 | 6 [6 to 7] | 1,080 [1,046 to 1,155] | 180 [149 to 192] |

## Runs that fell short or were set aside

| run file | arm | date | exit | quality | notes |
|---|---|---|---|---|---|
| `runs/tofu-sonnet-2.json` | tofu-sonnet | 2026-10-13 16:40 | 0 | 9 of 10 | turn outcome stopped; 1 sub-agents |
| `runs/tofu-gpt-2.json` | tofu-gpt | 2026-10-13 17:05 | 0 | 4 of 10 | turn outcome stopped; hidden bug: hidden_bug_test.go:19: Page(5 items, 3, 2) = "", want "e"; hidden feature: .\hidden_feature_test.go:10:16: inv.Remove undefined (type *Inventory has no field or method Remove); hidden rename: .\hidden_rename_test.go:10:14: c.Subtotal undefined (type Cart has no field or method Subtotal); hidden search: hidden_search_test.go:12: open ANSWER.txt: The system cannot find the file specified.; hidden newfile: .\hidden_newfile_test.go:15:12: undefined: ExportCSV |
| `runs/0.5.15-rc7/discarded/tofu-sonnet-omitted-1.json` | tofu-sonnet | 2026-10-17 01:36 | 0 | 9 of 10 | setting: thinkingSummary resolves to summarized in the fresh home, -setting omitted; turn outcome stopped; stray: rtk/history.db |

## Conditions

| arm | build on 2026-10-13 | build now | models the responses reported | thinking sent | sub-agents | credential | machine | dates |
|---|---|---|---|---|---|---|---|---|
| tofu-sonnet | version   0.5.13-fix1+dev commit not recorded | 0.5.15-rc7 commit 9fa88559 | claude-sonnet-5-5, claude-haiku-4-5-20251001 | thinking {"type":"adaptive"} | 0 | tofu's own claude-sub login, spend subscription windows 5h and 7d, no money | windows/amd64, 32 logical CPUs | 2026-10-17 01:52 to 2026-10-17 02:05 |
| claude-sonnet | 2.1.296 (Claude Code) | 2.1.301 (Claude Code) | claude-sonnet-5-5 | not read | 0 | apiKeySource none | windows/amd64, 32 logical CPUs | 2026-10-17 01:53 to 2026-10-17 02:05 |
| omp-sonnet | omp/18.3.2 | omp/18.3.2 | claude-sonnet-5-5 | not read | 0 | provider anthropic through omp's own login, credential 3 | windows/amd64, 32 logical CPUs | 2026-10-17 01:55 to 2026-10-17 02:06 |
| tofu-gpt | version   0.5.13-fix1+dev commit not recorded | 0.5.15-rc7 commit 9fa88559 | gpt-5.6-sol, claude-haiku-4-5-20251001 | reasoning {"context":"all_turns","effort":"medium"} | 0 | tofu's own codex-sub login, spend subscription windows 5h and 7d, no money | windows/amd64, 32 logical CPUs | 2026-10-17 01:56 to 2026-10-17 02:07 |
| codex-gpt | codex-cli 0.155.1 | codex-cli 0.155.1 | gpt-5.6-sol | not read | 0 | Logged in using ChatGPT | windows/amd64, 32 logical CPUs | 2026-10-17 01:57 to 2026-10-17 02:08 |

| arm | posture |
|---|---|
| tofu-sonnet | --wire anthropic --gate shadow --no-instructions, a fresh HOME per run holding a copy of the owner's claude-sub login, scratch project folder |
| claude-sonnet | --permission-mode bypassPermissions --safe-mode |
| omp-sonnet | --auto-approve --no-rules --no-skills --no-extensions, foreign config providers off by --config overlay |
| tofu-gpt | --wire codex --gate shadow --no-instructions, a fresh HOME per run holding a copy of the owner's codex-sub login, scratch project folder |
| codex-gpt | --sandbox workspace-write with the Go cache added, --ignore-rules |

## Method

| item | what it is |
|---|---|
| task | one small Go package, `testdata/seed`, and one prompt, `testdata/prompt.txt`: a bug, a feature, a rename, a question and a new file |
| quality | hidden tests from `testdata/hidden` plus the rubric in `score.go`: visible tests, gofmt, go vet, the old name gone, no stray files |
| verdict | a win or a loss only when the lowest to highest spreads of the two arms do not meet; otherwise a tie |
| order | one run at a time behind the rival slot, on the owner's subscriptions, no money |
| network | this bench needs the network and spends subscription quota; the report itself reads only `runs/` |
| rerun | `go run ./bench/rivals run -arm <arm> -rep <n> -setting omitted -batch <batch> -slot <dir>`, then `go run ./bench/rivals report -batch <batch>` |

## Parity

Read off the rival's own stdout or rollout of the first run of each arm; the models and the thinking tofu sent are in the conditions table.

| arm | effort, and how it was set | thinking summary, and what shows it |
|---|---|---|
| tofu-sonnet | medium, `--effort medium` | none, `--thinking-summary omitted` |
| claude-sonnet | medium, `--effort medium` | none: Claude Code sends `display` only when `showThinkingSummaries` is set, it is not, and its thinking came back empty |
| omp-sonnet | medium, `--thinking medium` | none: `omitThinking: true` in `testdata/omp-overlay.yml`, and its thinking came back empty |
| tofu-gpt | medium, `--effort medium` | none, `--thinking-summary omitted` |
| codex-gpt | medium, `-c model_reasoning_effort="medium"` | none: the rollout `turn_context` says `summary: none` |

## Fresh input, by source

Fresh input is the prompt the provider served from no cache. Split per request from `tofu session trace` of each run's work folder and, for codex, the `last_token_usage` of its rollout; every total equals the run file's `fresh_in`.

| source | tofu-sonnet runs 1, 2, 3 | tofu-gpt runs 1, 2, 3 | codex-gpt runs 1, 2, 3 | does it earn its cost |
|---|---|---|---|---|
| conversation, first request | 4, 4, 4 | 7,556, 7,556, 7,556 | 3,142, 4,142, 4,142 | Sonnet reads the prefix the previous run cached. On GPT tofu's prefix is cold every session while codex's first request reads 11,392 cached; a guess to test: `prompt_cache_key` is the session id, `internal/llm/wire/codex/request.go:159` |
| conversation, messages new since the last request | 8, 6, 8 | 5,727, 5,970, 6,396 | 7,535, 8,071, 6,195 | yes: tool results the model has not seen yet, about what codex sends |
| conversation, a later request with no cache read | 0, 0, 0 | 12,890 at step 7, 12,668 at step 6, 0 | 0, 0, 0 | no, waste: the instructions, tools and first message were byte-identical to the first request of the run, checked on run 1, and the request before it did read the cache, so the cause is not found in the body; needs a ticket |
| memory model, claude-haiku-4-5 | 2,766, 4,167, 2,772 | 2,724, 2,706, 3,209 | none | tofu's own feature: three one-shot calls of about 900 tokens, none read from cache; whether memory pays is the memory bench's question, not this one |
| total, the run file's fresh input | 2,778, 4,177, 2,784 | 28,897, 28,900, 17,161 | 10,677, 12,213, 10,337 | on Sonnet the conversation alone is 10 to 12, level with Claude Code's 6 and omp's 12 |

## Caveats

| measure | caveat |
|---|---|
| kept out of the comparison | memory model calls, time to first token, edit call p50 and cache write: not every arm can have them |
| tool time | tofu times each call itself; Claude Code, omp and codex are timed from the call event on stdout to its result |
| CPU use | CPU seconds of the whole process tree over wall seconds, so cores busy on average |
| load | the load tables come from earlier benches, `bench/scale` and `bench/longsession`, on older builds; their dates are in the rows |

## Appendix

Every table below is printed from the run files as they are.

Process tree: a Windows job object holds the whole process tree from its first instruction: CPU is the job's total user plus kernel time, peak tree RSS is the largest sum of every live member's working set sampled every 250 ms, peak commit is the job's PeakJobMemoryUsed.

### Every run

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

### Where the wall time goes

One row per run whose transcript times each model request: tofu from `tofu session request`, codex from its rollout file, where the first token is the first finished output item and so an upper bound. Own work is the time between requests that no tool call covers, and outside is the wall clock before the first request and after the last.

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

### Tool against tool

Every call of the runs pooled; calls per run is the median. tofu reports each call's duration itself, the other three are timed here from the call event on stdout to its result.

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

### Task bigread, from `runs/bigread`

Prompt, rubric and seed in `testdata/tasks/bigread`. Memory or judge calls are the calls outside the conversation, tofu's memory model and omp's auto-thinking judge.

| arm | tool version | runs | quality of 6 | conversation requests | memory or judge calls | fresh in | cache read | cache write | total in | out | wall s | peak tree RSS MiB | failed calls | sub-agents |
|---|---|---|---|---|---|---|---|---|---|---|---|---|---|---|
| tofu-sonnet | 0.5.14 commit b0e2401d | 3 | 6 [6 to 6] | 6 [6 to 6] | 3 [3 to 3] | 5110 [5023 to 5149] | 120510 [120299 to 121263] | 3101 [3081 to 3445] | 128721 [128403 to 129857] | 1063 [966 to 1085] | 21.5 [20.8 to 22.8] | 57.5 [53.3 to 70.3] | 0 [0 to 0] | 0 [0 to 0] |
| omp-sonnet | omp/18.3.2 | 3 | 6 [6 to 6] | 7 [7 to 7] | 0 [0 to 0] | 16 [16 to 16] | 91850 [91078 to 91884] | 4649 [4246 to 4662] | 96528 [95340 to 96549] | 982 [980 to 1041] | 22.6 [20.4 to 23.2] | 537.6 [531.9 to 541.7] | 0 [0 to 0] | 0 [0 to 0] |

### Task longedit, from `runs/longedit`

Prompt, rubric and seed in `testdata/tasks/longedit`. Memory or judge calls are the calls outside the conversation, tofu's memory model and omp's auto-thinking judge.

| arm | tool version | runs | quality of 10 | conversation requests | memory or judge calls | fresh in | cache read | cache write | total in | out | wall s | peak tree RSS MiB | failed calls | sub-agents |
|---|---|---|---|---|---|---|---|---|---|---|---|---|---|---|
| tofu-sonnet | 0.5.14 commit b0e2401d | 3 | 10 [10 to 10] | 16 [12 to 18] | 8 [7 to 9] | 11296 [10118 to 13413] | 418778 [277977 to 466108] | 45990 [35919 to 52822] | 478181 [340917 to 513323] | 14937 [13270 to 19335] | 146.4 [143.5 to 167.5] | 347.1 [344.3 to 347.5] | 14 [6 to 23] | 1 [1 to 1] |
| omp-sonnet | omp/18.3.2 | 3 | 10 [10 to 10] | 7 [6 to 7] | 0 [0 to 0] | 16 [14 to 16] | 114697 [93075 to 114726] | 11505 [9727 to 12016] | 126247 [102816 to 126729] | 6185 [4197 to 6210] | 91.4 [80.6 to 116.6] | 778.2 [762.9 to 886.6] | 0 [0 to 0] | 0 [0 to 0] |

### Task subagents, from `runs/subagents`

Prompt, rubric and seed in `testdata/tasks/subagents`. Memory or judge calls are the calls outside the conversation, tofu's memory model and omp's auto-thinking judge.

| arm | tool version | runs | quality of 6 | conversation requests | memory or judge calls | fresh in | cache read | cache write | total in | out | wall s | peak tree RSS MiB | failed calls | sub-agents |
|---|---|---|---|---|---|---|---|---|---|---|---|---|---|---|
| tofu-sonnet | 0.5.14 commit b0e2401d | 3 | 6 [6 to 6] | 26 [25 to 27] | 8 [7 to 9] | 17252 [17114 to 23695] | 321872 [311789 to 349454] | 21765 [20898 to 26301] | 365425 [350668 to 394047] | 6965 [6374 to 7001] | 74.0 [71.9 to 80.1] | 362.5 [353.6 to 367.8] | 0 [0 to 1] | 6 [6 to 6] |
| omp-sonnet | omp/18.3.2 | 3 | 6 [6 to 6] | 31 [30 to 46] | 6 [0 to 7] | 2839 [76 to 2895] | 387604 [266793 to 574561] | 65005 [57744 to 123356] | 455504 [324613 to 700756] | 14501 [7325 to 21323] | 82.9 [80.3 to 124.6] | 1080.0 [1046.2 to 1154.8] | 4 [3 to 9] | 6 [6 to 7] |
