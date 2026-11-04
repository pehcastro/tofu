package loop

import (
	"cmp"
	"encoding/json"
	"fmt"
	"slices"
	"strconv"
	"strings"

	"tofu/internal/agent/context/recall"
	"tofu/internal/agent/subagents"
	"tofu/internal/konst"
	"tofu/internal/models/llm/core"
	"tofu/internal/project/boardy"
)

type SubAgentCommand struct {
	Tool       string `json:"tool"`
	Command    string `json:"command,omitempty"`
	Asked      string `json:"asked,omitempty"`
	ExitCode   *int   `json:"exit_code,omitempty"`
	Error      string `json:"error,omitempty"`
	Failed     bool   `json:"failed,omitempty"`
	FirstError string `json:"first_error,omitempty"`
	Skipped    string `json:"skipped,omitempty"`
}

func commandOf(call ToolCallRow, printed string) SubAgentCommand {
	ran := SubAgentCommand{Tool: call.Tool, Command: call.Command, ExitCode: call.ExitCode, Error: call.Error}
	if call.Proxy != nil {
		ran.Asked = call.Proxy.Asked
	}
	line := errorLine(printed)
	reason, skipped := skipReason(call.Error)
	switch {
	case skipped:
		ran.Skipped = reason
	case call.Error != "":
		ran.Failed, line = true, cmp.Or(line, call.Error)
	case call.ExitCode != nil && *call.ExitCode != 0:
		ran.Failed = true
	default:
		ran.Failed = call.ExitCode != nil && slices.ContainsFunc([]string{"go test", "go vet", "cargo test"}, func(check string) bool {
			return commandRuns(call.Command, check) && checkPrinted(check, printed, call.ExitCode) == printedFailed
		})
	}
	if ran.Failed {
		ran.FirstError = line
	}
	return ran
}

func (c SubAgentCommand) checked() (string, bool) {
	name := strings.Join(strings.Fields(c.Command), " ")
	if c.Tool != bashToolName {
		name = strings.TrimSpace(c.Tool + " " + name)
	}
	switch {
	case c.ExitCode == nil && !slices.Contains([]string{bashToolName, "test", "typecheck"}, c.Tool):
		return "", false
	case c.Skipped != "":
		return name + " skipped: " + c.Skipped, true
	case c.ExitCode == nil && c.Failed:
		name += " failed"
	case c.ExitCode == nil:
		name += " passed"
	case c.Failed && *c.ExitCode == 0:
		name += " exited 0 at the last stage of its pipe, and printed a failure"
	default:
		name += " exited " + strconv.Itoa(*c.ExitCode)
	}
	switch {
	case c.FirstError != "":
		return name + ": " + c.FirstError, true
	case c.Failed:
		return name + ", and printed no error line", true
	}
	return name, true
}

const TicketNotPossibleToolName = "ticket_not_possible"

type SubAgentReport struct {
	ID         string               `json:"id"`
	Mission    string               `json:"mission"`
	Owns       []string             `json:"owns"`
	State      string               `json:"state"`
	Completion subagents.Completion `json:"completion"`
	Outcome    Outcome              `json:"outcome"`
	Steps      int                  `json:"steps"`
	Forks      int                  `json:"forks,omitempty"`
	Attempts   []subagents.Attempt  `json:"attempts"`
	Findings   []subagents.Finding  `json:"findings"`
	Learned    []string             `json:"learned"`
	Wrote      []string             `json:"wrote,omitempty"`
	Impact     boardy.Impact        `json:"impact,omitzero"`
	Ran        []SubAgentCommand    `json:"ran,omitempty"`
	CostUSD    float64              `json:"cost_usd"`
	Asked      []subagents.Question `json:"asked,omitempty"`
	Prose      string               `json:"prose,omitempty"`
	ProseStep  int                  `json:"prose_step,omitempty"`
}

func reportOf(agent subagents.SubAgent, forked, attempts []Row, state subagents.State) SubAgentReport {
	row := attempts[len(attempts)-1]
	ended := row.Outcome
	if state == subagents.Parked {
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
				report.Ran = append(report.Ran, commandOf(call, printedBy([]Row{segment}, call.Call)))
				if path := writtenPath(call); path != "" && !slices.Contains(report.Wrote, path) {
					report.Wrote = append(report.Wrote, path)
				}
			}
		}
	}
	report.Completion = completionOf(state, report.Findings)
	if ruledOut(append(slices.Clone(forked), row)) {
		report.Completion = subagents.NotPossible
	}
	for i := len(row.Steps) - 1; i >= 0; i-- {
		if spoken := strings.TrimSpace(row.Steps[i].AssistantText); spoken != "" {
			report.Prose, report.ProseStep = spoken, i+1
			break
		}
	}
	return report
}

func ruledOut(rows []Row) bool {
	for _, row := range rows {
		for _, step := range row.Steps {
			if slices.ContainsFunc(step.ToolCalls, func(call ToolCallRow) bool { return call.Tool == TicketNotPossibleToolName && call.Error == "" }) {
				return true
			}
		}
	}
	return false
}

func attemptOf(row Row, ended Outcome) subagents.Attempt {
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
	return subagents.Attempt{ID: row.ID, Tried: tried, Outcome: ended.String()}
}

func completionOf(state subagents.State, found []subagents.Finding) subagents.Completion {
	switch state {
	case subagents.Errored, subagents.Parked:
		return subagents.Blocked
	case subagents.WaitingAnswer:
		return subagents.NeedsContext
	case subagents.Working, subagents.InReview, subagents.Finished:
		for _, finding := range found {
			if finding.Bucket.Concerns() {
				return subagents.DoneWithConcerns
			}
		}
		return subagents.Done
	}
	panic("turn: unknown sub-agent state " + strconv.Itoa(int(state)))
}

func findings(row Row, state subagents.State) []subagents.Finding {
	found := []subagents.Finding{}
	if outcome, carries := outcomeFinding(row, state); carries {
		found = append(found, outcome)
	}
	var calls []ToolCallRow
	for _, step := range row.Steps {
		calls = append(calls, step.ToolCalls...)
	}
	for i, call := range calls {
		if !call.Outcome().Failed() || slices.ContainsFunc(calls[i+1:], func(later ToolCallRow) bool {
			return later.Tool == call.Tool && later.Outcome() == core.ToolOutcomeRan
		}) {
			continue
		}
		if call.Outcome() == core.ToolOutcomeSkipped {
			found = append(found, subagents.Finding{Bucket: subagents.Consider, Text: call.Error + ", and nothing after it ran " + call.Tool + ", so what it checks is unverified"})
			continue
		}
		failure := call.Error
		if failure == "" {
			failure = "exit code " + strconv.Itoa(*call.ExitCode)
		}
		found = append(found, subagents.Finding{Bucket: subagents.ActOn, Text: call.Tool + " failed and nothing after it made " + call.Tool + " work: " + failure})
	}
	return found
}

func outcomeFinding(row Row, state subagents.State) (subagents.Finding, bool) {
	if state == subagents.Parked {
		return subagents.Finding{Bucket: subagents.ActOn,
			Text: "the sub-agent was stopped from outside partway through, so its work is unfinished and what it did do stands"}, true
	}
	outcome := row.Outcome
	switch outcome {
	case OutcomeUnset, OutcomeStopped:
		return subagents.Finding{}, false
	case OutcomeStepCap, OutcomeRetiredCostCap, OutcomeRetiredWallClockCap, OutcomeDecisionCap:
		return subagents.Finding{Bucket: subagents.ActOn,
			Text: "the sub-agent was stopped by the " + outcome.String() + " and its work is unfinished"}, true
	case OutcomeError:
		return subagents.Finding{Bucket: subagents.ActOn, Text: "the sub-agent ended on an error and its work is unfinished"}, true
	case OutcomeCancelled:
		return subagents.Finding{Bucket: subagents.ActOn, Text: "the sub-agent was cancelled partway through, so its work is unfinished and what it did do stands"}, true
	case OutcomeLoopGuard:
		return subagents.Finding{Bucket: subagents.ActOn, Text: "the sub-agent repeated one call until the loop guard stopped it"}, true
	case OutcomeForkStop:
		return subagents.Finding{Bucket: subagents.ActOn, Text: "the sub-agent was stopped by its forks and its work is unfinished: " + row.ForkStop}, true
	case OutcomeTruncated:
		return subagents.Finding{Bucket: subagents.Consider,
			Text: "output was truncated, so what the sub-agent read may be short of what it asked for"}, true
	case OutcomeForked:
		return subagents.Finding{Bucket: subagents.Noted, Text: "the sub-agent forked its conversation and this report is the fork"}, true
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
	if len(r.Wrote) > 0 {
		fmt.Fprintf(body, "wrote %s\n", strings.Join(r.Wrote, ", "))
	}
	if len(r.Impact.Items) > 0 || len(r.Impact.Unread) > 0 {
		body.WriteString("impact, each exported item it changed and the call sites outside its file that the symbols tool matched by name:\n")
		for _, item := range r.Impact.Items {
			fmt.Fprintf(body, "  %s: %s\n", item.Item, cmp.Or(strings.Join(item.Callers, ", "), "no call site found"))
		}
		for _, unread := range r.Impact.Unread {
			fmt.Fprintf(body, "  unread %s\n", unread)
		}
		if r.Impact.Stopped != "" {
			fmt.Fprintf(body, "  %s\n", r.Impact.Stopped)
		}
	}
	var checks []string
	notPassed := false
	for _, ran := range r.Ran {
		if line, check := ran.checked(); check {
			checks, notPassed = append(checks, "  "+line), notPassed || ran.Failed || ran.Skipped != ""
		}
	}
	if notPassed {
		fmt.Fprintf(body, "checks it ran, each with its exit code and first error line:\n%s\n", strings.Join(checks, "\n"))
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
	if r.ProseStep > 0 && r.ProseStep < r.Steps {
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
		if !strings.HasPrefix(said, acceptancePrefix) {
			continue
		}
		if id := subagents.TicketID(said); w.status[id] != "" {
			line += "; " + id + " is " + string(w.status[id])
		}
	}
	return line + "; free todo: " + listed(w.free)
}

func BoardNudges(store boardy.Store, sizes map[string]int64) string {
	return NudgesText(store.NudgesSince(sizes))
}

func NudgesText(nudges []string) string {
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
