# tofu against omp on sub-agents, a long edit and a large read, 2026-10-15

TOFU-1400 ran tofu-sonnet three times per task on the installed tofu 0.5.15-rc1 (commit b440a772) at omp's thinking setting, rows in bench/rivals/runs/0.5.15-rc1. omp is 18.3.2, unchanged, so its rows are TOFU-1370's from bench/rivals/runs, not rerun. Peak tree MiB is bytes over 2^20, the unit of the omp rows.

Median of 3 runs, lowest and highest in brackets.

## subagents

| arm | version | runs | quality of 6 | wall s | requests | total in | out | peak tree MiB | CPU s | failed calls |
|---|---|---|---|---|---|---|---|---|---|---|
| tofu-sonnet at omp's setting | tofu 0.5.15-rc1 | 3 | 6 [6 to 6] | 61.4 [58.6 to 66.4] | 40 [39 to 43] | 365047 [339196 to 402908] | 6749 [6685 to 6836] | 360.5 [340.5 to 366.2] | 34.1 [34.1 to 34.9] | 0 [0 to 1] |
| omp | omp omp/18.3.2 | 3 | 6 [6 to 6] | 82.9 [80.3 to 124.6] | 37 [30 to 53] | 455504 [324613 to 700756] | 14501 [7325 to 21323] | 1080 [1046.2 to 1154.8] | 53.3 [52 to 57.5] | 4 [3 to 9] |

| run | tool and version | date | thinking sent | quality | wall s | requests | total in | out | peak tree MiB | CPU s | failed |
|---|---|---|---|---|---|---|---|---|---|---|---|
| subagents-tofu-sonnet-summarized-1 | tofu 0.5.15-rc1 | 2026-10-15 03:29 | thinking {"display":"summarized","type":"adaptive"} | 6 | 61.4 | 43 | 402908 | 6836 | 360.5 | 34.9 | 0 |
| subagents-tofu-sonnet-summarized-2 | tofu 0.5.15-rc1 | 2026-10-15 03:33 | thinking {"display":"summarized","type":"adaptive"} | 6 | 58.6 | 39 | 365047 | 6749 | 366.2 | 34.1 | 1 |
| subagents-tofu-sonnet-summarized-3 | tofu 0.5.15-rc1 | 2026-10-15 03:37 | thinking {"display":"summarized","type":"adaptive"} | 6 | 66.4 | 40 | 339196 | 6685 | 340.5 | 34.1 | 0 |

## longedit

| arm | version | runs | quality of 10 | wall s | requests | total in | out | peak tree MiB | CPU s | failed calls |
|---|---|---|---|---|---|---|---|---|---|---|
| tofu-sonnet at omp's setting | tofu 0.5.15-rc1 | 3 | 10 [10 to 10] | 73.1 [69 to 73.7] | 12 [11 to 13] | 186187 [122776 to 204045] | 7373 [7197 to 7523] | 343.1 [338.6 to 356.5] | 38.1 [37.3 to 38.2] | 2 [1 to 2] |
| omp | omp omp/18.3.2 | 3 | 10 [10 to 10] | 91.4 [80.6 to 116.6] | 7 [6 to 7] | 126247 [102816 to 126729] | 6185 [4197 to 6210] | 778.2 [762.9 to 886.6] | 59.2 [55.4 to 61.8] | 0 [0 to 0] |

| run | tool and version | date | thinking sent | quality | wall s | requests | total in | out | peak tree MiB | CPU s | failed |
|---|---|---|---|---|---|---|---|---|---|---|---|
| longedit-tofu-sonnet-summarized-1 | tofu 0.5.15-rc1 | 2026-10-15 03:30 | thinking {"display":"summarized","type":"adaptive"} | 10 | 73.7 | 12 | 204045 | 7373 | 343.1 | 38.2 | 2 |
| longedit-tofu-sonnet-summarized-2 | tofu 0.5.15-rc1 | 2026-10-15 03:34 | thinking {"display":"summarized","type":"adaptive"} | 10 | 69 | 11 | 122776 | 7197 | 356.5 | 37.3 | 1 |
| longedit-tofu-sonnet-summarized-3 | tofu 0.5.15-rc1 | 2026-10-15 03:39 | thinking {"display":"summarized","type":"adaptive"} | 10 | 73.1 | 13 | 186187 | 7523 | 338.6 | 38.1 | 2 |

## bigread

| arm | version | runs | quality of 6 | wall s | requests | total in | out | peak tree MiB | CPU s | failed calls |
|---|---|---|---|---|---|---|---|---|---|---|
| tofu-sonnet at omp's setting | tofu 0.5.15-rc1 | 3 | 6 [6 to 6] | 22.6 [22.2 to 23.2] | 10 [10 to 10] | 114008 [113795 to 115259] | 1189 [1070 to 1191] | 53.7 [53.6 to 72.1] | 2.2 [2 to 2.4] | 0 [0 to 0] |
| omp | omp omp/18.3.2 | 3 | 6 [6 to 6] | 22.6 [20.4 to 23.2] | 7 [7 to 7] | 96528 [95340 to 96549] | 982 [980 to 1041] | 537.6 [531.9 to 541.7] | 7.4 [6.8 to 7.4] | 0 [0 to 0] |

| run | tool and version | date | thinking sent | quality | wall s | requests | total in | out | peak tree MiB | CPU s | failed |
|---|---|---|---|---|---|---|---|---|---|---|---|
| bigread-tofu-sonnet-summarized-1 | tofu 0.5.15-rc1 | 2026-10-15 03:32 | thinking {"display":"summarized","type":"adaptive"} | 6 | 22.2 | 10 | 114008 | 1070 | 53.6 | 2 | 0 |
| bigread-tofu-sonnet-summarized-2 | tofu 0.5.15-rc1 | 2026-10-15 03:36 | thinking {"display":"summarized","type":"adaptive"} | 6 | 23.2 | 10 | 115259 | 1191 | 72.1 | 2.4 | 0 |
| bigread-tofu-sonnet-summarized-3 | tofu 0.5.15-rc1 | 2026-10-15 03:40 | thinking {"display":"summarized","type":"adaptive"} | 6 | 22.6 | 10 | 113795 | 1189 | 53.7 | 2.2 | 0 |
