package tools_test

import (
	"context"
	"encoding/json"
	"go/parser"
	"go/token"
	"path/filepath"
	"strconv"
	"strings"
	"testing"

	"tofu/internal/judge/ledger"
	"tofu/internal/konst"
	"tofu/internal/recall"
	"tofu/internal/sys"
	"tofu/internal/turn/tools"
)

func recordOneDecision(t *testing.T, root string) ledger.Row {
	t.Helper()
	home := t.TempDir()
	t.Setenv("HOME", home)
	t.Setenv("USERPROFILE", home)
	state, err := sys.ProjectStateDirAt(root)
	if err != nil {
		t.Fatal(err)
	}
	writer := ledger.NewWriter(filepath.Join(state, "log"))
	row, err := writer.Append(ledger.Row{
		Point:         "tool_gate",
		Questions:     "tool_gate",
		Version:       3,
		Build:         "typesafe/jev-1.13-20260917",
		Model:         "~typesafe/jev-latest",
		State:         []byte(`{"agent":"tofu","input":{"command":"ls"},"tool":"bash"}`),
		Policy:        "tool_gate",
		PolicyVersion: 3,
		Verdict:       ledger.VerdictAllow,
		Answers: []ledger.Answer{
			{Question: "risk", Wording: 1, Kind: ledger.AnswerScore, Score: 0, Dist: []ledger.Slice{
				{Option: "0", P: 1}, {Option: "1", P: 0}, {Option: "2", P: 0}, {Option: "3", P: 0},
			}},
			{Question: "approval", Wording: 1, Kind: ledger.AnswerNoul, Noul: 0.9},
			{Question: "user_requested", Wording: 1, Kind: ledger.AnswerNoul, Noul: 0.05},
			{Question: "from_untrusted", Wording: 1, Kind: ledger.AnswerNoul, Noul: 0.02},
		},
		Reason: &ledger.Reason{
			Question:   "risk",
			Comparison: "risk_ask_at",
			Threshold:  1.5,
			Value:      0,
			Mode:       ledger.ModeEnforced,
		},
		LatencyMS: 503,
		Cost:      3.7842e-05,
		RequestID: "gen-dec-stub",
	})
	if err != nil {
		t.Fatalf("recording the fixture decision: %v", err)
	}
	return row
}

func TestTheWhyToolReturnsTheChainBehindARecordedDecision(t *testing.T) {
	standInForwardsToARealTofu(t)
	root := t.TempDir()
	recorded := recordOneDecision(t, root)

	result, err := verbTool(t, root, "tofu_why").Run(context.Background(),
		json.RawMessage(`{"id":`+strconv.Quote(recorded.ID)+`}`))
	if err != nil {
		t.Fatalf("running the why tool: %v", err)
	}
	t.Logf("tofu_why %s printed:\n%s", recorded.ID, result.Content)
	if result.ExitCode == nil || *result.ExitCode != 0 {
		t.Fatalf("tofu why did not explain a recorded id: %v", result.ExitCode)
	}
	if result.Command != "tofu why "+recorded.ID {
		t.Fatalf("the row does not name the verb that ran: %q", result.Command)
	}
	for _, want := range []string{"risk", "approval", "from_untrusted", "ALLOW", "1.5", `"command":"ls"`} {
		if !strings.Contains(result.Content, want) {
			t.Fatalf("the chain does not carry %q: %q", want, result.Content)
		}
	}
}

func TestTheReplayToolReturnsTheChangedVerdictCountForAMovedThreshold(t *testing.T) {
	standInForwardsToARealTofu(t)
	root := t.TempDir()
	recordOneDecision(t, root)

	result, err := verbTool(t, root, "tofu_replay").Run(context.Background(),
		json.RawMessage(`{"point":"tool_gate","set":"risk_ask_at=-1"}`))
	if err != nil {
		t.Fatalf("running the replay tool: %v", err)
	}
	t.Logf("tofu_replay printed:\n%s", result.Content)
	if result.ExitCode == nil || *result.ExitCode != 0 {
		t.Fatalf("tofu replay did not rescore: %v", result.ExitCode)
	}
	if result.Command != "tofu replay --point tool_gate --set risk_ask_at=-1" {
		t.Fatalf("the set argument did not reach the verb as its own flag: %q", result.Command)
	}
	if !strings.Contains(result.Content, "1 rows read, 1 rescored") {
		t.Fatalf("the recorded row was not rescored: %q", result.Content)
	}
	if !strings.Contains(result.Content, "verdict changes: 1") {
		t.Fatalf("the moved threshold changed no verdict, so the tool answers nothing: %q", result.Content)
	}
	if !strings.Contains(result.Content, "0 API calls") {
		t.Fatalf("the result does not state that nothing was spent: %q", result.Content)
	}
}

func TestTheReplayToolSplitsEveryThresholdIntoItsOwnFlag(t *testing.T) {
	result, err := verbTool(t, t.TempDir(), "tofu_replay").Run(context.Background(),
		json.RawMessage(`{"point":"tool_gate","set":"risk_ask_at=1.2, risk_deny_at=2.8"}`))
	if err != nil {
		t.Fatalf("running the replay tool: %v", err)
	}
	t.Logf("result: %s", strings.TrimSpace(result.Content))
	if result.Command != "tofu replay --point tool_gate --set risk_ask_at=1.2 --set risk_deny_at=2.8" {
		t.Fatalf("two thresholds did not become two flags: %q", result.Command)
	}
}

func TestTheRecordToolsCarryNoWireInTheirImportGraph(t *testing.T) {
	file, err := parser.ParseFile(token.NewFileSet(), "verb.go", nil, parser.ImportsOnly)
	if err != nil {
		t.Fatalf("parsing verb.go: %v", err)
	}
	var seen []string
	for _, imported := range file.Imports {
		path := strings.Trim(imported.Path.Value, `"`)
		seen = append(seen, path)
		for _, wire := range []string{"jev", "transport", "net/http", "openrouter"} {
			if strings.Contains(path, wire) {
				t.Fatalf("verb.go imports %q: a tool that reads the record must never be able to reach a wire", path)
			}
		}
	}
	t.Logf("verb.go imports %s", strings.Join(seen, ", "))
}

func TestTheRecordToolsRefuseAtTheSameDepthBoundAsTheOthers(t *testing.T) {
	root := t.TempDir()
	calls := map[string]string{
		"tofu_why":    `{"id":"20260918T192434Z-3f2a"}`,
		"tofu_replay": `{"point":"tool_gate"}`,
	}
	for name, arguments := range calls {
		t.Setenv(depthEnvar, strconv.Itoa(konst.VerbMaxDepth-1))
		if _, err := verbTool(t, root, name).Run(context.Background(), json.RawMessage(arguments)); err != nil {
			t.Fatalf("%s one below the bound has to run: %v", name, err)
		}

		t.Setenv(depthEnvar, strconv.Itoa(konst.VerbMaxDepth))
		_, err := verbTool(t, root, name).Run(context.Background(), json.RawMessage(arguments))
		if err == nil {
			t.Fatalf("%s went through the bound", name)
		}
		t.Logf("%s refusal: %v", name, err)
		if !strings.Contains(err.Error(), "already nested "+strconv.Itoa(konst.VerbMaxDepth)+" deep") {
			t.Fatalf("%s refused for some other reason: %v", name, err)
		}
	}
}

func TestAMissingRequiredParameterIsNamedRatherThanDumpedAsUsage(t *testing.T) {
	wanted := map[string]string{"tofu_why": "id", "tofu_replay": "point"}
	for name, parameter := range wanted {
		_, err := verbTool(t, t.TempDir(), name).Run(context.Background(), json.RawMessage(`{}`))
		if err == nil {
			t.Fatalf("%s ran without %s", name, parameter)
		}
		t.Logf("%s refusal: %v", name, err)
		if !strings.Contains(err.Error(), parameter+" is required") {
			t.Fatalf("%s does not name the parameter it wants: %v", name, err)
		}
		if strings.Count(err.Error(), "\n") > 0 {
			t.Fatalf("%s answered with more than one line, which is a usage dump: %v", name, err)
		}
	}
}

func TestTheTwoRecordToolsAddAMeasuredCostToEveryRequest(t *testing.T) {
	cfg, err := recall.LoadConfig()
	if err != nil {
		t.Fatalf("loading the elide library: %v", err)
	}
	verbs, err := tools.NewVerbs(t.TempDir())
	if err != nil {
		t.Fatalf("building the verb tools: %v", err)
	}
	if len(verbs) != 6 {
		t.Fatalf("expected six verb tools, got %d", len(verbs))
	}
	var allBytes, allTokens, addedBytes, addedTokens int
	for _, verb := range verbs {
		encoded, err := json.Marshal(verb.Definition())
		if err != nil {
			t.Fatalf("encoding %s: %v", verb.Name(), err)
		}
		allBytes += len(encoded)
		allTokens += cfg.Tokens(string(encoded))
		if verb.Name() == "tofu_why" || verb.Name() == "tofu_replay" {
			addedBytes += len(encoded)
			addedTokens += cfg.Tokens(string(encoded))
			t.Logf("%s: %d bytes, %d tokens", verb.Name(), len(encoded), cfg.Tokens(string(encoded)))
		}
	}
	t.Logf("six verb tools are %d bytes and %d tokens; tofu_why and tofu_replay add %d bytes and %d tokens at %d bytes per thousand tokens",
		allBytes, allTokens, addedBytes, addedTokens, cfg.BytesPerThousandTokens)
	if addedTokens == 0 {
		t.Fatal("the two new tools measured as nothing, so the names they are matched on are wrong")
	}
}
