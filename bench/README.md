# Bench

## What bench is for

A number that is measured and compared. Every suite turns a mechanism on and off, runs it over recorded input, and writes the result with its conditions: model build, credential kind, wire, machine, date. Bench is never product code: the binary runs without it.

## Layout

| Folder | Holds |
|---|---|
| `bench/cmd/` | the `bench <target>` dispatcher |
| `bench/corpus/` | recorded sessions and states more than one suite reads, and the reader for them; shared fixtures in `bench/corpus/recorded/` |
| `bench/kit/` | libraries suites import, never a suite: `api/`, `report/`, `harness/` |
| `bench/gates/` | programs that stamp or compare builds: `flows/`, `treewide/`, `parity/` |
| `bench/suites/<area>/<suite>/` | one measurement per folder |

Areas:

| Area | Measures |
|---|---|
| `bench/suites/judge/` | decision points: accuracy, calibration, cost, wording |
| `bench/suites/rules/` | rules, enforcement, test quality, skills, review |
| `bench/suites/context/` | prompt, cache, tokens, memory, recall |
| `bench/suites/tools/` | search, symbols, web, tool shape |
| `bench/suites/agent/` | turns, sub-agents, sessions, rival harnesses |
| `bench/suites/perf/` | startup, memory, scale, interface speed |

## Add a suite

1. Pick the area by what the number is about. If none fits, ask; do not add a seventh area for one suite.
2. Make `bench/suites/<area>/<suite>/`. Copy the shape of `bench/suites/tools/search/`.
3. Write either a `main.go` in `package main`, run with `go run ./bench/suites/<area>/<suite>`, or a `_test.go`, run with `go test ./bench/suites/<area>/<suite>`.
4. Give it an arm that turns the mechanism off, and state that arm in the report.
5. A suite that needs the network skips unless `TOFU_LIVE` is set.
6. Write the report beside the code as `report-YYYY-MM-DD.md`, with the conditions on every row.

A suite anywhere else, `bench/<suite>/` or `bench/suites/<suite>/`, is in the wrong place.

## Outputs

Dated reports (`report-*.md`), `runs/`, `results/` and `results.jsonl` are run outputs. A later ticket takes them out of git; they stay on disk. Inputs a suite reads, such as corpora, cases and hand-written answers, stay tracked.

## Retire a suite

1. Delete its folder.
2. Remove its target from `bench/cmd/` if it has one.
3. Remove its rows from the hand inputs in `bench/kit/report/`.
4. Say in the retiring ticket what the last number was and why it no longer matters.

## Imports

- bench may import `internal/` and `apps/`.
- Nothing outside bench imports bench, and nothing outside bench needs bench to build, test or run.
- Code two suites share goes in `bench/kit/`.
- `tofu doctor --imports` checks that nothing under `apps/` or `internal/` imports `bench/`, from `apps/cli/verbs/doctor/imports.go`.
