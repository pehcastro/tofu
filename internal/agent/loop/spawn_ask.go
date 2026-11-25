package loop

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
	rule "tofu/internal/librarian/rules"
	"tofu/internal/models/llm/core"
	"tofu/internal/platform/outside"
	"tofu/internal/platform/project"
	"tofu/internal/platform/safe"
	"tofu/internal/project/boardy"
	"tofu/internal/store/session"
	"tofu/internal/store/walls"
	"tofu/internal/tools/contract"
	"tofu/internal/tools/shell/process"
	shipped "tofu/library"
)

type askTool struct {
	orchestrator *SpawnTool
	asking       subagents.SubAgent
	conversation []core.Message
}

type askArgs struct {
	Question string   `json:"question"`
	Why      string   `json:"why"`
	Default  string   `json:"default"`
	Options  []string `json:"options"`
	Refs     []string `json:"refs"`
}

func (args askArgs) refused() error {
	for _, field := range []struct {
		name, text string
		most       int
	}{{"question", args.Question, konst.SubAgentAskQuestionBytes}, {"why", args.Why, konst.SubAgentAskWhyBytes}, {"default", args.Default, konst.SubAgentAskDefaultBytes}} {
		if strings.TrimSpace(field.text) == "" {
			return fmt.Errorf("ask: %s is required. nothing was asked", field.name)
		}
		if len(field.text) > field.most {
			return fmt.Errorf("ask: %s is %d bytes and holds at most %d: say it shorter and name what you read in refs instead of pasting it. nothing was asked", field.name, len(field.text), field.most)
		}
	}
	for _, list := range []struct {
		name               string
		items              []string
		fewest, most, each int
	}{
		{"options", args.Options, konst.SubAgentAskOptionsFewest, konst.SubAgentAskOptionsMost, konst.SubAgentAskOptionBytes},
		{"refs", args.Refs, 0, konst.SubAgentAskRefsMost, konst.SubAgentAskRefBytes},
	} {
		if len(list.items) > 0 && (len(list.items) < list.fewest || len(list.items) > list.most) {
			return fmt.Errorf("ask: %s holds %d and takes %d to %d, or none. nothing was asked", list.name, len(list.items), list.fewest, list.most)
		}
		for i, item := range list.items {
			if len(item) > list.each {
				return fmt.Errorf("ask: %s %d is %d bytes and holds at most %d. nothing was asked", list.name, i+1, len(item), list.each)
			}
		}
	}
	return nil
}

const (
	AskAnswered = "the orchestrator answers: "
	AskAssumed  = "the orchestrator did not answer, so your default stands, assumed and not confirmed: "
)

func (askTool) Name() string { return "ask" }

func (askTool) Definition() core.Tool {
	text := map[string]any{"type": "string"}
	list := map[string]any{"type": "array", "items": text}
	return core.Tool{
		Name: "ask",
		Description: "asks the orchestrator one question and waits for its answer, which comes back as this result. " +
			"ask before you start a server or a watcher, bind a port, kill a process you did not start, or change a file outside the paths you hold. " +
			"question is the question alone, in one or two sentences, and why carries the reasoning and the facts behind it. " +
			"default is what you would do with no answer: when the orchestrator cannot answer in time it stands, and the result says it was assumed. " +
			fmt.Sprintf("question holds at most %d bytes, why %d and default %d. ", konst.SubAgentAskQuestionBytes, konst.SubAgentAskWhyBytes, konst.SubAgentAskDefaultBytes) +
			fmt.Sprintf("options, when you give them, are %d to %d choices of at most %d bytes each, your default first. ", konst.SubAgentAskOptionsFewest, konst.SubAgentAskOptionsMost, konst.SubAgentAskOptionBytes) +
			fmt.Sprintf("refs are up to %d places the orchestrator may open, such as file:line, an artifact handle or a ticket id, at most %d bytes each: name what you read there rather than pasting it. ", konst.SubAgentAskRefsMost, konst.SubAgentAskRefBytes) +
			"an ask over any of these is refused whole, naming the field",
		Parameters: map[string]any{
			"type":       "object",
			"properties": map[string]any{"question": text, "why": text, "default": text, "options": list, "refs": list},
			"required":   []string{"question", "why", "default"},
		},
	}
}

func (a askTool) Run(ctx context.Context, raw json.RawMessage) (contract.Result, error) {
	var args askArgs
	if err := json.Unmarshal(raw, &args); err != nil {
		return contract.Result{}, fmt.Errorf("ask: arguments are not the expected shape: %w", err)
	}
	if err := args.refused(); err != nil {
		return contract.Result{}, err
	}
	t, id := a.orchestrator, a.asking.ID
	asked := boardy.Event{At: t.clock().UTC().Truncate(time.Second), Kind: boardy.Asked, Author: boardy.SubAgentAuthor(t.base.Session, id), Agent: id,
		Text: args.Question, Why: args.Why, Default: args.Default}
	t.roster.Reached(id, subagents.WaitingAnswer, "asks: "+args.Question)
	waited := outside.Wait(ctx, outside.Person)
	answer, err := t.answer(ctx, a.asking, a.conversation, args)
	waited()
	t.roster.Reached(id, subagents.Working, "")
	if err != nil {
		asked.Unanswered = err.Error()
		return contract.Result{Content: AskAssumed + args.Default + "\n\nwhy there was no answer: " + err.Error() + a.onThread(asked), Command: "ask assumed: " + args.Question}, nil
	}
	written := a.onThread(asked, boardy.Event{Kind: boardy.Answered, Actor: t.base.Session, Author: t.speaker(), Agent: id, Text: answer})
	return contract.Result{Content: AskAnswered + answer + written, Command: "ask answered: " + args.Question}, nil
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

func (t *SpawnTool) answer(ctx context.Context, asking subagents.SubAgent, conversation []core.Message, args askArgs) (string, error) {
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
	site, _ := ctx.Value(spawnSiteKey{}).(spawnSite)
	request, leadRequest, err := t.leadStep(site, asking.ID)
	if err != nil {
		return "", err
	}
	if leadRequest == "" {
		request = core.Request{Messages: Sendable(WithoutThinking(conversation))}
		if len(request.Messages) == 0 {
			request.Messages = []core.Message{{Role: core.RoleSystem, Content: t.base.System}}
		}
		if tools := t.base.Tools.Definitions(); len(tools) > 0 {
			request.Tools, request.ToolChoice = tools, core.ToolChoiceNone
		}
	}
	var system, unruled string
	if request.Messages[0].Role == core.RoleSystem {
		system = request.Messages[0].Content
	}
	for _, loaded := range rules {
		if loaded.ID == "answer_asks" && !strings.Contains(system, loaded.Text) {
			unruled = loaded.Text + "\n\n"
		}
	}
	running, err := t.runningNow(ctx)
	if err != nil {
		return "", err
	}
	brief := "its brief is in your spawn call for it"
	if leadRequest == "" {
		brief, _ = fitted(asking.Brief, konst.SubAgentAskBriefBytes)
		brief = fmt.Sprintf("whose brief is %q", brief)
	}
	asked := &strings.Builder{}
	fmt.Fprintf(asked, "%ssub-agent %s, on ticket %s, mission %q, %s, asks you: %s\nwhy: %s\nwhat it does with no answer: %s\n",
		unruled, asking.ID, cmp.Or(asking.Ticket, "none"), asking.Mission, brief, args.Question, args.Why, args.Default)
	for i, option := range args.Options {
		fmt.Fprintf(asked, "option %d: %s\n", i+1, option)
	}
	if len(args.Refs) > 0 {
		fmt.Fprintf(asked, "refs: %s\n", strings.Join(args.Refs, "; "))
	}
	fmt.Fprintf(asked, "\nwhat runs now: %s\n\nanswer it in one line of text: that text is the whole answer %s receives, and no tool runs in this reply, so do not reach for message or any other tool.",
		running, asking.ID)
	request.Messages = append(request.Messages, core.Message{Role: core.RoleUser, Content: asked.String(), Origin: core.Origin{Source: core.SourceAfterTheCachedPrefix}})
	within, cancel := context.WithTimeout(ctx, konst.SubAgentAskMillis*time.Millisecond)
	defer cancel()
	tapped, tap := core.Tapped(within)
	decision, timing, err := askCountingAttempts(tapped, model, request)
	if errors.Is(err, context.DeadlineExceeded) {
		err = fmt.Errorf("the orchestrator's model gave no answer within %v", konst.SubAgentAskMillis*time.Millisecond)
	}
	t.traceAnswer(ctx, asking.ID, leadRequest, request, timing, tap.Attempts(), decision, err)
	if err != nil {
		return "", err
	}
	if answer := strings.TrimSpace(decision.Content); decision.Outcome == core.OutcomeMessage && answer != "" {
		answer, _ = fitted(answer, konst.SubAgentAskAnswerBytes)
		return answer, nil
	}
	var called []string
	for _, call := range decision.ToolCalls {
		called = append(called, call.Name)
	}
	return "", fmt.Errorf("the orchestrator's model returned no answer text: it ended as %s, stop reason %q, calling %s",
		decision.Outcome, decision.Stop, cmp.Or(strings.Join(called, ", "), "no tool"))
}

const leadStepWhy = "step "

func (t *SpawnTool) leadStep(site spawnSite, asking string) (core.Request, string, error) {
	lead, recorded := leadOf(site, asking)
	if !recorded || t.base.Sessions == nil {
		return core.Request{}, "", nil
	}
	exchanges, err := t.base.Sessions.Exchanges(site.log.ID())
	if err != nil {
		return core.Request{}, "", err
	}
	for _, exchange := range slices.Backward(exchanges) {
		if exchange.Agent != lead || exchange.Error != "" || !strings.HasPrefix(exchange.Why, leadStepWhy) {
			continue
		}
		blobs, err := t.base.Sessions.Blobs(site.log.ID())
		if err != nil {
			return core.Request{}, "", err
		}
		var request core.Request
		if exchange.ToolChoice == core.ToolChoiceNone.OpenAIValue() {
			request.ToolChoice = core.ToolChoiceNone
		}
		if err := json.Unmarshal(blobs[exchange.Tools], &request.Tools); err != nil {
			return core.Request{}, "", fmt.Errorf("the tools of lead request %s did not read back: %w", exchange.Request, err)
		}
		for _, hash := range exchange.Messages {
			var row MessageRow
			if err := json.Unmarshal(blobs[hash], &row); err != nil {
				return core.Request{}, "", fmt.Errorf("message %s of lead request %s did not read back: %w", hash, exchange.Request, err)
			}
			for i, picture := range row.Pictures {
				if err := json.Unmarshal(blobs[picture.Blob], &row.Pictures[i].Data); err != nil {
					return core.Request{}, "", fmt.Errorf("picture %s of lead request %s did not read back: %w", picture.Blob, exchange.Request, err)
				}
			}
			message, err := messageOf(row)
			if err != nil {
				return core.Request{}, "", err
			}
			request.Messages = append(request.Messages, message)
		}
		return request, exchange.Request, nil
	}
	return core.Request{}, "", nil
}

func (t *SpawnTool) runningNow(ctx context.Context) (string, error) {
	var running []string
	if registry := ShellRegistryFrom(ctx); registry != nil {
		shells, err := registry.List()
		if err != nil {
			return "", err
		}
		for _, held := range shells {
			if held.State != process.Running {
				continue
			}
			line := held.Name + " runs " + held.Command
			if port := process.NamedPort(held.Dir, held.Command, nil); port > 0 {
				line += " on port " + strconv.Itoa(port)
			}
			running = append(running, line)
		}
	}
	ended := 0
	for _, held := range t.roster.SubAgents() {
		switch held.State {
		case subagents.Working, subagents.WaitingAnswer, subagents.InReview, subagents.Reopened:
			running = append(running, fmt.Sprintf("sub-agent %s is %s on %q, holding %s", held.ID, held.State, held.Mission, strings.Join(held.Owns, ", ")))
		case subagents.Parked, subagents.Errored, subagents.Finished:
			ended++
		}
	}
	const between = "; "
	var listed []string
	room := konst.SubAgentAskRunningRunes
	for _, line := range running {
		cut := []rune(line)
		cut = cut[:min(len(cut), konst.SubAgentAskLineRunes)]
		if room -= len(cut) + len(between); room < 0 {
			break
		}
		listed = append(listed, string(cut))
	}
	counted := fmt.Sprintf("%d sub-agents ended or parked and are not listed", ended)
	if unlisted := len(running) - len(listed); unlisted > 0 {
		counted += fmt.Sprintf(", nor %d more that run, past the %d characters this list holds", unlisted, konst.SubAgentAskRunningRunes)
	}
	return strings.Join(append(listed, counted), between), nil
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

type personAwayKey struct{}

func WithPersonAway(ctx context.Context, away func() bool) context.Context {
	return context.WithValue(ctx, personAwayKey{}, away)
}

func PersonAway(ctx context.Context) bool {
	away, _ := ctx.Value(personAwayKey{}).(func() bool)
	return away != nil && away()
}

func (t *SpawnTool) decidedAway(ticket boardy.Ticket, q boardy.Question) string {
	decided, err := t.base.Board.DecideAway(ticket.ID, q, t.ticketActor(ticket.ID))
	if err != nil {
		return fmt.Sprintf("\n\nthe person is away, and taking Q%d's recommended option on %s failed, so it stays %s for them: %v", q.ID, ticket.ID, ticket.Status, err)
	}
	at := slices.IndexFunc(decided.Questions, func(left boardy.Question) bool { return left.ID == q.ID })
	if decided.Status == boardy.NeedsDecision {
		return fmt.Sprintf("\n\n%s is %s on Q%d: %s. go on with other work and take no option for them", ticket.ID, decided.Status, q.ID, decided.Questions[at].Away)
	}
	return fmt.Sprintf("\n\nthe person is away, so tofu took Q%d %q on %s: %s. %s is %s again: carry the work on with that option and say so in your reply",
		q.ID, q.TLDR, ticket.ID, decided.Questions[at].Answer, ticket.ID, decided.Status)
}

func (t *SpawnTool) askThePerson(ctx context.Context, agent string, ticket boardy.Ticket, q boardy.Question) string {
	if PersonAway(ctx) {
		return t.decidedAway(ticket, q)
	}
	note := fmt.Sprintf("\n\n%s reports %s not possible, so %s is %s on Q%d %q and only the person decides it. take no option for them, and do not repeat or re-ask the question in chat: ",
		agent, ticket.ID, ticket.ID, ticket.Status, q.ID, q.TLDR)
	asked, form := boardQuestion(ticket.ID, q), PersonFormFrom(ctx)
	unshown := CheckQuestions([]PersonQuestion{asked})
	if form == nil || t.Inbox == nil || unshown != nil {
		why := ""
		if unshown != nil {
			why = ". it is not shown as a form, because " + unshown.Error()
		}
		return note + "they answer it in catch up, so say in one line that " + ticket.ID + " waits on their decision and go on with other work" + why
	}
	open := t.Inbox.Open()
	safe.Go(project.Crashes(t.Project), "not possible question", func() {
		defer open.Close()
		replies, err := form(context.WithoutCancel(ctx), []PersonQuestion{asked}, 0)
		said, shown := t.answeredOnBoard(ticket.ID, q, asked, replies, err)
		open.Answer(said, core.Origin{Source: core.SourceAnswer, Name: asked.ID, Shown: shown})
	})
	return note + "tofu has put it to the person as a form, and their answer reaches you as a message once it is written to " + ticket.ID + ". go on with other work"
}

func boardQuestion(id string, q boardy.Question) PersonQuestion {
	recommended := q.Recommend - 1
	asked := PersonQuestion{ID: "Q" + strconv.Itoa(q.ID), Header: id + " not possible", Question: id + " cannot be done as asked. How should it go?",
		Context: q.ELI5 + "\n\nRecommended: " + strings.TrimSuffix(q.Options[recommended], ".") + ". " + q.Why, Type: QuestionChoice, Recommended: &recommended}
	fits := func(text string) bool { return len(strings.Fields(text)) <= konst.AskPersonLabelWordsMost }
	for _, option := range q.Options {
		switch label, description, split := strings.Cut(option, ": "); {
		case split && fits(label):
			asked.Options = append(asked.Options, PersonOption{Label: label, Description: description})
		case fits(option):
			asked.Options = append(asked.Options, PersonOption{Label: option})
		default:
			asked.Options = append(asked.Options, PersonOption{Label: strings.Join(strings.Fields(option)[:konst.AskPersonLabelWordsMost], " "), Description: option})
		}
	}
	return asked
}

func (t *SpawnTool) answeredOnBoard(id string, q boardy.Question, asked PersonQuestion, replies []PersonReply, failed error) (string, string) {
	answer := ""
	for _, reply := range replies {
		for _, chosen := range reply.Chosen {
			if at := slices.IndexFunc(asked.Options, func(o PersonOption) bool { return o.Label == chosen }); at >= 0 {
				answer = strconv.Itoa(at + 1)
			}
		}
		answer = strings.TrimSpace(answer + " " + reply.Text)
	}
	var said, head string
	if answer == "" {
		why := "the person closed it without choosing"
		if failed != nil && !errors.As(failed, &QuestionDismissed{}) {
			why = "the form could not be shown: " + failed.Error()
		}
		said = fmt.Sprintf("%s Q%d got no answer, because %s: nothing was chosen and it stays %s until they answer it in catch up. take no option for them and do not ask it again", id, q.ID, why, boardy.NeedsDecision)
		head = "nothing was chosen; " + id + " waits on your answer in catch up"
	} else if ticket, err := t.base.Board.Answer(id, q.ID, answer, cmp.Or(t.base.Board.Person, string(boardy.RolePerson))); err != nil {
		said = fmt.Sprintf("the person answered %s Q%d with %q, and writing it to the ticket failed: %v", id, q.ID, answer, err)
		head = "you answered, and writing it to " + id + " failed"
	} else {
		said = fmt.Sprintf("the person answered %s Q%d %q with option %s, and %s is %s again", id, q.ID, q.TLDR, answer, id, ticket.Status)
		head = "you answered; " + id + " is " + string(ticket.Status) + " again"
	}
	return said, ShownAnswers(head, []PersonQuestion{asked}, replies)
}

func (t *SpawnTool) traceAnswer(ctx context.Context, asking, leadRequest string, request core.Request, timing requestTiming, attempts []core.Attempt, decision core.Decision, failed error) {
	site, _ := ctx.Value(spawnSiteKey{}).(spawnSite)
	lead, recorded := leadOf(site, asking)
	if !recorded {
		return
	}
	answering := &record{log: site.log, scope: site.turn, turn: site.turn, agent: lead, said: map[string]string{}, announce: t.base.Sent, lastSent: &messagesKept{}}
	id, why := session.NewEventID(), "answers "+asking+"'s ask on lead request "+leadRequest
	if leadRequest == "" {
		why = "answers " + asking + "'s ask with no lead request recorded, so no cache to reuse"
	}
	if failed == nil {
		why += fmt.Sprintf(": ended as %s, stop reason %q", decision.Outcome, decision.Stop)
	}
	preview, err := recall.LoadConfig()
	answering.note(err)
	answering.sent(id, why, request, preview.OnWire(t.base.Wire))
	answering.exchange(id, why, t.base.Wire, request, timing, attempts, decision, failed)
	answering.requested(id, walls.OpRequest, cmp.Or(decision.Build, t.base.Wire), timing, decision, failed)
	if failed == nil {
		answering.step(session.EventRequest, stepFrom(id, 0, timing, decision))
	}
}
