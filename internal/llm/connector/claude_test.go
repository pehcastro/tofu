package connector

import (
	"context"
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"boji/internal/konst"
	"boji/internal/llm"
	"boji/internal/transport"
)

func TestMain(m *testing.M) {
	if os.Getenv("BOJI_CONNECTOR_SILENT_CHILD") != "" {
		time.Sleep(10 * time.Second)
		os.Exit(0)
	}
	os.Exit(m.Run())
}

func readTestdata(t *testing.T, name string) Transcript {
	t.Helper()
	file, err := os.Open("testdata/" + name)
	if err != nil {
		t.Fatalf("opening the recorded stream: %v", err)
	}
	defer func() { _ = file.Close() }()
	transcript, err := ReadTranscript(file)
	if err != nil {
		t.Fatalf("reading the recorded stream: %v", err)
	}
	return transcript
}

func TestEachRecordedStreamMapsToItsOwnStopReason(t *testing.T) {
	cases := []struct {
		file string
		want StopReason
	}{
		{"model_stopped.jsonl", StopModelStopped},
		{"cap_reached.jsonl", StopCapReached},
		{"permission_denied.jsonl", StopPermissionDenied},
		{"quota_closed.jsonl", StopQuotaClosed},
		{"process_failed.jsonl", StopProcessFailed},
	}
	seen := make(map[StopReason]string, len(cases))
	for _, test := range cases {
		got := readTestdata(t, test.file).Stop
		if got != test.want {
			t.Fatalf("%s mapped to %s, wanted %s", test.file, got, test.want)
		}
		if other, taken := seen[got]; taken {
			t.Fatalf("%s and %s both map to %s", other, test.file, got)
		}
		seen[got] = test.file
	}
	if len(seen) != 5 {
		t.Fatalf("expected five distinct stop reasons, got %d", len(seen))
	}
}

func TestACapReachedStreamCarriesTheCapThatWasHit(t *testing.T) {
	transcript := readTestdata(t, "cap_reached.jsonl")
	if !strings.Contains(transcript.Detail, "maximum budget") {
		t.Fatalf("the cap detail is %q", transcript.Detail)
	}
}

func TestAModelStoppedStreamBecomesAToolCallWithNoActualSpend(t *testing.T) {
	decision, err := decide(readTestdata(t, "model_stopped.jsonl"), time.Second, 100)
	if err != nil {
		t.Fatalf("deciding: %v", err)
	}
	if decision.Outcome != llm.OutcomeToolCalls || len(decision.ToolCalls) != 1 {
		t.Fatalf("decision is %+v", decision)
	}
	if decision.ToolCalls[0].Name != "Write" || string(decision.ToolCalls[0].Arguments) == "" {
		t.Fatalf("tool call is %+v", decision.ToolCalls[0])
	}
	if decision.Usage.Cost != 0 {
		t.Fatalf("a subscription call spent %v, and it must spend nothing", decision.Usage.Cost)
	}
	if decision.ListCostUSD <= 0 {
		t.Fatalf("the list price is %v, and the recorded stream reports one", decision.ListCostUSD)
	}
	if decision.Stop != "model_stopped" || decision.Build != "claude-sonnet-5" {
		t.Fatalf("decision is %+v", decision)
	}
}

func TestAPermissionDeniedStreamBecomesARefusalNamingTheTool(t *testing.T) {
	decision, err := decide(readTestdata(t, "permission_denied.jsonl"), time.Second, 100)
	if err != nil {
		t.Fatalf("deciding: %v", err)
	}
	if decision.Outcome != llm.OutcomeRefusal || !strings.Contains(decision.Refusal, "Write") {
		t.Fatalf("decision is %+v", decision)
	}
}

func TestACapReachedStreamIsABudgetError(t *testing.T) {
	_, err := decide(readTestdata(t, "cap_reached.jsonl"), time.Second, 100)
	if transport.KindOf(err) != transport.KindBudget {
		t.Fatalf("expected kind budget, got %v", err)
	}
}

func TestAQuotaClosedStreamIsARateLimitError(t *testing.T) {
	_, err := decide(readTestdata(t, "quota_closed.jsonl"), time.Second, 100)
	if transport.KindOf(err) != transport.KindRateLimit {
		t.Fatalf("expected kind rate_limit, got %v", err)
	}
}

func TestAStreamWithNoResultEventIsAProviderError(t *testing.T) {
	_, err := decide(readTestdata(t, "process_failed.jsonl"), time.Second, 100)
	if transport.KindOf(err) != transport.KindProvider {
		t.Fatalf("expected kind provider, got %v", err)
	}
	if !strings.Contains(err.Error(), "without a result event") {
		t.Fatalf("the error does not say what was missing: %v", err)
	}
}

func TestReadTranscriptRefusesALineThatIsNotJSON(t *testing.T) {
	_, err := ReadTranscript(strings.NewReader("{\"type\":\"system\"}\nnot json\n"))
	if transport.KindOf(err) != transport.KindInvalidAnswer {
		t.Fatalf("expected kind invalid_answer, got %v", err)
	}
}

func TestAskFailsWithANamedErrorWhenTheSubprocessWritesNothing(t *testing.T) {
	t.Setenv("BOJI_CONNECTOR_SILENT_CHILD", "1")
	claude := Claude{Bin: os.Args[0], Model: "sonnet", Dir: t.TempDir(), WallClock: 200 * time.Millisecond}

	started := time.Now()
	_, err := claude.Ask(context.Background(), llm.Request{Messages: []llm.Message{{Role: llm.RoleUser, Content: "hi"}}})
	waited := time.Since(started)

	if transport.KindOf(err) != transport.KindTimeout {
		t.Fatalf("expected kind timeout, got %v", err)
	}
	if !strings.Contains(err.Error(), "wall clock cap") {
		t.Fatalf("the error does not name the cap: %v", err)
	}
	if waited > 5*time.Second {
		t.Fatalf("the connector waited %s, so it hung rather than capping", waited)
	}
}

func TestALiveClaudeCallDrivesATwoStepLoopAndSpendsNoMoney(t *testing.T) {
	if os.Getenv("BOJI_LIVE_CLAUDE") == "" {
		t.Skip("set BOJI_LIVE_CLAUDE to spend a turn of the subscription quota")
	}
	dir := t.TempDir()
	claude, err := NewClaude(dir)
	if err != nil {
		t.Fatalf("building the connector: %v", err)
	}
	claude.WallClock = 90 * time.Second
	claude.ListBudgetUSD = 0.10

	tools := []llm.Tool{{Name: "write", Description: "writes a file under the working directory",
		Parameters: map[string]any{"type": "object", "properties": map[string]any{
			"path": map[string]any{"type": "string"}, "content": map[string]any{"type": "string"}},
			"required": []string{"path", "content"}}}}
	messages := []llm.Message{{Role: llm.RoleUser, Content: "write the word hello into hello.txt, then say done"}}

	first, err := claude.Ask(context.Background(), llm.Request{Messages: messages, Tools: tools})
	if err != nil {
		t.Fatalf("the first step: %v", err)
	}
	if first.Outcome != llm.OutcomeToolCalls || first.ToolCalls[0].Name != "write" {
		t.Fatalf("the first step is %+v", first)
	}
	var arguments struct{ Path, Content string }
	if err := json.Unmarshal(first.ToolCalls[0].Arguments, &arguments); err != nil {
		t.Fatalf("the tool call arguments: %v", err)
	}
	written := filepath.Join(dir, filepath.Base(arguments.Path))
	if err := os.WriteFile(written, []byte(arguments.Content), 0o644); err != nil {
		t.Fatalf("running the tool: %v", err)
	}

	messages = append(messages,
		llm.Message{Role: llm.RoleAssistant, ToolCalls: first.ToolCalls},
		llm.Message{Role: llm.RoleTool, ToolCallID: first.ToolCalls[0].ID, Content: "wrote " + arguments.Path})
	second, err := claude.Ask(context.Background(), llm.Request{Messages: messages, Tools: tools})
	if err != nil {
		t.Fatalf("the second step: %v", err)
	}
	if second.Outcome != llm.OutcomeMessage {
		t.Fatalf("the second step is %+v", second)
	}

	body, err := os.ReadFile(written)
	if err != nil || !strings.Contains(strings.ToLower(string(body)), "hello") {
		t.Fatalf("the file is %q, error %v", body, err)
	}
	if first.Usage.Cost != 0 || second.Usage.Cost != 0 {
		t.Fatalf("a subscription call reported money spent: %v and %v", first.Usage.Cost, second.Usage.Cost)
	}
	t.Logf("actual $%.2f, list $%.6f, stops %s and %s, file %s says %q",
		first.Usage.Cost+second.Usage.Cost, first.ListCostUSD+second.ListCostUSD, first.Stop, second.Stop,
		filepath.Base(written), body)
}

func TestNewClaudeRefusesAConnectorWithNoDirectory(t *testing.T) {
	_, err := NewClaude("")
	if transport.KindOf(err) != transport.KindBadRequest {
		t.Fatalf("expected kind bad_request, got %v", err)
	}
}

func TestAskRefusesARequestWithNoMessages(t *testing.T) {
	claude, err := NewClaude(t.TempDir())
	if err != nil {
		t.Fatalf("building the connector: %v", err)
	}
	if _, err := claude.Ask(context.Background(), llm.Request{}); transport.KindOf(err) != transport.KindBadRequest {
		t.Fatalf("expected kind bad_request, got %v", err)
	}
}

func TestRenderPutsToolsInTheSystemPromptAndTheConversationOnStdin(t *testing.T) {
	system, prompt, err := render(llm.Request{
		Messages: []llm.Message{
			{Role: llm.RoleSystem, Content: "be terse"},
			{Role: llm.RoleUser, Content: "write a.txt"},
			{Role: llm.RoleAssistant, ToolCalls: []llm.ToolCall{{ID: "1", Name: "write", Arguments: []byte(`{"path":"a.txt"}`)}}},
			{Role: llm.RoleTool, ToolCallID: "1", Content: "wrote 2 bytes"},
		},
		Tools: []llm.Tool{{Name: "write", Description: "writes a file", Parameters: map[string]any{"type": "object"}}},
	})
	if err != nil {
		t.Fatalf("rendering: %v", err)
	}
	if !strings.Contains(system, "write: writes a file") || !strings.Contains(system, "be terse") {
		t.Fatalf("system prompt is %q", system)
	}
	if !strings.Contains(prompt, "user: write a.txt") || !strings.Contains(prompt, "wrote 2 bytes") {
		t.Fatalf("prompt is %q", prompt)
	}
	if strings.Contains(prompt, "be terse") {
		t.Fatalf("the system prompt leaked into the conversation: %q", prompt)
	}
}

func TestProbeReadsTheRealVersionAndLoginState(t *testing.T) {
	if os.Getenv("BOJI_LIVE_CLAUDE") == "" {
		t.Skip("set BOJI_LIVE_CLAUDE to probe the installed claude")
	}
	health := Probe(context.Background(), konst.ClaudeBin)
	if health.Problem != "" || !strings.Contains(health.Version, "Claude Code") || !health.LoggedIn {
		t.Fatalf("health is %+v", health)
	}
	t.Log(health.String())
}

func TestProbeReportsClaudeIsNotOnAnEmptyPath(t *testing.T) {
	t.Setenv("PATH", "")
	health := Probe(context.Background(), konst.ClaudeBin)
	if health.Problem != "not on the path" || health.Version != "" || health.LoggedIn {
		t.Fatalf("health is %+v", health)
	}
	if health.String() != "claude: not on the path" {
		t.Fatalf("the line a person reads is %q", health.String())
	}
}

func TestHealthNeverPrintsAnythingIdentifying(t *testing.T) {
	line := Health{Bin: "claude", Path: "/x/claude", Version: "2.1.277 (Claude Code)", LoggedIn: true,
		AuthMethod: "claude.ai", Subscription: "max"}.String()
	if !strings.Contains(line, "2.1.277") || !strings.Contains(line, "logged in by claude.ai, max") {
		t.Fatalf("the line a person reads is %q", line)
	}
	if strings.Contains(line, "@") {
		t.Fatalf("the line carries an identity: %q", line)
	}
}
