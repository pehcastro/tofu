package done

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"slices"
	"strconv"
	"strings"

	"tofu/internal/judge/ledger"
	question "tofu/internal/judge/questions"
	"tofu/internal/konst"
	jev "tofu/internal/models/classifiers/core"
	"tofu/internal/models/llm/core"
	"tofu/internal/store/session"
	"tofu/internal/tools/code/test"
	"tofu/internal/tools/contract"
	"tofu/internal/tools/human/plan"
	"tofu/library/questions"
)

const (
	Name         = "done"
	questionsRef = "done_line@1"
	stateBuilder = "done_line_state@1"
)

type Met string

const (
	Yes        Met = "yes"
	No         Met = "no"
	NotChecked Met = "not_checked"
)

type Line struct {
	Text string `json:"text"`
	Call string `json:"call"`
}

type Read struct {
	Line
	Met      Met
	Evidence string
	cited    cited
}

type cited struct {
	Tool     string `json:"tool"`
	Args     string `json:"args"`
	ExitCode *int   `json:"exit_code"`
	Output   string `json:"output"`
}

type shownLine struct {
	Text string `json:"text"`
	Call cited  `json:"call"`
}

type Done struct {
	Dir    string
	Ledger *ledger.Writer
	Open   func() (*jev.Client, error)
}

type asked struct {
	Lines []Line `json:"lines"`
}

func (Done) Name() string { return Name }

func (Done) Definition() core.Tool {
	return core.Tool{
		Name: Name,
		Description: "answers whether you can stop. lines are the task's acceptance lines, each with call, the tool call whose command checked it: the [tool#abc12] reference at the top of its result, or its call id. " +
			"each reads yes when that command exited 0 or that test call ran its tests and they passed and Jev reads that call as checking that line, no when it failed, " +
			"and not_checked when no command checked it: no call, a reference no earlier call or more than one has, a call that ran no command, or a passing call that does not check what the line says, such as a test run cited for a line about which files changed. " +
			"every plan item that carries a check is read too, against the call its done named. " +
			"once you call done, the turn does not end while a line reads no: you get one more turn to fix it, run its check again and call done with the new call id.",
		Parameters: map[string]any{
			"type": "object",
			"properties": map[string]any{
				"lines": map[string]any{
					"type": "array",
					"items": map[string]any{
						"type":       "object",
						"properties": map[string]any{"text": map[string]any{"type": "string"}, "call": map[string]any{"type": "string"}},
						"required":   []string{"text"},
					},
				},
			},
		},
	}
}

func (d Done) Run(ctx context.Context, raw json.RawMessage) (contract.Result, error) {
	var args asked
	if err := json.Unmarshal(raw, &args); err != nil {
		return contract.Result{}, fmt.Errorf("done: arguments are not the expected shape: %w", err)
	}
	reads := readAll(ctx, args.Lines, plan.EarlierCalls(ctx))
	if len(reads) == 0 {
		return contract.Result{Content: "done: 0 lines, since none was given and no plan item carries a check\n", Command: Name}, nil
	}
	if err := d.confirm(ctx, reads); err != nil {
		for i := range reads {
			if reads[i].Met == Yes {
				reads[i].Met, reads[i].Evidence = NotChecked, reads[i].Evidence+", not read as checking this line: "+err.Error()
			}
		}
	}
	count := map[Met]int{}
	var said strings.Builder
	for _, read := range reads {
		count[read.Met]++
		fmt.Fprintf(&said, "  %-11s %s: %s\n", read.Met, read.Evidence, read.Text)
	}
	head := fmt.Sprintf("done: %d lines, %d yes, %d no, %d not_checked\n", len(reads), count[Yes], count[No], count[NotChecked])
	if count[No] > 0 {
		said.WriteString("the turn does not end while a line reads no: fix it, run its check again, and call done with the new call id\n")
	}
	return contract.Result{Content: head + said.String(), Command: Name}, nil
}

func Unmet(ctx context.Context, messages []core.Message) []string {
	var last json.RawMessage
	for _, message := range messages {
		for _, call := range message.ToolCalls {
			if call.Name == Name && resultOf(messages, call.ID) != nil {
				last = call.Arguments
			}
		}
	}
	var args asked
	if last == nil || json.Unmarshal(last, &args) != nil {
		return nil
	}
	var unmet []string
	for _, read := range readAll(ctx, args.Lines, messages) {
		if read.Met == No {
			unmet = append(unmet, read.Text+" ("+read.Evidence+")")
		}
	}
	return unmet
}

func readAll(ctx context.Context, lines []Line, earlier []core.Message) []Read {
	reads := make([]Read, 0, len(lines))
	for _, line := range append(slices.Clone(lines), planLines(ctx, earlier)...) {
		reads = append(reads, readLine(line, earlier))
	}
	return reads
}

func readLine(line Line, earlier []core.Message) Read {
	call := strings.TrimSpace(line.Call)
	if call == "" {
		return Read{Line: line, Met: NotChecked, Evidence: "no call"}
	}
	results := calledAs(earlier, call)
	if len(results) != 1 {
		return Read{Line: line, Met: NotChecked, Evidence: fmt.Sprintf("%d earlier calls have the id or reference %q", len(results), call)}
	}
	result := results[0]
	var asked core.ToolCall
	for _, message := range earlier {
		if at := slices.IndexFunc(message.ToolCalls, func(c core.ToolCall) bool { return c.ID == result.ToolCallID }); at >= 0 {
			asked = message.ToolCalls[at]
		}
	}
	shown := cited{Tool: asked.Name, Args: tail(string(asked.Arguments), konst.ClaimsArgsBytes), ExitCode: result.ToolExitCode, Output: tail(result.Content, konst.ClaimsOutputBytes)}
	switch {
	case result.ToolExitCode == nil && asked.Name != test.Test{}.Name():
		return Read{Line: line, Met: NotChecked, Evidence: call + " ran no command"}
	case result.ToolExitCode == nil && result.ToolOutcome != core.ToolOutcomeRan:
		return Read{Line: line, Met: No, Evidence: call + " test did not pass"}
	case result.ToolExitCode == nil:
		return Read{Line: line, Met: Yes, Evidence: call + " test passed", cited: shown}
	case *result.ToolExitCode != 0 || result.ToolOutcome != core.ToolOutcomeRan:
		return Read{Line: line, Met: No, Evidence: call + " exit " + strconv.Itoa(*result.ToolExitCode)}
	}
	return Read{Line: line, Met: Yes, Evidence: call + " exit 0", cited: shown}
}

func (d Done) confirm(ctx context.Context, reads []Read) error {
	var at []int
	var lines []shownLine
	for i, read := range reads {
		if read.Met == Yes {
			at, lines = append(at, i), append(lines, shownLine{Text: read.Text, Call: read.cited})
		}
	}
	if len(lines) == 0 {
		return nil
	}
	if d.Open == nil {
		return errors.New("no classifier is bound")
	}
	layers, err := question.Layers(questions.Files(), d.Dir)
	if err != nil {
		return err
	}
	set, fields, err := question.Resolve(questionsRef, layers)
	if err != nil {
		return err
	}
	field := slices.IndexFunc(fields, func(f question.Field) bool { return f.Path == "thresholds.checks_at" })
	var threshold float64
	if field >= 0 {
		threshold, err = strconv.ParseFloat(fields[field].Value, 64)
	}
	if field < 0 || err != nil || threshold <= 0 || threshold > 1 {
		return fmt.Errorf("%s declares no thresholds.checks_at above 0 and at or below 1", set.File)
	}
	checks, _ := set.Question("checks")
	var asking []jev.Question
	for i := range lines {
		each := checks.ToJev()
		each.ID, each.Instructions = "checks_"+strconv.Itoa(i+1), each.Instructions+" The line is lines["+strconv.Itoa(i)+"]."
		asking = append(asking, each)
	}
	client, err := d.Open()
	if err != nil {
		return fmt.Errorf("jev cannot be reached: %w", err)
	}
	body, err := json.Marshal(struct {
		Lines []shownLine `json:"lines"`
	}{lines})
	if err != nil {
		return err
	}
	row := ledger.Row{Point: Name, Questions: set.Name, Version: set.QuestionsVersion, StateHash: ledger.HashOf(body), StateBuilder: stateBuilder, State: body,
		Verdict: ledger.VerdictAsk, Reason: &ledger.Reason{Question: "checks", Comparison: "checks_at", Threshold: threshold, Value: 1, Mode: ledger.ModeEnforced}}
	decision, err := client.Ask(ctx, jev.Request{State: json.RawMessage(body), Questions: asking})
	if err != nil {
		cause := "jev could not answer: " + err.Error()
		row.Reason.ModeReason = &cause
		_, unwritten := d.Ledger.Append(row)
		return errors.Join(errors.New(cause), unwritten)
	}
	decision.Record(&row, asking, set.QuestionsVersion)
	row.Verdict = ledger.VerdictAllow
	for k, i := range at {
		noul := decision.Answers[asking[k].ID].Noul
		row.Reason.Value = min(row.Reason.Value, noul)
		reads[i].Evidence += fmt.Sprintf(", jev reads %.2f that it checks this line, against %.2f", noul, threshold)
		if noul < threshold {
			row.Verdict, reads[i].Met = ledger.VerdictAsk, NotChecked
		}
	}
	if _, err := d.Ledger.Append(row); err != nil {
		return fmt.Errorf("the decision did not reach the ledger: %w", err)
	}
	return nil
}

func tail(text string, bytes int) string {
	return strings.ToValidUTF8(text[max(0, len(text)-bytes):], "")
}

func calledAs(messages []core.Message, call string) []core.Message {
	_, hash, referred := strings.Cut(strings.TrimSuffix(call, "]"), "#")
	var found []core.Message
	for _, message := range messages {
		shown, _, _ := strings.Cut(message.Content, "\n")
		ref, shownRef := strings.CutPrefix(strings.TrimSuffix(shown, "]"), "[tool#")
		if message.Role == core.RoleTool && (message.ToolCallID == call || referred && shownRef && session.DrawnAs(ref, hash)) {
			found = append(found, message)
		}
	}
	return found
}

func resultOf(messages []core.Message, call string) *core.Message {
	at := slices.IndexFunc(messages, func(message core.Message) bool { return message.Role == core.RoleTool && message.ToolCallID == call })
	if call == "" || at < 0 {
		return nil
	}
	return &messages[at]
}

func planLines(ctx context.Context, messages []core.Message) []Line {
	replayed := plan.NewPlan()
	for at, message := range messages {
		for _, call := range message.ToolCalls {
			if result := resultOf(messages, call.ID); call.Name == plan.PlanToolName && result != nil && !result.ToolOutcome.Failed() {
				_, _ = replayed.Run(plan.WithEarlierCalls(ctx, messages[:at]), call.Arguments)
			}
		}
	}
	var lines []Line
	for _, item := range replayed.Items() {
		if item.Check != "" && item.State != plan.PlanDropped {
			lines = append(lines, Line{Text: item.Text + ", check " + item.Check, Call: item.Call})
		}
	}
	return lines
}
