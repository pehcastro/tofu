package state

import (
	memoryscope "tofu/internal/judge/points/memory_scope"
	toolgate "tofu/internal/judge/points/tool_gate"
)

type ToolGateInput = toolgate.ToolGateInput

func TargetsOf(in ToolGateInput) toolgate.WriteTargets { return toolgate.TargetsOf(in) }

const (
	MemoryScopePoint           = memoryscope.MemoryScopePoint
	MemoryScopeQuestion        = memoryscope.MemoryScopeQuestion
	MemoryNamesPrivateQuestion = memoryscope.MemoryNamesPrivateQuestion
	MemoryScopeNone            = memoryscope.MemoryScopeNone
)

type MemoryScopeState = memoryscope.MemoryScopeState

func BuildMemoryScope(in MemoryScopeState) ([]byte, string, error) {
	return memoryscope.BuildMemoryScope(in)
}

func MemoryScopeOf(option string) string { return memoryscope.MemoryScopeOf(option) }

func RefusesProjectLocal(namesPrivate float64, answered bool) bool {
	return memoryscope.RefusesProjectLocal(namesPrivate, answered)
}
