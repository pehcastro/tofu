package main

import (
	"bytes"
	"encoding/json"
	"strings"
	"testing"

	"boji/internal/konst"
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

func TestRunDefaultsToTheSubscriptionAndItsModel(t *testing.T) {
	opts, err := parseRunArgs([]string{"--dir", t.TempDir(), "a task"})
	if err != nil {
		t.Fatalf("parseRunArgs returned an error: %v", err)
	}
	if opts.wire != wireSubscription {
		t.Fatalf("expected the subscription wire by default, got %q", opts.wire)
	}
	if opts.modelID() != konst.TurnAnthropicModel {
		t.Fatalf("expected the subscription model %q, got %q", konst.TurnAnthropicModel, opts.modelID())
	}
}

func TestRunKeepsTheOpenRouterArmReachableWithItsOwnModel(t *testing.T) {
	opts, err := parseRunArgs([]string{"--dir", t.TempDir(), "--wire", "openrouter", "a task"})
	if err != nil {
		t.Fatalf("parseRunArgs returned an error: %v", err)
	}
	if opts.wire != wireKey || opts.modelID() != konst.TurnOpenRouterModel {
		t.Fatalf("expected the openrouter arm on %q, got %+v", konst.TurnOpenRouterModel, opts)
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
	if body.Model != konst.TurnAnthropicModel {
		t.Fatalf("the dry run model is %q", body.Model)
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
	const chosen = "claude-opus-4-1-20250805"
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
