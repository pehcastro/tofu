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
