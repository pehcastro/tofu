---
id: go-traps
domain: dev
document: cc-skills-golang golang-safety and the common-go-bugs reference of golang-troubleshooting; golang-skills go-defensive and go-code-review; each check named here confirmed against go tool vet help, golangci-lint help linters and staticcheck's check list
found: local clones of the two collections, kept outside this repository
---

# Traps the compiler accepts

Each of these builds, usually passes a happy-path test, and fails later. The check that finds it, when one exists, is named; where none is, a test that drives the failing case is the only proof.

## Nil

| Trap | What happens | Instead |
| --- | --- | --- |
| a nil `*T` returned as `error` or another interface | `!= nil` is true, the caller takes the failure path | return the literal `nil` |
| write to a nil map | panic | `make` it, or create it lazily in the method that writes |
| method on a nil pointer field never set by a constructor | panic at the first call | set it in the constructor, or make the zero value usable |
| call through a nil func value | panic | check it, or default it to a no-op |
| send or receive on a nil channel | blocks forever | create it before the goroutine starts |
| `x.(T)` on the wrong type | panic | `v, ok := x.(T)`; `forcetypeassert` |

Reading a nil map, ranging over a nil slice or map, and `len` of either are all safe and need no check.

## Slices, maps and strings

- `append` writes into the backing array when it has room, so a sub-slice appended to overwrites its parent. Cap it, `s[:n:n]`, or `slices.Clone` it.
- A method returning an internal slice or map hands the caller a way to change it. Return a clone.
- A small slice of a large one keeps the whole array alive. Clone the part you keep.
- Map iteration order changes run to run. Sort the keys wherever order reaches output or a test: `slices.Sorted(maps.Keys(m))` from Go 1.23, a collected and sorted key slice below it.
- `len(s)` counts bytes and `s[i]` is a byte. Count characters with `utf8.RuneCountInString`, and range over the string to walk them.
- `strings.Trim(s, "abc")` strips any of those characters from both ends. To remove a prefix or suffix, `strings.TrimPrefix` and `strings.TrimSuffix`.

## Numbers and time

- A narrowing or sign-changing conversion wraps silently. Bounds-check input first; `gosec` reports it as `G115`.
- Integer division by zero panics; float division gives `Inf` or `NaN`. Check the divisor.
- Floats are compared within a tolerance, never with `==`.
- `time.Time` values are compared with `Equal`, `Before` and `After`: `==` also compares the location and the monotonic reading.
- An `iota` enum starting at the meaningful first value makes the zero value of every field look set. Start at an `Unknown` value.

## Control flow

- `:=` inside a block declares a new `err` that hides the outer one, and the outer one returns nil. Assign with `=` when the outer variable is meant. govet's `shadow` analyzer in golangci-lint reports it; plain `go vet` does not run it.
- `defer` in a loop runs at function return. Move the body into a function.
- A deferred call's arguments are evaluated at the `defer` line.
- `os.Exit` and `log.Fatal` skip every deferred call. Return an error to `main` and exit there.
- `fallthrough` runs the next case's body without testing its condition.
- After `http.Error` the handler keeps going. Return on the next line.

## Resources

- `sql.Rows` is closed with `defer rows.Close()` right after the error check, and `rows.Err()` is checked after the loop; `rowserrcheck` and `sqlclosecheck` report each.
- An HTTP response body is closed after reading, error status included; `bodyclose` reports a body left open.
- A file you wrote is closed with its error checked, because the final write can fail there.

## JSON

- A number decoded into `any` is `float64`. Decode into a typed struct, or call `UseNumber` on the decoder.
- An unexported field is skipped without an error in both directions. vet's `structtag` reports a json tag on one.
- A missing field leaves the zero value. Use a pointer field when absent and zero must differ.

## Version-dependent

The go.mod `go` line decides what exists. `for i := range n` over an integer and per-iteration loop variables need 1.22; `slices`, `maps`, `min`, `max` and `clear` need 1.21; `os.Root` and `b.Loop()` need 1.24; `sync.WaitGroup.Go` needs 1.25. vet's `stdversion` reports a too-new standard library symbol but not new syntax.
