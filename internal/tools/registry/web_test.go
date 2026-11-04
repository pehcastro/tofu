package registry_test

import (
	"context"

	llm "tofu/internal/models/llm/core"
)

type scriptedModel struct {
	calls []llm.ToolCall
	step  int
}

func (m *scriptedModel) Ask(context.Context, llm.Request) (llm.Decision, error) {
	if m.step >= len(m.calls) {
		return llm.Decision{Build: "scripted", Outcome: llm.OutcomeMessage, Content: "done"}, nil
	}
	call := m.calls[m.step]
	m.step++
	return llm.Decision{Build: "scripted", Outcome: llm.OutcomeToolCalls, ToolCalls: []llm.ToolCall{call}}, nil
}
