package turn

import (
	"context"
	"encoding/json"
	"testing"

	"tofu/internal/llm"
	"tofu/internal/recall"
)

func toolNames(defs []llm.Tool) []string {
	names := make([]string, len(defs))
	for i, def := range defs {
		names[i] = def.Name
	}
	return names
}

func hasTool(defs []llm.Tool, name string) bool {
	for _, def := range defs {
		if def.Name == name {
			return true
		}
	}
	return false
}

func TestTheRegistryIsReadFreshSoAToolAddedBetweenStepsAppearsInTheSecondRequest(t *testing.T) {
	read := &stubTool{name: "read", result: Result{Content: "ok"}}
	write := &stubTool{name: "write", result: Result{Content: "ok"}}
	model := &stubModel{decisions: []llm.Decision{
		toolCallDecision(llm.ToolCall{ID: "call-1", Name: "read", Arguments: json.RawMessage(`{}`)}),
		messageDecision(),
	}}
	config := baseConfig(t, model, Registry{})
	served := 0
	config.ToolSource = func() Registry {
		served++
		if served == 1 {
			return NewRegistry(read)
		}
		return NewRegistry(read, write)
	}

	if _, err := Run(context.Background(), config); err != nil {
		t.Fatalf("run: %v", err)
	}
	if len(model.requests) != 2 {
		t.Fatalf("expected 2 requests, got %d", len(model.requests))
	}
	if hasTool(model.requests[0].Tools, "write") {
		t.Fatalf("the first request already carries write: %v", toolNames(model.requests[0].Tools))
	}
	if !hasTool(model.requests[1].Tools, "write") {
		t.Fatalf("the second request does not carry write: %v", toolNames(model.requests[1].Tools))
	}
}

func TestTheRegistryIsReadFreshSoAToolRemovedBetweenStepsIsGoneFromTheSecondRequest(t *testing.T) {
	read := &stubTool{name: "read", result: Result{Content: "ok"}}
	write := &stubTool{name: "write", result: Result{Content: "ok"}}
	model := &stubModel{decisions: []llm.Decision{
		toolCallDecision(llm.ToolCall{ID: "call-1", Name: "read", Arguments: json.RawMessage(`{}`)}),
		messageDecision(),
	}}
	config := baseConfig(t, model, Registry{})
	served := 0
	config.ToolSource = func() Registry {
		served++
		if served == 1 {
			return NewRegistry(read, write)
		}
		return NewRegistry(read)
	}

	if _, err := Run(context.Background(), config); err != nil {
		t.Fatalf("run: %v", err)
	}
	if len(model.requests) != 2 {
		t.Fatalf("expected 2 requests, got %d", len(model.requests))
	}
	if !hasTool(model.requests[0].Tools, "write") {
		t.Fatalf("the first request does not carry write: %v", toolNames(model.requests[0].Tools))
	}
	if hasTool(model.requests[1].Tools, "write") {
		t.Fatalf("the second request still carries write: %v", toolNames(model.requests[1].Tools))
	}
}

func TestAForkTakesTheCurrentRegistry(t *testing.T) {
	switched := false
	before := &childTool{name: "read", run: func(context.Context) (Result, error) {
		switched = true
		return Result{Content: "ok"}, nil
	}}
	after := &stubTool{name: "glob", result: Result{Content: "ok"}}
	model := &stubModel{decisions: []llm.Decision{
		toolCallDecision(llm.ToolCall{ID: "call-1", Name: "read", Arguments: json.RawMessage(`{}`)}),
		messageDecision(),
	}}
	config := baseConfig(t, model, Registry{})
	config.ArtifactDir = t.TempDir()
	config.Budget = recall.Budget{Bands: recall.Bands{Identity: 1, Facts: 1, WorkingSet: 1, Recent: 1}}
	config.ToolSource = func() Registry {
		if switched {
			return NewRegistry(before, after)
		}
		return NewRegistry(before)
	}
	var ended Row
	config.EndedSession = func(row Row) error { ended = row; return nil }

	if _, err := Run(context.Background(), config); err != nil {
		t.Fatalf("run: %v", err)
	}
	if len(ended.Steps) == 0 || ended.Steps[len(ended.Steps)-1].Fork == nil {
		t.Fatal("the turn never forked, so nothing is proved about what a fork carries forward")
	}
	if len(model.requests) != 2 {
		t.Fatalf("expected 2 requests, got %d", len(model.requests))
	}
	if hasTool(model.requests[0].Tools, "glob") {
		t.Fatalf("the request before the fork already carries glob: %v", toolNames(model.requests[0].Tools))
	}
	if !hasTool(model.requests[1].Tools, "glob") {
		t.Fatalf("the request after the fork does not carry the registry current at that point: %v", toolNames(model.requests[1].Tools))
	}
}
