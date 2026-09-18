package turn

import (
	"context"
	"encoding/json"
	"errors"

	"boji/internal/llm"
)

type stubModel struct {
	decisions []llm.Decision
	calls     int
}

func (m *stubModel) Ask(_ context.Context, _ llm.Request) (llm.Decision, error) {
	if m.calls >= len(m.decisions) {
		return llm.Decision{}, errors.New("stubModel: no more decisions queued")
	}
	decision := m.decisions[m.calls]
	m.calls++
	return decision, nil
}

type errorModel struct {
	err error
}

func (m *errorModel) Ask(_ context.Context, _ llm.Request) (llm.Decision, error) {
	return llm.Decision{}, m.err
}

type stubTool struct {
	name   string
	result Result
	err    error
	calls  int
}

func (t *stubTool) Name() string { return t.name }

func (t *stubTool) Definition() llm.Tool {
	return llm.Tool{Name: t.name, Description: "a stub tool", Parameters: map[string]any{"type": "object"}}
}

func (t *stubTool) Run(_ context.Context, _ json.RawMessage) (Result, error) {
	t.calls++
	return t.result, t.err
}

func toolCallDecision(calls ...llm.ToolCall) llm.Decision {
	return llm.Decision{Build: "m1", Outcome: llm.OutcomeToolCalls, ToolCalls: calls}
}

func messageDecision() llm.Decision {
	return llm.Decision{Build: "m1", Outcome: llm.OutcomeMessage, Content: "done"}
}
