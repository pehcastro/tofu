package done

import (
	"context"
	"encoding/json"
	"fmt"
	"slices"
	"strconv"
	"strings"

	"tofu/internal/models/llm/core"
	"tofu/internal/store/session"
	"tofu/internal/tools/code/test"
	"tofu/internal/tools/contract"
	"tofu/internal/tools/human/plan"
)

const Name = "done"

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
}

type Done struct{}

type asked struct {
	Lines []Line `json:"lines"`
}

func (Done) Name() string { return Name }

func (Done) Definition() core.Tool {
	return core.Tool{
		Name: Name,
		Description: "answers whether you can stop. lines are the task's acceptance lines, each with call, the tool call whose command checked it: the [tool#abc12] reference at the top of its result, or its call id. " +
			"each reads yes when that command exited 0 or that test call ran its tests and they passed, no when it failed, and not_checked when no command checked it: no call, a reference no earlier call or more than one has, or a call that ran no command. " +
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

func (Done) Run(ctx context.Context, raw json.RawMessage) (contract.Result, error) {
	var args asked
	if err := json.Unmarshal(raw, &args); err != nil {
		return contract.Result{}, fmt.Errorf("done: arguments are not the expected shape: %w", err)
	}
	reads := readAll(ctx, args.Lines, plan.EarlierCalls(ctx))
	if len(reads) == 0 {
		return contract.Result{Content: "done: 0 lines, since none was given and no plan item carries a check\n", Command: Name}, nil
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
	tested := slices.ContainsFunc(earlier, func(message core.Message) bool {
		return slices.ContainsFunc(message.ToolCalls, func(asked core.ToolCall) bool {
			return asked.ID == result.ToolCallID && asked.Name == test.Test{}.Name()
		})
	})
	switch {
	case result.ToolExitCode == nil && !tested:
		return Read{Line: line, Met: NotChecked, Evidence: call + " ran no command"}
	case result.ToolExitCode == nil && result.ToolOutcome != core.ToolOutcomeRan:
		return Read{Line: line, Met: No, Evidence: call + " test did not pass"}
	case result.ToolExitCode == nil:
		return Read{Line: line, Met: Yes, Evidence: call + " test passed"}
	case *result.ToolExitCode != 0 || result.ToolOutcome != core.ToolOutcomeRan:
		return Read{Line: line, Met: No, Evidence: call + " exit " + strconv.Itoa(*result.ToolExitCode)}
	}
	return Read{Line: line, Met: Yes, Evidence: call + " exit 0"}
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
