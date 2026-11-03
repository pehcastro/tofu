# Where sample.go and sample_test.go came from

**Written**, on 2026-09-21, two files, 409 bytes, and written is right: this is a decoy set. It carries exactly three defects, one per dead-test rule, and nothing else, so a rule that fires twice or not at all is wrong rather than debatable.

The three, each asserted by name in a test beside it:

- `TestAddReturnsSomething` claims only `result == 0`, which is an existential claim. It holds a comparison operator, so the free pattern arm cannot see it and the judgment arm must. That contrast is the whole point of the file and is what `pattern_test.go:TestPatternMissesAnExistentialComparisonTheJudgmentCatches` measures.
- `TestSaveIsCalled` reassigns the package-level `Save` variable and then asserts the reassignment, a mock that tests itself.
- no test in the file passes nil, zero, empty or a limit, so the file has no boundary coverage.

A recorded corpus cannot do this. Real test files carry several defects at once and a rule firing on one of them proves nothing about which one it saw.

## Leakage: present in the names, inert today, and named here so it stays that way

**Method, run on 2026-09-23.** Both arms were read to find what they actually consume, rather than assuming. `PatternFlagsTautologicalTests` in `pattern.go` parses the file and walks the AST for comparison operators and an `err` identifier. The judgment arms call `rule.Builtins()` in `internal/rule/gotest.go`, which reads the AST and touches a function name only at `gotest.go:97`, to check the `Test` prefix. **Neither arm reads the descriptive part of a name.**

**But the names give the answers away to a reader.** `TestAddReturnsSomething` states that its only claim is existential and `TestSaveIsCalled` states that it asserts a call. **Count: 2 of 2 defective test functions name their own defect.** It costs nothing today because no arm reads the name. **The day a judged arm is pointed at these files and sees the source text, this fixture stops separating anything**, and that arm's score over it would be meaningless. Not repaired: renaming them would change a fixture, which this ticket does not do.

## What it does not carry

Scanned on 2026-09-23 for every identity and credential pattern in `bench/corpus.Scrub`: no hit.
