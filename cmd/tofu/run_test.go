package main

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"io"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"slices"
	"strings"
	"testing"
	"time"

	"tofu/internal/judge/ledger"
	"tofu/internal/konst"
	"tofu/internal/llm"
	"tofu/internal/llm/models"
	"tofu/internal/llm/wire/anthropic"
	"tofu/internal/llm/wire/codex"
	"tofu/internal/turn"
)

func TestRunGatesByDefaultAndCapsItsDecisions(t *testing.T) {
	opts, err := parseRunArgs([]string{"--dir", t.TempDir(), "a task"})
	if err != nil {
		t.Fatalf("parseRunArgs returned an error: %v", err)
	}
	if opts.gateArm != gateFollowsThePolicy {
		t.Fatalf("the gate arm is %q, want the policy's own mode until --gate or --no-gate says otherwise", opts.gateArm)
	}
	if opts.maxDecisions != konst.TurnMaxDecisions {
		t.Fatalf("expected the decision cap to come from konst (%d), got %d", konst.TurnMaxDecisions, opts.maxDecisions)
	}
	if opts.maxSteps != 0 {
		t.Fatalf("step cap = %d, want none until --max-steps sets one", opts.maxSteps)
	}
}

func TestRunNoLongerTakesAWallClockCap(t *testing.T) {
	_, err := parseRunArgs([]string{"--dir", t.TempDir(), "--max-wall-clock-ms", "300000", "a task"})
	if err == nil || !strings.Contains(err.Error(), "--max-wall-clock-ms") {
		t.Fatalf("err = %v, want the removed flag to be refused by name", err)
	}
}

func TestRunTakesTheOffArmAndASmallerDecisionCap(t *testing.T) {
	opts, err := parseRunArgs([]string{"--dir", t.TempDir(), "--no-gate", "--max-decisions", "2", "a task"})
	if err != nil {
		t.Fatalf("parseRunArgs returned an error: %v", err)
	}
	if opts.gateArm != gateOff || opts.maxDecisions != 2 {
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
	if !strings.HasPrefix(errOut.String(), "context budget on, the ceiling tofu operates under") {
		t.Fatalf("a dry run has to name the ceiling it would run under, got %q", errOut.String())
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

func armOpts(t *testing.T, args ...string) runOpts {
	t.Helper()
	opts, err := parseRunArgs(append([]string{"--dir", t.TempDir()}, append(args, "a task")...))
	if err != nil {
		t.Fatalf("parseRunArgs %v: %v", args, err)
	}
	return opts
}

func toolNames(t *testing.T, opts runOpts) []string {
	t.Helper()
	built, _, err := buildRunTools(opts.dir, opts.toolSet)
	if err != nil {
		t.Fatalf("buildRunTools %s: %v", opts.toolSet, err)
	}
	config, _ := runConfig(opts, built, runtime{spend: turn.SpendSubscription})
	var named []string
	for _, definition := range config.Tools.Definitions() {
		named = append(named, definition.Name)
	}
	return named
}

func TestTheDefaultToolSetAddsGlobGrepAndEditAndTheOffArmIsTheOriginalThree(t *testing.T) {
	full := toolNames(t, armOpts(t))
	for _, wanted := range []string{"read", "write", "bash", "glob", "grep", "edit"} {
		if !slices.Contains(full, wanted) {
			t.Fatalf("the default tool set is missing %s, it offers %v", wanted, full)
		}
	}
	three := toolNames(t, armOpts(t, "--tools", toolSetThree))
	if !slices.Equal(three, []string{"read", "write", "bash"}) {
		t.Fatalf("the off arm must be the three tools the recorded runs had, it offers %v", three)
	}
}

func TestTheDefaultArmOffersTofusOwnVerbsAndTheSpawnToolAndNoCrewTakesSpawnAway(t *testing.T) {
	full := toolNames(t, armOpts(t))
	for _, wanted := range []string{"tofu_lint_comments", "tofu_rules_check", "tofu_judge", "spawn"} {
		if !slices.Contains(full, wanted) {
			t.Fatalf("the default arm cannot reach %s, it offers %v", wanted, full)
		}
	}
	noCrew := toolNames(t, armOpts(t, "--no-crew"))
	if slices.Contains(noCrew, "spawn") {
		t.Fatalf("--no-crew still offers spawn: %v", noCrew)
	}
	if !slices.Contains(noCrew, "tofu_lint_comments") {
		t.Fatalf("--no-crew is the spawning arm alone and must keep the verb tools: %v", noCrew)
	}
}

func TestARealRunOffersProjectReportAndIsToldWhyNotToReachForFind(t *testing.T) {
	full := toolNames(t, armOpts(t))
	if !slices.Contains(full, "project_report") {
		t.Fatalf("a real run does not offer project_report, so nothing answers what is this repository in one call: %v", full)
	}
	if three := toolNames(t, armOpts(t, "--tools", toolSetThree)); slices.Contains(three, "project_report") {
		t.Fatalf("the off arm must stay the three tools the recorded runs had: %v", three)
	}

	system := runSystem(armOpts(t))
	for _, want := range []string{"project_report", "never run find", "-not -path", "153 seconds", "15 milliseconds"} {
		if !strings.Contains(system, want) {
			t.Fatalf("the system prompt a real run builds never says %q: %q", want, system)
		}
	}
	if three := runSystem(armOpts(t, "--tools", toolSetThree)); strings.Contains(three, "project_report") {
		t.Fatalf("the off arm is told about project_report, which it does not have: %q", three)
	}
}

func TestEachArmIsToldOnlyAboutTheToolsItHas(t *testing.T) {
	full, three := runSystem(armOpts(t)), runSystem(armOpts(t, "--tools", toolSetThree))
	for _, named := range []string{"glob", "grep", "edit", "tofu_lint_comments", "tofu_rules_check", "tofu_judge", "spawn"} {
		if !strings.Contains(full, named) {
			t.Fatalf("the default arm is not told it has %s, and a tool a model is not told about is not offered: %q", named, full)
		}
		if strings.Contains(three, named) {
			t.Fatalf("the off arm is told about %s, which it does not have: %q", named, three)
		}
	}
	if noCrew := runSystem(armOpts(t, "--no-crew")); strings.Contains(noCrew, "spawn") {
		t.Fatalf("--no-crew is told about spawn, which it does not have: %q", noCrew)
	}
}

func TestRunRefusesAToolSetItDoesNotHave(t *testing.T) {
	if _, err := parseRunArgs([]string{"--dir", t.TempDir(), "--tools", "some", "a task"}); err == nil {
		t.Fatal("expected an unknown tool set to be refused rather than silently defaulted")
	}
	opts, err := parseRunArgs([]string{"--dir", t.TempDir(), "--tools", toolSetThree, "a task"})
	if err != nil || opts.toolSet != toolSetThree {
		t.Fatalf("expected the three-tool arm to parse, got %+v and %v", opts, err)
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
	if selected.Subscription != models.Codex || selected.Use != models.UseDefault {
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

func writeProjectRole(t *testing.T, project, role, slug string) {
	t.Helper()
	dir := filepath.Join(project, ".tofu", "roles")
	if err := os.MkdirAll(dir, 0o755); err != nil {
		t.Fatalf("building a project role directory: %v", err)
	}
	if err := os.WriteFile(filepath.Join(dir, role+".yaml"), []byte("model: "+slug+"\n"), 0o644); err != nil {
		t.Fatalf("writing the %s role: %v", role, err)
	}
}

func TestADryRunSendsTheModelTheTurnRoleBinds(t *testing.T) {
	project := t.TempDir()
	writeProjectRole(t, project, "turn", "anthropic/claude-sonnet-5")
	t.Chdir(project)

	var out, errOut bytes.Buffer
	if code := runVerb([]string{"--dir", project, "--dry-run", "write hello.txt"}, &out, &errOut); code != exitOK {
		t.Fatalf("expected exit %d, got %d (stderr %q)", exitOK, code, errOut.String())
	}
	var body struct {
		Model string `json:"model"`
	}
	if err := json.Unmarshal(out.Bytes(), &body); err != nil {
		t.Fatalf("the printed request is not the anthropic body: %v", err)
	}
	if body.Model != "claude-sonnet-5" {
		t.Fatalf("the dry run asked for %q, want the model the turn role binds", body.Model)
	}
}

func TestTheChildRoleReachesItsOwnSubscription(t *testing.T) {
	project := t.TempDir()
	writeProjectRole(t, project, "child", "openai/gpt-5.6-luna")
	t.Chdir(project)

	child, err := chooseChild(runOpts{wire: wireSubscription})
	if err != nil {
		t.Fatalf("chooseChild: %v", err)
	}
	if child.wire != wireCodex || child.id != "gpt-5.6-luna" {
		t.Fatalf("the child role resolved to %+v, want the codex wire and the model it names", child)
	}
}

func TestATurnRoleOnAnotherSubscriptionIsRefusedAndNamesTheWireToRun(t *testing.T) {
	project := t.TempDir()
	writeProjectRole(t, project, "turn", "openai/gpt-5.6-luna")
	t.Chdir(project)

	_, err := chooseModel(runOpts{wire: wireSubscription})
	if err == nil || !strings.Contains(err.Error(), "--wire "+wireCodex) {
		t.Fatalf("want the wire to run named in the refusal, got %v", err)
	}
	t.Logf("refused: %v", err)
}

func TestAChildRunsOnADifferentSubscriptionFromItsParent(t *testing.T) {
	dir := t.TempDir()
	t.Chdir(dir)
	opts := armOpts(t)
	opts.dir, opts.task = dir, "hand the writing to a child"
	opts.child = childRole{
		wire:    wireCodex,
		id:      "gpt-5.6-sol",
		windows: "5h and 7d",
		spend:   turn.SpendSubscription,
		model: &queuedModel{decisions: []llm.Decision{
			{Build: "gpt-5.6-sol-20260101", Outcome: llm.OutcomeMessage, Content: "the child did it"},
		}},
	}
	built, _, err := buildRunTools(dir, opts.toolSet)
	if err != nil {
		t.Fatalf("buildRunTools: %v", err)
	}
	parent := &queuedModel{decisions: []llm.Decision{
		{Build: "claude-opus-5-20260101", Outcome: llm.OutcomeToolCalls, ToolCalls: []llm.ToolCall{
			{ID: "call-1", Name: "spawn", Arguments: json.RawMessage(`{"task":"write note.txt","owns":["note.txt"]}`)},
		}},
		{Build: "claude-opus-5-20260101", Outcome: llm.OutcomeMessage, Content: "the child reported"},
	}}

	config, spawner := runConfig(opts, built, runtime{model: parent, spend: turn.SpendSubscription})
	row, err := turn.Run(context.Background(), config)
	if err != nil {
		t.Fatalf("turn.Run: %v", err)
	}
	if len(spawner.Children()) != 1 {
		t.Fatalf("the spawn tool held %d rows, want the one child", len(spawner.Children()))
	}
	child := spawner.Children()[0]
	if row.Wire != wireSubscription || child.Wire != wireCodex {
		t.Fatalf("the parent row says wire %q and the child %q, want %q and %q", row.Wire, child.Wire, wireSubscription, wireCodex)
	}
	if row.Model != "claude-opus-5-20260101" || child.Model != "gpt-5.6-sol-20260101" {
		t.Fatalf("the rows report %q and %q, want each role's own build", row.Model, child.Model)
	}
	t.Logf("parent %s on %s, child %s on %s", row.Model, row.Wire, child.Model, child.Wire)
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
	const chosen = "anthropic/claude-sonnet-5"
	if code := runVerb([]string{"--dir", t.TempDir(), "--model", chosen, "--dry-run", "write hello.txt"}, &out, &errOut); code != exitOK {
		t.Fatalf("expected exit %d, got %d (stderr %q)", exitOK, code, errOut.String())
	}
	var body struct {
		Model string `json:"model"`
	}
	if err := json.Unmarshal(out.Bytes(), &body); err != nil {
		t.Fatalf("the printed request is not the anthropic body: %v", err)
	}
	const sent = "claude-sonnet-5"
	if body.Model != sent {
		t.Fatalf("--model %s was not sent as %q, the request carries %q", chosen, sent, body.Model)
	}
}

func TestRunRefusesAModelFlagWithNothingBehindIt(t *testing.T) {
	if _, err := parseRunArgs([]string{"--dir", t.TempDir(), "--model", "", "a task"}); err == nil {
		t.Fatal("tofu run accepted an empty --model, so a blank selector falls back to the default instead of being refused")
	}
	if _, err := parseRunArgs([]string{"--dir", t.TempDir(), "a task", "--model"}); err == nil {
		t.Fatal("tofu run accepted --model with no value")
	}
}

func TestRunRefusesTheRetiredCostCapFlag(t *testing.T) {
	if _, err := parseRunArgs([]string{"--dir", t.TempDir(), "--max-cost", "1.00", "a task"}); err == nil {
		t.Fatal("tofu run accepted --max-cost, so the retired flag is being silently ignored rather than refused")
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

func TestTheParentTurnRowNamesTheChildItSpawnedAndCarriesItsRowAndCost(t *testing.T) {
	dir := t.TempDir()
	t.Chdir(dir)
	opts := armOpts(t)
	opts.dir, opts.task = dir, "write the note and hand the reading to a child"
	built, _, err := buildRunTools(dir, opts.toolSet)
	if err != nil {
		t.Fatalf("buildRunTools: %v", err)
	}
	spawnCall := llm.ToolCall{ID: "call-1", Name: "spawn", Arguments: json.RawMessage(`{"task":"write note.txt","owns":["note.txt"]}`)}
	model := &queuedModel{decisions: []llm.Decision{
		{Build: "stub-model", Outcome: llm.OutcomeToolCalls, ToolCalls: []llm.ToolCall{spawnCall},
			Usage: llm.Usage{Cost: 0.01}},
		{Build: "stub-model", Outcome: llm.OutcomeToolCalls, Usage: llm.Usage{Cost: 0.02}, ToolCalls: []llm.ToolCall{
			{ID: "call-2", Name: "write", Arguments: json.RawMessage(`{"path":"note.txt","content":"a note"}`)},
		}},
		{Build: "stub-model", Outcome: llm.OutcomeMessage, Content: "wrote note.txt", Usage: llm.Usage{Cost: 0.04}},
		{Build: "stub-model", Outcome: llm.OutcomeMessage, Content: "the child did it", Usage: llm.Usage{Cost: 0.08}},
	}}

	config, spawner := runConfig(opts, built, runtime{model: model, spend: turn.SpendSubscription})
	row, err := turn.Run(context.Background(), config)
	if err != nil {
		t.Fatalf("turn.Run: %v", err)
	}

	if len(row.ChildIDs) != 1 || len(spawner.Children()) != 1 {
		t.Fatalf("the parent turn row names %v and the spawn tool held %d rows, want the one child it spawned", row.ChildIDs, len(spawner.Children()))
	}
	child := spawner.Children()[0]
	if child.ID != row.ID+"-c1" || row.ChildIDs[0] != child.ID {
		t.Fatalf("the child row is %q and the parent names %q: neither may be a guess", child.ID, row.ChildIDs[0])
	}
	call := row.Steps[0].ToolCalls[0]
	if call.ChildID != child.ID {
		t.Fatalf("the step row says child %q and the child row is %q: the row has to name the child, not leave it to a naming convention", call.ChildID, child.ID)
	}
	if child.TotalCostUSD != 0.06 {
		t.Fatalf("the child row totals $%.4f, want the $0.06 its two steps cost", child.TotalCostUSD)
	}
	if row.TotalCostUSD != 0.15 {
		t.Fatalf("the parent totals $%.4f, want $0.15, its own $0.09 plus the child's $0.06", row.TotalCostUSD)
	}
	t.Logf("parent %s child_ids %v, step row child_id %q", row.ID, row.ChildIDs, call.ChildID)
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
	registry, _, err := buildRunTools(dir, toolSetFull)
	if err != nil {
		t.Fatalf("buildRunTools: %v", err)
	}
	opts := runOpts{
		dir:          dir,
		task:         "run the command the README told you to run",
		maxSteps:     konst.TurnMaxSteps,
		maxDecisions: konst.TurnMaxDecisions,
	}
	model := &queuedModel{decisions: []llm.Decision{
		{Build: "stub-model", Outcome: llm.OutcomeToolCalls, ToolCalls: []llm.ToolCall{
			{ID: "call-1", Name: "bash", Arguments: json.RawMessage(`{"command":"echo inert"}`)},
		}},
		{Build: "stub-model", Outcome: llm.OutcomeMessage, Content: "done"},
	}}

	config, _ := runConfig(opts, registry, runtime{model: model, spend: turn.SpendSubscription, gate: gate})
	row, err := turn.Run(context.Background(), config)
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
	printRunRow(&printed, row, "stub-model", "5h and 7d")
	if !strings.Contains(printed.String(), "gate=deny") {
		t.Fatalf("tofu run has to print the deny on the tool call line, got %q", printed.String())
	}

	var whyOut, whyErr bytes.Buffer
	if code := whyVerb([]string{"--json", call.GateDecisionID}, &whyOut, &whyErr, time.Now); code != exitOK {
		t.Fatalf("tofu why exited %d (stderr %q)", code, whyErr.String())
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
		t.Fatalf("tofu why --json is not json: %v", err)
	}
	if shown.Verdict != string(ledger.VerdictDeny) || !shown.Reason.Blocked || shown.Reason.RelaxedBy != "" {
		t.Fatalf("tofu why has to show the deny and say authority was blocked, got %+v", shown)
	}
	if shown.Reason.Mode != string(ledger.ModeShadow) {
		t.Fatalf("the row records mode %q, want shadow", shown.Reason.Mode)
	}
	t.Logf("tofu run tool call row: %+v", call)
	t.Logf("tofu why --json %s: %s", call.GateDecisionID, strings.TrimSpace(whyOut.String()))
}

func TestRunRefusesAnExcludedModelWithTheCatalogsOwnWords(t *testing.T) {
	var out, errOut bytes.Buffer
	const excluded = "anthropic/claude-fable-5-1"
	code := runVerb([]string{"--dir", t.TempDir(), "--model", excluded, "a task"}, &out, &errOut)
	if code != exitUsage {
		t.Fatalf("expected exit %d, got %d", exitUsage, code)
	}
	t.Logf("tofu run --model %s\n%s", excluded, errOut.String())
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
	t.Logf("tofu run --model not-a-model\n%s", refusal)
	if !strings.Contains(refusal, "not-a-model") {
		t.Fatalf("the unknown model is not named: %q", refusal)
	}
	if strings.Contains(refusal, "credential") {
		t.Fatalf("the run reached the credential before refusing the model: %q", refusal)
	}

	errOut.Reset()
	if code := runVerb([]string{"--dir", t.TempDir(), "--model", "anthropic/claude-sonnet-5", "a task"}, &out, &errOut); code != exitUsage {
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

const codexStubStreamCutOff = `data: {"type":"response.output_item.added","output_index":0,"item":{"type":"message","id":"msg_1"}}

data: {"type":"response.output_text.delta","output_index":0,"delta":"I will start by"}

data: {"type":"response.incomplete","response":{"id":"resp_2","model":"gpt-5.6-sol","status":"incomplete","incomplete_details":{"reason":"max_output_tokens"}}}

`

func TestCodexTurnReportsALengthStopAsTruncatedRatherThanAMessage(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.Header().Set("Content-Type", "text/event-stream")
		_, _ = io.WriteString(w, codexStubStreamCutOff)
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
		Messages: []llm.Message{{Role: llm.RoleUser, Content: "write the plan"}},
	})
	if err != nil {
		t.Fatalf("the codex branch failed against a stub: %v", err)
	}
	if decision.Outcome != llm.OutcomeTruncated {
		t.Fatalf("a cut off codex answer is reported as %s", decision.Outcome)
	}
	if decision.Stop != "incomplete:max_output_tokens" || decision.Content != "I will start by" {
		t.Fatalf("the partial answer and its stop reason are %+v", decision)
	}
}

func TestEveryCodexStopMapsToAnOutcomeAndAnUnknownOneFails(t *testing.T) {
	for _, stop := range []codex.Stop{
		codex.StopUnknown, codex.StopEnd, codex.StopLength, codex.StopToolUse, codex.StopError,
	} {
		t.Logf("%s with no tool calls is %s, with tool calls %s",
			stop, llm.OutcomeAfter(stop, 0), llm.OutcomeAfter(stop, 1))
	}
	defer func() {
		if recover() == nil {
			t.Fatal("a stop the wire never defines was mapped instead of failing")
		}
	}()
	llm.OutcomeAfter(codex.Stop(99), 0)
}

func TestBothWiresMapALengthStopToTheSameOutcome(t *testing.T) {
	for _, calls := range []int{0, 2} {
		subscription := llm.OutcomeAfter(anthropic.StopLength, calls)
		codexArm := llm.OutcomeAfter(codex.StopLength, calls)
		if subscription != codexArm {
			t.Fatalf("with %d tool calls the anthropic wire says %s and the codex wire says %s", calls, subscription, codexArm)
		}
		if subscription != llm.OutcomeTruncated {
			t.Fatalf("with %d tool calls a length stop is %s on both wires, and it has to be truncated", calls, subscription)
		}
	}
}
