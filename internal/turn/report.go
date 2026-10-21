package turn

import (
	"encoding/json"
	"fmt"
	"slices"
	"strconv"
	"strings"

	"tofu/internal/boardy"
	"tofu/internal/konst"
	"tofu/internal/llm"
	"tofu/internal/recall"
	"tofu/internal/subagent"
)

type SubAgentCommand struct {
	Tool     string `json:"tool"`
	Command  string `json:"command,omitempty"`
	Asked    string `json:"asked,omitempty"`
	ExitCode *int   `json:"exit_code,omitempty"`
	Error    string `json:"error,omitempty"`
}

type SubAgentReport struct {
	ID         string              `json:"id"`
	Mission    string              `json:"mission"`
	Owns       []string            `json:"owns"`
	State      string              `json:"state"`
	Completion subagent.Completion `json:"completion"`
	Outcome    Outcome             `json:"outcome"`
	Steps      int                 `json:"steps"`
	Forks      int                 `json:"forks,omitempty"`
	Attempts   []subagent.Attempt  `json:"attempts"`
	Findings   []subagent.Finding  `json:"findings"`
	Learned    []string            `json:"learned"`
	Wrote      []string            `json:"wrote,omitempty"`
	Ran        []SubAgentCommand   `json:"ran,omitempty"`
	CostUSD    float64             `json:"cost_usd"`
	Asked      []subagent.Question `json:"asked,omitempty"`
	Prose      string              `json:"prose,omitempty"`
	ProseStep  int                 `json:"prose_step,omitempty"`
	Result     *subagent.Result    `json:"result,omitempty"`
}

func reportOf(agent subagent.SubAgent, forked, attempts []Row, state subagent.State) SubAgentReport {
	row := attempts[len(attempts)-1]
	ended := row.Outcome
	if state == subagent.Parked {
		ended = OutcomeStopped
	}
	report := SubAgentReport{
		ID:       agent.ID,
		Mission:  agent.Mission,
		Owns:     agent.Owns,
		State:    state.String(),
		Outcome:  ended,
		Forks:    len(forked),
		Findings: findings(row, state),
		Learned:  learned(row),
	}
	for _, earlier := range attempts[:len(attempts)-1] {
		report.Attempts = append(report.Attempts, attemptOf(earlier, earlier.Outcome))
	}
	report.Attempts = append(report.Attempts, attemptOf(row, ended))
	for _, segment := range append(slices.Clone(forked), row) {
		report.Steps, report.CostUSD = report.Steps+len(segment.Steps), report.CostUSD+segment.TotalCostUSD
		for _, step := range segment.Steps {
			for _, call := range step.ToolCalls {
				if call.Tool == FinishToolName {
					continue
				}
				ran := SubAgentCommand{Tool: call.Tool, Command: call.Command, ExitCode: call.ExitCode, Error: call.Error}
				if call.Proxy != nil {
					ran.Asked = call.Proxy.Asked
				}
				report.Ran = append(report.Ran, ran)
				if path := writtenPath(call); path != "" {
					report.Wrote = append(report.Wrote, path)
				}
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
		if call.Outcome() != llm.ToolOutcomeFailed || slices.ContainsFunc(calls[i+1:], func(later ToolCallRow) bool {
			return later.Tool == call.Tool && later.Outcome() == llm.ToolOutcomeRan
		}) {
			continue
		}
		failure := call.Error
		if failure == "" {
			failure = "exit code " + strconv.Itoa(*call.ExitCode)
		}
		found = append(found, subagent.Finding{Bucket: subagent.ActOn, Text: call.Tool + " failed and nothing after it made " + call.Tool + " work: " + failure})
	}
	return found
}

func outcomeFinding(outcome Outcome, state subagent.State) (subagent.Finding, bool) {
	if state == subagent.Parked {
		return subagent.Finding{Bucket: subagent.ActOn,
			Text: "the sub-agent was stopped from outside partway through, so its work is unfinished and what it did do stands"}, true
	}
	switch outcome {
	case OutcomeUnset, OutcomeStopped:
		return subagent.Finding{}, false
	case OutcomeStepCap, OutcomeRetiredCostCap, OutcomeRetiredWallClockCap, OutcomeDecisionCap:
		return subagent.Finding{Bucket: subagent.ActOn,
			Text: "the sub-agent was stopped by the " + outcome.String() + " and its work is unfinished"}, true
	case OutcomeError:
		return subagent.Finding{Bucket: subagent.ActOn, Text: "the sub-agent ended on an error and its work is unfinished"}, true
	case OutcomeLoopGuard:
		return subagent.Finding{Bucket: subagent.ActOn, Text: "the sub-agent repeated one call until the loop guard stopped it"}, true
	case OutcomeTruncated:
		return subagent.Finding{Bucket: subagent.Consider,
			Text: "output was truncated, so what the sub-agent read may be short of what it asked for"}, true
	case OutcomeForked:
		return subagent.Finding{Bucket: subagent.Noted, Text: "the sub-agent forked its conversation and this report is the fork"}, true
	}
	panic("turn: unknown sub-agent outcome " + strconv.Itoa(int(outcome)))
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

func (r SubAgentReport) Text(store *recall.Store) string {
	body := &strings.Builder{}
	forks := ""
	if r.Forks > 0 {
		forks = fmt.Sprintf(" across %d forks", r.Forks)
	}
	fmt.Fprintf(body, "sub-agent %s is %s, %s, %s after %d steps and %d tool calls%s, costing $%.4f\n",
		r.ID, r.State, r.Completion, r.Outcome, r.Steps, len(r.Ran), forks, r.CostUSD)
	if len(r.Attempts) > 1 {
		fmt.Fprintf(body, "escalating after %d attempts:\n", len(r.Attempts))
		for i, attempt := range r.Attempts {
			fmt.Fprintf(body, "  attempt %d, %s, tried %s, ended %s\n", i+1, attempt.ID, attempt.Tried, attempt.Outcome)
		}
	}
	finished := r.Result != nil && r.Result.Measured.Finished
	if len(r.Wrote) > 0 && !finished {
		fmt.Fprintf(body, "wrote %s\n", strings.Join(r.Wrote, ", "))
	}
	for _, finding := range r.Findings {
		fmt.Fprintf(body, "%s: %s\n", finding.Bucket, finding.Text)
	}
	if len(r.Learned) > 0 {
		fmt.Fprintf(body, "learned: %s\n", strings.Join(r.Learned, "; "))
	}
	for _, question := range r.Asked {
		fmt.Fprintf(body, "asks a %s: %s\n", question.Kind, question.Ask)
	}
	prose, spoke := r.Prose, "its last message"
	switch {
	case finished:
		body.WriteString(r.Result.Text())
		prose, spoke = r.Result.Body, "its body"
	case r.ProseStep > 0 && r.ProseStep < r.Steps:
		spoke = fmt.Sprintf("it last spoke at step %d of %d", r.ProseStep, r.Steps)
	}
	if prose == "" {
		return body.String()
	}
	if len(prose) <= konst.SubAgentProseBytes {
		fmt.Fprintf(body, "%s, each line marked >:\n%s", spoke, marked(prose))
		return body.String()
	}
	head, tail := runeSafeHead(prose, konst.SubAgentProseEndBytes), runeSafeTail(prose, konst.SubAgentProseEndBytes)
	dropped := len(prose) - len(head) - len(tail)
	held := fmt.Sprintf("the %d bytes between were dropped and not stored anywhere", dropped)
	if store != nil {
		elided, err := recall.Elide(store, recall.Config{ElideAboveBytes: konst.SubAgentProseBytes}, []byte(prose), true)
		if err != nil {
			held += ", because storing them failed: " + err.Error()
		} else {
			held = "artifact " + elided.Reference.ID + " holds it whole: call artifact_fetch with this handle, an offset and a length to read any other range"
		}
	}
	fmt.Fprintf(body, "%s is %d bytes, each line marked >. the first %d and the last %d bytes follow, and %s:\n%s\n...(%d bytes cut)...\n%s",
		spoke, len(prose), len(head), len(tail), held, marked(head), dropped, marked(tail))
	return body.String()
}

func (t *SpawnTool) boardLine(reports string) string {
	w, held := t.work()
	if !held {
		return ""
	}
	line := fmt.Sprintf("board: %d of %d sub-agents running", w.running, w.limit)
	for _, said := range strings.Split(reports, "\n") {
		if !strings.HasPrefix(said, "contract ") {
			continue
		}
		if id := subagent.TicketID(said); w.status[id] != "" {
			line += "; " + id + " is " + string(w.status[id])
		}
	}
	return line + "; free todo: " + listed(w.free)
}

func BoardNudges(store boardy.Store, sizes map[string]int64) string {
	nudges := store.NudgesSince(sizes)
	if len(nudges) == 0 {
		return ""
	}
	return "\n\na board hook nudges you: " + strings.Join(nudges, "; ")
}

func marked(prose string) string {
	lines := strings.Split(strings.NewReplacer("\r\n", "\n", "\r", "\n", "\u2028", "\n", "\u2029", "\n", "\u0085", "\n").Replace(prose), "\n")
	for i, line := range lines {
		if line == "" {
			lines[i] = ">"
		} else {
			lines[i] = "> " + line
		}
	}
	return strings.Join(lines, "\n")
}
