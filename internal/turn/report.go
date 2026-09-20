package turn

import (
	"encoding/json"
	"fmt"
	"strings"

	"tofu/internal/crew"
)

type ChildCommand struct {
	Tool     string `json:"tool"`
	Command  string `json:"command,omitempty"`
	ExitCode *int   `json:"exit_code,omitempty"`
	Error    string `json:"error,omitempty"`
}

type ChildReport struct {
	ID      string         `json:"id"`
	Mission string         `json:"mission"`
	Owns    []string       `json:"owns"`
	State   string         `json:"state"`
	Outcome Outcome        `json:"outcome"`
	Steps   int            `json:"steps"`
	Wrote   []string       `json:"wrote,omitempty"`
	Ran     []ChildCommand `json:"ran,omitempty"`
	CostUSD float64        `json:"cost_usd"`
	Prose   string         `json:"prose,omitempty"`
}

func reportOf(agent crew.SubAgent, row Row, state crew.State) ChildReport {
	report := ChildReport{
		ID:      row.ID,
		Mission: agent.Mission,
		Owns:    agent.Owns,
		State:   state.String(),
		Outcome: row.Outcome,
		Steps:   len(row.Steps),
		CostUSD: row.TotalCostUSD,
	}
	for _, step := range row.Steps {
		for _, call := range step.ToolCalls {
			report.Ran = append(report.Ran, ChildCommand{
				Tool: call.Tool, Command: call.Command, ExitCode: call.ExitCode, Error: call.Error,
			})
			if path := writtenPath(call); path != "" {
				report.Wrote = append(report.Wrote, path)
			}
		}
	}
	if len(row.Steps) > 0 {
		report.Prose = row.Steps[len(row.Steps)-1].AssistantText
	}
	return report
}

func writtenPath(call ToolCallRow) string {
	if call.Error != "" || (call.Tool != "write" && call.Tool != "edit") {
		return ""
	}
	var args struct {
		Path string `json:"path"`
	}
	if err := json.Unmarshal(call.Args, &args); err != nil {
		return ""
	}
	return args.Path
}

func (r ChildReport) Text() string {
	body := &strings.Builder{}
	fmt.Fprintf(body, "sub-agent %s is %s, %s after %d steps and %d tool calls, costing $%.4f\n",
		r.ID, r.State, r.Outcome, r.Steps, len(r.Ran), r.CostUSD)
	if len(r.Wrote) > 0 {
		fmt.Fprintf(body, "wrote %s\n", strings.Join(r.Wrote, ", "))
	}
	for _, call := range r.Ran {
		if call.Error != "" {
			fmt.Fprintf(body, "could not: %s: %s\n", call.Tool, call.Error)
		}
	}
	body.WriteString(r.Prose)
	return body.String()
}
