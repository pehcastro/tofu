# internal/dummy

Shared test data, one folder per category. Data lives here only when two package trees read it, or when bench and a package under `apps` or `internal` both read it. Data one package reads stays in that package's `testdata`.

- Tests and bench read it through `dummy.Path`, rooted at the source tree.
- Product code never imports this package. Nothing under `apps` imports it.
- Every category folder carries a `PROVENANCE.md` saying where the data came from; `dummy_test.go` fails on a folder without one.
