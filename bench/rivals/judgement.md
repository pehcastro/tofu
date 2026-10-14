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
