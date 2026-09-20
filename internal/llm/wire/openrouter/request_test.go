package openrouter

import (
	"bytes"
	"testing"

	"tofu/internal/llm"
	"tofu/internal/llm/wire/anthropic"
)

func TestMarkStablePrefixMarksTheStablePrefixTheWayAnthropicDoes(t *testing.T) {
	anthropicBody, err := anthropic.Request{
		Model:    "claude-opus-4-1-20250805",
		System:   []string{"be terse"},
		Messages: []llm.Message{{Role: llm.RoleUser, Content: "hi"}},
		Tools:    []llm.Tool{{Name: "t", Description: "d", Parameters: map[string]any{"type": "object"}}},
	}.Encode(false)
	if err != nil {
		t.Fatalf("encoding the anthropic request: %v", err)
	}
	if !bytes.Contains(anthropicBody, []byte(`"cache_control"`)) {
		t.Fatal("the anthropic request carries no cache_control to compare against")
	}

	request := llm.Request{
		Messages: []llm.Message{
			{Role: llm.RoleSystem, Content: "be terse"},
			{Role: llm.RoleUser, Content: "hi"},
		},
		Tools: []llm.Tool{{Name: "t", Description: "d", Parameters: map[string]any{"type": "object"}}},
	}
	body, err := request.Encode("m")
	if err != nil {
		t.Fatalf("encoding the openrouter request: %v", err)
	}
	marked, err := MarkStablePrefix(body)
	if err != nil {
		t.Fatalf("marking: %v", err)
	}
	if count := bytes.Count(marked, []byte(`"cache_control"`)); count != 2 {
		t.Fatalf("expected one marker on the system block and one on the last tool, got %d in %s", count, marked)
	}
}

func TestMarkStablePrefixLeavesARequestWithNoStablePrefixUnchanged(t *testing.T) {
	request := llm.Request{
		Messages: []llm.Message{{Role: llm.RoleUser, Content: "hi"}},
	}
	body, err := request.Encode("m")
	if err != nil {
		t.Fatalf("encoding: %v", err)
	}
	marked, err := MarkStablePrefix(body)
	if err != nil {
		t.Fatalf("marking: %v", err)
	}
	if !bytes.Equal(marked, body) {
		t.Fatalf("a request with no stable prefix was changed:\nbefore %s\nafter  %s", body, marked)
	}
}
