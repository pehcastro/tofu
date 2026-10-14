# Where a sub-agent's wall time goes

Machine: windows/amd64 32 logical CPUs. Each row is the bench/scale tiny task driven by `wall.sh`: the lead spawns N sub-agents, each reads one file and answers its first line, and the lead prints them, under `tofu run --no-instructions --no-gate --sift free`. The cassette arm runs in a HOME made fresh for each run on scripted replies, so its model column is the cassette's own pacing and every other column is tofu. The live arm runs claude-sub/claude-sonnet-5-5 on the owner's subscription in his own HOME, because the subscription login lives there; it needs the network and is skipped without it. No run was discarded and nothing was warmed up.

Per sub-agent, from the end of the lead request that asked for the spawn to the end of the lead turn that read its report, the steps add up to read. notice is the spawn tool's own steps as its timing notice reports them; wait is the rest until the sub-agent's run is recorded as begun, which holds a sibling of one definition until the first sibling's first request returns; start is that to its first model request; model is the sum of its request durations; tools the sum of its tool calls; between is the rest of its own run, tofu's work between steps; gather is its end to the lead taking its report, where the 1.5 s quiet and 10 s cap of the report gathering sit; lead is the lead turn that read it. Per run: startup is process start to the lead's first request, ask is the request that spawned, fan is that request's end to the final answer, memory is the memory model's requests after it, exit is the rest of the tail. Every cell is a median in seconds, per sub-agent over every sub-agent of every run, per run over runs; read max is the slowest sub-agent.

## Per sub-agent

| arm | agents | runs | version | model | notice | wait | start | model | tools | between | gather | lead | requests | read | read max | answered |
|---|---|---|---|---|---|---|---|---|---|---|---|---|---|---|---|---|
| cassette | 2 | 3 | 0.5.14+dev 4009ae9f | cassette | 0.01 | 0.10 | 0.00 | 0.08 | 0.00 | 0.01 | 0.01 | 0.08 | 2 | 0.31 | 0.32 | 6/6 |
| cassette | 4 | 3 | 0.5.14+dev 4009ae9f | cassette | 0.01 | 0.10 | 0.00 | 0.09 | 0.00 | 0.01 | 0.02 | 0.07 | 2 | 0.31 | 0.32 | 12/12 |
| cassette | 10 | 3 | 0.5.14+dev 4009ae9f | cassette | 0.02 | 0.11 | 0.00 | 0.09 | 0.00 | 0.02 | 0.03 | 0.08 | 2 | 0.36 | 0.42 | 30/30 |
| live | 2 | 2 | 0.5.14+dev 4009ae9f | claude-sonnet-5-5 | 0.03 | 1.71 | 0.00 | 3.15 | 0.00 | 0.01 | 0.25 | 1.73 | 2 | 5.44 | 5.44 | 4/4 |
| live | 4 | 3 | 0.5.14+dev 4009ae9f | claude-sonnet-5-5 | 0.03 | 1.41 | 0.00 | 2.39 | 0.00 | 0.01 | 0.68 | 1.67 | 2 | 6.47 | 7.37 | 12/12 |
| live | 10 | 3 | 0.5.14+dev 4009ae9f | claude-sonnet-5-5 | 0.04 | 1.68 | 0.01 | 2.52 | 0.00 | 0.00 | 1.59 | 2.29 | 2 | 7.01 | 9.19 | 30/30 |
| live | 4 | 3 | 0.5.14 75d2b58b | claude-sonnet-5-5 | 0.04 | 1.82 | 0.01 | 3.23 | 0.00 | 0.00 | 0.85 | 2.05 | 2 | 7.37 | 7.78 | 12/12 |
| live | 10 | 3 | 0.5.14 75d2b58b | claude-sonnet-5-5 | 0.05 | 1.79 | 0.01 | 3.07 | 0.00 | 0.01 | 1.91 | 2.65 | 2 | 9.68 | 14.87 | 30/30 |

## Per run

| arm | agents | commit | startup | ask | fan | memory | exit | wall | lead turns |
|---|---|---|---|---|---|---|---|---|---|
| cassette | 2 | 4009ae9f | 0.20 | 0.04 | 0.31 | 0.04 | 0.10 | 0.69 | 2.00 |
| cassette | 4 | 4009ae9f | 0.19 | 0.04 | 0.31 | 0.04 | 0.09 | 0.68 | 2.00 |
| cassette | 10 | 4009ae9f | 0.21 | 0.04 | 0.36 | 0.04 | 0.11 | 0.77 | 2.00 |
| live | 2 | 4009ae9f | 3.73 | 3.10 | 5.44 | 3.61 | 0.16 | 15.77 | 2.00 |
| live | 4 | 4009ae9f | 0.29 | 3.81 | 6.47 | 5.75 | 0.11 | 17.40 | 2.00 |
| live | 10 | 4009ae9f | 0.34 | 9.11 | 7.01 | 3.42 | 0.11 | 20.94 | 2.00 |
| live | 4 | 75d2b58b | 0.42 | 3.98 | 7.37 | 4.54 | 0.09 | 15.71 | 2.00 |
| live | 10 | 75d2b58b | 0.39 | 7.73 | 12.40 | 6.39 | 0.08 | 23.03 | 2.00 |

## Share of read, by step

| arm | agents | commit | notice | wait | start | model | tools | between | gather | lead |
|---|---|---|---|---|---|---|---|---|---|---|
| cassette | 2 | 4009ae9f | 2% | 34% | 1% | 28% | 0% | 3% | 5% | 27% |
| cassette | 4 | 4009ae9f | 3% | 33% | 1% | 28% | 0% | 4% | 6% | 24% |
| cassette | 10 | 4009ae9f | 6% | 31% | 1% | 24% | 0% | 6% | 9% | 23% |
| live | 2 | 4009ae9f | 0% | 17% | 0% | 50% | 0% | 0% | 3% | 30% |
| live | 4 | 4009ae9f | 0% | 18% | 0% | 38% | 0% | 0% | 14% | 29% |
| live | 10 | 4009ae9f | 1% | 19% | 0% | 30% | 0% | 0% | 20% | 30% |
| live | 4 | 75d2b58b | 0% | 19% | 0% | 42% | 0% | 0% | 12% | 27% |
| live | 10 | 75d2b58b | 1% | 15% | 0% | 30% | 0% | 0% | 18% | 36% |

## The spawn notice's own steps, median ms

| arm | agents | commit | account pick | brief | earlier siblings | hook | memory | prompt | scratch | ticket grant | total |
|---|---|---|---|---|---|---|---|---|---|---|---|
| cassette | 2 | 4009ae9f | 1 | 1 | 0 | 1 | 0 | 1 | 2 | 0 | 8 |
| cassette | 4 | 4009ae9f | 1 | 1 | 4 | 0 | 0 | 2 | 2 | 0 | 11 |
| cassette | 10 | 4009ae9f | 1 | 1 | 15 | 0 | 0 | 2 | 3 | 0 | 23 |
| live | 2 | 4009ae9f | 14 | 8 | 0 | 1 | 0 | 1 | 3 | 0 | 28 |
| live | 4 | 4009ae9f | 10 | 2 | 4 | 1 | 0 | 1 | 3 | 0 | 28 |
| live | 10 | 4009ae9f | 12 | 1 | 16 | 1 | 0 | 1 | 3 | 0 | 38 |
| live | 4 | 75d2b58b | 13 | 12 | 2 | 1 | 0 | 1 | 6 | 0 | 36 |
| live | 10 | 75d2b58b | 8 | 11 | 25 | 1 | 0 | 1 | 3 | 0 | 48 |

## How these runs were made

- Build: `go build ./cmd/tofu` from the working tree at 4009ae9f with uncommitted changes in `internal/turn`, `internal/host` and `cmd/tofu` (TOFU-1325's spent subscription work), so `tofu version` says `0.5.14+dev`, commit `4009ae9f-dirty`. Date 2026-10-14.
- Credential: the cassette arm has none; the live arm is the claude-sub subscription. The two are not compared cell by cell: the cassette arm is tofu's own overhead, the live arm is where the time goes.
- The live rows were taken before `wall.sh` gave each run its own project, so all eight live runs shared one project directory, and the episode memory of every earlier run sat in the next run's prompt and in its memory compaction. The merges the memory model ran after a turn grew from one to four across the runs for that reason. `wall.sh` now makes a fresh project per run.
- Live, 2 agents: the first run was a probe made before the startup, ask and memory columns existed; its row was rebuilt from its session with `scale retrace` and is counted as run 1. A second run answered 0 of 2 without spawning: its lead read the probe's markers from the episode memory and printed them, so it has no sub-agent and no row, and it counts against the three run cap. A median of two runs is the larger of the two, so the live 2 per run row is the probe's: the other run took 9.98 s, with 0.30 s startup, 2.75 s ask and 1.39 s memory.
- Rows at 75d2b58b: `go build ./cmd/tofu` in a clean clone of HEAD, so `tofu version` says `0.5.14`, commit `75d2b58b`. Live only, 4 and 10 agents, three runs each, each in a fresh project, taken live by `wall.sh` rather than rebuilt. Date 2026-10-14. Per-run walls: 4 agents 16.89, 13.70, 15.71 s against 17.40, 14.45, 17.44 s at 4009ae9f; 10 agents 22.61, 28.65, 23.03 s against 29.90, 18.01, 20.94 s. The medians moved by less than the spread of either build's three runs, so neither direction is a finding. Live 10 run 2 had a 7.2 s report turn that read nine reports at once, which is most of its 28.65 s.
- Live, 4 and 10 agents: three runs each, rebuilt from their sessions with `scale retrace` after the memory column was narrowed to the requests that start after the final answer.

## What the numbers say

- The memory model sits on the critical path. `answered` in `internal/turn/loop.go` runs the episode compaction (`internal/turn/tools/zoom.go` `Compact`, one compress and then one merge per level, each a serial haiku request from `cmd/tofu/memory_tree.go`) before a lead turn returns. After the final answer it is 1.39 to 6.92 s of every live run's wall, median 3.42 to 5.75 s. After the spawning turn it ran four serial merges, 6.8 s, while the reports waited: in live 10 run 3 the first report waited 4.1 s and the last 2.4 s, and the report turn began 56 ms after the last merge ended.
- The warm-cache stagger holds every sibling of one definition until the first sibling's first request returns (`warmup.stagger` in `internal/turn/spawn_parallel.go`, capped at `konst.SubAgentWarmMillis`). That is the wait column, 1.41 to 1.71 s median, and the slowest sub-agent is always a staggered one, so it is on the critical path. In the three runs checked the leader's first request read 7975 cached tokens and wrote none: the cache was already warm from the previous run and the stagger bought nothing. The leader's first token came 0.57 to 0.58 s before its request ended, which is what releasing the siblings at the first token rather than at the end would save when the cache is cold.
- The gather column is mostly waiting for staggered siblings, not the gather window: the last report of a run is taken 12 to 18 ms after it is posted, except where a memory compaction or an extra lead turn held it. The window cost an extra lead turn once in eight runs (live 4 run 1: the first report's 1.5 s of quiet ran out before the staggered three finished, a 1.3 s lead turn ran on one report, and the other three waited 0.7 to 0.9 s for it).
- tofu's own work per sub-agent is under 0.2 s: on the cassette the spawn tool's notice is 0.01 to 0.03 s, the dispatch 0.09 to 0.11 s and the work between steps 0.01 s.
- The spawning request is the largest single cell at 10 agents, 7.1 to 16.6 s for about 1,500 output tokens, of which the ten spawn calls' arguments are a few hundred; it is model time and varies by more than 2x between runs.
- The probe paid 3.73 s from process start to its first request; every later live run paid 0.25 to 0.38 s. Not attributed: a subscription token refresh or a cold model catalogue are the candidates, and neither is in the session trace.

## Why the live 10 fan rose from 7.01 to 12.40 s

Model and run variance, not tofu. Split by `scale fan` from the six recorded sessions, read by the 75d2b58b binary on 2026-10-14, no new live run. The split follows the slowest report back from the final answer: the stagger and the sub-agent's own requests are model wait, the lead was either busy on an earlier report turn or running the final one, and what is left between its end and the lead taking it is the memory model or the gather window. tofu own is the spawn notice, dispatch, tools and the work between steps. Each row adds up to fan. First request is every sub-agent's first request in the run.

| commit | run | fan | slowest report | model wait: stagger | model wait: its requests | lead: busy on an earlier report turn | lead: final report turn | memory model | gather window | tofu own | report turns | first request median | first request max |
|---|---|---|---|---|---|---|---|---|---|---|---|---|---|
| 4009ae9f | 1 | 6.92 | research-6 | 1.52 | 3.05 | 0.00 | 2.25 | 0.00 | 0.02 | 0.09 | 1 | 1.53 | 1.81 |
| 4009ae9f | 2 | 7.01 | research-4 | 1.68 | 2.86 | 0.00 | 2.40 | 0.00 | 0.01 | 0.06 | 1 | 1.64 | 1.74 |
| 4009ae9f | 3 | 9.19 | research-8 | 1.73 | 2.70 | 0.00 | 2.29 | 2.38 | 0.06 | 0.03 | 1 | 1.57 | 2.51 |
| 75d2b58b | 1 | 12.40 | research-4 | 1.55 | 5.39 | 2.67 | 2.65 | 0.07 | 0.00 | 0.07 | 3 | 1.82 | 3.91 |
| 75d2b58b | 2 | 14.87 | research-7 | 1.80 | 9.85 | 0.00 | 3.17 | 0.00 | 0.01 | 0.06 | 1 | 1.97 | 8.66 |
| 75d2b58b | 3 | 8.38 | research-3 | 1.86 | 3.93 | 0.00 | 2.51 | 0.00 | 0.02 | 0.06 | 1 | 1.85 | 2.59 |

- Two sub-agent requests make the difference. 75d2b58b run 2: research-7's first request took 8.66 s for 607 tokens in, 50 out, 7975 cache read, one attempt, status 200, while its nine siblings sent the same request in 1.52 to 2.96 s. 75d2b58b run 1: research-4's first request took 3.91 s on the same payload against 1.25 to 2.03 s for its siblings, so it ended at 7.01 s, 0.10 s after the second report turn took four reports at 6.91 s, and waited 2.67 s for that turn before the third one read it.
- tofu's own work on the path is 0.03 to 0.09 s before and 0.06 to 0.07 s after. The stagger is 1.52 to 1.73 s before and 1.55 to 1.86 s after. The final report turn is 2.25 to 2.40 s before and 2.51 to 3.17 s after, model time for 204 to 270 output tokens.
- Between the two builds nothing on this path changed: `internal/llm/wire/anthropic` and `internal/turn/spawn_parallel.go` have no diff, `konst.ReportGatherQuietMillis` is 1500 and `konst.SubAgentWarmMillis` 8000 in both. 58da3db3 moved episode compaction off the turn, which removed the 2.38 s memory cell of 4009ae9f run 3 and cannot add time here. 72016ba4 added about 30 input tokens to each sub-agent's first request (577 to 580 before, 604 to 610 after), with cache reads unchanged.
- The run 3 of 75d2b58b, with no slow request, is 8.38 s, inside the 6.92 to 9.19 s of 4009ae9f.
- Open: the first request median moved 1.53 to 1.64 s before against 1.82 to 1.97 s after, about 0.3 s. Three runs a side cannot separate the 30 extra input tokens from the hour of day on the subscription.
