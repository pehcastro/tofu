package codex

import (
	"cmp"
	"context"
	"os"
	"testing"

	"tofu/internal/models/llm/accounts"
	"tofu/internal/models/llm/core"
	codexsub "tofu/internal/models/llm/providers/codex-sub"
)

const liveModel = "gpt-5.1-codex"

func liveWire(t *testing.T) *Wire {
	t.Helper()
	if os.Getenv("TOFU_LIVE_CODEX") != "1" {
		t.Skip("set TOFU_LIVE_CODEX=1 to spend a turn of the subscription quota; " +
			"on 2026-09-19 tofu usage read the codex 7d window at 100% with a reset at 2026-09-19T18:44:30Z, " +
			"so a request before that reset answers with a quota rejection rather than a completion")
	}
	path, err := accounts.Path()
	if err != nil {
		t.Fatalf("locating the credential store: %v", err)
	}
	spec := codexsub.Spec()
	store, err := accounts.Open(path, accounts.Specs{accounts.CodexSub: spec})
	if err != nil {
		t.Skipf("no credential store, run tofu login codex-sub: %v", err)
	}
	t.Cleanup(func() { _ = store.Close() })

	wire, err := New(Config{
		Model:          cmp.Or(os.Getenv("TOFU_LIVE_CODEX_MODEL"), liveModel),
		Token:          accounts.NewManager(store, spec).Token,
		InstallationID: "tofu-live-test",
		SessionID:      "00000000-0000-4000-8000-000000000002",
	})
	if err != nil {
		t.Fatalf("building the wire: %v", err)
	}
	return wire
}

func TestLiveCompletion(t *testing.T) {
	wire := liveWire(t)
	result, dump, err := wire.Ask(context.Background(), Request{
		Instructions: "Answer in one word.",
		Messages:     []core.Message{{Role: core.RoleUser, Content: "Reply with the single word: ok"}},
	})
	t.Logf("request dump:\n%s", dump)
	if err != nil {
		t.Fatalf("asking: %v", err)
	}
	if !dump.Subscription {
		t.Fatal("the live request did not take the subscription branch")
	}
	t.Logf("model %s stop %s content %q usage %+v warnings %v",
		result.Model, result.Stop, result.Content, result.Usage, result.Warnings)
	if result.Content == "" {
		t.Fatal("the live turn returned no content")
	}
}

func TestLiveToolCallRoundTrip(t *testing.T) {
	wire := liveWire(t)
	tools := []core.Tool{{
		Name:        "probe",
		Description: "Return the answer to a fixed question.",
		Parameters: map[string]any{
			"type":       "object",
			"properties": map[string]any{"question": map[string]any{"type": "string"}},
			"required":   []string{"question"},
		},
	}}
	messages := []core.Message{{Role: core.RoleUser,
		Content: "Call the probe tool once with question=\"colour\", then reply with its answer and nothing else."}}

	first, dump, err := wire.Ask(context.Background(), Request{Tools: tools, Messages: messages})
	t.Logf("request dump:\n%s", dump)
	if err != nil {
		t.Fatalf("first turn: %v", err)
	}
	if first.Stop != StopToolUse || len(first.ToolCalls) != 1 {
		t.Fatalf("the model did not call the tool once: %+v", first)
	}
	t.Logf("first turn stop %s call %s %s %s", first.Stop,
		first.ToolCalls[0].ID, first.ToolCalls[0].Name, first.ToolCalls[0].Arguments)

	messages = append(messages,
		core.Message{Role: core.RoleAssistant, Content: first.Content, ToolCalls: first.ToolCalls},
		core.Message{Role: core.RoleTool, ToolCallID: first.ToolCalls[0].ID, Content: "chartreuse"},
	)
	second, secondDump, err := wire.Ask(context.Background(), Request{Tools: tools, Messages: messages})
	t.Logf("second request dump:\n%s", secondDump)
	if err != nil {
		t.Fatalf("second turn: %v", err)
	}
	t.Logf("second turn stop %s content %q usage %+v", second.Stop, second.Content, second.Usage)
	if second.Stop != StopEnd || second.Content == "" {
		t.Fatalf("the turn did not complete: %+v", second)
	}
}
