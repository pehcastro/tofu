package turn

import (
	"cmp"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"slices"
	"strconv"
	"strings"
	"time"

	"tofu/internal/boardy"
	"tofu/internal/konst"
	"tofu/internal/llm"
	"tofu/internal/rule"
	"tofu/internal/session"
	"tofu/internal/shell"
	"tofu/internal/subagent"
	shipped "tofu/library"
)

type askTool struct {
	orchestrator *SpawnTool
	asking       subagent.SubAgent
	conversation []llm.Message
}

type askArgs struct {
	Question string `json:"question"`
	Why      string `json:"why"`
	Default  string `json:"default"`
}

const (
	AskAnswered = "the orchestrator answers: "
	AskAssumed  = "the orchestrator did not answer, so your default stands, assumed and not confirmed: "
)

func (askTool) Name() string { return "ask" }

func (askTool) Definition() llm.Tool {
	text := map[string]any{"type": "string"}
	return llm.Tool{
		Name: "ask",
		Description: "asks the orchestrator one question and waits for its answer, which comes back as this result. " +
			"ask before you start a server or a watcher, bind a port, kill a process you did not start, or change a file outside the paths you hold. " +
			"question is the question alone, in one or two sentences, and why carries the reasoning and the facts behind it. " +
			"default is what you would do with no answer: when the orchestrator cannot answer in time it stands, and the result says it was assumed",
		Parameters: map[string]any{
			"type":       "object",
			"properties": map[string]any{"question": text, "why": text, "default": text},
			"required":   []string{"question", "why", "default"},
		},
	}
}

func (a askTool) Run(ctx context.Context, raw json.RawMessage) (Result, error) {
	var args askArgs
	if err := json.Unmarshal(raw, &args); err != nil {
		return Result{}, fmt.Errorf("ask: arguments are not the expected shape: %w", err)
	}
	if strings.TrimSpace(args.Question) == "" || strings.TrimSpace(args.Default) == "" {
		return Result{}, errors.New("ask: question and default are both required")
	}
	t, id := a.orchestrator, a.asking.ID
	asked := boardy.Event{At: t.clock().UTC().Truncate(time.Second), Kind: boardy.Asked, Author: boardy.SubAgentAuthor(t.base.Session, id), Agent: id,
		Text: args.Question, Why: args.Why, Default: args.Default}
	t.roster.Reached(id, subagent.WaitingAnswer, "asks: "+args.Question)
	answer, err := t.answer(ctx, a.asking, a.conversation, args)
	t.roster.Reached(id, subagent.Working, "")
	if err != nil {
		asked.Unanswered = err.Error()
		return Result{Content: AskAssumed + args.Default + "\n\nwhy there was no answer: " + err.Error() + a.onThread(asked), Command: "ask assumed: " + args.Question}, nil
	}
	written := a.onThread(asked, boardy.Event{Kind: boardy.Answered, Actor: t.base.Session, Author: t.speaker(), Agent: id, Text: answer})
	return Result{Content: AskAnswered + answer + written, Command: "ask answered: " + args.Question}, nil
}

func (a askTool) onThread(asked boardy.Event, answered ...boardy.Event) string {
	t, ticket := a.orchestrator, a.asking.Ticket
	if ticket == "" || t.base.Board == nil {
		return ""
	}
	asked.Actor = t.ticketActor(ticket)
	written := t.onThread(ticket, asked)
	for _, event := range answered {
		written += t.onThread(ticket, event)
	}
	return written
}

func (t *SpawnTool) answer(ctx context.Context, asking subagent.SubAgent, conversation []llm.Message, args askArgs) (string, error) {
	model := t.base.Model
	if t.base.Accounts.Pick != nil {
		account, err := t.base.Accounts.Pick(ctx)
		if err != nil {
			return "", err
		}
		model = account.Model
	}
	if model == nil {
		return "", errors.New("the orchestrator has no model to answer with")
	}
	rules, err := rule.LoadFS(shipped.Files(), "library")
	if err != nil {
		return "", err
	}
	system := t.base.System
	for _, loaded := range rules {
		if loaded.ID == "answer_asks" && !strings.Contains(system, loaded.Text) {
			system = strings.TrimSpace(loaded.Text + "\n\n" + system)
		}
	}
	var running []string
	if registry := ShellRegistryFrom(ctx); registry != nil {
		shells, err := registry.List()
		if err != nil {
			return "", err
		}
		for _, held := range shells {
			if held.State != shell.Running {
				continue
			}
			line := held.Name + " runs " + held.Command
			if port := shell.NamedPort(held.Dir, held.Command, nil); port > 0 {
				line += " on port " + strconv.Itoa(port)
			}
			running = append(running, line)
		}
	}
	for _, held := range t.roster.SubAgents() {
		running = append(running, fmt.Sprintf("sub-agent %s is %s on %q, holding %s", held.ID, held.State, held.Mission, strings.Join(held.Owns, ", ")))
	}
	question := fmt.Sprintf("sub-agent %s, whose brief is %q, asks you: %s\nwhy: %s\nwhat it does with no answer: %s\n\nwhat runs now: %s\n\nanswer it in one line.",
		asking.ID, asking.Brief, args.Question, args.Why, args.Default, strings.Join(running, "; "))
	spoken := slices.DeleteFunc(slices.Clone(conversation), func(message llm.Message) bool { return message.Role == llm.RoleSystem })
	messages := append([]llm.Message{{Role: llm.RoleSystem, Content: system}}, Sendable(WithoutThinking(spoken))...)
	request := llm.Request{Messages: append(messages, llm.Message{Role: llm.RoleUser, Content: question})}
	if tools := t.base.Tools.Definitions(); len(tools) > 0 {
		request.Tools, request.ToolChoice = tools, llm.ToolChoiceNone
	}
	within, cancel := context.WithTimeout(ctx, konst.SubAgentAskMillis*time.Millisecond)
	defer cancel()
	tapped, tap := llm.Tapped(within)
	started := time.Now()
	decision, err := model.Ask(tapped, request)
	if errors.Is(err, context.DeadlineExceeded) {
		err = fmt.Errorf("the orchestrator's model gave no answer within %v", konst.SubAgentAskMillis*time.Millisecond)
	}
	t.traceAnswer(ctx, asking.ID, request, started, tap.Attempts(), decision, err)
	if err != nil {
		return "", err
	}
	if answer := strings.TrimSpace(decision.Content); decision.Outcome == llm.OutcomeMessage && answer != "" {
		return answer, nil
	}
	var called []string
	for _, call := range decision.ToolCalls {
		called = append(called, call.Name)
	}
	return "", fmt.Errorf("the orchestrator's model returned no answer text: it ended as %s, stop reason %q, calling %s",
		decision.Outcome, decision.Stop, cmp.Or(strings.Join(called, ", "), "no tool"))
}

func (t *SpawnTool) traceAnswer(ctx context.Context, asking string, request llm.Request, started time.Time, attempts []llm.Attempt, decision llm.Decision, failed error) {
	site, _ := ctx.Value(spawnSiteKey{}).(spawnSite)
	if site.log == nil {
		return
	}
	runs := site.log.Header().Agents
	at := slices.IndexFunc(runs, func(run session.AgentRun) bool { return run.Agent == asking })
	if at < 0 {
		return
	}
	answering := &record{log: site.log, scope: site.turn, turn: site.turn, agent: runs[at].ParentAgent, said: map[string]string{}}
	id, why := session.NewEventID(), "answers "+asking+"'s ask"
	if failed == nil {
		why += fmt.Sprintf(": ended as %s, stop reason %q", decision.Outcome, decision.Stop)
	}
	answering.exchange(id, why, t.base.Wire, request, started, attempts, decision, failed)
	if failed == nil {
		answering.step(session.EventRequest, stepFrom(id, 0, requestTiming{attempt: len(attempts), durationMS: time.Since(started).Milliseconds()}, decision))
	}
}
