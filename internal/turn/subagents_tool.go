package turn

import (
	"cmp"
	"context"
	"encoding/json"
	"fmt"
	"strconv"
	"strings"
	"time"

	"tofu/internal/konst"
	"tofu/internal/llm"
	"tofu/internal/subagent"
)

type subAgentsTool struct {
	orchestrator *SpawnTool
}

func (subAgentsTool) Name() string { return "subagents" }

const (
	showRead     = "read"
	showDiagnose = "diagnose"
)

func (subAgentsTool) Definition() llm.Tool {
	return llm.Tool{
		Name: "subagents",
		Description: "lists every sub-agent of this session and what it is doing now, and returns at once without waiting for any of them: " +
			"its name, definition, state, effort, how long it has run or ran, its step and tool call counts, the last tool it called, the paths it holds, or that they are free until it resumes once it has ended or was released, and every grant, revoke and replace of them. " +
			"use it rather than guessing whether a sub-agent still runs or who holds a path. " +
			"name with show read gives that sub-agent's report, the files it changed and its conversation, without resuming it. " +
			"name with show diagnose gives what it is doing now: its open calls and how long each has run, its shells and their last lines, its last request, and its failed requests and calls since the last that worked",
		Parameters: map[string]any{"type": "object", "properties": map[string]any{
			"name": map[string]any{"type": "string"},
			"show": map[string]any{"type": "string", "enum": []string{showRead, showDiagnose}},
		}},
	}
}

func (l subAgentsTool) Run(ctx context.Context, raw json.RawMessage) (Result, error) {
	t := l.orchestrator
	var args struct {
		Name string `json:"name"`
		Show string `json:"show"`
	}
	if len(raw) > 0 {
		if err := json.Unmarshal(raw, &args); err != nil {
			return Result{}, fmt.Errorf("subagents: arguments are not the expected shape: %w", err)
		}
	}
	switch {
	case args.Name == "" && args.Show == "":
	case args.Show == showRead:
		return t.read(args.Name)
	case args.Show == showDiagnose:
		return t.diagnose(ctx, args.Name)
	default:
		return Result{}, fmt.Errorf("subagents refused: name goes with show %s or %s, and show %q with name %q is neither", showRead, showDiagnose, args.Show, args.Name)
	}
	effort := map[string]llm.Effort{}
	for _, ran := range t.Spawned() {
		effort[ran.ID] = ran.Effort
	}
	now, listed := t.clock(), "no sub-agent has been spawned in this session"
	var lines []string
	for _, agent := range t.roster.SubAgents() {
		state, ran, until := agent.State.String(), "ran", agent.Active
		switch agent.State {
		case subagent.Working, subagent.InReview, subagent.Reopened:
			state, ran, until = "running", "has run", now
			if agent.State != subagent.Working {
				state += " " + agent.State.String()
			}
		case subagent.WaitingAnswer, subagent.Parked, subagent.Errored, subagent.Finished:
		}
		line := fmt.Sprintf("%s: %s, %s, %s %s, %d steps, %d tool calls", agent.ID, cmp.Or(agent.Agent, "no definition"), state, ran,
			until.Sub(agent.Started).Round(time.Second), agent.Steps, len(agent.Calling)+agent.CallsDropped)
		if level := effort[agent.ID]; level != "" {
			line += ", effort " + string(level)
		}
		if len(agent.Calling) > 0 {
			line += ", last tool " + agent.Calling[len(agent.Calling)-1]
		}
		if agent.Released {
			line += ", released"
		}
		switch {
		case agent.Holds() && len(agent.Owns) > 0:
			line += ", holding " + strings.Join(agent.Owns, " ")
		case len(agent.Owns) > 0:
			line += ", its paths are free, and it holds " + strings.Join(agent.Owns, " ") + " again when it resumes"
		}
		for _, change := range agent.Regranted {
			line += ", " + change.Did + " " + strings.Join(change.Paths, " ") + " at " + change.At.Format(time.TimeOnly)
		}
		lines = append(lines, line+": "+agent.Mission)
	}
	if len(lines) > 0 {
		listed = strings.Join(lines, "\n")
	}
	return Result{Content: listed, Command: "subagents"}, nil
}

func conversationText(history []llm.Message) string {
	var text strings.Builder
	for i, message := range history {
		fmt.Fprintf(&text, "\n[%d] %s", i+1, message.Role)
		if message.ToolCallID != "" {
			text.WriteString(" " + message.ToolCallID)
		}
		text.WriteString(": " + message.Content)
		for _, call := range message.ToolCalls {
			fmt.Fprintf(&text, "\n    calls %s %s %s", call.ID, call.Name, call.Arguments)
		}
	}
	return text.String()
}

func (t *SpawnTool) read(name string) (Result, error) {
	agent, found := t.roster.SubAgent(name)
	if !found {
		return Result{}, t.unknown(name)
	}
	said := name + " is " + agent.State.String()
	if agent.Released {
		said += " and released"
	}
	said += ". its report:\n" + cmp.Or(agent.Report, "none yet")
	held, running := t.Inbox.find(name)
	if held == nil {
		return Result{Content: said + "\n\nits conversation is kept by the sub-agent that spawned it, not by you.", Command: "read " + name, SubAgent: name}, nil
	}
	history, err := t.conversationSoFar(held, running)
	if err != nil {
		return Result{}, fmt.Errorf("subagents: %s's conversation did not read back: %w", name, err)
	}
	said += "\n\nfiles it changed: " + cmp.Or(strings.Join(filesWritten(history), ", "), "none")
	said += fmt.Sprintf("\n\nits conversation, %d messages", len(history))
	if running {
		said += ", so far, since it is running"
	}
	return Result{Content: said + ":" + conversationText(history), Command: "read " + name, SubAgent: name}, nil
}

func (t *SpawnTool) failedCalls(held *heldSubAgent) []string {
	t.Inbox.mu.Lock()
	check := held.check
	t.Inbox.mu.Unlock()
	if check == nil {
		return nil
	}
	check.mu.Lock()
	defer check.mu.Unlock()
	var failed []string
	for _, step := range check.steps {
		for _, call := range step.ToolCalls {
			switch call.Outcome() {
			case llm.ToolOutcomeRan:
				failed = nil
			case llm.ToolOutcomeFailed:
				why := call.Error
				if why == "" && call.ExitCode != nil {
					why = "exit code " + strconv.Itoa(*call.ExitCode)
				}
				failed = append(failed, call.Tool+" "+call.Command+": "+why)
			case llm.ToolOutcomeUnset, llm.ToolOutcomeAborted:
			}
		}
	}
	return failed
}

func (t *SpawnTool) requestsSeen(held *heldSubAgent) []string {
	t.Inbox.mu.Lock()
	log := held.log
	t.Inbox.mu.Unlock()
	if log == nil || t.base.Sessions == nil {
		return []string{"no request of its is recorded in this session yet"}
	}
	exchanges, err := t.base.Sessions.Exchanges(log.ID())
	if err != nil {
		return []string{"its requests did not read back: " + err.Error()}
	}
	var last, failed []string
	for _, exchange := range exchanges {
		if exchange.Agent != held.agent.ID {
			continue
		}
		line := fmt.Sprintf("request %s, %s, at %s, took %s", exchange.Request, exchange.Why, exchange.At.Format(time.TimeOnly), (time.Duration(exchange.DurationMS) * time.Millisecond).Round(time.Millisecond))
		last = []string{"last " + line}
		if exchange.Error == "" {
			failed = nil
			continue
		}
		failed = append(failed, "failed "+line+": "+exchange.Error)
	}
	return append(failed, last...)
}

func (t *SpawnTool) diagnose(ctx context.Context, name string) (Result, error) {
	agent, found := t.roster.SubAgent(name)
	if !found {
		return Result{}, t.unknown(name)
	}
	held, running := t.Inbox.find(name)
	runs := "not running"
	if running {
		runs = "running"
	}
	lines := []string{fmt.Sprintf("%s is %s and %s, %d steps, the last %s ago", name, agent.State, runs, agent.Steps, t.clock().Sub(agent.Active).Round(time.Second))}
	if len(agent.Calling) > 0 {
		lines = append(lines, "its last tools: "+strings.Join(agent.Calling, ", "))
	}
	if held != nil {
		open := held.openCalls()
		if running && len(open) == 0 {
			lines = append(lines, "no tool call is open, so it waits on its model or is between steps")
		}
		for _, call := range open {
			lines = append(lines, fmt.Sprintf("open call: %s %s, for %s", call.tool, call.target, time.Since(call.since).Round(time.Second)))
		}
		for _, failure := range t.failedCalls(held) {
			lines = append(lines, "failed call since the last that worked: "+failure)
		}
		lines = append(lines, t.requestsSeen(held)...)
	}
	if registry := ShellRegistryFrom(ctx); registry != nil {
		for _, running := range registry.Own() {
			if !ownedBy(running.Owner, []string{name}) {
				continue
			}
			tail, err := registry.Tail(running.Name, konst.SubAgentDiagnoseTailLines)
			if err != nil {
				tail = "its output did not read back: " + err.Error()
			}
			lines = append(lines, fmt.Sprintf("shell %s runs %s as pid %d since %s, and its last lines are:\n%s", running.Name, running.Command, running.PID, running.Started.Format(time.TimeOnly), tail))
		}
	}
	return Result{Content: strings.Join(lines, "\n"), Command: "diagnose " + name, SubAgent: name}, nil
}
