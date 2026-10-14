# What each extra sub-agent costs in memory and CPU

Machine: windows/amd64 32 logical CPUs. Sampled every 250ms. A job object holds the whole tree from its first instruction: RSS is the sum of every live member's working set, CPU is the job's user plus kernel time including members that already exited, commit is the job's own peak. Peak and steady RSS are the maximum and the median of the samples, and a page shared by two processes counts in both working sets. No run was discarded and nothing was warmed up. The live arms need the network and each tool's subscription login; the cassette and known arms need neither.

Each arm is one run per rung, driven by `run.sh` in a scratch project outside any repository, the task being one file read per sub-agent and its first line repeated back. tofu: `tofu run --no-instructions --no-gate --sift free`, so no Jev call is made; on the cassette the replies are scripted, so answered there only says every sub-agent turn ran. Claude Code: `claude -p --setting-sources project --strict-mcp-config`, so none of the owner's hooks or MCP servers start. omp: `omp -p --no-extensions --no-skills --no-rules --no-session` with `omp-overlay.yml` binding the task role to Sonnet; its own MCP config still loads. codex: `codex exec --sandbox read-only` on the owner's config with the model forced to gpt-5.6-sol, because its default is Astra; it runs a different model from the other three, so its numbers compare harness against harness only loosely. Cores is CPU seconds over wall seconds; a live run waits on the network most of the time, so the 2 vCPU column says how many such runs side by side would fill two cores.

## Summary

| tool | arm | model | peak RSS base MB | peak RSS per agent MB | commit base MB | commit per agent MB | CPU base s | CPU per agent s | agents in 2 GB by peak RSS | agents in 2 vCPU |
|---|---|---|---|---|---|---|---|---|---|---|
| tofu | cassette | cassette | 29 | 0.5 | 92 | 0.6 | 0.53 | 0.05 | over 100, a straight line past 10 measured | 44 |
| tofu | live | claude-sonnet-5-5 | 37 | 0.7 | 95 | 0.8 | 0.81 | 0.06 | over 100, a straight line past 10 measured | no limit found, the fitted cost per agent is not positive |
| claude | live | claude-sonnet-5-5 | 281 | 4.3 | 380 | 1.7 | 2.10 | 0.30 | over 100, a straight line past 10 measured | no limit found, the fitted cost per agent is not positive |
| omp | live | claude-sonnet-5-5 | 699 | 20.9 | 1321 | 36.5 | 5.57 | 0.62 | 64 | over 100, a straight line past 10 measured |
| codex | live | gpt-5.6-sol | 215 | 22.0 | 169 | 19.7 | 3.12 | 3.44 | 83 | 55 |

## tofu, cassette arm, none credential

| agents | answered | wall s | peak RSS MB | steady RSS MB | peak commit MB | CPU s | cores | processes | exit | model | wire | version | date |
|---|---|---|---|---|---|---|---|---|---|---|---|---|---|
| 0 | 1/1 | 0.6 | 29 | 26 | 92 | 0.44 | 0.77 | 2 | 0 | cassette | cassette | 0.5.14 75d2b58b | 2026-10-14 |
| 1 | 1/1 | 0.7 | 31 | 25 | 92 | 0.66 | 0.94 | 2 | 0 | cassette | cassette | 0.5.14 75d2b58b | 2026-10-14 |
| 2 | 2/2 | 0.7 | 30 | 27 | 94 | 0.59 | 0.83 | 2 | 0 | cassette | cassette | 0.5.14 75d2b58b | 2026-10-14 |
| 4 | 4/4 | 0.8 | 30 | 28 | 95 | 0.86 | 1.12 | 2 | 0 | cassette | cassette | 0.5.14 75d2b58b | 2026-10-14 |
| 6 | 6/6 | 1.0 | 31 | 31 | 96 | 0.72 | 0.73 | 2 | 0 | cassette | cassette | 0.5.14 75d2b58b | 2026-10-14 |
| 8 | 8/8 | 0.8 | 38 | 38 | 97 | 0.89 | 1.09 | 2 | 0 | cassette | cassette | 0.5.14 75d2b58b | 2026-10-14 |
| 10 | 10/10 | 0.9 | 31 | 28 | 98 | 1.02 | 1.13 | 1 | 0 | cassette | cassette | 0.5.14 75d2b58b | 2026-10-14 |

- peak RSS: base 29 MB, 0.5 MB per agent; agents that fit in 2 GB: over 100, a straight line past 10 measured
- steady RSS: base 26 MB, 0.7 MB per agent
- peak commit: base 92 MB, 0.6 MB per agent
- CPU: base 0.53 s, 0.05 s per agent
- cores while running: base 0.829, 0.026 per agent; agents that fit in 2 vCPU: 44

## tofu, live arm, subscription credential

| agents | answered | wall s | peak RSS MB | steady RSS MB | peak commit MB | CPU s | cores | processes | exit | model | wire | version | date |
|---|---|---|---|---|---|---|---|---|---|---|---|---|---|
| 0 | 1/1 | 4.3 | 35 | 33 | 94 | 0.75 | 0.17 | 2 | 0 | claude-sonnet-5-5 | anthropic | 0.5.14 75d2b58b | 2026-10-14 |
| 1 | 1/1 | 7.1 | 40 | 34 | 96 | 0.83 | 0.12 | 2 | 0 | claude-sonnet-5-5 | anthropic | 0.5.14 75d2b58b | 2026-10-14 |
| 2 | 2/2 | 16.9 | 42 | 35 | 96 | 0.95 | 0.06 | 2 | 0 | claude-sonnet-5-5 | anthropic | 0.5.14 75d2b58b | 2026-10-14 |
| 4 | 4/4 | 19.4 | 38 | 36 | 98 | 1.05 | 0.05 | 2 | 0 | claude-sonnet-5-5 | anthropic | 0.5.14 75d2b58b | 2026-10-14 |
| 6 | 6/6 | 21.5 | 42 | 38 | 99 | 1.38 | 0.06 | 2 | 0 | claude-sonnet-5-5 | anthropic | 0.5.14 75d2b58b | 2026-10-14 |
| 8 | 8/8 | 20.1 | 40 | 39 | 100 | 1.09 | 0.05 | 1 | 0 | claude-sonnet-5-5 | anthropic | 0.5.14 75d2b58b | 2026-10-14 |
| 10 | 10/10 | 20.3 | 47 | 41 | 103 | 1.38 | 0.07 | 2 | 0 | claude-sonnet-5-5 | anthropic | 0.5.14 75d2b58b | 2026-10-14 |

- peak RSS: base 37 MB, 0.7 MB per agent; agents that fit in 2 GB: over 100, a straight line past 10 measured
- steady RSS: base 33 MB, 0.7 MB per agent
- peak commit: base 95 MB, 0.8 MB per agent
- CPU: base 0.81 s, 0.06 s per agent
- cores while running: base 0.118, -0.008 per agent; agents that fit in 2 vCPU: no limit found, the fitted cost per agent is not positive

## claude, live arm, subscription credential

| agents | answered | wall s | peak RSS MB | steady RSS MB | peak commit MB | CPU s | cores | processes | exit | model | wire | version | date |
|---|---|---|---|---|---|---|---|---|---|---|---|---|---|
| 0 | 1/1 | 6.5 | 227 | 219 | 359 | 1.91 | 0.30 | 3 | 0 | claude-sonnet-5-5 | anthropic | not recorded | 2026-10-13 |
| 1 | 1/1 | 9.7 | 282 | 223 | 396 | 2.28 | 0.23 | 3 | 0 | claude-sonnet-5-5 | anthropic | not recorded | 2026-10-13 |
| 2 | 2/2 | 10.3 | 269 | 223 | 386 | 2.78 | 0.27 | 3 | 0 | claude-sonnet-5-5 | anthropic | not recorded | 2026-10-13 |
| 4 | 4/4 | 13.3 | 413 | 242 | 401 | 3.47 | 0.26 | 4 | 0 | claude-sonnet-5-5 | anthropic | not recorded | 2026-10-13 |
| 6 | 6/6 | 15.1 | 333 | 253 | 387 | 4.28 | 0.28 | 3 | 0 | claude-sonnet-5-5 | anthropic | not recorded | 2026-10-13 |
| 8 | 8/8 | 17.9 | 282 | 261 | 388 | 4.53 | 0.25 | 3 | 0 | claude-haiku-5-5+claude-sonnet-5-5 | anthropic | not recorded | 2026-10-13 |
| 10 | 10/10 | 20.3 | 292 | 259 | 395 | 4.81 | 0.24 | 3 | 0 | claude-sonnet-5-5 | anthropic | not recorded | 2026-10-13 |

- peak RSS: base 281 MB, 4.3 MB per agent; agents that fit in 2 GB: over 100, a straight line past 10 measured
- steady RSS: base 219 MB, 4.7 MB per agent
- peak commit: base 380 MB, 1.7 MB per agent
- CPU: base 2.10 s, 0.30 s per agent
- cores while running: base 0.273, -0.002 per agent; agents that fit in 2 vCPU: no limit found, the fitted cost per agent is not positive

## omp, live arm, subscription credential

| agents | answered | wall s | peak RSS MB | steady RSS MB | peak commit MB | CPU s | cores | processes | exit | model | wire | version | date |
|---|---|---|---|---|---|---|---|---|---|---|---|---|---|
| 0 | 1/1 | 7.3 | 543 | 474 | 998 | 4.23 | 0.58 | 2 | 0 | claude-sonnet-5-5 | anthropic | not recorded | 2026-10-13 |
| 1 | 1/1 | 13.1 | 800 | 488 | 1515 | 6.41 | 0.49 | 4 | 0 | claude-sonnet-5-5 | anthropic | not recorded | 2026-10-13 |
| 2 | 2/2 | 15.4 | 814 | 505 | 1510 | 7.80 | 0.51 | 4 | 0 | claude-sonnet-5-5 | anthropic | not recorded | 2026-10-13 |
| 4 | 4/4 | 15.5 | 826 | 512 | 1610 | 8.17 | 0.53 | 4 | 0 | claude-sonnet-5-5 | anthropic | not recorded | 2026-10-13 |
| 6 | 6/6 | 18.5 | 828 | 524 | 1565 | 9.94 | 0.54 | 4 | 0 | claude-sonnet-5-5 | anthropic | not recorded | 2026-10-13 |
| 8 | 8/8 | 15.3 | 852 | 522 | 1570 | 10.64 | 0.70 | 4 | 0 | claude-sonnet-5-5 | anthropic | not recorded | 2026-10-13 |
| 10 | 10/10 | 19.3 | 876 | 527 | 1609 | 11.00 | 0.57 | 4 | 0 | claude-sonnet-5-5 | anthropic | not recorded | 2026-10-13 |

- peak RSS: base 699 MB, 20.9 MB per agent; agents that fit in 2 GB: 64
- steady RSS: base 486 MB, 4.8 MB per agent
- peak commit: base 1321 MB, 36.5 MB per agent
- CPU: base 5.57 s, 0.62 s per agent
- cores while running: base 0.516, 0.010 per agent; agents that fit in 2 vCPU: over 100, a straight line past 10 measured

## codex, live arm, subscription credential

| agents | answered | wall s | peak RSS MB | steady RSS MB | peak commit MB | CPU s | cores | processes | exit | model | wire | version | date |
|---|---|---|---|---|---|---|---|---|---|---|---|---|---|
| 0 | 1/1 | 17.1 | 213 | 100 | 149 | 4.88 | 0.29 | 3 | 0 | gpt-5.6-sol | codex | not recorded | 2026-10-13 |
| 1 | 1/1 | 24.2 | 237 | 131 | 179 | 4.89 | 0.20 | 3 | 0 | gpt-5.6-sol | codex | not recorded | 2026-10-13 |
| 2 | 2/2 | 39.1 | 258 | 175 | 223 | 8.61 | 0.22 | 3 | 0 | gpt-5.6-sol | codex | not recorded | 2026-10-13 |
| 4 | 4/4 | 44.1 | 331 | 167 | 305 | 17.62 | 0.40 | 4 | 0 | gpt-5.6-sol | codex | not recorded | 2026-10-13 |
| 6 | 6/6 | 58.0 | 307 | 189 | 243 | 24.88 | 0.43 | 3 | 0 | gpt-5.6-sol | codex | not recorded | 2026-10-13 |
| 8 | 8/8 | 69.6 | 400 | 226 | 330 | 30.39 | 0.44 | 4 | 0 | gpt-5.6-sol | codex | not recorded | 2026-10-13 |
| 10 | 10/10 | 66.7 | 440 | 267 | 366 | 37.16 | 0.56 | 4 | 0 | gpt-5.6-sol | codex | not recorded | 2026-10-13 |

- peak RSS: base 215 MB, 22.0 MB per agent; agents that fit in 2 GB: 83
- steady RSS: base 115 MB, 14.4 MB per agent
- peak commit: base 169 MB, 19.7 MB per agent
- CPU: base 3.12 s, 3.44 s per agent
- cores while running: base 0.220, 0.032 per agent; agents that fit in 2 vCPU: 55

## The sampler against a known child

Each row is this binary's own `alloc` verb, which allocates and touches a fixed size, spins one core for a fixed time, starts `kids` children doing the same, and holds for 2 s. Expected is above the matching zero-size row.

| kids | expected MB | peak RSS MB | above zero row MB | error MB | expected CPU s | CPU s | above zero row s | processes | date |
|---|---|---|---|---|---|---|---|---|---|
| 0 | 0 | 5.9 | 0.0 | +0.0 | 0.00 | 0.00 | 0.00 | 1 | 2026-10-14 |
| 0 | 256 | 263.2 | 257.3 | +1.3 | 1.00 | 1.11 | 1.11 | 1 | 2026-10-14 |
| 2 | 0 | 19.3 | 0.0 | +0.0 | 0.00 | 0.02 | 0.00 | 3 | 2026-10-14 |
| 2 | 384 | 405.9 | 386.6 | +2.6 | 1.50 | 1.84 | 1.83 | 3 | 2026-10-14 |

