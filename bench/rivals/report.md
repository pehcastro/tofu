# bench rivals: tofu against Claude Code, omp and codex on one task

**On Sonnet 5.5, tofu does the task as well as Claude Code and omp, on the least memory, and loses on time, requests, tokens and refused calls. On gpt-5.6-sol it loses to codex on every measure except memory and CPU, and one run in three did nothing at all.** Most of tofu's losses come from its own orchestration rules, not from its tools: its read, edit, write and search calls are the fastest measured here.

Fifteen live runs on 2026-10-13, 16:17 to 17:14 local time, one at a time, on the owner's subscriptions: three arms on Sonnet 5.5 (tofu, Claude Code, omp) and two on gpt-5.6-sol (tofu, codex), three runs each. No call was discarded as a warm-up. Every arm got the same prompt, `testdata/prompt.txt`, in a fresh copy of the same seed repository with a fresh Go build cache, so every run compiled the standard library cold. **This benchmark needs the network and spends subscription quota**, so it runs only when asked: `go run ./bench/rivals run -arm <arm> -rep <n> -slot <dir>`.

## The task and how it is scored

`testdata/seed` is a Go package of 8 files. The prompt asks for five things: a bug fix (`Page` drops a partial last page), a feature (`Inventory.Remove` with two errors), a rename across four files (`CalcTotal` to `Subtotal`, tests included), a search question (which unexported function records stock changes, answer in `ANSWER.txt`), and a new file (`export.go` with `ExportCSV`).

**Hidden tests**: five, one per part, in `testdata/hidden`, copied in one at a time after the run, so a compile error in one part cannot fail another. They fail on the untouched seed (0 of 5) and pass on the reference solution in `testdata/reference` (5 of 5): `go run ./bench/rivals check`.

**Rubric**: five deterministic checks: the visible tests pass, `gofmt -l` is empty, `go vet` is clean, the old name is gone from every `.go` file, and no file outside the expected set was added. Quality is hidden plus rubric, out of 10.

## The judgement, measure by measure

Faster is not always better. Each measure is judged by the rule written beside it, and a difference inside the spread of three runs is called a tie.

- **Quality, higher is better and it is checked first.** Sonnet: tie, all three at a median of 10; tofu lost one rubric point in one run, keeping the test name `TestCalcTotal`. GPT: codex is better, 10 in all three runs; tofu scored 10, 10 and 4, and the 4 is a run that changed nothing.
- **Tokens, fewer is better only at equal quality.** Sonnet: omp is best at 55 k total input per run, Claude Code 151 k, tofu 234 k. GPT: codex 125 k, tofu 494 k. tofu is worst in both pairings.
- **Model requests, fewer is better because each one is a wait.** Sonnet: omp 4, Claude Code 6, tofu 11. GPT: codex 7, tofu 27. tofu's split into a lead and a sub-agent is where most of the extra requests come from.
- **Wall time, lower is better at equal quality.** Sonnet: omp 38.7 s, Claude Code 50.5 s, tofu 63.1 s. GPT: codex 110.8 s, tofu 325.8 s, and in tofu's two good GPT runs 290 s and 305 s of that was waiting on 27 to 29 model requests.
- **Failed and retried calls, fewer is better because each one is a wasted turn.** tofu is the only arm with any: 4 in every Sonnet run and 3 to 5 in the GPT runs. Every Sonnet failure was tofu refusing its own model (detail below). Claude Code, omp and codex had none.
- **Peak memory of the whole process tree, lower is better.** tofu is best in both pairings: 393 MB median on Sonnet against 616 MB for Claude Code and 872 MB for omp, and 393 MB on GPT against 543 MB for codex.
- **CPU seconds of the whole tree, lower is better.** A near tie on Sonnet, 53.0 s for tofu against 57.5 s and 55.5 s, because a cold `go test` dominates every run. On GPT tofu is lower, 55.8 s against 67.1 s.
- **Time per tool call, lower is better.** tofu's in-process tools are the fastest measured: read, edit, write and search at a p50 of 1 to 3 ms, against 22 to 127 ms for Claude Code and omp and about 0.5 to 4 s for codex, whose every read goes through a sandboxed PowerShell and every edit through `apply_patch`. The methods differ: tofu reports its own durations, the others are timed here from stdout, so this proves the order of magnitude, not a ratio.
- **Lines changed, smaller is better only at equal quality.** tofu wrote the smallest diff on Sonnet, 47 lines against 93 and 89, because it added fewer tests. That is a tie on quality and not a win: the hidden tests pass either way.

## Where tofu is better

1. **Memory.** The lowest peak tree RSS in both pairings: 393 MB against 616 MB (Claude Code), 872 MB (omp) and 543 MB (codex).
2. **Tool speed.** Its file tools run in-process in 1 to 3 ms at the median, an order of magnitude under the others, and 2 to 3 orders under codex's shell-based reads and patch edits.
3. **CPU on the GPT pairing.** 55.8 s against codex's 67.1 s for the same task.
4. **Quality on Sonnet is level with both rivals**, 10 of 10 at the median, with the smallest diff.

## Where tofu is worse, and the change proposed

1. **It refuses its own model four times a run, on every Sonnet run.** The same four refusals each time: the lead tries to write a file through bash and is told "the orchestrator never writes source through the shell"; the sub-agent runs `gofmt -l .` and is told it is "a tree-wide command: the orchestrator runs it, not a sub-agent"; then `gofmt -l <file>` and is told "gofmt is not on the read list"; then `go vet . && go test .` the same way. In a one-package repository the tree is the package. **Change**: put `gofmt -l` and `go vet` on the sub-agent read list, since both only read, and count a command as tree-wide by what it reaches, not by the `.` or `./...` in its argument.
2. **It spends roughly twice the requests and tokens of the next arm.** 11 requests against 6 and 4 on Sonnet; 27 against 7 on GPT. The lead reads the code, spawns `go-dev`, waits, then reads it again to verify, and every sub-agent request re-sends its own system prompt. omp did the same five parts in 4 requests by batching edits into one shell call. **Change**: let the lead do a task of this size itself rather than spawning, which is the 10-line edit allowance the refusal message already names, and measure the same task with `--no-subagents` as the arm that turns the split off.
3. **One GPT run in three did nothing.** In `tofu-gpt` run 2 the lead read the code, found the `board_lead` rule on, found no board in the project, asked for a rule override that no person could answer, and stopped: hidden 0 of 5, no file changed. The rule is on because `projectManagement` is `boardy` in the owner's global settings (`tofu settings` reports the source as `global`), and it reaches every new project. Sonnet ignored the same rule and finished; GPT obeyed it. **Change**: a project with no board should not get `board_lead` at all, or the lead should create the board, rather than a headless run ending with the work undone and exit code 0.
4. **GPT runs are about three times slower than codex.** 325.8 s against 110.8 s at the median, nearly all of it model wait across 27 to 29 requests, at the same effort on the same model. The fix is item 2.
5. **`tofu run` refuses `--model codex-sub/gpt-5.6-sol` without `--wire codex`**, with "codex-sub/gpt-6-sol belongs to the codex-sub subscription, and --wire anthropic reaches claude-sub". The slug already names the source. **Change**: take the wire from the slug's source when `--wire` is not given.
6. **The headless default refuses a plain new file.** In a probe before the runs, `tofu run` with the gate at its default of `enforce` refused writing `x.txt` at risk 1.83 over `risk_ask_at` 1.50 because no person was there to answer, and the turn stopped. Every rival has a one-flag headless posture that runs; tofu's is `--gate shadow`, which is what these runs used. **Change**: say in the refusal that `--gate shadow` is the headless posture, or make it the `tofu run` default when no terminal is attached.

## Where tofu-gpt's last 12 s went, 2026-10-14

**After the plan tool was withheld (TOFU-1346), tofu-gpt's median wall was 122.8 s against codex's 110.8 s at the same 7 requests. The gap is model wait, not tofu: 0.1 to 0.3 s of tofu's own work per run, and tofu's tools are 13 s against codex's 24 to 31 s. Every small request (under 400 output tokens) took 7.3 s in tofu against 5.4 s in codex, because tofu asks the codex backend for a reasoning summary on every request and codex asks for none.** Turning the summary off cut the median wall to 103.2 s at quality 10, under codex.

Rows for tofu-gpt after TOFU-1346 at `ba31cca9` plus its patch, 2026-10-14 07:54 to 08:05; codex-gpt rows are the ones in `runs/`. Split from the recorded transcripts, no new run.

| arm | rep | wall s | requests | model wait s | to first token s | after first token s | tools s | own work s | outside s |
|---|---|---|---|---|---|---|---|---|---|
| tofu-gpt | 1 | 137.8 | 10 | 119.8 | 75.1 | 44.7 | 13.3 | 0.2 | 4.5 |
| tofu-gpt | 2 | 122.8 | 7 | 102.8 | 53.4 | 49.4 | 13.3 | 0.1 | 6.6 |
| tofu-gpt | 3 | 105.2 | 7 | 87.9 | 53.3 | 34.5 | 12.1 | 0.2 | 5.1 |
| codex-gpt | 1 | 138.9 | 7 | 95.0 | 38.3 | 56.7 | 30.5 | 2.7 | 10.6 |
| codex-gpt | 2 | 110.8 | 7 | 72.9 | 36.3 | 36.6 | 25.4 | 2.4 | 10.2 |
| codex-gpt | 3 | 108.0 | 6 | 76.2 | 39.9 | 36.3 | 23.8 | 2.2 | 5.8 |

Request shape, every request of the three runs. Codex's is read from its rollout's `turn_context` and its model cache, which sets `default_reasoning_summary: none` for all nine models it lists.

| arm | requests | output tokens | reasoning effort | reasoning summary | input per request | cache read after the first | median small request s |
|---|---|---|---|---|---|---|---|
| tofu-gpt | 24 | 11,789 | medium | auto | 13 k to 21 k | 93% | 7.3 |
| codex-gpt | 20 | 12,171 | medium | none | 15 k to 21 k | 91% | 5.4 |

The fix, measured: `summary` left out of the codex wire's `reasoning`, built from `e11b9f4f` in a scratch clone, three runs alternating with the same commit unchanged, 2026-10-14 12:29 to 12:41, `tofu version` 0.5.14+dev for both.

| build | walls s | median wall s | requests | quality | median small request s | model wait median s |
|---|---|---|---|---|---|---|
| e11b9f4f, summary auto | 125.3, 108.7, 122.1 | 122.1 | 8, 7, 8 | 10, 10, 10 | 7.4 | 89.8 |
| e11b9f4f, summary off | 114.2, 90.0, 103.2 | 103.2 | 7, 6, 7 | 10, 10, 10 | 6.0 | 84.5 |

The spreads overlap (114.2 against 108.7), so three runs say the direction, not the size. What is left: a small request still waits 6.0 s against codex's 5.4 s. Codex also sends `text.verbosity: low`, puts its tools in the input under Responses Lite, and talks to the backend over a websocket; none of the three was measured here.

## What these numbers cannot say

- **Three runs is a spread, not a distribution.** Only differences larger than the spread are called above.
- **Each harness counts tokens its own way.** Claude Code and omp cache almost everything, so their fresh input is 10 to 12 tokens; tofu sends about 1.2 k fresh per run; codex reports input including cache, and the table subtracts it.
- **Tool timings are measured two ways**, stated above, and codex's file reads are shell commands, counted under bash, while its edits are `apply_patch` calls, counted under edit or write.
- **tofu ran against the owner's real home** because its login lives there, so his global settings applied, `projectManagement boardy` among them. The other three ran with their user customizations turned off by flag: Claude Code `--safe-mode`, codex `--ignore-rules`, omp `--no-rules --no-skills --no-extensions` and a `--config` overlay that turns off every foreign config provider and binds omp's `default` and `task` roles to Sonnet 5.5. omp runs 1 and 2 ran before the role binding was added; neither spawned a sub-agent, so the binding could not have changed them.
- **The tofu gate ran in shadow** in every run and its decisions cost $0.000000 in all six, by tofu's own count, so no run spent money on a key.
- **Four earlier GPT runs were discarded**, two of tofu and two of codex, because they ran on `gpt-6-sol`. The pairing was moved to `gpt-5.6-sol`, passed as `-m gpt-5.6-sol -c model="gpt-5.6-sol"` to codex, and every codex rollout in the table reports `gpt-5.6-sol`. The discarded runs pointed the same way: codex 85.6 s and 137.9 s, tofu 218.4 s and 256.2 s with four parallel sub-agents breaking each other's builds in one Go package.
- **Quota spent**: the claude-sub weekly window moved from 93% to 94% and its five-hour window from 1% to 5%; codex-sub's weekly window stayed at 4%.
- **Not measured**: the cost of each extra sub-agent, which is TOFU-1312 in `bench/scale`.

## Conditions

Process tree: a Windows job object holds the whole process tree from its first instruction: CPU is the job's total user plus kernel time, peak tree RSS is the largest sum of every live member's working set sampled every 250 ms, peak commit is the job's PeakJobMemoryUsed.

| arm | tool version | asked for | every model the responses reported | credential | posture | machine | dates |
|---|---|---|---|---|---|---|---|
| tofu-sonnet | version   0.5.13-fix1+dev commit not recorded | claude-sub/claude-sonnet-5-5 | claude-sonnet-5-5 | tofu's own claude-sub login, spend subscription windows 5h and 7d, no money | --gate shadow --no-instructions, real home for the login, scratch project folder | windows/amd64, 32 logical CPUs | 2026-10-13 16:17 to 2026-10-13 16:50 |
| claude-sonnet | 2.1.296 (Claude Code) | claude-sonnet-5-5 | claude-sonnet-5-5 | apiKeySource none | --permission-mode bypassPermissions --safe-mode | windows/amd64, 32 logical CPUs | 2026-10-13 16:19 to 2026-10-13 16:53 |
| omp-sonnet | omp/18.3.2 | anthropic/claude-sonnet-5-5 | claude-sonnet-5-5 | provider anthropic through omp's own login, credential 3 | --auto-approve --no-rules --no-skills --no-extensions, foreign config providers off by --config overlay | windows/amd64, 32 logical CPUs | 2026-10-13 16:21 to 2026-10-13 16:54 |
| tofu-gpt | version   0.5.13-fix1+dev commit not recorded | codex-sub/gpt-5.6-sol | gpt-5.6-sol | tofu's own codex-sub login, spend subscription windows 5h and 7d, no money | --wire codex --gate shadow --no-instructions, real home for the login, scratch project folder | windows/amd64, 32 logical CPUs | 2026-10-13 16:57 to 2026-10-13 17:09 |
| codex-gpt | codex-cli 0.155.1 | gpt-5.6-sol | gpt-5.6-sol | Logged in using ChatGPT | --sandbox workspace-write with the Go cache added, --ignore-rules | windows/amd64, 32 logical CPUs | 2026-10-13 16:54 to 2026-10-13 17:07 |

## Every run

| arm | rep | exit | wall s | CPU s | peak tree RSS MB | peak commit MB | processes | requests | fresh in | cache read | cache write | out | calls | failed | retried | hidden of 5 | rubric of 5 | lines +/- | notes |
|---|---|---|---|---|---|---|---|---|---|---|---|---|---|---|---|---|---|---|---|
| tofu-sonnet | 1 | 0 | 62.4 | 52.7 | 376 | 535 | 426 | 11 | 1194 | 205352 | 26584 | 3874 | 18 | 4 | 4 | 5 | 5 | +41/-6 | turn outcome stopped; 1 sub-agents |
| tofu-sonnet | 2 | 0 | 63.1 | 53.0 | 399 | 541 | 427 | 11 | 1194 | 221172 | 11875 | 3729 | 16 | 4 | 4 | 5 | 4 | +40/-5 | turn outcome stopped; 1 sub-agents |
| tofu-sonnet | 3 | 0 | 66.7 | 53.8 | 393 | 544 | 431 | 11 | 1194 | 224213 | 13686 | 4916 | 19 | 4 | 4 | 5 | 5 | +84/-6 | turn outcome stopped; 1 sub-agents |
| claude-sonnet | 1 | 0 | 50.5 | 57.5 | 616 | 981 | 572 | 5 | 10 | 115929 | 13121 | 4316 | 18 | 0 | 0 | 5 | 5 | +87/-6 |  |
| claude-sonnet | 2 | 0 | 46.8 | 49.8 | 643 | 1017 | 492 | 6 | 12 | 139712 | 11734 | 4315 | 18 | 0 | 0 | 5 | 5 | +86/-6 |  |
| claude-sonnet | 3 | 0 | 55.2 | 59.1 | 590 | 1023 | 576 | 6 | 12 | 139737 | 11783 | 4388 | 18 | 0 | 0 | 5 | 5 | +87/-6 |  |
| omp-sonnet | 1 | 0 | 38.7 | 58.6 | 896 | 1496 | 390 | 4 | 10 | 51369 | 3522 | 1753 | 4 | 0 | 0 | 5 | 5 | +80/-6 |  |
| omp-sonnet | 2 | 0 | 38.9 | 55.5 | 791 | 1396 | 377 | 4 | 10 | 50709 | 5366 | 2362 | 9 | 0 | 0 | 5 | 5 | +83/-6 |  |
| omp-sonnet | 3 | 0 | 35.8 | 52.5 | 872 | 1484 | 310 | 4 | 10 | 51424 | 3549 | 1854 | 4 | 0 | 0 | 5 | 5 | +87/-6 |  |
| tofu-gpt | 1 | 0 | 325.8 | 59.0 | 388 | 535 | 477 | 29 | 91618 | 426368 | 0 | 8401 | 54 | 3 | 3 | 5 | 5 | +160/-8 | turn outcome stopped; 1 sub-agents |
| tofu-gpt | 2 | 0 | 105.2 | 45.2 | 393 | 514 | 326 | 12 | 11111 | 197248 | 0 | 1883 | 21 | 0 | 0 | 0 | 4 | +0/-0 | turn outcome stopped; hidden bug: hidden_bug_test.go:19: Page(5 items, 3, 2) = "", want "e"; hidden feature: .\hidden_feature_test.go:10:16: inv.Remove undefined (type *Inventory has no field or method Remove); hidden rename: .\hidden_rename_test.go:10:14: c.Subtotal undefined (type Cart has no field or method Subtotal); hidden search: hidden_search_test.go:12: open ANSWER.txt: The system cannot find the file specified.; hidden newfile: .\hidden_newfile_test.go:15:12: undefined: ExportCSV |
| tofu-gpt | 3 | 0 | 333.2 | 55.8 | 409 | 545 | 482 | 27 | 83143 | 411136 | 0 | 11170 | 73 | 5 | 5 | 5 | 5 | +148/-7 | turn outcome stopped; 1 sub-agents |
| codex-gpt | 1 | 0 | 138.9 | 75.7 | 543 | 593 | 483 | 7 | 17805 | 107264 | 0 | 3255 | 6 | 0 | 0 | 5 | 5 | +54/-7 |  |
| codex-gpt | 2 | 0 | 110.8 | 67.1 | 539 | 581 | 482 | 7 | 10876 | 114176 | 0 | 4447 | 6 | 0 | 0 | 5 | 5 | +95/-6 |  |
| codex-gpt | 3 | 0 | 108.0 | 62.9 | 565 | 613 | 468 | 6 | 10740 | 94464 | 0 | 4469 | 5 | 0 | 0 | 5 | 5 | +49/-6 |  |

## Turn against turn

Median of the runs, with the lowest and highest in brackets. Total in is fresh plus cache read plus cache write.

| arm | runs | quality of 10 | wall s | CPU s | peak tree RSS MB | requests | total in | fresh in | out | calls | failed | retried | lines changed |
|---|---|---|---|---|---|---|---|---|---|---|---|---|---|
| tofu-sonnet | 3 | 10 [9 to 10] | 63.1 [62.4 to 66.7] | 53.0 [52.7 to 53.8] | 393.0 [375.6 to 398.6] | 11 [11 to 11] | 234241 [233130 to 239093] | 1194 [1194 to 1194] | 3874 [3729 to 4916] | 18 [16 to 19] | 4 [4 to 4] | 4 [4 to 4] | 47 [45 to 90] |
| claude-sonnet | 3 | 10 [10 to 10] | 50.5 [46.8 to 55.2] | 57.5 [49.8 to 59.1] | 615.9 [590.3 to 643.0] | 6 [5 to 6] | 151458 [129060 to 151532] | 12 [10 to 12] | 4316 [4315 to 4388] | 18 [18 to 18] | 0 [0 to 0] | 0 [0 to 0] | 93 [92 to 93] |
| omp-sonnet | 3 | 10 [10 to 10] | 38.7 [35.8 to 38.9] | 55.5 [52.5 to 58.6] | 872.4 [790.9 to 896.4] | 4 [4 to 4] | 54983 [54901 to 56085] | 10 [10 to 10] | 1854 [1753 to 2362] | 4 [4 to 9] | 0 [0 to 0] | 0 [0 to 0] | 89 [86 to 93] |
| tofu-gpt | 3 | 10 [4 to 10] | 325.8 [105.2 to 333.2] | 55.8 [45.2 to 59.0] | 393.2 [387.6 to 409.0] | 27 [12 to 29] | 494279 [208359 to 517986] | 83143 [11111 to 91618] | 8401 [1883 to 11170] | 54 [21 to 73] | 3 [0 to 5] | 3 [0 to 5] | 155 [0 to 168] |
| codex-gpt | 3 | 10 [10 to 10] | 110.8 [108.0 to 138.9] | 67.1 [62.9 to 75.7] | 543.0 [538.9 to 564.9] | 7 [6 to 7] | 125052 [105204 to 125069] | 10876 [10740 to 17805] | 4447 [3255 to 4469] | 6 [5 to 6] | 0 [0 to 0] | 0 [0 to 0] | 61 [55 to 101] |

## Where the wall time goes

One row per run whose transcript times each model request: tofu from `tofu session request`, codex from its rollout file. Model wait is every request from sent to its last byte, split at the first token; for codex the first token is the first finished output item, so its share is an upper bound. Own work is the time between requests that no tool call covers; it goes below zero when a tool starts before its request finishes streaming. Outside is the wall clock before the first request and after the last.

| arm | rep | wall s | requests | model wait s | to first token s | after first token s | tools s | own work s | outside s |
|---|---|---|---|---|---|---|---|---|---|
| tofu-sonnet | 1 | 62.4 | 11 | 34.3 | 22.5 | 11.8 | 21.2 | 0.6 | 6.2 |
| tofu-sonnet | 2 | 63.1 | 11 | 33.7 | 22.5 | 11.2 | 19.7 | 0.8 | 8.8 |
| tofu-sonnet | 3 | 66.7 | 11 | 36.6 | 19.4 | 17.3 | 21.5 | 0.5 | 8.0 |
| tofu-gpt | 1 | 325.8 | 29 | 289.8 | 223.7 | 66.1 | 23.8 | 1.2 | 11.0 |
| tofu-gpt | 2 | 105.2 | 12 | 84.6 | 70.8 | 13.8 | 14.4 | 0.3 | 5.8 |
| tofu-gpt | 3 | 333.2 | 27 | 305.2 | 197.5 | 107.7 | 19.3 | 1.0 | 7.7 |
| codex-gpt | 1 | 138.9 | 7 | 95.0 | 38.3 | 56.7 | 30.5 | 2.7 | 10.6 |
| codex-gpt | 2 | 110.8 | 7 | 72.9 | 36.3 | 36.6 | 25.4 | 2.4 | 10.2 |
| codex-gpt | 3 | 108.0 | 6 | 76.2 | 39.9 | 36.3 | 23.8 | 2.2 | 5.8 |

Every timed request of an arm pooled, fitted as a fixed wait plus a time per output token, least squares.

| arm | requests | fixed s per request | ms per output token |
|---|---|---|---|
| tofu-sonnet | 33 | 1.5 | 4.3 |
| tofu-gpt | 68 | 5.4 | 14.5 |
| codex-gpt | 20 | 2.7 | 15.7 |

## Tool against tool

Every call of the three runs pooled. Calls per run is the median. Time is from the tool's own transcript: tofu reports each call's duration itself, the other three are timed here from the moment the call event reached stdout to the moment its result did. An untimed call is one whose start the transcript did not show.

| arm | kind | calls per run | p50 ms | p95 ms | max ms | failed | untimed |
|---|---|---|---|---|---|---|---|
| tofu-sonnet | edit | 6 | 2 | 7 | 7 | 0 | 0 |
| tofu-sonnet | write | 3 | 2 | 11 | 11 | 0 | 0 |
| tofu-sonnet | search | 1 | 3 | 3 | 3 | 0 | 0 |
| tofu-sonnet | bash | 7 | 2424 | 17582 | 17582 | 12 | 12 |
| tofu-sonnet | spawn | 1 | 35 | 38 | 38 | 0 | 0 |
| claude-sonnet | read | 6 | 34 | 54 | 54 | 0 | 0 |
| claude-sonnet | edit | 6 | 39 | 60 | 60 | 0 | 0 |
| claude-sonnet | write | 3 | 41 | 53 | 53 | 0 | 0 |
| claude-sonnet | search | 2 | 127 | 263 | 263 | 0 | 0 |
| claude-sonnet | bash | 1 | 18879 | 21051 | 21051 | 0 | 0 |
| omp-sonnet | read | 0 | 22 | 24 | 24 | 0 | 0 |
| omp-sonnet | edit | 0 | 83 | 83 | 83 | 0 | 0 |
| omp-sonnet | write | 0 | 8 | 64 | 64 | 0 | 0 |
| omp-sonnet | search | 1 | 38 | 61 | 61 | 0 | 0 |
| omp-sonnet | bash | 3 | 1153 | 17308 | 17308 | 0 | 0 |
| tofu-gpt | read | 13 | 1 | 12 | 13 | 0 | 0 |
| tofu-gpt | edit | 6 | 2 | 7 | 7 | 0 | 0 |
| tofu-gpt | write | 5 | 1 | 3 | 3 | 0 | 0 |
| tofu-gpt | search | 7 | 1 | 14 | 14 | 0 | 0 |
| tofu-gpt | bash | 13 | 533 | 13771 | 14668 | 8 | 7 |
| tofu-gpt | spawn | 1 | 45 | 62 | 62 | 0 | 0 |
| tofu-gpt | other | 9 | 1 | 57 | 59 | 0 | 0 |
| codex-gpt | edit | 1 | 4199 | 4781 | 4781 | 0 | 0 |
| codex-gpt | write | 0 | 478 | 478 | 478 | 0 | 0 |
| codex-gpt | bash | 4 | 2024 | 15535 | 15535 | 0 | 0 |
