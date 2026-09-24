package turn

import (
	"encoding/json"
	"fmt"
	"slices"
	"strconv"
	"strings"

	"tofu/internal/llm"
	"tofu/internal/subagent"
)

type ChildCommand struct {
	Tool     string `json:"tool"`
	Command  string `json:"command,omitempty"`
	ExitCode *int   `json:"exit_code,omitempty"`
	Error    string `json:"error,omitempty"`
}

type ChildReport struct {
	ID         string              `json:"id"`
	Mission    string              `json:"mission"`
	Owns       []string            `json:"owns"`
	State      string              `json:"state"`
	Completion subagent.Completion `json:"completion"`
	Outcome    Outcome             `json:"outcome"`
	Steps      int                 `json:"steps"`
	Attempts   []subagent.Attempt  `json:"attempts"`
	Findings   []subagent.Finding  `json:"findings"`
	Learned    []string            `json:"learned"`
	Wrote      []string            `json:"wrote,omitempty"`
	Ran        []ChildCommand      `json:"ran,omitempty"`
	CostUSD    float64             `json:"cost_usd"`
	Asked      []subagent.Question `json:"asked,omitempty"`
	Prose      string              `json:"prose,omitempty"`
	ProseStep  int                 `json:"prose_step,omitempty"`
}

func reportOf(agent subagent.SubAgent, attempts []Row, state subagent.State) ChildReport {
	row := attempts[len(attempts)-1]
	ended := row.Outcome
	if state == subagent.Parked {
		ended = OutcomeStopped
	}
	report := ChildReport{
		ID:       row.ID,
		Mission:  agent.Mission,
		Owns:     agent.Owns,
		State:    state.String(),
		Outcome:  ended,
		Steps:    len(row.Steps),
		Findings: findings(row, state),
		Learned:  learned(row),
		CostUSD:  row.TotalCostUSD,
	}
	for _, earlier := range attempts[:len(attempts)-1] {
		report.Attempts = append(report.Attempts, attemptOf(earlier, earlier.Outcome))
	}
	report.Attempts = append(report.Attempts, attemptOf(row, ended))
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
	report.Completion = completionOf(state, report.Findings)
	for i := len(row.Steps) - 1; i >= 0; i-- {
		if spoken := strings.TrimSpace(row.Steps[i].AssistantText); spoken != "" {
			report.Prose, report.ProseStep = spoken, i+1
			break
		}
	}
	return report
}

func attemptOf(row Row, ended Outcome) subagent.Attempt {
	var tools []string
	for _, step := range row.Steps {
		for _, call := range step.ToolCalls {
			if !slices.Contains(tools, call.Tool) {
				tools = append(tools, call.Tool)
			}
		}
	}
	tried := "nothing ran"
	if len(tools) > 0 {
		tried = strings.Join(tools, ", ")
	}
	return subagent.Attempt{ID: row.ID, Tried: tried, Outcome: ended.String()}
}

func completionOf(state subagent.State, found []subagent.Finding) subagent.Completion {
	switch state {
	case subagent.Errored, subagent.Parked:
		return subagent.Blocked
	case subagent.WaitingAnswer:
		return subagent.NeedsContext
	case subagent.Working, subagent.InReview, subagent.Finished:
		for _, finding := range found {
			if finding.Bucket.Concerns() {
				return subagent.DoneWithConcerns
			}
		}
		return subagent.Done
	}
	panic("turn: unknown sub-agent state " + strconv.Itoa(int(state)))
}

func findings(row Row, state subagent.State) []subagent.Finding {
	found := []subagent.Finding{}
	if outcome, carries := outcomeFinding(row.Outcome, state); carries {
		found = append(found, outcome)
	}
	var calls []ToolCallRow
	for _, step := range row.Steps {
		calls = append(calls, step.ToolCalls...)
	}
	for i, call := range calls {
		if call.Outcome() != llm.ToolOutcomeFailed {
			continue
		}
		bucket, after := subagent.ActOn, "and nothing after it made "+call.Tool+" work"
		for _, later := range calls[i+1:] {
			if later.Tool == call.Tool && later.Outcome() == llm.ToolOutcomeRan {
				bucket, after = subagent.Dismissed, "and "+call.Tool+" ran after it"
				break
			}
		}
		failure := call.Error
		if failure == "" {
			failure = "exit code " + strconv.Itoa(*call.ExitCode)
		}
		found = append(found, subagent.Finding{Bucket: bucket, Reason: call.Tool + " failed " + after + ": " + failure})
	}
	return found
}

func outcomeFinding(outcome Outcome, state subagent.State) (subagent.Finding, bool) {
	if state == subagent.Parked {
		return subagent.Finding{Bucket: subagent.ActOn,
			Reason: "the child was stopped from outside partway through, so its work is unfinished and what it did do stands"}, true
	}
	switch outcome {
	case OutcomeUnset, OutcomeStopped:
		return subagent.Finding{}, false
	case OutcomeStepCap, OutcomeRetiredCostCap, OutcomeRetiredWallClockCap, OutcomeDecisionCap:
		return subagent.Finding{Bucket: subagent.ActOn,
			Reason: "the child was stopped by the " + outcome.String() + " and its work is unfinished"}, true
	case OutcomeError:
		return subagent.Finding{Bucket: subagent.ActOn, Reason: "the child ended on an error and its work is unfinished"}, true
	case OutcomeLoopGuard:
		return subagent.Finding{Bucket: subagent.ActOn, Reason: "the child repeated one call until the loop guard stopped it"}, true
	case OutcomeTruncated:
		return subagent.Finding{Bucket: subagent.Consider,
			Reason: "output was truncated, so what the child read may be short of what it asked for"}, true
	case OutcomeForked:
		return subagent.Finding{Bucket: subagent.Noted, Reason: "the child forked its conversation and this report is the fork"}, true
	}
	panic("turn: unknown child outcome " + strconv.Itoa(int(outcome)))
}

func learned(row Row) []string {
	var raised []string
	for _, step := range row.Steps {
		raised = append(raised, step.Warnings...)
	}
	carried := []string{}
	for _, warning := range append(raised, row.Warnings...) {
		if !slices.Contains(carried, warning) {
			carried = append(carried, warning)
		}
	}
	return carried
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
	fmt.Fprintf(body, "sub-agent %s is %s, %s, %s after %d steps and %d tool calls, costing $%.4f\n",
		r.ID, r.State, r.Completion, r.Outcome, r.Steps, len(r.Ran), r.CostUSD)
	if len(r.Attempts) > 1 {
		fmt.Fprintf(body, "escalating after %d attempts:\n", len(r.Attempts))
		for i, attempt := range r.Attempts {
			fmt.Fprintf(body, "  attempt %d, %s, tried %s, ended %s\n", i+1, attempt.ID, attempt.Tried, attempt.Outcome)
		}
	}
	if len(r.Wrote) > 0 {
		fmt.Fprintf(body, "wrote %s\n", strings.Join(r.Wrote, ", "))
	}
	for _, finding := range r.Findings {
		fmt.Fprintf(body, "%s: %s\n", finding.Bucket, finding.Reason)
	}
	if len(r.Learned) == 0 {
		body.WriteString("learned nothing: this run raised nothing to carry into the next one\n")
	} else {
		fmt.Fprintf(body, "learned: %s\n", strings.Join(r.Learned, "; "))
	}
	for _, question := range r.Asked {
		fmt.Fprintf(body, "asks a %s: %s\n", question.Kind, question.Ask)
	}
	if r.ProseStep > 0 && r.ProseStep < r.Steps {
		fmt.Fprintf(body, "it last spoke at step %d of %d:\n", r.ProseStep, r.Steps)
	}
	body.WriteString(r.Prose)
	return body.String()
}
