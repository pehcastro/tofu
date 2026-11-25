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
	"tofu/internal/store/session"
	"tofu/internal/tools/contract"
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
	Outcome    session.Outcome      `json:"outcome"`
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
	Board      string               `json:"-"`
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

type reportPart struct {
	label, sep, what string
	whole, shown     []string
	each, most       int
}

const (
	cutMark  = "...(%d bytes cut)..."
	moreMark = "+%d more %s in the full report"
)

func (r SubAgentReport) parts() (string, []reportPart) {
	forks := ""
	if r.Forks > 0 {
		forks = fmt.Sprintf(" across %d forks", r.Forks)
	}
	var attempts, impact, checks, failing, findings, shown, asked []string
	if len(r.Attempts) > 1 {
		for i, attempt := range r.Attempts {
			attempts = append(attempts, fmt.Sprintf("  attempt %d, %s, tried %s, ended %s", i+1, attempt.ID, attempt.Tried, attempt.Outcome))
		}
	}
	for _, item := range r.Impact.Items {
		impact = append(impact, fmt.Sprintf("  %s: %s", item.Item, cmp.Or(strings.Join(item.Callers, ", "), "no call site found")))
	}
	for _, unread := range r.Impact.Unread {
		impact = append(impact, "  unread "+unread)
	}
	if r.Impact.Stopped != "" {
		impact = append(impact, "  "+r.Impact.Stopped)
	}
	failed, skipped := 0, 0
	for _, ran := range r.Ran {
		line, check := ran.checked()
		if !check {
			continue
		}
		checks = append(checks, "  "+line)
		switch {
		case ran.Skipped != "":
			failing, skipped = append(failing, "  "+line), skipped+1
		case ran.Failed:
			failing, failed = append(failing, "  "+line), failed+1
		}
	}
	bucketed := map[subagents.Bucket]int{}
	for _, finding := range r.Findings {
		findings, bucketed[finding.Bucket] = append(findings, fmt.Sprintf("%s: %s", finding.Bucket, finding.Text)), bucketed[finding.Bucket]+1
	}
	for _, bucket := range []subagents.Bucket{subagents.ActOn, subagents.Consider, subagents.Noted} {
		for i, finding := range r.Findings {
			if finding.Bucket == bucket {
				shown = append(shown, findings[i])
			}
		}
	}
	for _, question := range r.Asked {
		asked = append(asked, fmt.Sprintf("asks a %s: %s", question.Kind, question.Ask))
	}
	var counted []string
	for _, count := range []struct {
		n    int
		what string
	}{
		{bucketed[subagents.ActOn], "findings to act on"}, {bucketed[subagents.Consider], "findings to consider"}, {bucketed[subagents.Noted], "findings noted"},
		{bucketed[subagents.Dismissed], "findings dismissed"}, {failed, "checks failed"}, {skipped, "checks skipped"}, {len(checks) - failed - skipped, "checks passed"},
		{len(r.Wrote), "files written"},
	} {
		if count.n > 0 {
			counted = append(counted, fmt.Sprintf("%d %s", count.n, count.what))
		}
	}
	head := fmt.Sprintf("sub-agent %s is %s, %s, %s after %d steps and %d tool calls%s, costing $%.4f\n",
		r.ID, r.State, r.Completion, r.Outcome, r.Steps, len(r.Ran), forks, r.CostUSD)
	if len(counted) > 0 {
		head += "counts: " + strings.Join(counted, ", ") + "\n"
	}
	return head, []reportPart{
		{label: fmt.Sprintf("escalating after %d attempts:\n", len(r.Attempts)), sep: "\n", what: "attempts", whole: attempts, shown: attempts,
			each: konst.SubAgentAttemptBytes, most: konst.SubAgentAttemptsBytes},
		{label: "wrote ", sep: ", ", what: "paths", whole: r.Wrote, shown: r.Wrote, each: konst.SubAgentWroteBytes, most: konst.SubAgentWroteBytes},
		{label: "impact, each exported item it changed and the call sites outside its file that the symbols tool matched by name:\n", sep: "\n",
			what: "impact lines", whole: impact, shown: impact, each: konst.SubAgentImpactBytes, most: konst.SubAgentImpactBytes},
		{label: "checks it ran that did not pass, each with its exit code and first error line:\n", sep: "\n",
			what: "checks that did not pass", whole: checks, shown: failing, each: konst.SubAgentCheckBytes, most: konst.SubAgentChecksBytes},
		{sep: "\n", what: "findings", whole: findings, shown: shown, each: konst.SubAgentFindingBytes, most: konst.SubAgentFindingsBytes},
		{label: "learned: ", sep: "; ", what: "warnings", whole: r.Learned, shown: r.Learned, each: konst.SubAgentLearnedItemBytes, most: konst.SubAgentLearnedBytes},
		{sep: "\n", what: "asks", whole: asked, shown: asked, each: konst.SubAgentAskedBytes, most: konst.SubAgentAskedBytes},
	}
}

func (r SubAgentReport) Text(store *recall.Store) string {
	head, parts := r.parts()
	whole, rest, card := &strings.Builder{}, &strings.Builder{}, &reportCard{}
	whole.WriteString(head)
	head = card.fit(head, konst.SubAgentReportHeadBytes)
	for _, part := range parts {
		if len(part.whole) > 0 {
			fmt.Fprintf(whole, "%s%s\n", part.label, strings.Join(part.whole, part.sep))
		}
		if len(part.shown) > 0 {
			fmt.Fprintf(rest, "%s%s\n", part.label, card.list(part.shown, part.each, part.most-len(part.label)-1, part.sep, part.what))
		}
		card.cut = card.cut || len(part.shown) != len(part.whole)
	}
	if r.Prose != "" {
		label := "its last message, each line marked >:\n"
		if r.ProseStep > 0 && r.ProseStep < r.Steps {
			label = fmt.Sprintf("it last spoke at step %d of %d, each line marked >:\n", r.ProseStep, r.Steps)
		}
		shown := marked(r.Prose)
		whole.WriteString(label + shown)
		room := min(konst.SubAgentProseBytes+len(shown)-len(r.Prose), konst.SubAgentReportBytes-konst.SubAgentReportFullBytes-len(head)-rest.Len()-len(label))
		rest.WriteString(label + card.marked(r.Prose, room))
	}
	whole.WriteString(r.Board)
	rest.WriteString(card.fit(r.Board, konst.SubAgentBoardBytes))
	full := "full report: nothing was cut, this is all of it"
	if card.cut {
		full = "full report: not kept, because this turn keeps no artifacts, so what was cut is lost"
		if store != nil {
			if elided, err := recall.Elide(store, recall.Config{}, []byte(whole.String()), true); err != nil {
				full = "full report: not kept, writing it failed: " + err.Error() + ". what was cut is lost"
			} else {
				full = fmt.Sprintf("full report: artifact %s holds all %d bytes, every check, finding and line whole: read any range with artifact_fetch",
					elided.Reference.ID, elided.Reference.Bytes)
			}
		}
	}
	return head + card.fit(full, konst.SubAgentReportFullBytes-1) + "\n" + rest.String()
}

type reportCard struct {
	cut bool
}

func (c *reportCard) fit(text string, most int) string {
	shown, cut := fitted(text, most)
	c.cut = c.cut || cut
	return shown
}

func (c *reportCard) list(items []string, each, most int, sep, what string) string {
	reserve := len(fmt.Sprintf(moreMark, len(items), what)) + len(sep)
	var kept []string
	used := 0
	for i, item := range items {
		shown := c.fit(item, each)
		if used+len(shown)+len(sep) > most-reserve {
			c.cut = true
			return strings.Join(append(kept, fmt.Sprintf(moreMark, len(items)-i, what)), sep)
		}
		kept, used = append(kept, shown), used+len(shown)+len(sep)
	}
	return strings.Join(kept, sep)
}

func (c *reportCard) marked(prose string, room int) string {
	if shown := marked(prose); len(shown) <= room {
		return shown
	}
	c.cut = true
	for most := room; most > 0; {
		head, tail := cutAt(prose, most)
		shown := fmt.Sprintf("%s\n"+cutMark+"\n%s", marked(strings.TrimSuffix(head, "\n")), len(prose)-len(head)-len(tail), marked(strings.TrimPrefix(tail, "\n")))
		if len(shown) <= room {
			return shown
		}
		most -= len(shown) - room
	}
	return ""
}

func cutAt(text string, most int) (string, string) {
	head, tail := runeSafeHead(text, most*2/3), runeSafeTail(text, most/3)
	if nl := strings.LastIndexByte(head, '\n'); nl >= len(head)/2 {
		head = head[:nl+1]
	}
	return head, tail
}

func fitted(text string, most int) (string, bool) {
	if len(text) <= most {
		return text, false
	}
	head, tail := cutAt(text, max(most-len(fmt.Sprintf(cutMark, len(text))), 0))
	return head + fmt.Sprintf(cutMark, len(text)-len(head)-len(tail)) + tail, true
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
	return line + "; free todo: " + contract.Listed(w.free)
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
