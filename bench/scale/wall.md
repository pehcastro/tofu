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

## Per run

| arm | agents | startup | ask | fan | memory | exit | wall | lead turns |
|---|---|---|---|---|---|---|---|---|
| cassette | 2 | 0.20 | 0.04 | 0.31 | 0.04 | 0.10 | 0.69 | 2.00 |
| cassette | 4 | 0.19 | 0.04 | 0.31 | 0.04 | 0.09 | 0.68 | 2.00 |
| cassette | 10 | 0.21 | 0.04 | 0.36 | 0.04 | 0.11 | 0.77 | 2.00 |
| live | 2 | 3.73 | 3.10 | 5.44 | 3.61 | 0.16 | 15.77 | 2.00 |
| live | 4 | 0.29 | 3.81 | 6.47 | 5.75 | 0.11 | 17.40 | 2.00 |
| live | 10 | 0.34 | 9.11 | 7.01 | 3.42 | 0.11 | 20.94 | 2.00 |

## Share of read, by step

| arm | agents | notice | wait | start | model | tools | between | gather | lead |
|---|---|---|---|---|---|---|---|---|---|
| cassette | 2 | 2% | 34% | 1% | 28% | 0% | 3% | 5% | 27% |
| cassette | 4 | 3% | 33% | 1% | 28% | 0% | 4% | 6% | 24% |
| cassette | 10 | 6% | 31% | 1% | 24% | 0% | 6% | 9% | 23% |
| live | 2 | 0% | 17% | 0% | 50% | 0% | 0% | 3% | 30% |
| live | 4 | 0% | 18% | 0% | 38% | 0% | 0% | 14% | 29% |
| live | 10 | 1% | 19% | 0% | 30% | 0% | 0% | 20% | 30% |

## The spawn notice's own steps, median ms

| arm | agents | account pick | brief | earlier siblings | hook | memory | prompt | scratch | ticket grant | total |
|---|---|---|---|---|---|---|---|---|---|---|
| cassette | 2 | 1 | 1 | 0 | 1 | 0 | 1 | 2 | 0 | 8 |
| cassette | 4 | 1 | 1 | 4 | 0 | 0 | 2 | 2 | 0 | 11 |
| cassette | 10 | 1 | 1 | 15 | 0 | 0 | 2 | 3 | 0 | 23 |
| live | 2 | 14 | 8 | 0 | 1 | 0 | 1 | 3 | 0 | 28 |
| live | 4 | 10 | 2 | 4 | 1 | 0 | 1 | 3 | 0 | 28 |
| live | 10 | 12 | 1 | 16 | 1 | 0 | 1 | 3 | 0 | 38 |

## How these runs were made

- Build: `go build ./cmd/tofu` from the working tree at 4009ae9f with uncommitted changes in `internal/turn`, `internal/host` and `cmd/tofu` (TOFU-1325's spent subscription work), so `tofu version` says `0.5.14+dev`, commit `4009ae9f-dirty`. Date 2026-10-14.
- Credential: the cassette arm has none; the live arm is the claude-sub subscription. The two are not compared cell by cell: the cassette arm is tofu's own overhead, the live arm is where the time goes.
- The live rows were taken before `wall.sh` gave each run its own project, so all eight live runs shared one project directory, and the episode memory of every earlier run sat in the next run's prompt and in its memory compaction. The merges the memory model ran after a turn grew from one to four across the runs for that reason. `wall.sh` now makes a fresh project per run.
- Live, 2 agents: the first run was a probe made before the startup, ask and memory columns existed; its row was rebuilt from its session with `scale retrace` and is counted as run 1. A second run answered 0 of 2 without spawning: its lead read the probe's markers from the episode memory and printed them, so it has no sub-agent and no row, and it counts against the three run cap. A median of two runs is the larger of the two, so the live 2 per run row is the probe's: the other run took 9.98 s, with 0.30 s startup, 2.75 s ask and 1.39 s memory.
- Live, 4 and 10 agents: three runs each, rebuilt from their sessions with `scale retrace` after the memory column was narrowed to the requests that start after the final answer.

## What the numbers say

- The memory model sits on the critical path. `answered` in `internal/turn/loop.go` runs the episode compaction (`internal/turn/tools/zoom.go` `Compact`, one compress and then one merge per level, each a serial haiku request from `cmd/tofu/memory_tree.go`) before a lead turn returns. After the final answer it is 1.39 to 6.92 s of every live run's wall, median 3.42 to 5.75 s. After the spawning turn it ran four serial merges, 6.8 s, while the reports waited: in live 10 run 3 the first report waited 4.1 s and the last 2.4 s, and the report turn began 56 ms after the last merge ended.
- The warm-cache stagger holds every sibling of one definition until the first sibling's first request returns (`warmup.stagger` in `internal/turn/spawn_parallel.go`, capped at `konst.SubAgentWarmMillis`). That is the wait column, 1.41 to 1.71 s median, and the slowest sub-agent is always a staggered one, so it is on the critical path. In the three runs checked the leader's first request read 7975 cached tokens and wrote none: the cache was already warm from the previous run and the stagger bought nothing. The leader's first token came 0.57 to 0.58 s before its request ended, which is what releasing the siblings at the first token rather than at the end would save when the cache is cold.
- The gather column is mostly waiting for staggered siblings, not the gather window: the last report of a run is taken 12 to 18 ms after it is posted, except where a memory compaction or an extra lead turn held it. The window cost an extra lead turn once in eight runs (live 4 run 1: the first report's 1.5 s of quiet ran out before the staggered three finished, a 1.3 s lead turn ran on one report, and the other three waited 0.7 to 0.9 s for it).
- tofu's own work per sub-agent is under 0.2 s: on the cassette the spawn tool's notice is 0.01 to 0.03 s, the dispatch 0.09 to 0.11 s and the work between steps 0.01 s.
- The spawning request is the largest single cell at 10 agents, 7.1 to 16.6 s for about 1,500 output tokens, of which the ten spawn calls' arguments are a few hundred; it is model time and varies by more than 2x between runs.
- The probe paid 3.73 s from process start to its first request; every later live run paid 0.25 to 0.38 s. Not attributed: a subscription token refresh or a cold model catalogue are the candidates, and neither is in the session trace.
