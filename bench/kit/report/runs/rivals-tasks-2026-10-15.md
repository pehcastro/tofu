# tofu against omp on sub-agents, a long edit and a large read, 2026-10-15

TOFU-1400 ran tofu-sonnet three times per task on the installed tofu 0.5.15-rc1 (commit b440a772) at omp's thinking setting, rows in bench/rivals/runs/0.5.15-rc1. omp is 18.3.2, unchanged, so its rows are TOFU-1370's from bench/rivals/runs, not rerun. Peak tree MiB is bytes over 2^20, the unit of the omp rows.

TOFU-1403 reparsed the tofu rows the same day from their recorded stdout and tofu's session records. Conversation requests count the model calls of the conversation and its sub-agents only; tofu's memory model calls and omp's auto-thinking judge calls are their own column. The omp rows of TOFU-1370 have no work folder left to reparse, so their judge calls, already recorded apart in each row, were subtracted from requests. Each section is printed by `go run ./bench/rivals parity -batch 0.5.15-rc1 -task <task> -rivals . -unit MiB`.

Median of 3 runs, lowest and highest in brackets.

## subagents

## subagents

| arm | version | runs | quality of 6 | wall s | conversation requests | memory or judge calls | total in | out | peak tree MiB | CPU s | failed calls |
|---|---|---|---|---|---|---|---|---|---|---|---|
| tofu-sonnet at omp's setting | tofu 0.5.15-rc1 | 3 | 6 [6 to 6] | 61.4 [58.6 to 66.4] | 28 [27 to 32] | 12 [11 to 12] | 365047 [339196 to 402908] | 6749 [6685 to 6836] | 360.5 [340.5 to 366.2] | 34.1 [34.1 to 34.9] | 0 [0 to 1] |
| omp | omp omp/18.3.2 | 3 | 6 [6 to 6] | 82.9 [80.3 to 124.6] | 31 [30 to 46] | 6 [0 to 7] | 455504 [324613 to 700756] | 14501 [7325 to 21323] | 1080 [1046.2 to 1154.8] | 53.3 [52 to 57.5] | 4 [3 to 9] |

| run | tool and version | date | thinking sent | quality of 6 | wall s | conversation requests | memory or judge calls | total in | out | peak tree MiB | CPU s | failed calls |
|---|---|---|---|---|---|---|---|---|---|---|---|---|
| subagents-omp-sonnet-1 | omp omp/18.3.2 | 2026-10-14 17:20 | not read | 6 | 80.3 | 30 | 0 | 324613 | 7325 | 1154.8 | 53.3 | 4 |
| subagents-omp-sonnet-2 | omp omp/18.3.2 | 2026-10-14 17:31 | not read | 6 | 124.6 | 46 | 7 | 700756 | 21323 | 1046.2 | 57.5 | 9 |
| subagents-omp-sonnet-3 | omp omp/18.3.2 | 2026-10-14 17:46 | not read | 6 | 82.9 | 31 | 6 | 455504 | 14501 | 1080 | 52 | 3 |
| subagents-tofu-sonnet-summarized-1 | tofu 0.5.15-rc1 | 2026-10-15 03:29 | thinking {"display":"summarized","type":"adaptive"} | 6 | 61.4 | 32 | 11 | 402908 | 6836 | 360.5 | 34.9 | 0 |
| subagents-tofu-sonnet-summarized-2 | tofu 0.5.15-rc1 | 2026-10-15 03:33 | thinking {"display":"summarized","type":"adaptive"} | 6 | 58.6 | 27 | 12 | 365047 | 6749 | 366.2 | 34.1 | 1 |
| subagents-tofu-sonnet-summarized-3 | tofu 0.5.15-rc1 | 2026-10-15 03:37 | thinking {"display":"summarized","type":"adaptive"} | 6 | 66.4 | 28 | 12 | 339196 | 6685 | 340.5 | 34.1 | 0 |

## longedit

| arm | version | runs | quality of 10 | wall s | conversation requests | memory or judge calls | total in | out | peak tree MiB | CPU s | failed calls |
|---|---|---|---|---|---|---|---|---|---|---|---|
| tofu-sonnet at omp's setting | tofu 0.5.15-rc1 | 3 | 10 [10 to 10] | 73.1 [69 to 73.7] | 7 [5 to 8] | 6 [4 to 6] | 186187 [122776 to 204045] | 7373 [7197 to 7523] | 343.1 [338.6 to 356.5] | 38.1 [37.3 to 38.2] | 2 [1 to 2] |
| omp | omp omp/18.3.2 | 3 | 10 [10 to 10] | 91.4 [80.6 to 116.6] | 7 [6 to 7] | 0 [0 to 0] | 126247 [102816 to 126729] | 6185 [4197 to 6210] | 778.2 [762.9 to 886.6] | 59.2 [55.4 to 61.8] | 0 [0 to 0] |

| run | tool and version | date | thinking sent | quality of 10 | wall s | conversation requests | memory or judge calls | total in | out | peak tree MiB | CPU s | failed calls |
|---|---|---|---|---|---|---|---|---|---|---|---|---|
| longedit-omp-sonnet-1 | omp omp/18.3.2 | 2026-10-14 17:24 | not read | 10 | 116.6 | 7 | 0 | 126247 | 6185 | 886.6 | 61.8 | 0 |
| longedit-omp-sonnet-2 | omp omp/18.3.2 | 2026-10-14 17:35 | not read | 10 | 80.6 | 6 | 0 | 102816 | 4197 | 762.9 | 55.4 | 0 |
| longedit-omp-sonnet-3 | omp omp/18.3.2 | 2026-10-14 17:50 | not read | 10 | 91.4 | 7 | 0 | 126729 | 6210 | 778.2 | 59.2 | 0 |
| longedit-tofu-sonnet-summarized-1 | tofu 0.5.15-rc1 | 2026-10-15 03:30 | thinking {"display":"summarized","type":"adaptive"} | 10 | 73.7 | 8 | 4 | 204045 | 7373 | 343.1 | 38.2 | 2 |
| longedit-tofu-sonnet-summarized-2 | tofu 0.5.15-rc1 | 2026-10-15 03:34 | thinking {"display":"summarized","type":"adaptive"} | 10 | 69 | 5 | 6 | 122776 | 7197 | 356.5 | 37.3 | 1 |
| longedit-tofu-sonnet-summarized-3 | tofu 0.5.15-rc1 | 2026-10-15 03:39 | thinking {"display":"summarized","type":"adaptive"} | 10 | 73.1 | 7 | 6 | 186187 | 7523 | 338.6 | 38.1 | 2 |

## bigread

| arm | version | runs | quality of 6 | wall s | conversation requests | memory or judge calls | total in | out | peak tree MiB | CPU s | failed calls |
|---|---|---|---|---|---|---|---|---|---|---|---|
| tofu-sonnet at omp's setting | tofu 0.5.15-rc1 | 3 | 6 [6 to 6] | 22.6 [22.2 to 23.2] | 7 [7 to 7] | 3 [3 to 3] | 114008 [113795 to 115259] | 1189 [1070 to 1191] | 53.7 [53.6 to 72.1] | 2.2 [2 to 2.4] | 0 [0 to 0] |
| omp | omp omp/18.3.2 | 3 | 6 [6 to 6] | 22.6 [20.4 to 23.2] | 7 [7 to 7] | 0 [0 to 0] | 96528 [95340 to 96549] | 982 [980 to 1041] | 537.6 [531.9 to 541.7] | 7.4 [6.8 to 7.4] | 0 [0 to 0] |

| run | tool and version | date | thinking sent | quality of 6 | wall s | conversation requests | memory or judge calls | total in | out | peak tree MiB | CPU s | failed calls |
|---|---|---|---|---|---|---|---|---|---|---|---|---|
| bigread-omp-sonnet-1 | omp omp/18.3.2 | 2026-10-14 17:29 | not read | 6 | 22.6 | 7 | 0 | 95340 | 980 | 531.9 | 7.4 | 0 |
| bigread-omp-sonnet-2 | omp omp/18.3.2 | 2026-10-14 17:40 | not read | 6 | 23.2 | 7 | 0 | 96528 | 982 | 537.6 | 7.4 | 0 |
| bigread-omp-sonnet-3 | omp omp/18.3.2 | 2026-10-14 17:54 | not read | 6 | 20.4 | 7 | 0 | 96549 | 1041 | 541.7 | 6.8 | 0 |
| bigread-tofu-sonnet-summarized-1 | tofu 0.5.15-rc1 | 2026-10-15 03:32 | thinking {"display":"summarized","type":"adaptive"} | 6 | 22.2 | 7 | 3 | 114008 | 1070 | 53.6 | 2 | 0 |
| bigread-tofu-sonnet-summarized-2 | tofu 0.5.15-rc1 | 2026-10-15 03:36 | thinking {"display":"summarized","type":"adaptive"} | 6 | 23.2 | 7 | 3 | 115259 | 1191 | 72.1 | 2.4 | 0 |
| bigread-tofu-sonnet-summarized-3 | tofu 0.5.15-rc1 | 2026-10-15 03:40 | thinking {"display":"summarized","type":"adaptive"} | 6 | 22.6 | 7 | 3 | 113795 | 1189 | 53.7 | 2.2 | 0 |
