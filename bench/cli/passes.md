# CLI passes

Written by `go run ./bench/cli -pass <name> -note <what changed>` from the repository root, with `-modfile` to build an earlier dependency set. Each pass builds `./cmd/tofu`, then runs every command below in a home and working directory made fresh for each run, with GOMAXPROCS=2, stdin empty and HTTP(S)_PROXY pointed at a dead port so no verb reaches the network. Init is the sum of `GODEBUG=inittrace=1` over `tofu version`. Every figure is the median of the pass's runs, wall also at p90. Each pass is compared with the earlier pass named by `-against`, v0 when none is named, and every ratio is the earlier figure over the pass's.

## Init

| pass | tofu | commit | recorded | platform | runs | init clock ms | init allocations MB | note |
|---|---|---|---|---|---|---|---|---|
| v0 | 0.5.11-fix2 | 06042c16 dirty | 2026-10-13 05:19:28 | windows/amd64 | 7 | 78.6 | 8.39 | baseline: runewidth v0.0.27, chroma v2.24.1 from the module cache |
| v1-runewidth | 0.5.11-fix2 | 06042c16 dirty | 2026-10-13 05:20:18 | windows/amd64 | 7 | 46.8 | 8.37 | runewidth v0.0.30, whose strict width table is built on first lookup; every rune width identical to v0.0.27 |
| v1-chroma | 0.5.11-fix2 | 06042c16 dirty | 2026-10-13 05:20:55 | windows/amd64 | 7 | 26.7 | 3.80 | runewidth v0.0.30 and chroma v2.24.1 from third_party/chroma, whose lexer and style registries are built on first use |
| v2 | 0.5.14+dev | e68578c9 archive | 2026-10-14 06:57:03 | windows/amd64 | 7 | 40.8 | 3.40 | e68578c9, the tree before TOFU-1328: hook config parsed by BurntSushi/toml, whose init reads the local zone |
| v3 | 0.5.14+dev | 9f1aaf88 dirty | 2026-10-14 07:18:44 | windows/amd64 | 7 | 10.5 | 3.37 | the working tree with hook config parsed by pelletier/go-toml/v2, so no package reads the local zone at init, and third_party/clipboard loading user32 on first use |
| v4 | 0.5.14+dev | d358248b dirty | 2026-10-14 09:31:31 | windows/amd64 | 7 | 8.9 | 2.95 | the working tree with package-level regular expressions in learn, shell, boardy, sift, judge/state and judge/gate compiled on first use through sync.OnceValue |
| v5 | 0.5.14+dev | c5999050 dirty | 2026-10-14 09:44:56 | windows/amd64 | 7 | 7.8 | 2.60 | v4 plus one sys.LazyRegexp shared by learn, shell, boardy, sift, judge/state, judge/gate and guard, and search's and main's package-level regular expressions compiled on first use |

CPU is QueryProcessCycleTime at a cycle rate calibrated by spinning one locked thread, because Windows user and kernel times move in 15.6 ms ticks; RSS is the peak working set.

## v1-runewidth 0.5.11-fix2 against v0 0.5.11-fix2

| command | v0 0.5.11-fix2 CPU ms | v1-runewidth 0.5.11-fix2 CPU ms | CPU ratio | v0 0.5.11-fix2 wall ms | v1-runewidth 0.5.11-fix2 wall ms | wall ratio | v1-runewidth 0.5.11-fix2 wall p90 ms | v0 0.5.11-fix2 RSS MB | v1-runewidth 0.5.11-fix2 RSS MB |
|---|---|---|---|---|---|---|---|---|---|
| `version` | 103.7 | 62.1 | 1.67x | 102.0 | 60.0 | 1.70x | 63.0 | 25.9 | 23.4 |
| `help` | 99.7 | 57.6 | 1.73x | 97.5 | 57.0 | 1.71x | 60.3 | 25.7 | 23.3 |
| `docs` | 120.5 | 61.8 | 1.95x | 126.1 | 58.6 | 2.15x | 72.8 | 26.4 | 24.0 |
| `changelog` | 119.9 | 67.8 | 1.77x | 128.8 | 67.6 | 1.91x | 71.0 | 25.8 | 23.8 |
| `settings` | 108.9 | 59.8 | 1.82x | 108.5 | 58.9 | 1.84x | 83.2 | 25.5 | 23.8 |
| `memory` | 115.5 | 61.7 | 1.87x | 118.3 | 64.2 | 1.84x | 75.0 | 25.9 | 23.7 |
| `agents` | 127.8 | 82.6 | 1.55x | 124.7 | 78.0 | 1.60x | 86.0 | 26.9 | 24.6 |
| `models` | 116.6 | 69.9 | 1.67x | 113.8 | 69.6 | 1.63x | 79.4 | 26.4 | 24.3 |
| `library` | 133.9 | 87.1 | 1.54x | 128.8 | 79.0 | 1.63x | 90.0 | 29.3 | 26.5 |
| `hooks` | 97.8 | 62.7 | 1.56x | 95.0 | 59.8 | 1.59x | 85.6 | 25.7 | 23.8 |
| `context` | 98.7 | 70.6 | 1.40x | 96.4 | 66.5 | 1.45x | 79.3 | 26.1 | 23.7 |
| `scratch` | 104.8 | 72.6 | 1.44x | 103.2 | 72.4 | 1.43x | 80.7 | 25.5 | 23.7 |
| `learn` | 118.4 | 75.0 | 1.58x | 116.2 | 77.0 | 1.51x | 83.1 | 26.7 | 24.6 |
| `usage` | 133.8 | 72.3 | 1.85x | 168.0 | 96.1 | 1.75x | 116.8 | 27.9 | 25.7 |
| `doctor` | 172.1 | 127.4 | 1.35x | 212.9 | 168.2 | 1.27x | 182.0 | 31.2 | 28.7 |
| `frame` | 147.5 | 112.7 | 1.31x | 144.0 | 109.9 | 1.31x | 121.5 | 29.6 | 27.1 |
| `reload` | 137.0 | 82.1 | 1.67x | 129.0 | 80.2 | 1.61x | 86.1 | 28.7 | 26.8 |
| `migrate --dry-run` | 100.7 | 72.5 | 1.39x | 98.6 | 72.1 | 1.37x | 83.1 | 25.9 | 23.7 |
| `preview` | 91.8 | 80.0 | 1.15x | 93.8 | 86.0 | 1.09x | 164.1 | 25.9 | 23.3 |
| `serve --schema` | 121.2 | 87.9 | 1.38x | 119.3 | 89.0 | 1.34x | 94.0 | 29.7 | 27.4 |
| `drive` | 186.9 | 160.0 | 1.17x | 216.5 | 188.0 | 1.15x | 330.8 | 31.1 | 28.9 |
| `update --check` | 94.7 | 86.7 | 1.09x | 93.6 | 88.0 | 1.06x | 90.0 | 26.3 | 24.5 |
| `browser` | 87.7 | 78.0 | 1.12x | 84.0 | 76.0 | 1.10x | 85.0 | 26.2 | 23.6 |
| `session list` | 104.5 | 85.4 | 1.22x | 146.1 | 147.0 | 0.99x | 174.0 | 26.7 | 24.6 |
| `shells list` | 91.3 | 70.0 | 1.30x | 88.0 | 68.5 | 1.28x | 77.0 | 25.8 | 23.8 |
| `rules list` | 106.9 | 73.1 | 1.46x | 101.5 | 68.7 | 1.48x | 93.2 | 28.1 | 25.6 |
| `lint comments` | 106.2 | 69.2 | 1.53x | 102.9 | 66.6 | 1.55x | 69.7 | 25.7 | 23.8 |
| `boardy` | 93.5 | 68.1 | 1.37x | 95.0 | 68.3 | 1.39x | 76.3 | 25.9 | 23.7 |
| `why --last` | 99.7 | 69.1 | 1.44x | 97.0 | 68.4 | 1.42x | 74.6 | 25.6 | 23.9 |
| `label` | 85.0 | 65.9 | 1.29x | 83.0 | 62.8 | 1.32x | 73.8 | 25.8 | 23.5 |
| `replay` | 81.3 | 67.8 | 1.20x | 83.5 | 66.0 | 1.27x | 77.0 | 26.0 | 23.8 |
| `undo` | 83.5 | 79.5 | 1.05x | 81.6 | 76.9 | 1.06x | 85.1 | 26.0 | 23.6 |
| `check` | 87.9 | 77.5 | 1.13x | 87.3 | 74.0 | 1.18x | 84.7 | 25.7 | 23.5 |
| `judge` | 103.8 | 75.2 | 1.38x | 99.8 | 74.1 | 1.35x | 86.3 | 26.1 | 23.8 |
| `sift` | 102.5 | 80.3 | 1.28x | 100.2 | 76.5 | 1.31x | 80.8 | 25.6 | 23.1 |
| `run` | 104.3 | 89.5 | 1.17x | 159.2 | 143.1 | 1.11x | 148.8 | 26.3 | 24.2 |
| `logout` | 90.7 | 64.7 | 1.40x | 87.6 | 62.3 | 1.41x | 77.4 | 25.8 | 23.6 |
| `login --status` | 123.0 | 76.4 | 1.61x | 118.8 | 75.8 | 1.57x | 90.0 | 26.3 | 24.1 |

## v1-chroma 0.5.11-fix2 against v0 0.5.11-fix2

| command | v0 0.5.11-fix2 CPU ms | v1-chroma 0.5.11-fix2 CPU ms | CPU ratio | v0 0.5.11-fix2 wall ms | v1-chroma 0.5.11-fix2 wall ms | wall ratio | v1-chroma 0.5.11-fix2 wall p90 ms | v0 0.5.11-fix2 RSS MB | v1-chroma 0.5.11-fix2 RSS MB |
|---|---|---|---|---|---|---|---|---|---|
| `version` | 103.7 | 41.9 | 2.47x | 102.0 | 42.3 | 2.41x | 50.2 | 25.9 | 18.6 |
| `help` | 99.7 | 41.3 | 2.41x | 97.5 | 41.2 | 2.37x | 44.7 | 25.7 | 18.6 |
| `docs` | 120.5 | 43.5 | 2.77x | 126.1 | 42.0 | 3.00x | 53.0 | 26.4 | 19.6 |
| `changelog` | 119.9 | 44.5 | 2.69x | 128.8 | 44.9 | 2.87x | 49.0 | 25.8 | 19.3 |
| `settings` | 108.9 | 36.6 | 2.98x | 108.5 | 36.7 | 2.96x | 41.2 | 25.5 | 18.7 |
| `memory` | 115.5 | 44.8 | 2.58x | 118.3 | 44.7 | 2.65x | 50.3 | 25.9 | 18.8 |
| `agents` | 127.8 | 52.2 | 2.45x | 124.7 | 51.0 | 2.44x | 60.4 | 26.9 | 20.3 |
| `models` | 116.6 | 51.5 | 2.26x | 113.8 | 52.0 | 2.19x | 61.5 | 26.4 | 19.8 |
| `library` | 133.9 | 64.8 | 2.07x | 128.8 | 60.7 | 2.12x | 78.0 | 29.3 | 22.7 |
| `hooks` | 97.8 | 49.5 | 1.97x | 95.0 | 48.7 | 1.95x | 56.0 | 25.7 | 18.7 |
| `context` | 98.7 | 44.7 | 2.21x | 96.4 | 46.0 | 2.10x | 48.0 | 26.1 | 18.4 |
| `scratch` | 104.8 | 45.5 | 2.30x | 103.2 | 46.0 | 2.24x | 54.0 | 25.5 | 18.4 |
| `learn` | 118.4 | 50.0 | 2.37x | 116.2 | 49.0 | 2.37x | 61.6 | 26.7 | 20.5 |
| `usage` | 133.8 | 59.2 | 2.26x | 168.0 | 84.3 | 1.99x | 93.7 | 27.9 | 21.1 |
| `doctor` | 172.1 | 105.0 | 1.64x | 212.9 | 143.8 | 1.48x | 152.2 | 31.2 | 24.7 |
| `frame` | 147.5 | 82.0 | 1.80x | 144.0 | 76.4 | 1.89x | 96.0 | 29.6 | 22.9 |
| `reload` | 137.0 | 67.7 | 2.02x | 129.0 | 65.6 | 1.97x | 75.1 | 28.7 | 22.5 |
| `migrate --dry-run` | 100.7 | 46.2 | 2.18x | 98.6 | 48.0 | 2.05x | 58.0 | 25.9 | 18.7 |
| `preview` | 91.8 | 38.4 | 2.39x | 93.8 | 39.0 | 2.41x | 46.0 | 25.9 | 18.5 |
| `serve --schema` | 121.2 | 50.9 | 2.38x | 119.3 | 49.0 | 2.44x | 50.4 | 29.7 | 23.3 |
| `drive` | 186.9 | 96.8 | 1.93x | 216.5 | 115.9 | 1.87x | 125.1 | 31.1 | 24.3 |
| `update --check` | 94.7 | 40.6 | 2.33x | 93.6 | 39.3 | 2.38x | 43.2 | 26.3 | 19.5 |
| `browser` | 87.7 | 40.5 | 2.17x | 84.0 | 39.7 | 2.12x | 46.0 | 26.2 | 19.0 |
| `session list` | 104.5 | 53.9 | 1.94x | 146.1 | 103.1 | 1.42x | 119.8 | 26.7 | 19.7 |
| `shells list` | 91.3 | 44.3 | 2.06x | 88.0 | 46.0 | 1.91x | 51.0 | 25.8 | 18.4 |
| `rules list` | 106.9 | 54.4 | 1.97x | 101.5 | 51.1 | 1.99x | 65.0 | 28.1 | 21.2 |
| `lint comments` | 106.2 | 40.3 | 2.64x | 102.9 | 41.0 | 2.51x | 46.0 | 25.7 | 18.8 |
| `boardy` | 93.5 | 38.7 | 2.42x | 95.0 | 38.6 | 2.46x | 45.0 | 25.9 | 18.3 |
| `why --last` | 99.7 | 39.7 | 2.51x | 97.0 | 39.4 | 2.46x | 43.7 | 25.6 | 18.8 |
| `label` | 85.0 | 43.5 | 1.96x | 83.0 | 42.3 | 1.96x | 53.0 | 25.8 | 18.6 |
| `replay` | 81.3 | 52.0 | 1.56x | 83.5 | 54.0 | 1.55x | 62.2 | 26.0 | 18.6 |
| `undo` | 83.5 | 51.6 | 1.62x | 81.6 | 60.0 | 1.36x | 70.0 | 26.0 | 18.5 |
| `check` | 87.9 | 49.8 | 1.77x | 87.3 | 50.0 | 1.75x | 56.0 | 25.7 | 18.6 |
| `judge` | 103.8 | 49.1 | 2.11x | 99.8 | 49.0 | 2.04x | 60.4 | 26.1 | 18.6 |
| `sift` | 102.5 | 44.5 | 2.30x | 100.2 | 46.8 | 2.14x | 52.0 | 25.6 | 18.4 |
| `run` | 104.3 | 55.2 | 1.89x | 159.2 | 101.0 | 1.58x | 147.8 | 26.3 | 19.8 |
| `logout` | 90.7 | 44.6 | 2.03x | 87.6 | 46.5 | 1.88x | 60.2 | 25.8 | 18.3 |
| `login --status` | 123.0 | 61.2 | 2.01x | 118.8 | 63.4 | 1.87x | 72.0 | 26.3 | 19.2 |

## v2 0.5.14+dev against v0 0.5.11-fix2

| command | v0 0.5.11-fix2 CPU ms | v2 0.5.14+dev CPU ms | CPU ratio | v0 0.5.11-fix2 wall ms | v2 0.5.14+dev wall ms | wall ratio | v2 0.5.14+dev wall p90 ms | v0 0.5.11-fix2 RSS MB | v2 0.5.14+dev RSS MB |
|---|---|---|---|---|---|---|---|---|---|
| `version` | 103.7 | 50.1 | 2.07x | 102.0 | 49.0 | 2.08x | 60.0 | 25.9 | 18.2 |
| `help` | 99.7 | 47.8 | 2.09x | 97.5 | 50.0 | 1.95x | 58.5 | 25.7 | 18.5 |
| `docs` | 120.5 | 52.9 | 2.28x | 126.1 | 52.7 | 2.39x | 70.2 | 26.4 | 19.3 |
| `changelog` | 119.9 | 50.6 | 2.37x | 128.8 | 50.2 | 2.57x | 60.0 | 25.8 | 18.8 |
| `settings` | 108.9 | 42.5 | 2.56x | 108.5 | 42.3 | 2.56x | 52.7 | 25.5 | 18.7 |
| `memory` | 115.5 | 46.2 | 2.50x | 118.3 | 47.0 | 2.52x | 69.0 | 25.9 | 18.7 |
| `agents` | 127.8 | 52.0 | 2.46x | 124.7 | 52.0 | 2.40x | 55.2 | 26.9 | 20.1 |
| `models` | 116.6 | 44.8 | 2.60x | 113.8 | 45.9 | 2.48x | 64.0 | 26.4 | 19.7 |
| `library` | 133.9 | 56.2 | 2.38x | 128.8 | 52.6 | 2.45x | 62.9 | 29.3 | 22.8 |
| `hooks` | 97.8 | 44.7 | 2.19x | 95.0 | 45.8 | 2.08x | 50.8 | 25.7 | 18.5 |
| `context` | 98.7 | 48.9 | 2.02x | 96.4 | 48.0 | 2.01x | 55.7 | 26.1 | 18.7 |
| `scratch` | 104.8 | 45.7 | 2.29x | 103.2 | 48.0 | 2.15x | 51.0 | 25.5 | 18.8 |
| `learn` | 118.4 | 55.7 | 2.12x | 116.2 | 59.0 | 1.97x | 82.5 | 26.7 | 20.5 |
| `usage` | 133.8 | 74.9 | 1.79x | 168.0 | 107.7 | 1.56x | 309.7 | 27.9 | 21.0 |
| `doctor` | 172.1 | 129.9 | 1.32x | 212.9 | 170.0 | 1.25x | 185.1 | 31.2 | 25.1 |
| `frame` | 147.5 | 97.7 | 1.51x | 144.0 | 95.0 | 1.52x | 104.5 | 29.6 | 23.2 |
| `reload` | 137.0 | 74.8 | 1.83x | 129.0 | 74.0 | 1.74x | 85.0 | 28.7 | 22.8 |
| `migrate --dry-run` | 100.7 | 60.6 | 1.66x | 98.6 | 60.0 | 1.64x | 66.0 | 25.9 | 18.6 |
| `preview` | 91.8 | 54.7 | 1.68x | 93.8 | 56.0 | 1.68x | 59.0 | 25.9 | 18.6 |
| `serve --schema` | 121.2 | 66.2 | 1.83x | 119.3 | 65.4 | 1.82x | 72.0 | 29.7 | 23.5 |
| `drive` | 186.9 | 122.2 | 1.53x | 216.5 | 144.0 | 1.50x | 153.8 | 31.1 | 24.8 |
| `update --check` | 94.7 | 53.2 | 1.78x | 93.6 | 55.0 | 1.70x | 59.0 | 26.3 | 19.4 |
| `browser` | 87.7 | 46.6 | 1.88x | 84.0 | 47.7 | 1.76x | 54.9 | 26.2 | 19.0 |
| `session list` | 104.5 | 68.3 | 1.53x | 146.1 | 128.0 | 1.14x | 138.0 | 26.7 | 19.9 |
| `shells list` | 91.3 | 45.4 | 2.01x | 88.0 | 45.0 | 1.96x | 50.7 | 25.8 | 18.7 |
| `rules list` | 106.9 | 67.1 | 1.59x | 101.5 | 65.0 | 1.56x | 77.0 | 28.1 | 21.2 |
| `lint comments` | 106.2 | 41.9 | 2.53x | 102.9 | 42.0 | 2.45x | 50.7 | 25.7 | 18.4 |
| `boardy` | 93.5 | 49.1 | 1.90x | 95.0 | 49.6 | 1.91x | 52.6 | 25.9 | 18.5 |
| `why --last` | 99.7 | 56.0 | 1.78x | 97.0 | 56.4 | 1.72x | 64.7 | 25.6 | 18.6 |
| `label` | 85.0 | 49.5 | 1.72x | 83.0 | 51.0 | 1.63x | 64.0 | 25.8 | 18.3 |
| `replay` | 81.3 | 49.2 | 1.65x | 83.5 | 51.4 | 1.62x | 56.9 | 26.0 | 18.6 |
| `undo` | 83.5 | 46.7 | 1.79x | 81.6 | 48.0 | 1.70x | 67.5 | 26.0 | 18.6 |
| `check` | 87.9 | 48.5 | 1.81x | 87.3 | 49.5 | 1.76x | 52.7 | 25.7 | 18.6 |
| `judge` | 103.8 | 53.7 | 1.93x | 99.8 | 54.7 | 1.83x | 58.7 | 26.1 | 18.7 |
| `sift` | 102.5 | 49.0 | 2.09x | 100.2 | 48.1 | 2.08x | 62.0 | 25.6 | 18.7 |
| `run` | 104.3 | 66.3 | 1.57x | 159.2 | 133.5 | 1.19x | 156.4 | 26.3 | 19.6 |
| `logout` | 90.7 | 46.2 | 1.97x | 87.6 | 45.8 | 1.91x | 58.7 | 25.8 | 18.7 |
| `login --status` | 123.0 | 65.4 | 1.88x | 118.8 | 64.9 | 1.83x | 78.0 | 26.3 | 19.5 |

## v3 0.5.14+dev against v2 0.5.14+dev

| command | v2 0.5.14+dev CPU ms | v3 0.5.14+dev CPU ms | CPU ratio | v2 0.5.14+dev wall ms | v3 0.5.14+dev wall ms | wall ratio | v3 0.5.14+dev wall p90 ms | v2 0.5.14+dev RSS MB | v3 0.5.14+dev RSS MB |
|---|---|---|---|---|---|---|---|---|---|
| `version` | 50.1 | 18.5 | 2.70x | 49.0 | 18.8 | 2.61x | 19.5 | 18.2 | 16.9 |
| `help` | 47.8 | 17.5 | 2.73x | 50.0 | 18.2 | 2.75x | 21.6 | 18.5 | 16.8 |
| `docs` | 52.9 | 19.4 | 2.72x | 52.7 | 20.0 | 2.64x | 23.0 | 19.3 | 17.7 |
| `changelog` | 50.6 | 22.1 | 2.29x | 50.2 | 23.0 | 2.18x | 24.0 | 18.8 | 17.4 |
| `settings` | 42.5 | 18.6 | 2.29x | 42.3 | 18.9 | 2.24x | 23.0 | 18.7 | 17.1 |
| `memory` | 46.2 | 21.5 | 2.15x | 47.0 | 22.2 | 2.12x | 26.5 | 18.7 | 17.1 |
| `agents` | 52.0 | 25.3 | 2.06x | 52.0 | 26.0 | 2.00x | 29.0 | 20.1 | 18.6 |
| `models` | 44.8 | 20.0 | 2.24x | 45.9 | 20.3 | 2.25x | 23.0 | 19.7 | 18.1 |
| `library` | 56.2 | 31.4 | 1.79x | 52.6 | 30.3 | 1.74x | 38.0 | 22.8 | 21.5 |
| `hooks` | 44.7 | 20.0 | 2.24x | 45.8 | 20.6 | 2.22x | 23.3 | 18.5 | 17.0 |
| `context` | 48.9 | 20.0 | 2.45x | 48.0 | 20.8 | 2.31x | 25.0 | 18.7 | 17.1 |
| `scratch` | 45.7 | 19.4 | 2.36x | 48.0 | 20.5 | 2.34x | 22.5 | 18.8 | 17.1 |
| `learn` | 55.7 | 38.5 | 1.45x | 59.0 | 40.0 | 1.48x | 51.8 | 20.5 | 19.6 |
| `usage` | 74.9 | 36.4 | 2.05x | 107.7 | 58.7 | 1.83x | 60.3 | 21.0 | 19.5 |
| `doctor` | 129.9 | 73.1 | 1.78x | 170.0 | 99.0 | 1.72x | 144.6 | 25.1 | 24.2 |
| `frame` | 97.7 | 51.4 | 1.90x | 95.0 | 49.9 | 1.90x | 52.4 | 23.2 | 21.7 |
| `reload` | 74.8 | 37.6 | 1.99x | 74.0 | 36.0 | 2.06x | 39.6 | 22.8 | 21.3 |
| `migrate --dry-run` | 60.6 | 19.0 | 3.20x | 60.0 | 20.0 | 3.00x | 22.2 | 18.6 | 16.9 |
| `preview` | 54.7 | 18.6 | 2.94x | 56.0 | 19.3 | 2.91x | 21.0 | 18.6 | 17.1 |
| `serve --schema` | 66.2 | 27.7 | 2.38x | 65.4 | 27.7 | 2.36x | 30.1 | 23.5 | 22.3 |
| `drive` | 122.2 | 66.2 | 1.85x | 144.0 | 82.6 | 1.74x | 87.7 | 24.8 | 23.5 |
| `update --check` | 53.2 | 21.0 | 2.53x | 55.0 | 22.0 | 2.50x | 24.0 | 19.4 | 17.9 |
| `browser` | 46.6 | 22.5 | 2.07x | 47.7 | 23.2 | 2.06x | 26.0 | 19.0 | 17.5 |
| `session list` | 68.3 | 30.9 | 2.21x | 128.0 | 72.6 | 1.76x | 75.7 | 19.9 | 18.6 |
| `shells list` | 45.4 | 18.4 | 2.46x | 45.0 | 19.4 | 2.32x | 20.2 | 18.7 | 17.0 |
| `rules list` | 67.1 | 25.9 | 2.59x | 65.0 | 25.4 | 2.56x | 27.8 | 21.2 | 19.9 |
| `lint comments` | 41.9 | 18.1 | 2.31x | 42.0 | 19.3 | 2.18x | 20.6 | 18.4 | 16.9 |
| `boardy` | 49.1 | 17.6 | 2.79x | 49.6 | 18.5 | 2.69x | 20.1 | 18.5 | 16.9 |
| `why --last` | 56.0 | 18.1 | 3.10x | 56.4 | 19.1 | 2.96x | 20.6 | 18.6 | 16.9 |
| `label` | 49.5 | 17.8 | 2.78x | 51.0 | 18.3 | 2.79x | 20.7 | 18.3 | 17.0 |
| `replay` | 49.2 | 17.6 | 2.79x | 51.4 | 19.0 | 2.70x | 22.0 | 18.6 | 16.8 |
| `undo` | 46.7 | 18.5 | 2.53x | 48.0 | 19.8 | 2.43x | 20.4 | 18.6 | 17.1 |
| `check` | 48.5 | 18.1 | 2.68x | 49.5 | 18.7 | 2.66x | 22.0 | 18.6 | 16.9 |
| `judge` | 53.7 | 18.1 | 2.96x | 54.7 | 18.9 | 2.89x | 20.9 | 18.7 | 16.9 |
| `sift` | 49.0 | 17.7 | 2.77x | 48.1 | 18.3 | 2.62x | 20.6 | 18.7 | 17.0 |
| `run` | 66.3 | 31.9 | 2.07x | 133.5 | 74.9 | 1.78x | 77.5 | 19.6 | 18.4 |
| `logout` | 46.2 | 18.2 | 2.53x | 45.8 | 18.8 | 2.43x | 21.8 | 18.7 | 16.9 |
| `login --status` | 65.4 | 26.7 | 2.45x | 64.9 | 27.5 | 2.36x | 29.3 | 19.5 | 17.7 |

## v4 0.5.14+dev against v3 0.5.14+dev

| command | v3 0.5.14+dev CPU ms | v4 0.5.14+dev CPU ms | CPU ratio | v3 0.5.14+dev wall ms | v4 0.5.14+dev wall ms | wall ratio | v4 0.5.14+dev wall p90 ms | v3 0.5.14+dev RSS MB | v4 0.5.14+dev RSS MB |
|---|---|---|---|---|---|---|---|---|---|
| `version` | 18.5 | 18.0 | 1.03x | 18.8 | 20.0 | 0.94x | 21.0 | 16.9 | 16.5 |
| `help` | 17.5 | 17.6 | 0.99x | 18.2 | 20.0 | 0.91x | 24.0 | 16.8 | 16.4 |
| `docs` | 19.4 | 22.6 | 0.86x | 20.0 | 23.1 | 0.87x | 27.0 | 17.7 | 17.2 |
| `changelog` | 22.1 | 25.9 | 0.85x | 23.0 | 26.9 | 0.86x | 29.5 | 17.4 | 17.1 |
| `settings` | 18.6 | 19.5 | 0.95x | 18.9 | 21.1 | 0.90x | 28.4 | 17.1 | 16.8 |
| `memory` | 21.5 | 24.0 | 0.89x | 22.2 | 24.5 | 0.91x | 32.5 | 17.1 | 16.7 |
| `agents` | 25.3 | 25.8 | 0.98x | 26.0 | 29.0 | 0.90x | 32.5 | 18.6 | 18.1 |
| `models` | 20.0 | 20.9 | 0.96x | 20.3 | 21.7 | 0.94x | 27.5 | 18.1 | 17.7 |
| `library` | 31.4 | 32.9 | 0.95x | 30.3 | 31.4 | 0.96x | 38.9 | 21.5 | 20.7 |
| `hooks` | 20.0 | 25.2 | 0.79x | 20.6 | 25.6 | 0.80x | 28.6 | 17.0 | 16.5 |
| `context` | 20.0 | 21.6 | 0.92x | 20.8 | 23.7 | 0.88x | 25.0 | 17.1 | 16.6 |
| `scratch` | 19.4 | 22.3 | 0.87x | 20.5 | 23.0 | 0.89x | 25.0 | 17.1 | 16.6 |
| `learn` | 38.5 | 41.5 | 0.93x | 40.0 | 42.3 | 0.94x | 43.4 | 19.6 | 19.1 |
| `usage` | 36.4 | 36.8 | 0.99x | 58.7 | 60.8 | 0.96x | 73.4 | 19.5 | 19.1 |
| `doctor` | 73.1 | 84.3 | 0.87x | 99.0 | 123.4 | 0.80x | 313.7 | 24.2 | 23.9 |
| `frame` | 51.4 | 55.7 | 0.92x | 49.9 | 55.4 | 0.90x | 72.5 | 21.7 | 21.2 |
| `reload` | 37.6 | 44.2 | 0.85x | 36.0 | 43.6 | 0.83x | 46.9 | 21.3 | 21.1 |
| `migrate --dry-run` | 19.0 | 22.8 | 0.83x | 20.0 | 24.0 | 0.83x | 28.0 | 16.9 | 16.6 |
| `preview` | 18.6 | 22.1 | 0.84x | 19.3 | 24.0 | 0.80x | 26.0 | 17.1 | 16.6 |
| `serve --schema` | 27.7 | 32.5 | 0.85x | 27.7 | 31.6 | 0.88x | 37.6 | 22.3 | 21.5 |
| `drive` | 66.2 | 91.0 | 0.73x | 82.6 | 120.3 | 0.69x | 142.3 | 23.5 | 23.1 |
| `update --check` | 21.0 | 31.2 | 0.67x | 22.0 | 31.4 | 0.70x | 34.7 | 17.9 | 17.4 |
| `browser` | 22.5 | 27.6 | 0.81x | 23.2 | 28.7 | 0.81x | 31.5 | 17.5 | 17.0 |
| `session list` | 30.9 | 39.9 | 0.78x | 72.6 | 93.7 | 0.77x | 110.6 | 18.6 | 18.2 |
| `shells list` | 18.4 | 24.4 | 0.76x | 19.4 | 25.5 | 0.76x | 33.0 | 17.0 | 16.6 |
| `rules list` | 25.9 | 34.4 | 0.75x | 25.4 | 33.9 | 0.75x | 36.1 | 19.9 | 19.5 |
| `lint comments` | 18.1 | 22.1 | 0.82x | 19.3 | 22.4 | 0.86x | 25.8 | 16.9 | 16.5 |
| `boardy` | 17.6 | 23.3 | 0.76x | 18.5 | 24.2 | 0.76x | 24.9 | 16.9 | 16.5 |
| `why --last` | 18.1 | 19.4 | 0.93x | 19.1 | 21.5 | 0.89x | 25.9 | 16.9 | 16.6 |
| `label` | 17.8 | 21.3 | 0.84x | 18.3 | 22.6 | 0.81x | 31.0 | 17.0 | 16.6 |
| `replay` | 17.6 | 19.2 | 0.92x | 19.0 | 19.8 | 0.96x | 23.6 | 16.8 | 15.8 |
| `undo` | 18.5 | 22.5 | 0.82x | 19.8 | 24.0 | 0.83x | 26.5 | 17.1 | 16.6 |
| `check` | 18.1 | 22.1 | 0.82x | 18.7 | 23.6 | 0.79x | 26.8 | 16.9 | 16.5 |
| `judge` | 18.1 | 17.2 | 1.06x | 18.9 | 20.3 | 0.93x | 23.2 | 16.9 | 16.5 |
| `sift` | 17.7 | 19.3 | 0.92x | 18.3 | 20.2 | 0.91x | 23.2 | 17.0 | 16.3 |
| `run` | 31.9 | 34.4 | 0.93x | 74.9 | 84.2 | 0.89x | 101.3 | 18.4 | 18.0 |
| `logout` | 18.2 | 20.8 | 0.88x | 18.8 | 21.7 | 0.87x | 24.1 | 16.9 | 15.9 |
| `login --status` | 26.7 | 33.9 | 0.79x | 27.5 | 35.3 | 0.78x | 39.3 | 17.7 | 17.3 |

## v5 0.5.14+dev against v3 0.5.14+dev

| command | v3 0.5.14+dev CPU ms | v5 0.5.14+dev CPU ms | CPU ratio | v3 0.5.14+dev wall ms | v5 0.5.14+dev wall ms | wall ratio | v5 0.5.14+dev wall p90 ms | v3 0.5.14+dev RSS MB | v5 0.5.14+dev RSS MB |
|---|---|---|---|---|---|---|---|---|---|
| `version` | 18.5 | 15.4 | 1.21x | 18.8 | 16.8 | 1.12x | 18.7 | 16.9 | 14.7 |
| `help` | 17.5 | 15.3 | 1.15x | 18.2 | 17.0 | 1.07x | 18.0 | 16.8 | 14.6 |
| `docs` | 19.4 | 19.9 | 0.98x | 20.0 | 22.6 | 0.88x | 24.0 | 17.7 | 16.9 |
| `changelog` | 22.1 | 23.6 | 0.94x | 23.0 | 24.9 | 0.92x | 30.0 | 17.4 | 16.8 |
| `settings` | 18.6 | 20.5 | 0.91x | 18.9 | 25.4 | 0.74x | 29.9 | 17.1 | 15.0 |
| `memory` | 21.5 | 18.5 | 1.16x | 22.2 | 20.0 | 1.11x | 27.0 | 17.1 | 14.9 |
| `agents` | 25.3 | 24.1 | 1.05x | 26.0 | 24.5 | 1.06x | 30.2 | 18.6 | 17.8 |
| `models` | 20.0 | 18.1 | 1.10x | 20.3 | 19.3 | 1.05x | 23.4 | 18.1 | 16.5 |
| `library` | 31.4 | 35.6 | 0.88x | 30.3 | 33.4 | 0.91x | 41.0 | 21.5 | 20.3 |
| `hooks` | 20.0 | 18.6 | 1.07x | 20.6 | 21.0 | 0.98x | 28.4 | 17.0 | 14.7 |
| `context` | 20.0 | 16.8 | 1.19x | 20.8 | 20.3 | 1.02x | 24.0 | 17.1 | 14.9 |
| `scratch` | 19.4 | 20.8 | 0.93x | 20.5 | 23.0 | 0.89x | 26.0 | 17.1 | 14.8 |
| `learn` | 38.5 | 41.8 | 0.92x | 40.0 | 42.0 | 0.95x | 49.0 | 19.6 | 19.0 |
| `usage` | 36.4 | 43.6 | 0.84x | 58.7 | 72.7 | 0.81x | 81.8 | 19.5 | 17.5 |
| `doctor` | 73.1 | 82.4 | 0.89x | 99.0 | 124.0 | 0.80x | 148.0 | 24.2 | 23.7 |
| `frame` | 51.4 | 54.4 | 0.94x | 49.9 | 53.0 | 0.94x | 59.0 | 21.7 | 21.1 |
| `reload` | 37.6 | 38.0 | 0.99x | 36.0 | 37.4 | 0.96x | 42.0 | 21.3 | 20.8 |
| `migrate --dry-run` | 19.0 | 18.5 | 1.02x | 20.0 | 21.0 | 0.95x | 29.0 | 16.9 | 14.8 |
| `preview` | 18.6 | 17.1 | 1.09x | 19.3 | 20.0 | 0.96x | 22.0 | 17.1 | 14.8 |
| `serve --schema` | 27.7 | 30.6 | 0.91x | 27.7 | 29.7 | 0.93x | 40.0 | 22.3 | 21.1 |
| `drive` | 66.2 | 72.6 | 0.91x | 82.6 | 95.0 | 0.87x | 106.0 | 23.5 | 22.4 |
| `update --check` | 21.0 | 21.2 | 0.99x | 22.0 | 28.0 | 0.79x | 32.9 | 17.9 | 15.7 |
| `browser` | 22.5 | 26.4 | 0.85x | 23.2 | 30.5 | 0.76x | 33.7 | 17.5 | 15.2 |
| `session list` | 30.9 | 35.1 | 0.88x | 72.6 | 86.0 | 0.84x | 91.0 | 18.6 | 17.8 |
| `shells list` | 18.4 | 19.2 | 0.96x | 19.4 | 23.3 | 0.83x | 28.9 | 17.0 | 14.8 |
| `rules list` | 25.9 | 25.8 | 1.01x | 25.4 | 26.7 | 0.95x | 30.8 | 19.9 | 19.4 |
| `lint comments` | 18.1 | 17.2 | 1.06x | 19.3 | 19.5 | 0.99x | 21.4 | 16.9 | 14.7 |
| `boardy` | 17.6 | 19.0 | 0.93x | 18.5 | 21.2 | 0.87x | 29.7 | 16.9 | 14.7 |
| `why --last` | 18.1 | 17.9 | 1.01x | 19.1 | 21.5 | 0.89x | 25.3 | 16.9 | 14.8 |
| `label` | 17.8 | 16.3 | 1.10x | 18.3 | 20.0 | 0.91x | 30.0 | 17.0 | 14.7 |
| `replay` | 17.6 | 16.3 | 1.08x | 19.0 | 20.9 | 0.91x | 26.0 | 16.8 | 14.7 |
| `undo` | 18.5 | 18.2 | 1.01x | 19.8 | 23.0 | 0.86x | 25.7 | 17.1 | 14.8 |
| `check` | 18.1 | 15.8 | 1.14x | 18.7 | 19.0 | 0.98x | 25.0 | 16.9 | 14.7 |
| `judge` | 18.1 | 20.1 | 0.90x | 18.9 | 24.2 | 0.78x | 28.7 | 16.9 | 14.8 |
| `sift` | 17.7 | 18.1 | 0.98x | 18.3 | 23.0 | 0.80x | 29.2 | 17.0 | 14.8 |
| `run` | 31.9 | 36.9 | 0.86x | 74.9 | 93.0 | 0.81x | 103.0 | 18.4 | 17.7 |
| `logout` | 18.2 | 17.7 | 1.03x | 18.8 | 21.0 | 0.90x | 23.7 | 16.9 | 14.7 |
| `login --status` | 26.7 | 31.5 | 0.85x | 27.5 | 32.0 | 0.86x | 36.0 | 17.7 | 17.0 |

## Outside the verbs

Changes a verb timing cannot see, each measured by the command named in its row, before and after, on the same machine. The ratio is before over after where less is better and after over before where more is, so above 1x is a gain and below it a cost.

| pass | tofu | change | measure | before | after | ratio | commit | measured by |
|---|---|---|---|---|---|---|---|---|
| v2-ceiling | 0.5.12 | konst.ContextCeilingTokens 250000 to 450000, contextCeiling setting to go back | tokens before the first fork on a 1M window model | 200000 | 360000 | 1.80x | f347e62d dirty | `tofu run --dir . --dry-run --model claude-sub/claude-opus-5 hi` |
| v2-gogc | 0.5.12 | GOGC 200 and a 512 MiB soft memory limit in tofu app and tofu serve | GC CPU ms over the long-session TUI test, median of 3 | 219.3 | 102.1 | 2.15x | f347e62d dirty | `GODEBUG=gctrace=1 GOGC=100, then GOGC=200 GOMEMLIMIT=512MiB, tui.test -test.run TestLongSessionFramesStayInsideTheBudget` |
| v2-gogc | 0.5.12 | GOGC 200 and a 512 MiB soft memory limit in tofu app and tofu serve | GC cycles over the long-session TUI test, median of 3 | 94 | 42 | 2.24x | f347e62d dirty | `GODEBUG=gctrace=1 GOGC=100, then GOGC=200 GOMEMLIMIT=512MiB, tui.test -test.run TestLongSessionFramesStayInsideTheBudget` |
| v2-gogc | 0.5.12 | GOGC 200 and a 512 MiB soft memory limit in tofu app and tofu serve | frame median ms, sub-agents thinking streams, median of 3 | 5.21 | 5.19 | 1.00x | f347e62d dirty | `GOGC=100, then GOGC=200 GOMEMLIMIT=512MiB, tui.test -test.run TestLongSessionFramesStayInsideTheBudget` |
| v2-gogc | 0.5.12 | GOGC 200 and a 512 MiB soft memory limit in tofu app and tofu serve | frame median ms, file edits next and previous, median of 3 | 6.03 | 5.62 | 1.07x | f347e62d dirty | `GOGC=100, then GOGC=200 GOMEMLIMIT=512MiB, tui.test -test.run TestLongSessionFramesStayInsideTheBudget` |
| v2-gogc | 0.5.12 | GOGC 200 and a 512 MiB soft memory limit in tofu app and tofu serve | KB allocated a frame, sub-agents thinking streams, median of 3 | 1324 | 1308.7 | 1.01x | f347e62d dirty | `GOGC=100, then GOGC=200 GOMEMLIMIT=512MiB, tui.test -test.run TestLongSessionFramesStayInsideTheBudget` |
| v2-gogc | 0.5.12 | GOGC 200 and a 512 MiB soft memory limit in tofu app and tofu serve | peak heap MB over the long-session TUI test, median of 3 | 21 | 29 | 0.72x | f347e62d dirty | `GODEBUG=gctrace=1 GOGC=100, then GOGC=200 GOMEMLIMIT=512MiB, tui.test -test.run TestLongSessionFramesStayInsideTheBudget` |
| v2-pool | 0.5.12 | a sync.Pool request body buffer in the anthropic wire, dropped | bytes allocated per Encode of a 1419957 byte request, median of 5 | 2706804 | 3570784 | 0.76x | f347e62d dirty | `go test -run ^$ -bench EncodeALongConversation -count 5 ./internal/llm/wire/anthropic/` |
| v2-pool | 0.5.12 | a sync.Pool request body buffer in the anthropic wire, dropped | ns per Encode of a 1419957 byte request, median of 5 | 2135443 | 2256672 | 0.95x | f347e62d dirty | `go test -run ^$ -bench EncodeALongConversation -count 5 ./internal/llm/wire/anthropic/` |

## v0 0.5.11-fix2

| command | CPU ms | wall ms | wall p90 ms | RSS MB |
|---|---|---|---|---|
| `version` | 103.7 | 102.0 | 123.9 | 25.9 |
| `help` | 99.7 | 97.5 | 124.0 | 25.7 |
| `docs` | 120.5 | 126.1 | 175.8 | 26.4 |
| `changelog` | 119.9 | 128.8 | 146.4 | 25.8 |
| `settings` | 108.9 | 108.5 | 128.0 | 25.5 |
| `memory` | 115.5 | 118.3 | 130.5 | 25.9 |
| `agents` | 127.8 | 124.7 | 154.7 | 26.9 |
| `models` | 116.6 | 113.8 | 127.9 | 26.4 |
| `library` | 133.9 | 128.8 | 146.3 | 29.3 |
| `hooks` | 97.8 | 95.0 | 113.0 | 25.7 |
| `context` | 98.7 | 96.4 | 106.0 | 26.1 |
| `scratch` | 104.8 | 103.2 | 115.4 | 25.5 |
| `learn` | 118.4 | 116.2 | 120.0 | 26.7 |
| `usage` | 133.8 | 168.0 | 181.1 | 27.9 |
| `doctor` | 172.1 | 212.9 | 238.2 | 31.2 |
| `frame` | 147.5 | 144.0 | 157.9 | 29.6 |
| `reload` | 137.0 | 129.0 | 138.6 | 28.7 |
| `migrate --dry-run` | 100.7 | 98.6 | 121.4 | 25.9 |
| `preview` | 91.8 | 93.8 | 124.9 | 25.9 |
| `serve --schema` | 121.2 | 119.3 | 143.5 | 29.7 |
| `drive` | 186.9 | 216.5 | 229.4 | 31.1 |
| `update --check` | 94.7 | 93.6 | 113.0 | 26.3 |
| `browser` | 87.7 | 84.0 | 110.2 | 26.2 |
| `session list` | 104.5 | 146.1 | 176.0 | 26.7 |
| `shells list` | 91.3 | 88.0 | 102.9 | 25.8 |
| `rules list` | 106.9 | 101.5 | 119.2 | 28.1 |
| `lint comments` | 106.2 | 102.9 | 119.4 | 25.7 |
| `boardy` | 93.5 | 95.0 | 96.8 | 25.9 |
| `why --last` | 99.7 | 97.0 | 113.4 | 25.6 |
| `label` | 85.0 | 83.0 | 123.6 | 25.8 |
| `replay` | 81.3 | 83.5 | 90.1 | 26.0 |
| `undo` | 83.5 | 81.6 | 86.6 | 26.0 |
| `check` | 87.9 | 87.3 | 104.0 | 25.7 |
| `judge` | 103.8 | 99.8 | 119.7 | 26.1 |
| `sift` | 102.5 | 100.2 | 124.1 | 25.6 |
| `run` | 104.3 | 159.2 | 180.3 | 26.3 |
| `logout` | 90.7 | 87.6 | 103.4 | 25.8 |
| `login --status` | 123.0 | 118.8 | 136.8 | 26.3 |
