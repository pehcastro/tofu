# bigread: rubric

Written 2026-10-14, before any run of this task.

The repository is `example.com/atlas`: 12 hand-written files in 10 packages hold the path from `api.HandleOrder` to the queue, and 60 generated packages (`sea` in `task.json`, built by `sea.go` at stage time, the same bytes every run) surround it. Every generated package has its own `Pick` method, its own `X-Priority` header handling and its own queue names; every sixth one has a `Lane.Pick(intake.Order)` that satisfies the same interface as the answer. In the chain itself there are three decoys: `route.legacy.Pick`, which returns `orders-priority` and is reached only from `/quotes` and `cmd/replay`; `classify.FromQuery`, which maps `high` to bulk; and `dispatch.Fixed` and `dispatch.ByRegion`.

Quality is out of 6.

Hidden, one point each:

1. `function`: the first line of `ANSWER.txt` is `route.byClass.Pick`, with parentheses, `*`, backticks and spaces forgiven.
2. `queue`: `ANSWER.txt` has exactly two lines and the second is `orders-express`, with quotes forgiven.

Rubric, one point each:

3. `go test ./...` passes.
4. `gofmt -l .` prints nothing.
5. `go vet ./...` is clean.
6. No stray files: only `ANSWER.txt` was added.
