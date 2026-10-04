---
id: go-verify
domain: dev
document: checking Go code: go vet, golangci-lint and go test, their commands, flags and analyzers
---

# The Go gate and where it bends

Read the project's gate before using this one. A Makefile, a Taskfile, a magefile or a CI workflow that names a lint command, build tags or a test runner wins: it is what the project's own review runs.

## Each edit

```sh
gofmt -l <files>
go vet ./<pkg>
```

`gofmt -l` prints the name of every file that is not formatted and exits 0 either way, so the check is that it prints nothing; `gofmt -w <files>` fixes the files you changed. When the project uses `goimports` or `gofumpt`, its config or Makefile says so and that tool replaces gofmt. `go vet` type-checks the package and its tests, so it fails on anything that does not build, then runs every analyzer `go tool vet help` lists. Use the package path, never `./...` after a one-file edit: the whole module takes longer and reports what other people broke.

## Before done

```sh
golangci-lint run ./<pkg>/...
go test -run '<Name>' ./<pkg>
go test -race -run '<Name>' ./<pkg>
```

golangci-lint reads `.golangci.yml` from the project root. With no config it runs only its defaults, `errcheck`, `govet`, `ineffassign`, `staticcheck` and `unused`, and a linter the project never enabled is not part of its gate. On a project with a backlog of findings, `--new-from-rev=HEAD` reports only lines your change touched. Confirm a linter exists with `golangci-lint help linters` before naming it, and never run it with `--fix` unasked: read what it found and change the code yourself.

`-run` takes a regular expression matched against test names, so `-run '^TestParse$'` runs one test and `-run 'TestParse/empty'` one subtest. `-count=1` defeats the test cache when a test reads a file the cache does not track. A package with no test files prints `no test files` and passes, which proves nothing: say so.

`-race` needs cgo and a C compiler. Where `CGO_ENABLED=0` or no compiler is installed, the race run fails to build; report that it did not run rather than dropping it. It finds a race only on a path the test reaches, so a race fix needs a test that runs both sides at once.

## Once at the end

- The package's whole test run: `go test ./<pkg>/...`.
- After a go.mod change: `go mod tidy`, then `git diff --exit-code go.mod go.sum`. A diff means the module files were out of step; keep the tidy result only when the change meant to add or drop a dependency.
- After a go.mod change: `govulncheck ./...` when it is installed. It reports a vulnerable function only when the code calls it.
- Build tags: when the change sits behind `//go:build`, vet and test once with the tag set, `-tags <tag>`, and once without.

Each of these needs a tool the machine may not have. When one is missing, say the step was not run; never report it as passed.

## Reading a failure

Read the first error, not the last: the rest often follow from it. `go vet` names the analyzer in its message; `go tool vet help <name>` explains it with an example. staticcheck codes such as `SA4023` are explained on its checks page. Fix one thing, check again, and stop after three attempts at the same one: report it with the output rather than reshaping the design to get past it.
