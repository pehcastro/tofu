package wiring

import (
	"tofu/internal/agent/loop"
	"tofu/internal/models/llm/core"
)

type RunOpts struct {
	Dir              string
	Task             string
	TurnID           string
	Session          string
	Resume           string
	Wire             string
	DryRun           bool
	ShowPrompt       bool
	Agent            string
	GateArm          string
	NoSubAgents      bool
	ReadBeforeEdit   bool
	PlanEveryStep    bool
	DoneArm          string
	Model            string
	ToolSet          string
	Effort           llm.Effort
	MaxSteps         int
	LoopGuardRepeats int
	LoopGuardWindow  int
	ContextCeiling   int
	SiftArm          string
	ThinkingSummary  string
	NoInstructions   bool
	NoDocs           bool
	SubAgentList     string
	LeadMayWorkAlone bool
	LeadAlone        bool
	Shell            turn.RunShell
}
