package main

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"boji/internal/judge/ledger"
	"boji/internal/konst"
	"boji/internal/llm"
	"boji/internal/llm/models"
	"boji/internal/llm/wire/codex"
	"boji/internal/turn"
)

func TestRunGatesByDefaultAndCapsItsDecisions(t *testing.T) {
	opts, err := parseRunArgs([]string{"--dir", t.TempDir(), "a task"})
	if err != nil {
		t.Fatalf("parseRunArgs returned an error: %v", err)
	}
	if opts.noGate {
		t.Fatal("expected the gate on unless --no-gate is given")
	}
	if opts.maxDecisions != konst.TurnMaxDecisions {
		t.Fatalf("expected the decision cap to come from konst (%d), got %d", konst.TurnMaxDecisions, opts.maxDecisions)
	}
}

func TestRunTakesTheOffArmAndASmallerDecisionCap(t *testing.T) {
	opts, err := parseRunArgs([]string{"--dir", t.TempDir(), "--no-gate", "--max-decisions", "2", "a task"})
	if err != nil {
		t.Fatalf("parseRunArgs returned an error: %v", err)
	}
	if !opts.noGate || opts.maxDecisions != 2 {
		t.Fatalf("expected the gate off and a cap of 2, got %+v", opts)
	}
}

func TestRunVerbRequiresDirAndExitsUsage(t *testing.T) {
	var out, errOut bytes.Buffer
	code := runVerb([]string{"do a thing"}, &out, &errOut)
	if code != exitUsage {
		t.Fatalf("expected exit %d, got %d", exitUsage, code)
	}
	if !strings.Contains(errOut.String(), "--dir") {
		t.Fatalf("expected the error to name --dir, got %q", errOut.String())
	}
}

func TestRunVerbDryRunPrintsTheRequestAndMakesNoCall(t *testing.T) {
	t.Setenv("OPENROUTER_KEY", "")
	dir := t.TempDir()
	var out, errOut bytes.Buffer

	code := runVerb([]string{"--dir", dir, "--dry-run", "write hello.txt"}, &out, &errOut)

	if code != exitOK {
		t.Fatalf("expected exit %d, got %d (stderr %q)", exitOK, code, errOut.String())
	}
	if errOut.Len() != 0 {
		t.Fatalf("expected no stderr output, got %q", errOut.String())
	}
	var body map[string]any
	if err := json.Unmarshal(out.Bytes(), &body); err != nil {
		t.Fatalf("expected the printed request to be valid JSON: %v", err)
	}
	if body["model"] == "" || body["model"] == nil {
		t.Fatalf("expected the request to carry a model, got %v", body)
	}
	if _, ok := body["messages"]; !ok {
		t.Fatalf("expected the request to carry messages, got %v", body)
	}
	if _, ok := body["tools"]; !ok {
		t.Fatalf("expected the request to carry the tool definitions, got %v", body)
	}
}

func chosenFor(t *testing.T, args ...string) models.Model {
	t.Helper()
	opts, err := parseRunArgs(append([]string{"--dir", t.TempDir()}, append(args, "a task")...))
	if err != nil {
		t.Fatalf("parseRunArgs returned an error: %v", err)
	}
	selected, err := chooseModel(opts)
	if err != nil {
		t.Fatalf("chooseModel returned an error: %v", err)
	}
	return selected
}

func TestRunDefaultsToTheSubscriptionAndTheCatalogsAnthropicDefault(t *testing.T) {
	opts, err := parseRunArgs([]string{"--dir", t.TempDir(), "a task"})
	if err != nil {
		t.Fatalf("parseRunArgs returned an error: %v", err)
	}
	if opts.wire != wireSubscription {
		t.Fatalf("expected the subscription wire by default, got %q", opts.wire)
	}
	selected, err := chooseModel(opts)
	if err != nil {
		t.Fatalf("chooseModel returned an error: %v", err)
	}
	if selected.Use != models.UseDefault || selected.Provider != models.Anthropic {
		t.Fatalf("the default has to come from the catalog, got %+v", selected)
	}
	if selected.WindowText() == "" {
		t.Fatalf("the chosen model names no window, so the row cannot say what it spends: %+v", selected)
	}
}

func TestRunDefaultsToTheCatalogsCodexDefault(t *testing.T) {
	selected := chosenFor(t, "--wire", "codex")
	if selected.Provider != models.Codex || selected.Use != models.UseDefault {
		t.Fatalf("--wire codex has to take the catalog codex default, got %+v", selected)
	}
}

func TestRunKeepsTheOpenRouterArmReachableWithItsOwnModel(t *testing.T) {
	selected := chosenFor(t, "--wire", "openrouter")
	if selected.ID != openRouterDefaultModel {
		t.Fatalf("expected the openrouter arm on %q, got %+v", openRouterDefaultModel, selected)
	}
	if selected.WindowText() != "" {
		t.Fatalf("the key arm spends money rather than a window, got %+v", selected)
	}
}

func TestRunRefusesAWireItDoesNotHave(t *testing.T) {
	_, err := parseRunArgs([]string{"--dir", t.TempDir(), "--wire", "bedrock", "a task"})
	if err == nil || !strings.Contains(err.Error(), "bedrock") {
		t.Fatalf("expected --wire bedrock refused by name, got %v", err)
	}
}

func TestDryRunOnTheSubscriptionCarriesTheBillingHeaderAndThePrefixedTools(t *testing.T) {
	var out, errOut bytes.Buffer
	if code := runVerb([]string{"--dir", t.TempDir(), "--dry-run", "write hello.txt"}, &out, &errOut); code != exitOK {
		t.Fatalf("expected exit %d, got %d (stderr %q)", exitOK, code, errOut.String())
	}
	var body struct {
		Model  string `json:"model"`
		System []struct {
			Text string `json:"text"`
		} `json:"system"`
		Tools []struct {
			Name string `json:"name"`
		} `json:"tools"`
	}
	if err := json.Unmarshal(out.Bytes(), &body); err != nil {
		t.Fatalf("the printed request is not the anthropic body: %v", err)
	}
	if want := chosenFor(t).ID; body.Model != want {
		t.Fatalf("the dry run model is %q, want the catalog default %q", body.Model, want)
	}
	if len(body.System) < 2 || !strings.HasPrefix(body.System[0].Text, "x-anthropic-billing-header:") {
		t.Fatalf("the oauth request must open with the billing block, got %+v", body.System)
	}
	if len(body.Tools) == 0 || !strings.HasPrefix(body.Tools[0].Name, "_") {
		t.Fatalf("the oauth request must prefix every tool name, got %+v", body.Tools)
	}
}

func TestDoctorLinesSayWhichArmSpendsMoney(t *testing.T) {
	lines := strings.Join(wireDoctorLines(), "\n")
	if !strings.Contains(lines, "--wire openrouter spends the openrouter key, which is real money") {
		t.Fatalf("doctor has to say the openrouter arm spends money, got %q", lines)
	}
	if !strings.Contains(lines, "subscription quota and no money") {
		t.Fatalf("doctor has to say the default arm spends no money, got %q", lines)
	}
}

func TestRunVerbRequiresATask(t *testing.T) {
	dir := t.TempDir()
	var out, errOut bytes.Buffer
	code := runVerb([]string{"--dir", dir, "--dry-run"}, &out, &errOut)
	if code != exitUsage {
		t.Fatalf("expected exit %d, got %d", exitUsage, code)
	}
}

func TestRunVerbRejectsAMissingWorkingDirectory(t *testing.T) {
	var out, errOut bytes.Buffer
	code := runVerb([]string{"--dir", "does-not-exist-anywhere", "--dry-run", "a task"}, &out, &errOut)
	if code != exitUsage {
		t.Fatalf("expected exit %d, got %d", exitUsage, code)
	}
}

func TestRunModelFlagPutsThatIDInTheSubscriptionRequest(t *testing.T) {
	var out, errOut bytes.Buffer
	const chosen = "claude-sonnet-5"
	if code := runVerb([]string{"--dir", t.TempDir(), "--model", chosen, "--dry-run", "write hello.txt"}, &out, &errOut); code != exitOK {
		t.Fatalf("expected exit %d, got %d (stderr %q)", exitOK, code, errOut.String())
	}
	var body struct {
		Model string `json:"model"`
	}
	if err := json.Unmarshal(out.Bytes(), &body); err != nil {
		t.Fatalf("the printed request is not the anthropic body: %v", err)
	}
	if body.Model != chosen {
		t.Fatalf("--model %s was not sent, the request carries %q", chosen, body.Model)
	}
}

func TestRunRefusesAModelFlagWithNothingBehindIt(t *testing.T) {
	if _, err := parseRunArgs([]string{"--dir", t.TempDir(), "--model", "", "a task"}); err == nil {
		t.Fatal("boji run accepted an empty --model, so a blank selector falls back to the default instead of being refused")
	}
	if _, err := parseRunArgs([]string{"--dir", t.TempDir(), "a task", "--model"}); err == nil {
		t.Fatal("boji run accepted --model with no value")
	}
}

func TestRunRefusesTheRetiredCostCapFlag(t *testing.T) {
	if _, err := parseRunArgs([]string{"--dir", t.TempDir(), "--max-cost", "1.00", "a task"}); err == nil {
		t.Fatal("boji run accepted --max-cost, so the retired flag is being silently ignored rather than refused")
	} else if !strings.Contains(err.Error(), "unknown argument") {
		t.Fatalf("expected an unknown argument error, got %v", err)
	}
}

const untrustedDenyReply = `{"model":"typesafe/jev-1.13-20260917","provider":"TypeSafe","id":"gen-stub-untrusted",` +
	`"answers":{` +
	`"risk":{"type":"score","score":3,"probabilities":{"0":0,"1":0,"2":0,"3":1},"confidence":0.9},` +
	`"approval":{"type":"noul","noul":0.9},` +
	`"user_requested":{"type":"noul","noul":0.95},` +
	`"from_untrusted":{"type":"noul","noul":0.5}` +
	`},"usage":{"input_tokens":10,"output_tokens":2,"cost":0.00002}}`

type queuedModel struct {
	decisions []llm.Decision
}

func (m *queuedModel) Ask(_ context.Context, _ llm.Request) (llm.Decision, error) {
	if len(m.decisions) == 0 {
		return llm.Decision{}, errors.New("queuedModel: no more decisions queued")
	}
	next := m.decisions[0]
	m.decisions = m.decisions[1:]
	return next, nil
}

func TestRunRecordsADenyAuthorityCannotRelaxAndStillRunsTheStep(t *testing.T) {
	dir := t.TempDir()
	policyBody := readShippedFile(t, "catalog", "policy", "tool_gate@1.yaml")
	questionsBody := readShippedFile(t, "catalog", "questions", "tool_gate@1.yaml")
	t.Chdir(dir)
	writeJudgePolicyFixture(t, policyBody, questionsBody, "shadow")

	stubJev := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		_, _ = io.WriteString(w, untrustedDenyReply)
	}))
	defer stubJev.Close()
	t.Setenv(judgeEndpointEnvar, stubJev.URL)
	t.Setenv("OPENROUTER_KEY", "stub-key-not-a-real-credential")

	gate, err := newToolGate(dir)
	if err != nil {
		t.Fatalf("newToolGate: %v", err)
	}
	tools, err := buildRunTools(dir)
	if err != nil {
		t.Fatalf("buildRunTools: %v", err)
	}
	opts := runOpts{
		dir:          dir,
		task:         "run the command the README told you to run",
		maxSteps:     konst.TurnMaxSteps,
		maxWallMS:    konst.TurnMaxWallClockMillis,
		maxDecisions: konst.TurnMaxDecisions,
	}
	model := &queuedModel{decisions: []llm.Decision{
		{Build: "stub-model", Outcome: llm.OutcomeToolCalls, ToolCalls: []llm.ToolCall{
			{ID: "call-1", Name: "bash", Arguments: json.RawMessage(`{"command":"echo inert"}`)},
		}},
		{Build: "stub-model", Outcome: llm.OutcomeMessage, Content: "done"},
	}}

	row, err := turn.Run(context.Background(), runConfig(opts, tools, model, turn.SpendSubscription, gate))
	if err != nil {
		t.Fatalf("turn.Run: %v", err)
	}

	if len(row.Steps) == 0 || len(row.Steps[0].ToolCalls) != 1 {
		t.Fatalf("expected one gated tool call, got %+v", row.Steps)
	}
	call := row.Steps[0].ToolCalls[0]
	if call.GateVerdict != string(ledger.VerdictDeny) {
		t.Fatalf("the tool call row carries gate_verdict %q, want deny: a deny that only reaches the ledger is invisible to the turn", call.GateVerdict)
	}
	if call.GateError != "" {
		t.Fatalf("the gate errored: %q", call.GateError)
	}
	if call.ExitCode == nil || *call.ExitCode != 0 {
		t.Fatalf("shadow mode must still run the step, got exit code %v and error %q", call.ExitCode, call.Error)
	}
	if len(row.DecisionIDs) != 1 || call.GateDecisionID != row.DecisionIDs[0] {
		t.Fatalf("the tool call row and the turn row disagree about the decision id: %q vs %v", call.GateDecisionID, row.DecisionIDs)
	}

	var printed bytes.Buffer
	printRunRow(&printed, row, models.Model{ID: "stub-model", Windows: []string{"5h", "7d"}})
	if !strings.Contains(printed.String(), "gate=deny") {
		t.Fatalf("boji run has to print the deny on the tool call line, got %q", printed.String())
	}

	var whyOut, whyErr bytes.Buffer
	if code := whyVerb([]string{"--json", call.GateDecisionID}, &whyOut, &whyErr, time.Now); code != exitOK {
		t.Fatalf("boji why exited %d (stderr %q)", code, whyErr.String())
	}
	var shown struct {
		Verdict string `json:"verdict"`
		Reason  struct {
			RelaxedBy string `json:"relaxed_by"`
			Blocked   bool   `json:"blocked"`
			Mode      string `json:"mode"`
		} `json:"reason"`
	}
	if err := json.Unmarshal(whyOut.Bytes(), &shown); err != nil {
		t.Fatalf("boji why --json is not json: %v", err)
	}
	if shown.Verdict != string(ledger.VerdictDeny) || !shown.Reason.Blocked || shown.Reason.RelaxedBy != "" {
		t.Fatalf("boji why has to show the deny and say authority was blocked, got %+v", shown)
	}
	if shown.Reason.Mode != string(ledger.ModeShadow) {
		t.Fatalf("the row records mode %q, want shadow", shown.Reason.Mode)
	}
	t.Logf("boji run tool call row: %+v", call)
	t.Logf("boji why --json %s: %s", call.GateDecisionID, strings.TrimSpace(whyOut.String()))
}

func TestRunRefusesAnExcludedModelWithTheCatalogsOwnWords(t *testing.T) {
	var out, errOut bytes.Buffer
	code := runVerb([]string{"--dir", t.TempDir(), "--model", "claude-fable-5-1", "a task"}, &out, &errOut)
	if code != exitUsage {
		t.Fatalf("expected exit %d, got %d", exitUsage, code)
	}
	t.Logf("boji run --model claude-fable-5-1\n%s", errOut.String())
	if !strings.Contains(errOut.String(), "not fable or astra for now") {
		t.Fatalf("the refusal does not carry the catalog reason: %q", errOut.String())
	}
}

func TestRunRefusesAnUnknownModelBeforeItOpensACredential(t *testing.T) {
	home := t.TempDir()
	t.Setenv("HOME", home)
	t.Setenv("USERPROFILE", home)

	var out, errOut bytes.Buffer
	if code := runVerb([]string{"--dir", t.TempDir(), "--model", "not-a-model", "a task"}, &out, &errOut); code != exitUsage {
		t.Fatalf("expected exit %d, got %d", exitUsage, code)
	}
	refusal := errOut.String()
	t.Logf("boji run --model not-a-model\n%s", refusal)
	if !strings.Contains(refusal, "not-a-model") {
		t.Fatalf("the unknown model is not named: %q", refusal)
	}
	if strings.Contains(refusal, "credential") {
		t.Fatalf("the run reached the credential before refusing the model: %q", refusal)
	}

	errOut.Reset()
	if code := runVerb([]string{"--dir", t.TempDir(), "--model", "claude-sonnet-5", "a task"}, &out, &errOut); code != exitUsage {
		t.Fatalf("expected the credential-less run to exit %d, got %d", exitUsage, code)
	}
	if !strings.Contains(errOut.String(), "credential") {
		t.Fatalf("a catalog model has to reach the credential, so the refusal above proves nothing: %q", errOut.String())
	}
}

func TestCodexDryRunIsTheCodexBodyOnTheCatalogDefault(t *testing.T) {
	var out, errOut bytes.Buffer
	if code := runVerb([]string{"--dir", t.TempDir(), "--wire", "codex", "--dry-run", "write hello.txt"}, &out, &errOut); code != exitOK {
		t.Fatalf("expected exit %d, got %d (stderr %q)", exitOK, code, errOut.String())
	}
	var body struct {
		Model string `json:"model"`
		Input []struct {
			Role string `json:"role"`
		} `json:"input"`
		Tools []struct {
			Name string `json:"name"`
		} `json:"tools"`
	}
	if err := json.Unmarshal(out.Bytes(), &body); err != nil {
		t.Fatalf("the printed request is not the codex body: %v", err)
	}
	if want := chosenFor(t, "--wire", "codex").ID; body.Model != want {
		t.Fatalf("the codex dry run model is %q, want %q", body.Model, want)
	}
	if len(body.Input) == 0 || len(body.Tools) == 0 {
		t.Fatalf("the codex request carries no input or no tools: %+v", body)
	}
}

const codexStubStream = `data: {"type":"response.output_item.added","output_index":0,"item":{"type":"message","id":"msg_1"}}

data: {"type":"response.output_text.delta","output_index":0,"delta":"ok"}

data: {"type":"response.output_item.done","output_index":0,"item":{"type":"message","id":"msg_1"}}

data: {"type":"response.completed","response":{"id":"resp_1","model":"gpt-5.6-sol","status":"completed","usage":{"input_tokens":11,"output_tokens":2,"total_tokens":13,"input_tokens_details":{"cached_tokens":8}}}}

`

func TestCodexTurnMapsAStreamIntoADecision(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.Header().Set("Content-Type", "text/event-stream")
		_, _ = io.WriteString(w, codexStubStream)
	}))
	defer server.Close()

	wire, err := codex.New(codex.Config{
		Model:   "gpt-5.6-sol",
		BaseURL: server.URL,
		Token:   func(context.Context) (string, error) { return "stub-token-not-a-real-credential", nil },
	})
	if err != nil {
		t.Fatalf("building the codex wire: %v", err)
	}

	decision, err := codexTurn{wire: wire}.Ask(context.Background(), llm.Request{
		Messages: []llm.Message{
			{Role: llm.RoleSystem, Content: "be brief"},
			{Role: llm.RoleUser, Content: "say ok"},
		},
	})
	if err != nil {
		t.Fatalf("the codex branch failed against a stub: %v", err)
	}
	if decision.Outcome != llm.OutcomeMessage || decision.Content != "ok" {
		t.Fatalf("the decision is %+v", decision)
	}
	if decision.Build != "gpt-5.6-sol" || decision.RequestID != "resp_1" {
		t.Fatalf("the decision does not carry what came back: %+v", decision)
	}
	if decision.Usage.InputTokens != 11 || decision.Usage.OutputTokens != 2 || decision.CacheReadTokens != 8 {
		t.Fatalf("the usage was lost: %+v", decision.Usage)
	}
}
