package state

import (
	"encoding/json"
	"strings"
	"testing"
)

func TestBuildToolGateLeavesAnUnknownContextPresentAndEmpty(t *testing.T) {
	got, _, err := BuildToolGate(ToolGateInput{Agent: "boji", Tool: "bash", Input: map[string]any{"command": "ls"}, Cwd: "/tmp"})
	if err != nil {
		t.Fatalf("BuildToolGate: %v", err)
	}

	var decoded map[string]any
	if err := json.Unmarshal(got, &decoded); err != nil {
		t.Fatalf("decode built state: %v", err)
	}

	context, ok := decoded["context"].(map[string]any)
	if !ok {
		t.Fatalf("context field is missing or not an object, got %#v", decoded["context"])
	}

	messages, present := context["user_recent_messages"]
	if !present {
		t.Fatal("user_recent_messages is absent, it must be present and empty")
	}
	list, ok := messages.([]any)
	if !ok || len(list) != 0 {
		t.Fatalf("user_recent_messages = %#v, want an empty array", messages)
	}

	flagged, present := context["flagged_untrusted_content"]
	if !present {
		t.Fatal("flagged_untrusted_content is absent, it must be present and empty")
	}
	if flagged != nil {
		t.Fatalf("flagged_untrusted_content = %#v, want an explicit null", flagged)
	}
}

func TestBuildToolGateVersionIsDottedByPoint(t *testing.T) {
	_, version, err := BuildToolGate(ToolGateInput{Agent: "boji", Tool: "bash", Input: map[string]any{"command": "ls"}, Cwd: "/tmp"})
	if err != nil {
		t.Fatalf("BuildToolGate: %v", err)
	}
	if !strings.HasPrefix(version, ToolGatePoint+".") {
		t.Fatalf("version = %q, want prefix %q", version, ToolGatePoint+".")
	}
}
