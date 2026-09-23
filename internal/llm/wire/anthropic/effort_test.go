package anthropic

import (
	"encoding/json"
	"strings"
	"testing"

	"tofu/internal/llm"
)

func encodedEffort(t *testing.T, effort llm.Effort) map[string]any {
	t.Helper()
	body, err := Request{
		Model:    "claude-opus-5",
		Messages: []llm.Message{{Role: llm.RoleUser, Content: "write a note"}},
		Effort:   effort,
	}.Encode(true)
	if err != nil {
		t.Fatalf("encode at effort %q: %v", effort, err)
	}
	var decoded map[string]any
	if err := json.Unmarshal(body, &decoded); err != nil {
		t.Fatalf("decode: %v", err)
	}
	return decoded
}

func TestAnEffortReachesTheAnthropicRequestAsAnOutputConfig(t *testing.T) {
	config, carried := encodedEffort(t, llm.EffortMedium)["output_config"].(map[string]any)
	if !carried {
		t.Fatalf("the request carries no output_config, so the turn runs with no thinking at all")
	}
	if config["effort"] != string(llm.EffortMedium) {
		t.Fatalf("output_config.effort = %v, want %q", config["effort"], llm.EffortMedium)
	}
}

func TestNoEffortLeavesTheAnthropicRequestWithoutAnOutputConfig(t *testing.T) {
	if config, carried := encodedEffort(t, llm.EffortNone)["output_config"]; carried {
		t.Fatalf("effort none still sent %v", config)
	}
}

func TestTheAnthropicWireRefusesAnEffortItDoesNotTakeAndNamesItsOwn(t *testing.T) {
	_, err := Request{
		Model:    "claude-opus-5",
		Messages: []llm.Message{{Role: llm.RoleUser, Content: "write a note"}},
		Effort:   llm.EffortMinimal,
	}.Encode(true)
	if err == nil {
		t.Fatalf("minimal is not an anthropic level and it was accepted")
	}
	for _, level := range ReasoningEfforts() {
		if !strings.Contains(err.Error(), string(level)) {
			t.Fatalf("the refusal does not name %q, so nobody learns what this wire takes: %v", level, err)
		}
	}
}
