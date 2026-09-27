package tui

import (
	"cmp"
	"context"
	"slices"
	"strconv"
	"strings"
	"time"

	tea "charm.land/bubbletea/v2"

	"tofu/interface/tui/edits"
	"tofu/interface/tui/feed"
	"tofu/interface/tui/session"
	"tofu/interface/tui/shells"
	"tofu/interface/tui/subagent"
	"tofu/interface/tui/trace"
	"tofu/internal/judge/jev"
	"tofu/internal/konst"
	isettings "tofu/internal/settings"
	roster "tofu/internal/subagent"
)

const (
	gateOffLine        = "the gate is off, so no call on this session is judged."
	gateOffNoKey       = "put OPENROUTER_KEY in .tofu/.env and the next session is judged."
	gateOffKeyUnnamed  = ".tofu/.env carries no OPENROUTER_KEY line: add one and the next session is judged."
	gateOffKeyUnread   = "the .env file that should carry OPENROUTER_KEY could not be read."
	gateOffUnexplained = "tofu could not open the judge."
	gateOffHead        = "gate off"
	stoppingNote       = "stopping the turn"
	stoppingModel      = "stopping the model, and letting the running tools finish"
	queuedKept         = ", and the queue keeps "
	queuedTyped        = " you typed"
	quitAgainNote      = "press ctrl+c again to quit tofu"
	failureHead        = "failure"
	partialHead        = "answer, interrupted"
	charactersKept     = " characters were written and kept in sub-agents "
	orchestrator       = "orchestrator"
	verdictLine        = "verdict "
	noEngine           = "no engine is wired to this app"
)

func gateOffNote(why jev.Why) string {
	switch why {
	case jev.WhyNoFile:
		return gateOffLine + " " + gateOffNoKey
	case jev.WhyFileLacksName:
		return gateOffLine + " " + gateOffKeyUnnamed
	case jev.WhyUnreadable:
		return gateOffLine + " " + gateOffKeyUnread
	case jev.WhyUnexplained:
		return gateOffLine + " " + gateOffUnexplained
	}
	panic("tui: unknown gate reason")
}

func short(id string) string { return strings.TrimPrefix(trace.Short(id), "#") }

func (a *App) send() tea.Cmd {
	task := a.view.Value()
	if task == "" {
		return nil
	}
	if prefix, isID := idPrefix(task); isID {
		a.view.Reset()
		a.jumpToID(prefix)
		return nil
	}
	chips := a.view.Remember(task)
	whole := session.Expand(task, chips)
	a.view.Reset()
	if a.busy {
		a.view.Queue(task, whole, chips)
		a.steer(whole)
		return nil
	}
	a.view.Append(session.Entry{Kind: session.User, Body: task, Chips: chips})
	return a.start(whole)
}

func idPrefix(task string) (string, bool) {
	prefix, hasHash := strings.CutPrefix(task, "#")
	if !hasHash || prefix == "" {
		return "", false
	}
	for _, letter := range prefix {
		if !strings.ContainsRune("0123456789abcdefABCDEF", letter) {
			return "", false
		}
	}
	return strings.ToLower(prefix), true
}

func (a *App) jumpToID(typed string) {
	for _, event := range a.happened {
		if strings.HasPrefix(event.ID, typed) || strings.HasSuffix(event.ID, typed) {
			a.show(screenAgents)
			a.feed.Focus(event.ID)
			return
		}
	}
	a.view.Append(session.Entry{Kind: session.Note, Body: "nothing in sub-agents carries the id #" + typed})
}

func (a *App) steer(task string) {
	select {
	case a.options.Steering <- task:
	default:
	}
}

func (a *App) dropSteering() {
	for {
		select {
		case <-a.options.Steering:
		default:
			return
		}
	}
}

func (a *App) answer(answer Answer) {
	if a.options.Answers == nil {
		return
	}
	select {
	case a.options.Answers <- answer:
		a.view.Resume()
	default:
	}
}

func (a *App) mintID() string {
	a.minted++
	return strconv.FormatInt(a.options.Now().UnixNano(), 16) + strconv.Itoa(a.minted)
}

func (a *App) turnEventID() string {
	if a.keptAnswer != "" {
		return a.keptAnswer
	}
	for index := len(a.happened) - 1; index >= a.happenedAtTurn; index-- {
		if a.happened[index].Actor == orchestrator {
			return a.happened[index].ID
		}
	}
	return ""
}

func (a *App) interrupt() tea.Cmd {
	if a.view.Stopping {
		return nil
	}
	withinQuitWindow := a.options.Now().Sub(a.pressedAt) <= konst.QuitAgainMillis*time.Millisecond
	a.pressedAt = a.options.Now()
	switch {
	case !a.busy:
		if withinQuitWindow {
			return tea.Quit
		}
		a.view.Append(session.Entry{Kind: session.Note, Body: quitAgainNote})
	case a.view.LettingToolsFinish:
		a.stopTurn()
	case a.running == 0 || len(a.subAgentCalls) > 0 || a.view.TakesAnswerDigits():
		a.stopTurn()
	default:
		a.view.LettingToolsFinish = true
		a.noteStop(stoppingModel + a.queueTail())
	}
	return nil
}

func (a *App) queueTail() string {
	held := len(a.view.Queued())
	if held == 0 {
		return ""
	}
	word := " messages"
	if held == 1 {
		word = " message"
	}
	return queuedKept + strconv.Itoa(held) + word + queuedTyped
}

func (a *App) stopTurn() {
	if a.view.Stopping {
		return
	}
	a.view.LettingToolsFinish, a.view.Stopping = false, true
	a.cancel()
	a.dropSteering()
	a.noteStop(stoppingNote + a.queueTail())
}

func (a *App) noteStop(note string) {
	kept := a.keptPartial()
	a.view.Append(session.Entry{Kind: session.Note, Body: note})
	if kept != "" {
		a.view.Append(session.Entry{Kind: session.Note, Body: kept})
	}
}

func (a *App) keptPartial() string {
	partial, written := a.view.TakePartial()
	if !written {
		return ""
	}
	id := a.mintID()
	a.keptAnswer = short(id)
	a.record(feed.Event{ID: a.keptAnswer, Actor: orchestrator, Kind: feed.KindNote, State: feed.StateComplete, Title: partialHead, Body: partial, At: a.options.Now()})
	return strconv.Itoa(len([]rune(partial))) + charactersKept + "[note#" + a.keptAnswer + "]"
}

func (a *App) start(task string) tea.Cmd {
	a.view.Follow()
	a.status.Fresh = false
	a.intro.shown = false
	a.happenedAtTurn, a.keptAnswer, a.subAgentCalls = len(a.happened), "", nil
	if a.options.Turn == nil {
		a.view.Append(session.Entry{Kind: session.Failure, Body: noEngine})
		return nil
	}
	ctx, cancel := context.WithCancel(context.Background())
	events := make(chan Event, eventBuffer)
	a.busy, a.cancel, a.events, a.edits.Busy = true, cancel, events, true
	a.running = 0
	a.view.Start()
	turn, pick := a.options.Turn, Pick{Wire: a.wire, Model: a.picked, Effort: a.effort}
	deliver := func(event Event) {
		if !event.snapshot() {
			events <- event
			return
		}
		select {
		case events <- event:
		default:
		}
	}
	go func() {
		turn(ctx, pick, task, deliver)
		cancel()
		close(events)
	}()
	return a.waitForEvent()
}

func (a *App) waitForEvent() tea.Cmd {
	events := a.events
	return func() tea.Msg {
		event, open := <-events
		if !open {
			return Closed{}
		}
		return event
	}
}

func (a *App) absorb(event Event) {
	if event.answered() {
		a.view.Returned()
	}
	if len(a.subAgentCalls) > 0 && (event.Kind == EventText || event.Kind == EventTextDelta) {
		return
	}
	at := a.options.Now()
	switch event.Kind {
	case EventRequesting:
		a.view.Requesting()
	case EventTask:
		a.view.Append(session.Entry{Kind: session.User, Body: event.Text})
	case EventText:
		a.view.Append(session.Entry{Kind: session.Assistant, Body: event.Text, ID: event.ID})
	case EventTextDelta:
		a.view.Stream(event.Text)
	case EventToolCall:
		a.called(event, at)
	case EventToolResult:
		a.answered(event, at)
	case EventNote:
		a.view.Append(session.Entry{Kind: session.Note, Body: event.Text})
	case EventDone:
		if event.SubAgents != nil {
			a.showSubAgents(event.SubAgents)
		}
		labelled := a.turnEventID()
		if a.view.Stopping && a.keptAnswer == "" {
			labelled = ""
		}
		a.view.Close(event.Text, labelled)
	case EventFailure:
		id := cmp.Or(event.ID, a.mintID())
		a.record(feed.Event{ID: short(id), Actor: orchestrator, Kind: feed.KindFailure, State: feed.StateFailed, Title: strings.TrimSpace(failureHead + " " + event.Tool), Body: event.Text, At: at})
		a.view.Append(session.Entry{Kind: session.Failure, ID: id, Body: event.Text})
	case EventDecision:
		if event.Decision == nil {
			break
		}
		if event.Agent == "" && !event.Promote {
			a.view.Decide(*event.Decision)
		}
		a.judged(*event.Decision, cmp.Or(event.Agent, orchestrator))
	case EventSession:
		if event.ID != a.sessionID {
			a.started = at
		}
		a.sessionName, a.sessionID = event.Text, event.ID
	case EventGateOff:
		if !a.gateOff {
			a.gateOff = true
			id := short(a.mintID())
			a.record(feed.Event{ID: id, Actor: orchestrator, Kind: feed.KindNote, State: feed.StateComplete, Title: gateOffHead, Body: event.Text, At: at})
			a.happenedAtTurn = len(a.happened)
			a.view.Append(session.Entry{Kind: session.Note, ID: id, Body: gateOffNote(event.GateWhy)})
		}
	case EventContext:
		a.status.Context = event.Context
	case EventSubAgent:
		a.showSubAgents(event.SubAgents)
	case EventPlan:
		a.feed.SetPlan(planLine(event.Plan))
	case EventAwaitPerson:
		a.view.Await()
	case EventResumed:
		a.view.Resume()
	case EventSteered:
		a.view.Delivered(event.Text)
	case EventForkStart:
		a.forking = true
	case EventForkEnd:
		a.forking = false
	case EventStats:
		a.status.TokensIn, a.status.TokensOut, a.status.CacheRead = event.TokensIn, event.TokensOut, event.CacheRead
		a.status.Decisions = event.Decisions
		if event.Model != "" {
			a.model = event.Model
		}
	}
}

func (a *App) called(event Event, at time.Time) {
	call := feed.Event{ID: short(event.ID), Actor: cmp.Or(event.Agent, orchestrator), Kind: feed.KindTool, Title: event.Tool, Body: event.Text, Detail: lines(event.Detail), At: at}
	if event.Promote {
		call.Kind, call.Title, call.Body, call.Detail = feed.KindSpawn, event.Text, event.Detail, nil
	}
	a.record(call)
	if event.Agent != "" {
		return
	}
	a.running++
	if event.Promote {
		a.subAgentCalls = append(a.subAgentCalls, event.ID)
		return
	}
	a.view.Append(session.Entry{Kind: session.Tool, ID: event.ID, Head: event.Tool, Body: event.Text, Detail: event.Detail})
}

func (a *App) answered(event Event, at time.Time) {
	a.subAgentCalls = slices.DeleteFunc(a.subAgentCalls, func(called string) bool { return called == event.ID })
	status := event.Text
	finished := a.finish(short(event.ID), event.Text, event.Failed, at)
	if finished.Kind == feed.KindSpawn && a.view.Stopping {
		finished.State = feed.StateStopped
	}
	if edit, changed := edits.Changed(event.Agent, finished.Body, event.Diff, event.Created, event.ID, at); changed {
		a.edits.Add(edit)
		status = edit.Tally()
		finished.Kind, finished.Op, finished.Path, finished.Added, finished.Removed = feed.KindEdit, feed.Op(edit.Op()), edit.Path, edit.Added(), edit.Removed()
	}
	a.record(finished)
	if event.Agent != "" {
		return
	}
	if finished.Kind != feed.KindSpawn {
		a.view.Finish(event.ID, session.Result{Status: status, Output: event.Detail, Bytes: event.Bytes, Failed: event.Failed})
	}
	a.running = max(a.running-1, 0)
	if a.view.LettingToolsFinish && a.running == 0 {
		a.stopTurn()
	}
}

func planLine(plan []session.PlanItem) string {
	if len(plan) == 0 {
		return ""
	}
	closed, running, pending := 0, "", ""
	for _, item := range plan {
		switch item.State {
		case session.PlanDone, session.PlanDropped:
			closed++
		case session.PlanRunning:
			running = cmp.Or(running, item.Text)
		case session.PlanPending:
			pending = cmp.Or(pending, item.Text)
		}
	}
	return "plan " + strconv.Itoa(closed) + "/" + strconv.Itoa(len(plan)) + " · " + cmp.Or(running, pending, "done")
}

func lines(text string) []string {
	if text == "" {
		return nil
	}
	return strings.Split(strings.TrimRight(text, "\n"), "\n")
}

func (a *App) happenedAt(id string) int {
	return slices.IndexFunc(a.happened, func(held feed.Event) bool { return held.ID == id })
}

func (a *App) record(event feed.Event) {
	if at := a.happenedAt(event.ID); at >= 0 {
		a.happened[at] = event
	} else {
		a.happened = append(a.happened, event)
	}
	a.feedStale = true
}

func (a *App) flushFeed() {
	if a.feedStale {
		a.feed.SetEvents(slices.Clone(a.happened))
		a.feedStale = false
	}
}

func (a *App) stopWhatStillRuns() {
	for index := range a.happened {
		if a.happened[index].State == feed.StateRunning {
			a.happened[index].State = feed.StateStopped
			a.feedStale = true
		}
	}
}

func (a *App) finish(id, output string, failed bool, at time.Time) feed.Event {
	finished := feed.Event{ID: id, Actor: orchestrator, Kind: feed.KindTool, At: at}
	if found := a.happenedAt(id); found >= 0 {
		finished = a.happened[found]
		finished.Detail = slices.Clone(finished.Detail)
		finished.Elapsed = at.Sub(finished.At)
	}
	finished.State = feed.StateComplete
	if failed {
		finished.State = feed.StateFailed
	}
	finished.Detail = append(finished.Detail, lines(output)...)
	return finished
}

func (a *App) judged(decision session.Decision, actor string) {
	if decision.Verdict == session.Allow {
		return
	}
	for index := len(a.happened) - 1; index >= 0; index-- {
		held := a.happened[index]
		if held.Kind == feed.KindTool && held.Actor == actor && held.Title == decision.Tool && held.State == feed.StateRunning {
			held.Detail = append(slices.Clone(held.Detail), verdictLine+decision.Verdict.String())
			a.record(held)
			return
		}
	}
}

func (a *App) showSubAgents(subAgents []subagent.Row) {
	a.subAgents, a.view.SubAgents, a.edits.SubAgents = subAgents, subAgents, subAgents
	a.status.Agents = 0
	for _, subAgent := range subAgents {
		if subAgent.State == roster.Working {
			a.status.Agents++
		}
		a.linkSpawn(subAgent)
		for _, call := range subAgent.Calls {
			a.rosterCall(subAgent.Name, call)
		}
	}
}

func (a *App) rosterCall(actor string, call subagent.Call) {
	id := short(call.ID)
	if id == "" {
		return
	}
	if at := a.happenedAt(id); at >= 0 && (a.happened[at].State != feed.StateRunning || call.Result == "" || a.view.Stopping) {
		return
	}
	state := feed.StateRunning
	if call.Result != "" {
		state = feed.StateComplete
	}
	a.record(feed.Event{ID: id, Actor: actor, Kind: feed.KindTool, State: state, Title: call.Tool, Body: call.Text, Detail: lines(call.Result), At: cmp.Or(call.At, a.options.Now())})
}

func (a *App) linkSpawn(subAgent subagent.Row) {
	turn := a.happened[min(a.happenedAtTurn, len(a.happened)):]
	if slices.ContainsFunc(turn, func(held feed.Event) bool { return held.Kind == feed.KindSpawn && held.Target == subAgent.Name }) {
		return
	}
	at := slices.IndexFunc(turn, func(held feed.Event) bool {
		return held.Kind == feed.KindSpawn && held.State == feed.StateRunning && held.Target == ""
	})
	if at < 0 {
		return
	}
	spawn := turn[at]
	spawn.Target, spawn.Title = subAgent.Name, subAgent.Doing
	a.record(spawn)
	if spawn.Actor == orchestrator {
		a.view.Append(session.Entry{Kind: session.Note, Body: "spawning [&" + subAgent.Name + "] to " + subAgent.Doing})
	}
}

func (a *App) parkSubAgentsTheTurnLeftBehind() {
	parked := slices.Clone(a.subAgents)
	for i, subAgent := range parked {
		if subAgent.State == roster.Working || subAgent.State == roster.WaitingAnswer {
			parked[i].State = roster.Parked
		}
	}
	a.showSubAgents(parked)
}

func (a *App) showShells(entries []shells.Entry) {
	a.shells.SetKillConfirm(a.flag(isettings.KillConfirm))
	a.shells.Set(entries)
}
