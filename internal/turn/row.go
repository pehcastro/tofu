package turn

import (
	"encoding/json"
	"time"
)

type Outcome int

const (
	OutcomeUnset Outcome = iota
	OutcomeStopped
	OutcomeStepCap
	OutcomeCostCap
	OutcomeWallClockCap
	OutcomeError
)

func (o Outcome) String() string {
	switch o {
	case OutcomeUnset:
		return "unset"
	case OutcomeStopped:
		return "stopped"
	case OutcomeStepCap:
		return "step_cap"
	case OutcomeCostCap:
		return "cost_cap"
	case OutcomeWallClockCap:
		return "wall_clock_cap"
	case OutcomeError:
		return "error"
	}
	panic("turn: unknown outcome")
}

type ToolCallRow struct {
	Tool          string
	Args          json.RawMessage
	Command       string
	ExitCode      *int
	ResultBytes   int
	RenderedBytes int
	ResultHash    string
	DurationMS    int64
	Error         string
}

type StepRow struct {
	Index            int
	ToolCalls        []ToolCallRow
	AssistantText    string
	PromptTokens     int
	CompletionTokens int
	CostUSD          float64
}

type Row struct {
	ID           string
	At           time.Time
	Task         string
	Model        string
	Steps        []StepRow
	Outcome      Outcome
	TotalCostUSD float64
	WallClockMS  int64
}
