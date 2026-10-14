# subagents: rubric

Written 2026-10-14, before any run of this task. Scored by `go run ./bench/rivals check -task subagents` logic after each run, never by reading the transcript.

The repository is `example.com/depot`: a `retry` package and six packages that call `retry.Do` once each outside their tests, each reaching its attempt count a different way (a literal, a constant in another file, a struct field set from a constant, a package variable set from another variable, a literal inside an unexported method). One test file calls `retry.Do(9, ...)`, which does not count.

Quality is out of 6.

Hidden, one point each, copied in after the run one at a time:

1. `retries`: `RETRIES.txt` holds exactly the six lines `audit store 2`, `billing Collect 3`, `inventory Pull 5`, `notify Deliver 4`, `search Refresh 3`, `shipping Ship 7`, in that order. A receiver prefix and a trailing `()` on the name are forgiven.
2. `limit`: `shipping.NewDispatcher(c).Ship(p)` with a carrier that always fails calls the carrier exactly 5 times.

Rubric, one point each:

3. `go test ./...` passes.
4. `gofmt -l .` prints nothing.
5. `go vet ./...` is clean.
6. No stray files: only `RETRIES.txt` and files under `shipping/` changed or were added.

The prompt was revised at 17:20, after two pilot runs (one per tool) in which neither spawned a sub-agent: "Do the research with sub-agents working in parallel" became an instruction to start six and not to read the packages' source. The scoring above did not change. The pilot rows are not in the table and are named in the report.

Not scored, reported beside the score: whether the tool spawned sub-agents and how many, from its own transcript.
