package main

import (
	"bytes"
	"encoding/json"
	"strings"
	"testing"

	"tofu/internal/llm"
	"tofu/internal/llm/wire/anthropic"
)

func dryRunEffort(t *testing.T, args ...string) map[string]any {
	t.Helper()
	var out, errOut bytes.Buffer
	if code := runVerb(append([]string{"--dir", t.TempDir(), "--dry-run"}, args...), &out, &errOut); code != exitOK {
		t.Fatalf("exit %d, stderr %q", code, errOut.String())
	}
	var body map[string]any
	if err := json.Unmarshal(out.Bytes(), &body); err != nil {
		t.Fatalf("the printed request is not json: %v", err)
	}
	return body
}

func TestTheEffortFlagReachesTheAnthropicRequest(t *testing.T) {
	body := dryRunEffort(t, "--effort", string(llm.EffortMedium), "write hello.txt")
	config, carried := body["output_config"].(map[string]any)
	if !carried {
		t.Fatalf("--effort medium built a request with no output_config, so the turn thinks at none")
	}
	if config["effort"] != string(llm.EffortMedium) {
		t.Fatalf("output_config.effort = %v, want %q", config["effort"], llm.EffortMedium)
	}
}

func TestTheEffortFlagReachesTheCodexRequest(t *testing.T) {
	body := dryRunEffort(t, "--wire", wireCodex, "--effort", string(llm.EffortHigh), "write hello.txt")
	reasoning, carried := body["reasoning"].(map[string]any)
	if !carried {
		t.Fatalf("--effort high built a codex request with no reasoning block")
	}
	if reasoning["effort"] != string(llm.EffortHigh) {
		t.Fatalf("reasoning.effort = %v, want %q", reasoning["effort"], llm.EffortHigh)
	}
}

func TestTheFlagPathRefusesALevelTheAnthropicWireDoesNotTake(t *testing.T) {
	var out, errOut bytes.Buffer
	if code := runVerb([]string{"--dir", t.TempDir(), "--dry-run", "--effort", string(llm.EffortMinimal), "write hello.txt"}, &out, &errOut); code == exitOK {
		t.Fatalf("minimal is not an anthropic level and the run was built anyway: %s", out.String())
	}
	for _, level := range anthropic.ReasoningEfforts() {
		if !strings.Contains(errOut.String(), string(level)) {
			t.Fatalf("the refusal does not name %q: %s", level, errOut.String())
		}
	}
}

func TestAnEffortOutsideTheVocabularyIsRefusedWithTheList(t *testing.T) {
	_, err := parseRunArgs([]string{"--dir", t.TempDir(), "--effort", "turbo", "a task"})
	if err == nil {
		t.Fatal("turbo is no level and it was accepted")
	}
	for _, level := range llm.Efforts() {
		if !strings.Contains(err.Error(), string(level)) {
			t.Fatalf("the refusal does not name %q: %v", level, err)
		}
	}
}

func TestTheDefaultEffortIsTheOneTheHelpTextStates(t *testing.T) {
	opts, err := parseRunArgs([]string{"--dir", t.TempDir(), "a task"})
	if err != nil {
		t.Fatalf("parsing: %v", err)
	}
	if opts.effort != llm.EffortDefault {
		t.Fatalf("with no --effort the run is at %q, want %q", opts.effort, llm.EffortDefault)
	}
	if want := "the default is " + string(llm.EffortDefault); !strings.Contains(runUsage(), want) {
		t.Fatalf("the help text does not say %q:\n%s", want, runUsage())
	}
}

func TestTheOpenrouterWireRefusesAnEffortRatherThanDroppingIt(t *testing.T) {
	opts, err := parseRunArgs([]string{"--dir", t.TempDir(), "--wire", wireKey, "a task"})
	if err != nil {
		t.Fatalf("parsing: %v", err)
	}
	if opts.effort != "" {
		t.Fatalf("the openrouter wire was given effort %q and it sends none", opts.effort)
	}
	if _, err := parseRunArgs([]string{"--dir", t.TempDir(), "--wire", wireKey, "--effort", string(llm.EffortHigh), "a task"}); err == nil {
		t.Fatal("--effort on the openrouter wire was accepted and would be silently dropped")
	}
}
