package llm_test

import (
	"testing"

	"tofu/internal/llm"
	"tofu/internal/llm/wire/anthropic"
	"tofu/internal/llm/wire/codex"
	"tofu/internal/transport"
)

func TestAnthropicEncodeRefusesAnUnknownRole(t *testing.T) {
	request := anthropic.Request{Model: "m", Messages: []llm.Message{{Role: llm.RoleUnknown, Content: "hi"}}}
	_, err := request.Encode(false)
	if transport.KindOf(err) != transport.KindBadRequest {
		t.Fatalf("expected kind bad_request, got %v", err)
	}
}

func TestCodexEncodeRefusesAnUnknownRole(t *testing.T) {
	request := codex.Request{Model: "m", Messages: []llm.Message{{Role: llm.RoleUnknown, Content: "hi"}}}
	_, err := request.Encode(nil)
	if transport.KindOf(err) != transport.KindBadRequest {
		t.Fatalf("expected kind bad_request, got %v", err)
	}
}
