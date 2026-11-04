package loop

import (
	"encoding/json"
	"fmt"
	"strings"
	"testing"

	"tofu/internal/agent/subagents"
	"tofu/internal/models/llm/core"
)

var goDev = subagents.Definition{Name: "go-dev", Language: "go", Gate: []string{"go vet", "go test"}}

func TestAGateRunThroughRtkCountsAsTheCommandItWraps(t *testing.T) {
	reopenedFor(t, gatedRun(t, "", goDev, edited("src/x.go"), bashed("rtk go test ./pkg/", 0), bashed("rtk go vet ./pkg/", 0), claimDecision("done")).rows, 1)
	reopenedFor(t, gatedRun(t, "", pyDev, edited("src/x.py"), bashed("uv run rtk pytest -q", 0), bashed("rtk proxy uv run ruff check .", 0), claimDecision("done")).rows, 1)
	reopenedFor(t, gatedRun(t, "", goDev,
		edited("src/x.go"), bashed("rtk go test ./pkg/", 1), bashed("rtk go vet ./pkg/", 0), claimDecision("done"),
		bashed("rtk go test ./pkg/", 0), claimDecision("done")).rows, 2, "go test exited 1")
}

func TestTheProjectsOwnTypecheckAndTestScriptsCountThroughBash(t *testing.T) {
	scripted := tsProjectHolding(t, map[string]string{"package.json": `{"scripts":{"typecheck":"vue-tsc --noEmit","test":"vitest run"}}`})
	reopenedFor(t, gatedRun(t, scripted, tsDev, edited("src/a.ts"), bashed("rtk npm run typecheck", 0), bashed("pnpm test", 0), claimDecision("done")).rows, 1)
	reopenedFor(t, gatedRun(t, scripted, tsDev,
		edited("src/a.ts"), bashed("npm run typecheck", 2), bashed("bun test", 0), bashed("npx vue-tsc --noEmit", 0), claimDecision("done"),
		called("typecheck", map[string]any{}), called("test", map[string]any{}), claimDecision("done")).rows, 2, "typecheck exited 2", "test did not run")
}

func TestAGateCheckerThatIsNotInstalledIsNotAPass(t *testing.T) {
	exit := 0
	round := func(printed string) []Row {
		return []Row{{
			Steps: []StepRow{{ToolCalls: []ToolCallRow{
				{Tool: "edit", Args: json.RawMessage(`{"path":"src/x.py"}`)},
				{Call: "ruff", Tool: "bash", Command: "uv run ruff check src | tail -5", ExitCode: &exit},
				{Call: "pytest", Tool: "bash", Command: "uv run pytest", ExitCode: &exit},
			}}},
			Conversation: []core.Message{{Role: core.RoleTool, ToolCallID: "ruff", Content: printed}, {Role: core.RoleTool, ToolCallID: "pytest", Content: "3 passed"}},
		}}
	}
	const missing = "ruff check could not run: its checker is not installed"
	for printed, want := range map[string]string{
		"error: Failed to spawn: `ruff`\n  Caused by: No module named ruff": missing,
		"bash: ruff: command not found":                                     missing,
		"'ruff' is not recognized as an internal or external command":       missing,
		"All checks passed!": "",
	} {
		if missed := strings.Join(gateMissed("", nil, pyDev, nil, round(printed)), "; "); missed != want {
			t.Errorf("%q: the gate said %q, want %q", printed, missed, want)
		}
	}
}

func TestEachGoCheckInAChainedCommandIsJudgedByWhatItPrinted(t *testing.T) {
	const (
		failed  = "--- FAIL: TestFails (0.00s)\n    x_test.go:8: got 1\nFAIL\nFAIL\texample.com/x\t0.459s\nFAIL"
		passed  = "ok  \texample.com/shop\t0.518s"
		vetDiag = "x.go:5:41: fmt.Sprintf format %d has arg \"a\" of wrong type string"
		rtkFail = "Go test: 1 passed, 1 failed in 1 packages\n\nx (1 passed, 1 failed)\n  [FAIL] TestFails\n     x_test.go:8: got 1"
		rtkPass = "Go test: 11 passed in 1 packages"
		grepHit = "./cart.go:12:func (c Cart) CalcTotal() Money {"
	)
	for _, c := range []struct {
		command, printed string
		exit             int
		want             string
	}{
		{"go vet ./...; go test ./...", failed, 0, "go test failed: --- FAIL: TestFails (0.00s)"},
		{"go vet ./... ; go test ./... ; echo done", failed, 0, "go test failed: --- FAIL: TestFails (0.00s)"},
		{"gofmt -l . ; go vet . && go test . ; grep -rn CalcTotal . --include=*.go", passed, 1, ""},
		{"gofmt -l .; go vet ./... && go test ./... 2>&1; grep -rn CalcTotal .", passed, 1, ""},
		{"go test ./... 2>&1 | tail -5", failed, 0, "go vet did not run; go test failed: --- FAIL: TestFails (0.00s)"},
		{"go vet ./...; go test ./...", vetDiag + "\n" + passed, 0, "go vet failed: " + vetDiag},
		{"go vet ./... ; go test ./... ; grep -rn CalcTotal .", rtkPass + "\n" + grepHit, 0, ""},
		{"go vet . ; go test . | tail -3", rtkFail, 0, "go test failed: [FAIL] TestFails"},
		{"go vet ./...; go test ./...", "FAIL\texample.com/shop [build failed]", 1, "go test failed: FAIL\texample.com/shop [build failed]"},
		{"go vet ./... && go test ./...", "--- FAIL: TestX (0.00s)\r\nFAIL\r\n", 1, "go test failed: --- FAIL: TestX (0.00s)"},
		{"go vet ./...", "./x.go:3:1: syntax error\nthe command exited 1\n", 1, "go vet exited 1: ./x.go:3:1: syntax error; go test did not run"},
		{"go vet ./...", "the command exited 1\n", 1, "go vet exited 1; go test did not run"},
		{"go vet ./...; go test ./...", "", 1, "go vet exited 1; go test exited 1"},
		{"make lint && go vet ./... && go test ./...", "", 2, "go vet exited 2; go test exited 2"},
		{"go vet ./... && go test ./...", "", 0, ""},
		{"go test ./...", passed, 1, "go vet did not run; go test exited 1"},
		{"go vet ./...", "", 1, "go vet exited 1; go test did not run"},
	} {
		exit := c.exit
		rows := []Row{{
			Steps: []StepRow{{ToolCalls: []ToolCallRow{
				{Tool: "edit", Args: json.RawMessage(`{"path":"cart.go"}`)},
				{Call: "check", Tool: "bash", Command: c.command, ExitCode: &exit},
			}}},
			Conversation: []core.Message{{Role: core.RoleTool, ToolCallID: "check", Content: c.printed}},
		}}
		if missed := strings.Join(gateMissed("", nil, goDev, nil, rows), "; "); missed != c.want {
			t.Errorf("%q exit %d printing %q: the gate said %q, want %q", c.command, c.exit, c.printed, missed, c.want)
		}
	}
}

func typecheckRound(content string) []Row {
	call := ToolCallRow{Call: "tc", Tool: "typecheck", Command: "web/src", Args: json.RawMessage(`{"path":"web/src"}`), Error: "typecheck: tsc, errors in src: 1, in other files: 0"}
	return []Row{{
		Steps:        []StepRow{{ToolCalls: []ToolCallRow{{Tool: "edit", Args: json.RawMessage(`{"path":"web/src/mine.ts"}`)}, call}}},
		Conversation: []core.Message{{Role: core.RoleTool, ToolCallID: "tc", Content: content}},
	}}
}

func TestATypecheckErrorOnlyInAnotherAgentsFileDoesNotSendThisOneBack(t *testing.T) {
	project := tsProjectHolding(t, map[string]string{"web/tsconfig.json": "{}", "web/src/mine.ts": "", "web/src/theirs.ts": ""})
	tsGate := subagents.Definition{Name: "ts-dev", Language: "typescript", Gate: []string{"typecheck"}}
	owns := []string{"web/src/mine.ts"}
	head := "typecheck: tsc, errors in src: 1, in other files: 0:\n"
	for content, want := range map[string]string{
		head + "src/theirs.ts(3,5): error TS2322: Type 'string' is not assignable to type 'number'.\n  more about it": "",
		head + "src/theirs.svelte.test.ts:19:68: Object literal may only specify known properties":                    "",
		head + "src/mine.ts(3,5): error TS2322: Type 'string' is not assignable to type 'number'.":                    "typecheck failed",
		head + "src/theirs.ts(1,1): error TS1: x\nsrc/mine.ts:2:1: broken":                                            "typecheck failed",
		head + "error TS5083: Cannot read file 'tsconfig.base.json'.":                                                 "typecheck failed",
		head + "src/theirs.ts(1,1): error TS1: x\n(4 more lines not shown)":                                           "typecheck failed",
	} {
		if missed := strings.Join(gateMissed(project, nil, tsGate, owns, typecheckRound(content)), "; "); !strings.HasPrefix(missed, want) || (missed == "") != (want == "") {
			t.Errorf("%q: the gate said %q, want %q", content, missed, want)
		}
	}
	lost := typecheckRound("")
	lost[0].Conversation = nil
	if missed := gateMissed(project, nil, tsGate, owns, lost); len(missed) != 1 {
		t.Errorf("a typecheck failure whose result is gone passed the gate: %q", missed)
	}
	if missed := gateMissed(project, nil, tsGate, nil, typecheckRound(head+"src/theirs.ts(1,1): error TS1: x")); len(missed) != 1 {
		t.Errorf("an agent that owns nothing passed on another file's error: %q", missed)
	}
}

type cargoRun struct {
	command, printed string
	exit             int
}

func cargoRound(edit string, runs ...cargoRun) []Row {
	row := Row{Steps: []StepRow{{ToolCalls: []ToolCallRow{{Tool: "edit", Args: json.RawMessage(`{"path":"` + edit + `"}`)}}}}}
	for i, run := range runs {
		call := fmt.Sprint("cargo-", i)
		row.Steps[0].ToolCalls = append(row.Steps[0].ToolCalls, ToolCallRow{Call: call, Tool: "bash", Command: run.command, ExitCode: &run.exit})
		row.Conversation = append(row.Conversation, core.Message{Role: core.RoleTool, ToolCallID: call, Content: run.printed})
	}
	return []Row{row}
}

func TestACargoGateReadsWhatClippyAndTestPrinted(t *testing.T) {
	const (
		clean     = "    Checking shop v0.1.0\n    Finished `dev` profile in 0.4s"
		ranNone   = "     Running unittests src/lib.rs (target/debug/deps/shop-1)\n\nrunning 0 tests\n\ntest result: ok. 0 passed; 0 failed; 0 ignored; 0 measured; 3 filtered out\n\n   Doc-tests shop\n\nrunning 0 tests\n\ntest result: ok. 0 passed; 0 failed"
		ranSome   = "running 0 tests\n\ntest result: ok. 0 passed\n\n     Running tests/cart.rs\n\nrunning 10 tests\n\ntest result: ok. 10 passed"
		ranOne    = "running 1 test\ntest tests::adds ... ok\n\ntest result: ok. 1 passed"
		theirs    = "error[E0308]: mismatched types\n --> crates/other/src/lib.rs:4:5\n  |\nerror: could not compile `other` (lib) due to 1 previous error"
		mineBroke = "error[E0308]: mismatched types\n --> src/lib.rs:4:5\n  |\nerror: could not compile `shop` (lib) due to 1 previous error"
		unplaced  = "error: failed to parse manifest at `Cargo.toml`"
		deadMine  = "warning: function `orphan` is never used\n --> src\\lib.rs:7:4\n  |\n7 | fn orphan() {}\n  |    ^^^^^^\n  = note: `#[warn(dead_code)]` on by default\nwarning: `shop` (lib) generated 1 warning\n" + clean
		deadTheir = "warning: unused import: `std::fmt`\n --> crates/other/src/lib.rs:1:5\n" + clean
		testBroke = "running 1 test\ntest tests::adds ... FAILED\n\nfailures:\n\n---- tests::adds stdout ----\nthread 'tests::adds' panicked at src/lib.rs:5:9:\n\ntest result: FAILED. 0 passed; 1 failed\n\nerror: test failed, to rerun pass `--lib`"
	)
	project := tsProjectHolding(t, map[string]string{
		"Cargo.toml":                    "[workspace]\nmembers = [\"crates/*\"]\n",
		"crates/bare/Cargo.toml":        "[package]\nname = \"bare\"\n",
		"crates/bare/src/lib.rs":        "pub fn a() {}\n",
		"crates/bare/target/debug/x.rs": "#[test]\nfn built() {}\n",
		"crates/unit/Cargo.toml":        "[package]\nname = \"unit\"\n",
		"crates/unit/src/lib.rs":        "#[cfg(test)]\nmod tests {\n    #[test]\n    fn adds() {}\n}\n",
		"crates/integ/Cargo.toml":       "[package]\nname = \"integ\"\n",
		"crates/integ/src/lib.rs":       "pub fn a() {}\n",
		"crates/integ/tests/cart.rs":    "",
		"crates/shop/Cargo.toml":        "[package]\nname = \"shop\"\n",
		"crates/shop/src/lib.rs":        "#[tokio::test]\nasync fn adds() {}\n",
		"crates/other/Cargo.toml":       "[package]\nname = \"other\"\n",
		"crates/other/src/lib.rs":       "",
	})
	owns := []string{"crates/bare/**", "crates/unit/**", "crates/integ/**", "crates/shop/**"}
	clippy, tested := cargoRun{"cargo clippy", clean, 0}, cargoRun{"cargo test", ranOne, 0}
	for _, c := range []struct {
		name, edit string
		runs       []cargoRun
		want       string
	}{
		{"a crate with no test needs no cargo test", "crates/bare/src/lib.rs", []cargoRun{clippy}, ""},
		{"a #[test] in src needs cargo test", "crates/unit/src/lib.rs", []cargoRun{clippy}, "cargo test did not run"},
		{"a tests dir needs cargo test", "crates/integ/src/lib.rs", []cargoRun{clippy}, "cargo test did not run"},
		{"a tokio test needs cargo test", "crates/shop/src/lib.rs", []cargoRun{clippy}, "cargo test did not run"},
		{"0 tests in every target is not a pass", "crates/unit/src/lib.rs", []cargoRun{clippy, {"cargo test -p unit nothing", ranNone, 0}}, "cargo test ran 0 tests"},
		{"0 tests in one target and 10 in another passes", "crates/unit/src/lib.rs", []cargoRun{clippy, {"cargo test", ranSome, 0}}, ""},
		{"1 test passes", "crates/unit/src/lib.rs", []cargoRun{clippy, tested}, ""},
		{"0 tests in a crate with none is not a miss", "crates/bare/src/lib.rs", []cargoRun{clippy, {"cargo test", ranNone, 0}}, ""},
		{"clippy failing only in another crate is excused", "crates/shop/src/lib.rs", []cargoRun{{"cargo clippy", theirs, 101}, tested}, ""},
		{"clippy failing in an owned file is a miss", "crates/shop/src/lib.rs", []cargoRun{{"cd crates/shop && cargo clippy", mineBroke, 101}, tested}, "cargo clippy exited 101: error[E0308]: mismatched types"},
		{"clippy failing with no file is a miss", "crates/shop/src/lib.rs", []cargoRun{{"cargo clippy", unplaced, 101}, tested}, "cargo clippy exited 101: " + unplaced},
		{"cargo test piped into tail that printed a failure is a miss", "crates/unit/src/lib.rs", []cargoRun{clippy, {"cargo test 2>&1 | tail -5", testBroke, 0}}, "cargo test failed: test tests::adds ... FAILED"},
		{"cargo test piped into tail that passed is not a miss", "crates/unit/src/lib.rs", []cargoRun{clippy, {"cargo test 2>&1 | tail -5", ranOne, 0}}, ""},
		{"dead code in an owned file names the item and its line", "crates/shop/src/lib.rs", []cargoRun{{"cd crates/shop && cargo clippy", deadMine, 0}, tested}, "cargo clippy warned function `orphan` is never used at src\\lib.rs:7"},
		{"an unused import in another crate is not this agent's", "crates/shop/src/lib.rs", []cargoRun{{"cargo clippy", deadTheir, 0}, tested}, ""},
	} {
		missed := strings.Join(gateMissed(project, nil, rustDev, owns, cargoRound(c.edit, c.runs...)), "; ")
		if !strings.HasPrefix(missed, c.want) || (c.want == "") != (missed == "") {
			t.Errorf("%s: the gate said %q, want %q", c.name, missed, c.want)
		}
	}
	if missed := gateMissed(project, nil, rustDev, nil, cargoRound("crates/shop/src/lib.rs", cargoRun{"cargo clippy", theirs, 101}, tested)); len(missed) != 1 {
		t.Errorf("an agent that owns nothing passed on another crate's error: %q", missed)
	}
	if exempt := gateExemptions(project, rustDev, cargoRound("crates/bare/src/lib.rs")); !strings.Contains(exempt["cargo test"], "no #[test] and no tests") {
		t.Errorf("a crate with no tests is not said to be exempt: %q", exempt)
	}
}
