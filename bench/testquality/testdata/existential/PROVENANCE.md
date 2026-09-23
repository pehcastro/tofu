# Where sample.go and sample_test.go came from

**Written**, on 2026-09-21, two files, 236 bytes, and written is right: it is a single-defect decoy, the other half of the pair with `../fixture`.

`TestExists` calls `require.NotNil` and nothing else. It has no comparison operator and no `err`, so the free pattern arm must flag it, and `pattern_test.go:TestPatternFlagsATestWithNoComparisonAndNoErrorCheck` asserts that it flags exactly this function by name. The file also passes no nil, zero, empty or limit argument, so it has no boundary coverage: `deadtest_test.go` asserts a total of 2 over it, one of each.

Where `../fixture` proves the pattern arm misses something the judgment arm catches, this one proves the pattern arm catches something at all. **Neither file means anything without the other.**

A recording cannot produce this. A real test file with one defect and no others is a thing nobody writes by accident.

## Leakage: present in the name, inert today

**Method, run on 2026-09-23**, the same as for `../fixture`: both arms were read to find what they consume. `pattern.go` walks the AST for comparison operators and an `err` identifier. The judgment arms in `internal/rule/gotest.go` read the AST and touch a function name only at `gotest.go:97`, for the `Test` prefix. **Neither reads the descriptive part of a name.**

**Count: 1 of 1 defective test functions names its own defect.** `TestExists` says its claim is existence, and the package is called `existential`. Inert while every arm is static, live the moment a judged arm reads the source text. Not repaired here.

## What it does not carry

Scanned on 2026-09-23 for every identity and credential pattern in `bench/corpus.Scrub`: no hit.
