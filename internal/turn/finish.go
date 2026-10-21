package turn

import (
	"context"
	"encoding/json"
	"fmt"
	"path/filepath"
	"slices"
	"strconv"
	"strings"

	"tofu/internal/llm"
	"tofu/internal/session"
	"tofu/internal/subagent"
)

const FinishToolName = "finish"

const youEndedWithoutFinish = "you ended your turn without calling finish, so your report has not reached the lead. " +
	"if the work is done or you cannot go on, call finish now. if it is not done, carry on and call finish when it is."

const itEndedWithoutFinish = "it ended without calling finish after it was sent back once to call it, so this report is its last message and nothing checked it"

type ReportBody struct {
	session.ReportBody
	Result *subagent.Result `json:"result,omitempty"`
}

type finishTool struct{}

func (finishTool) Name() string { return FinishToolName }

func (finishTool) Definition() llm.Tool {
	text := map[string]any{"type": "string"}
	said := func(description string) map[string]any {
		return map[string]any{"type": "string", "description": description}
	}
	enum := func(values []string) map[string]any { return map[string]any{"type": "string", "enum": values} }
	list := func(items map[string]any) map[string]any { return map[string]any{"type": "array", "items": items} }
	object := func(required []string, properties map[string]any) map[string]any {
		return map[string]any{"type": "object", "required": required, "properties": properties, "additionalProperties": false}
	}
	return llm.Tool{
		Name: FinishToolName,
		Description: "ends your turn and hands your report to the lead, which reads it in place of your last message. call it once, as your last call, when the work is done or you cannot go on. " +
			"tofu checks files against the writes and edits it saw you make and each check's exit code against the command it saw run, and flags what does not match. " +
			"when your brief names a ticket with an ## Acceptance section, claims carries one entry per acceptance line, the line copied exactly, with the real command and the output it printed. " +
			"body is optional markdown for what the fields cannot hold.",
		Parameters: object([]string{"verdict", "summary", "files", "checks", "findings", "confidence"}, map[string]any{
			"verdict": enum(subagent.Completion(0).Enum()),
			"summary": said("the outcome in one to three sentences, the answer first"),
			"files":   list(object([]string{"path", "change"}, map[string]any{"path": text, "change": said("what changed in it, in a few words")})),
			"checks": list(object([]string{"command", "exit"}, map[string]any{
				"command": said("the command exactly as you ran it"), "exit": map[string]any{"type": "integer"}, "excerpt": said("the lines of its output that matter")})),
			"claims":        list(object([]string{"line", "command", "output", "met"}, map[string]any{"line": text, "command": text, "output": text, "met": map[string]any{"type": "boolean"}})),
			"findings":      list(object([]string{"bucket", "text"}, map[string]any{"bucket": enum(subagent.Bucket(0).Enum()), "text": text, "cite": said("path:line for code, a url for the web")})),
			"confidence":    object([]string{"level", "reason"}, map[string]any{"level": enum(subagent.Level(0).Enum()), "reason": said("why, and how much you read or ran")}),
			"openQuestions": list(object([]string{"question", "settleBy"}, map[string]any{"question": text, "settleBy": said("the file, command or person that would settle it")})),
			"asks":          list(object([]string{"kind", "ask"}, map[string]any{"kind": enum(subagent.AskKinds()), "ask": text})),
			"consulted":     list(text),
			"body":          text,
		}),
	}
}

func (finishTool) Run(_ context.Context, raw json.RawMessage) (Result, error) {
	if _, err := subagent.ReadResult(raw); err != nil {
		return Result{}, fmt.Errorf("finish was refused and your turn goes on: %w. call finish again with that fixed", err)
	}
	return Result{Content: "your report is recorded for the lead, and your turn ends here"}, nil
}

func finishCalled(call ToolCallRow) bool { return call.Tool == FinishToolName && call.Error == "" }

func finishedWith(row Row) (subagent.Result, bool) {
	for i := len(row.Steps) - 1; i >= 0; i-- {
		calls := row.Steps[i].ToolCalls
		for j := len(calls) - 1; j >= 0; j-- {
			if finishCalled(calls[j]) {
				result, err := subagent.ReadResult(calls[j].Args)
				return result, err == nil
			}
		}
	}
	return subagent.Result{}, false
}

func endedWithoutFinish(row Row) bool {
	_, finished := finishedWith(row)
	return !finished && row.Outcome == OutcomeStopped
}

func projectPath(project, path string) string {
	clean := filepath.Clean(path)
	if rel, err := filepath.Rel(project, clean); project != "" && filepath.IsAbs(clean) && err == nil {
		clean = rel
	}
	return filepath.ToSlash(clean)
}

func (r *SubAgentReport) conclude(claimed subagent.Result, finished bool, project string) {
	result := claimed
	if !finished {
		result = subagent.Result{Summary: firstLine(r.Prose), Files: []subagent.FileChange{}, Checks: []subagent.Check{}, Body: r.Prose,
			Confidence: subagent.Confidence{Level: subagent.Low, Reason: "it did not call finish, so tofu read this from its last message"}}
	}
	var flagged []subagent.Finding
	result.Files, flagged = r.seenFiles(result.Files, finished, project)
	result.Findings = slices.Concat(r.Findings, flagged, r.seenChecks(result.Checks), result.Findings)
	verdict := result.Verdict
	switch {
	case !finished || r.Completion == subagent.Blocked || r.Completion == subagent.NeedsContext:
		verdict = r.Completion
	case verdict == subagent.Done && slices.ContainsFunc(result.Findings, func(finding subagent.Finding) bool { return finding.Bucket.Concerns() }):
		verdict = subagent.DoneWithConcerns
	}
	result.Verdict, r.Completion, r.Findings = verdict, verdict, result.Findings
	result.Measured = subagent.Measured{Finished: finished, Outcome: r.Outcome.String(), Steps: r.Steps, CostUSD: r.CostUSD}
	r.Result = &result
}

func (r SubAgentReport) seenFiles(files []subagent.FileChange, finished bool, project string) ([]subagent.FileChange, []subagent.Finding) {
	var flagged []subagent.Finding
	wrote, named := map[string]bool{}, map[string]bool{}
	for _, path := range r.Wrote {
		wrote[projectPath(project, path)] = true
	}
	for i, file := range files {
		path := projectPath(project, file.Path)
		named[path], files[i].Seen = true, wrote[path]
		if !wrote[path] {
			flagged = append(flagged, subagent.Finding{Bucket: subagent.ActOn, Cite: file.Path, Text: "it says it changed " + file.Path + ", and tofu saw no write or edit of it"})
		}
	}
	for _, path := range r.Wrote {
		if at := projectPath(project, path); !named[at] {
			named[at] = true
			files = append(files, subagent.FileChange{Path: path, Change: "written by it and not named in its report", Seen: true})
			if finished {
				flagged = append(flagged, subagent.Finding{Bucket: subagent.Consider, Cite: path, Text: "tofu saw it write " + path + ", and its report does not name it"})
			}
		}
	}
	return files, flagged
}

func (r SubAgentReport) seenChecks(checks []subagent.Check) []subagent.Finding {
	var flagged []subagent.Finding
	for i, check := range checks {
		said, claimed := strconv.Itoa(*check.Exit), strings.TrimSpace(check.Command)
		ran := -1
		for at, command := range r.Ran {
			if strings.TrimSpace(command.Command) == claimed || strings.TrimSpace(command.Asked) == claimed {
				ran = at
			}
		}
		switch {
		case ran < 0:
			flagged = append(flagged, subagent.Finding{Bucket: subagent.Consider, Cite: check.Command, Text: "it says it ran " + check.Command + ", and tofu saw no such command run"})
			continue
		case r.Ran[ran].ExitCode == nil:
			flagged = append(flagged, subagent.Finding{Bucket: subagent.ActOn, Cite: check.Command, Text: "it says " + check.Command + " exited " + said + ", and tofu saw it fail with no exit code: " + r.Ran[ran].Error})
		case *r.Ran[ran].ExitCode != *check.Exit:
			flagged = append(flagged, subagent.Finding{Bucket: subagent.ActOn, Cite: check.Command, Text: "it says " + check.Command + " exited " + said + ", and tofu saw it exit " + strconv.Itoa(*r.Ran[ran].ExitCode)})
			checks[i].Exit = r.Ran[ran].ExitCode
		}
		checks[i].Seen = true
	}
	return flagged
}

func firstLine(text string) string {
	line, _, _ := strings.Cut(strings.TrimSpace(text), "\n")
	return line
}
