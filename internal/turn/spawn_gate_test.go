package turn

import (
	"encoding/json"
	"strings"
	"testing"

	"tofu/internal/llm"
	"tofu/internal/subagent"
)

var goDev = subagent.Definition{Name: "go-dev", Language: "go", Gate: []string{"go vet", "go test"}}

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
			Conversation: []llm.Message{{Role: llm.RoleTool, ToolCallID: "ruff", Content: printed}, {Role: llm.RoleTool, ToolCallID: "pytest", Content: "3 passed"}},
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
		{"go vet ./...; go test ./...", failed, 0, "go test failed"},
		{"go vet ./... ; go test ./... ; echo done", failed, 0, "go test failed"},
		{"gofmt -l . ; go vet . && go test . ; grep -rn CalcTotal . --include=*.go", passed, 1, ""},
		{"gofmt -l .; go vet ./... && go test ./... 2>&1; grep -rn CalcTotal .", passed, 1, ""},
		{"go test ./... 2>&1 | tail -5", failed, 0, "go vet did not run; go test failed"},
		{"go vet ./...; go test ./...", vetDiag + "\n" + passed, 0, "go vet failed"},
		{"go vet ./... ; go test ./... ; grep -rn CalcTotal .", rtkPass + "\n" + grepHit, 0, ""},
		{"go vet . ; go test . | tail -3", rtkFail, 0, "go test failed"},
		{"go vet ./...; go test ./...", "FAIL\texample.com/shop [build failed]", 1, "go test failed"},
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
			Conversation: []llm.Message{{Role: llm.RoleTool, ToolCallID: "check", Content: c.printed}},
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
		Conversation: []llm.Message{{Role: llm.RoleTool, ToolCallID: "tc", Content: content}},
	}}
}

func TestATypecheckErrorOnlyInAnotherAgentsFileDoesNotSendThisOneBack(t *testing.T) {
	project := tsProjectHolding(t, map[string]string{"web/tsconfig.json": "{}", "web/src/mine.ts": "", "web/src/theirs.ts": ""})
	tsGate := subagent.Definition{Name: "ts-dev", Language: "typescript", Gate: []string{"typecheck"}}
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
		if missed := strings.Join(gateMissed(project, nil, tsGate, owns, typecheckRound(content)), "; "); missed != want {
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
