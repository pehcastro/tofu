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

	"tofu/internal/agent/context/recall"
	"tofu/internal/agent/subagents"
	"tofu/internal/konst"
	"tofu/internal/librarian/rules"
	"tofu/internal/models/llm/core"
	"tofu/internal/platform/recover"
	"tofu/internal/project/boardy"
	"tofu/internal/store/session"
	"tofu/internal/sys"
	"tofu/internal/tools/shell/process"
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
	sent := Sendable(WithoutThinking(conversation))
	if len(sent) == 0 {
		sent = []llm.Message{{Role: llm.RoleSystem, Content: t.base.System}}
	}
	var system, unruled string
	if sent[0].Role == llm.RoleSystem {
		system = sent[0].Content
	}
	for _, loaded := range rules {
		if loaded.ID == "answer_asks" && !strings.Contains(system, loaded.Text) {
			unruled = loaded.Text + "\n\n"
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
	question := fmt.Sprintf("%ssub-agent %s, whose brief is %q, asks you: %s\nwhy: %s\nwhat it does with no answer: %s\n\nwhat runs now: %s\n\n"+
		"answer it in one line of text: that text is the whole answer %s receives, and no tool runs in this reply, so do not reach for message or any other tool.",
		unruled, asking.ID, asking.Brief, args.Question, args.Why, args.Default, strings.Join(running, "; "), asking.ID)
	request := llm.Request{Messages: append(sent, llm.Message{Role: llm.RoleUser, Content: question})}
	tools := t.base.Tools.Definitions()
	site, _ := ctx.Value(spawnSiteKey{}).(spawnSite)
	if lead, recorded := leadOf(site, asking.ID); recorded && t.base.Sessions != nil {
		held, err := HeldPrefix(t.base.Sessions, site.log.ID(), lead)
		if err != nil {
			return "", err
		}
		tools = held.toolsOr(tools)
	}
	if len(tools) > 0 {
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

func leadOf(site spawnSite, asking string) (string, bool) {
	if site.log == nil {
		return "", false
	}
	runs := site.log.Header().Agents
	at := slices.IndexFunc(runs, func(run session.AgentRun) bool { return run.Agent == asking })
	if at < 0 {
		return "", false
	}
	return runs[at].ParentAgent, true
}

func (t *SpawnTool) askThePerson(ctx context.Context, agent string, ticket boardy.Ticket, q boardy.Question) string {
	reason := strings.ReplaceAll(q.ELI5, "\n", " ")
	note := fmt.Sprintf("\n\n%s reports %s not possible, so %s is %s on Q%d and the person decides it: no move of yours takes it out until they answer, and you take no option for them:\nQ%d %s\nreason: %s",
		agent, ticket.ID, ticket.ID, ticket.Status, q.ID, q.ID, q.TLDR, reason)
	recommended := q.Recommend - 1
	asked := PersonQuestion{ID: "Q" + strconv.Itoa(q.ID), Header: ticket.ID + " not possible", Question: strings.TrimSuffix(reason, ".") + ": how should " + ticket.ID + " go?", Type: QuestionChoice, Recommended: &recommended}
	for i, option := range q.Options {
		note += fmt.Sprintf("\n%d. %s", i+1, option)
		asked.Options = append(asked.Options, PersonOption{Label: option})
	}
	note += fmt.Sprintf("\nrecommended %d: %s", q.Recommend, q.Why)
	form := PersonFormFrom(ctx)
	if form == nil || t.Inbox == nil {
		return note + fmt.Sprintf("\n\nnothing here shows the person a question, so put Q%d to them in this turn with %s, its reason and every option, the recommended one marked", q.ID, AskPersonToolName)
	}
	open := t.Inbox.Open(asked.Header + ": " + asked.Question)
	safe.Go(sys.Crashes(t.Project), "not possible question", func() {
		defer open.Close()
		replies, err := form(context.WithoutCancel(ctx), []PersonQuestion{asked}, 0)
		said := t.answeredOnBoard(ticket.ID, q, replies, err)
		open.Answer(said, llm.Origin{Source: llm.SourceAnswer, Name: asked.ID, Shown: said})
	})
	return note + fmt.Sprintf("\n\ntofu has put Q%d to the person itself, and their answer reaches you as a message once it is written to %s", q.ID, ticket.ID)
}

func (t *SpawnTool) answeredOnBoard(id string, q boardy.Question, replies []PersonReply, asked error) string {
	answer := ""
	for _, reply := range replies {
		for _, chosen := range reply.Chosen {
			if at := slices.Index(q.Options, chosen); at >= 0 {
				answer = strconv.Itoa(at + 1)
			}
		}
		answer = strings.TrimSpace(answer + " " + reply.Text)
	}
	if answer == "" {
		return fmt.Sprintf("the person gave no answer to %s Q%d (%v), so it stays in %s until they answer it in catch up", id, q.ID, asked, boardy.NeedsDecision)
	}
	ticket, err := t.base.Board.Answer(id, q.ID, answer, cmp.Or(t.base.Board.Person, string(boardy.RolePerson)))
	if err != nil {
		return fmt.Sprintf("the person answered %s Q%d with %q, and writing it to the ticket failed: %v", id, q.ID, answer, err)
	}
	return fmt.Sprintf("the person answered %s Q%d %q with %s, and %s is %s again", id, q.ID, q.TLDR, answer, id, ticket.Status)
}

func (t *SpawnTool) traceAnswer(ctx context.Context, asking string, request llm.Request, started time.Time, attempts []llm.Attempt, decision llm.Decision, failed error) {
	site, _ := ctx.Value(spawnSiteKey{}).(spawnSite)
	lead, recorded := leadOf(site, asking)
	if !recorded {
		return
	}
	answering := &record{log: site.log, scope: site.turn, turn: site.turn, agent: lead, said: map[string]string{}, announce: t.base.Sent, lastSent: &messagesKept{}}
	id, why := session.NewEventID(), "answers "+asking+"'s ask"
	if failed == nil {
		why += fmt.Sprintf(": ended as %s, stop reason %q", decision.Outcome, decision.Stop)
	}
	preview, err := recall.LoadConfig()
	answering.note(err)
	answering.sent(id, why, request, preview.OnWire(t.base.Wire))
	answering.exchange(id, why, t.base.Wire, request, started, attempts, decision, failed)
	if failed == nil {
		answering.step(session.EventRequest, stepFrom(id, 0, requestTiming{attempt: len(attempts), durationMS: time.Since(started).Milliseconds()}, decision))
	}
}
