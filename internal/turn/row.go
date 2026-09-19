package turn

import (
	"encoding/json"
	"fmt"
	"time"
)

const SchemaVersion = 1

type Outcome int

const (
	OutcomeUnset Outcome = iota
	OutcomeStopped
	OutcomeStepCap
	OutcomeRetiredCostCap
	OutcomeWallClockCap
	OutcomeDecisionCap
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
	case OutcomeRetiredCostCap:
		return "cost_cap"
	case OutcomeWallClockCap:
		return "wall_clock_cap"
	case OutcomeDecisionCap:
		return "decision_cap"
	case OutcomeError:
		return "error"
	}
	panic("turn: unknown outcome")
}

func (o Outcome) MarshalJSON() ([]byte, error) {
	return json.Marshal(o.String())
}

func (o *Outcome) UnmarshalJSON(data []byte) error {
	var text string
	if err := json.Unmarshal(data, &text); err != nil {
		return err
	}
	for candidate := OutcomeUnset; candidate <= OutcomeError; candidate++ {
		if candidate.String() == text {
			*o = candidate
			return nil
		}
	}
	return fmt.Errorf("turn: row carries unknown outcome %q", text)
}

type ToolCallRow struct {
	Tool           string          `json:"tool"`
	Args           json.RawMessage `json:"args,omitempty"`
	Command        string          `json:"command,omitempty"`
	ExitCode       *int            `json:"exit_code,omitempty"`
	ResultBytes    int             `json:"result_bytes"`
	RenderedBytes  int             `json:"rendered_bytes"`
	ResultHash     string          `json:"result_hash,omitempty"`
	GateDecisionID string          `json:"gate_decision_id,omitempty"`
	GateVerdict    string          `json:"gate_verdict,omitempty"`
	GateError      string          `json:"gate_error,omitempty"`
	DurationMS     int64           `json:"duration_ms"`
	Error          string          `json:"error,omitempty"`
}

type StepRow struct {
	Index            int           `json:"index"`
	ToolCalls        []ToolCallRow `json:"tool_calls,omitempty"`
	AssistantText    string        `json:"assistant_text,omitempty"`
	PromptTokens     int           `json:"prompt_tokens"`
	CompletionTokens int           `json:"completion_tokens"`
	CostUSD          float64       `json:"cost_usd"`
}

type Row struct {
	ID           string    `json:"id"`
	Schema       int       `json:"schema"`
	At           time.Time `json:"at"`
	Task         string    `json:"task"`
	Model        string    `json:"model"`
	Steps        []StepRow `json:"steps,omitempty"`
	Outcome      Outcome   `json:"outcome"`
	TotalCostUSD float64   `json:"total_cost_usd"`
	WallClockMS  int64     `json:"wall_clock_ms"`
	DecisionIDs  []string  `json:"decision_ids,omitempty"`
}
