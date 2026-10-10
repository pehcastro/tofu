# bench startup, shell resolution and the toolchain probe before the first token: 2026-09-23

Machine: DESKTOP-AHUN9RO. No live model call, no network call: every figure is `internal/turn.NewBashTool` timed directly, over 6 rounds.

`go test ./bench/startup/... -count=1` passes, 4 test functions.

## Threshold, chosen before the measurement

**100 ms.** 100 ms is Nielsen's own boundary for a response that feels instant rather than a wait. Below it there is nothing worth overlapping with the first model call; above it the delay is worth hiding behind one.

## Method

`NewBashTool` resolves the shell synchronously and starts the toolchain probe in a goroutine, returning before the probe finishes: this section's `NodeProbe` figure is still `NewBashTool` timed directly, on an empty baseline directory and then on the project directory, subtracted the same way as before, so it now measures construction alone rather than construction plus the wait. `NodeCold` forces the wait by calling `Definition()` immediately after construction, in a fresh directory with a fresh cache, so it is the cost of the probe when nothing else overlaps it. `NodeWarm` repeats that in the same directory with the same `turn.ToolchainCache`, which is the cost of a second turn once the first has already probed there.

## Headline

Shell resolution: median 96.0 ms, worst 102.0 ms, over 12 samples, crosses it.

Toolchain probe, go project, this repository, `go version` only: median 3.5 ms, worst 8.0 ms, over 6 rounds, stays under it.

Toolchain probe, node project, `NewBashTool` construction alone, no longer waited on: median 0.5 ms, worst 6.0 ms, over 6 rounds, stays under it. Before this ticket, the same call was median 780.7 ms and worst 904.7 ms, because it blocked on the probe; construction now returns before the probe is done.

Toolchain probe, node project, realized: construction plus a forced wait for `Definition()`, first turn in a fresh directory: median 722.0 ms, worst 768.5 ms, over 6 rounds, crosses it. This is the same 780.7 ms median and 904.7 ms worst work as before this ticket; it has moved rather than shrunk, from inside construction to whenever the toolchain answer is first read.

Toolchain probe, node project, a second turn in the same directory with the same cache: median 89.0 ms, worst 95.0 ms, over 6 rounds, stays under it. This is the cache half: the same project, the same shell, the same answer, paid once.

## Threshold, read against what changed

The 100 ms line was written against `NewBashTool` timed directly, so `NodeProbe` is the figure to read it against, and it now stays under it. The realized figure, `NodeCold`, still crosses it by design: the wait is real work and this ticket overlaps it rather than removing it. Whether a person ever sees that wait depends on whether the environment block that needs it is built after something else has already run for 780 ms; wiring the same `turn.ToolchainCache` through `cmd/tofu/run.go` so the block built in `runEnvironment` reuses the probe already started when the bash tool is built in `buildRunToolsReading` is what would make that true in production, and it is outside this ticket's owns.

## What this does not change

`cmd/tofu/run.go` is untouched by this ticket: it still builds the bash tool and the environment block through two independent calls, so today they still pay for two separate probes rather than sharing the one this ticket makes overlappable. Wiring them together is a separate ticket, flagged the same way TOFU-543 flagged the same file for the same reason.

## 2026-10-10, the probe left `Definition()`

At 6fad62ad (2026-10-05) `Definition()` stopped carrying the toolchain, so it no longer waits on the probe, and `NodeCold` and `NodeWarm` as described above both timed shell resolution alone: 55.0 ms cold against 56.0 ms warm, and the cache test failed. Both now time `turn.EnvironmentFromShell` on one `turn.RunShell`, the block a turn builds and the place it waits. On 2026-10-10, 5 rounds: cold median 332.3 ms, worst 343.5 ms; warm median 0 s, worst 0.5 ms. The figures above are not rerun.

