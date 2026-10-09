package tui

import (
	"cmp"
	"encoding/json"
	"slices"
	"strconv"
	"strings"
	"time"

	tea "charm.land/bubbletea/v2"

	"tofu/interface/tui/edits"
	"tofu/interface/tui/feed"
	"tofu/interface/tui/question"
	"tofu/interface/tui/session"
	"tofu/interface/tui/shells"
	"tofu/interface/tui/subagent"
	"tofu/interface/tui/trace"
	"tofu/internal/host"
	"tofu/internal/judge/jev"
	"tofu/internal/konst"
	isession "tofu/internal/session"
	isettings "tofu/internal/settings"
	roster "tofu/internal/subagent"
)

const (
	gateOffLine        = "the gate is off, so no call on this session is judged."
	gateOffNoKey       = "run tofu login classifier openrouter and the next session is judged."
	gateOffKeyUnnamed  = "the project .env carries no OPENROUTER_KEY line: run tofu login classifier openrouter and the next session is judged."
	gateOffKeyUnread   = "the project .env file that should carry OPENROUTER_KEY could not be read."
	gateOffStoreUnread = "the credential store that holds the openrouter key could not be read."
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
	sentNowNote        = "sent to the lead now"
	readHead           = "read by the lead at step "
	readClock          = "15:04"
)

func gateOffNote(why jev.Why) string {
	switch why {
	case jev.WhyNoFile:
		return gateOffLine + " " + gateOffNoKey
	case jev.WhyFileLacksName:
		return gateOffLine + " " + gateOffKeyUnnamed
	case jev.WhyUnreadable:
		return gateOffLine + " " + gateOffKeyUnread
	case jev.WhyStoreUnreadable:
		return gateOffLine + " " + gateOffStoreUnread
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
	if a.stopCommand != nil {
		a.view.Append(session.Entry{Kind: session.Note, Body: bangStillRunning})
		return nil
	}
	chips := a.view.Remember(task)
	whole := session.Expand(task, chips)
	if command, isBang := strings.CutPrefix(whole, bangPrefix); isBang {
		return a.runBang(task, chips, whole, strings.TrimSpace(command))
	}
	if a.rememberTyped(whole) {
		return nil
	}
	a.rememberPrompt(whole)
	a.view.Reset()
	if a.busy {
		a.view.Queue(task, whole, chips)
		a.steer(whole)
		return nil
	}
	a.view.Append(session.Entry{Kind: session.User, Body: task, Chips: chips})
	a.start(whole)
	return nil
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
	if a.options.Host != nil {
		a.steered[task] = a.options.Host.Steer(task)
	}
}

func (a *App) sendNow(task string) bool {
	sent := a.options.Host != nil && a.options.Host.SendNow(a.steered[task])
	if sent {
		a.notify(sentNowNote)
	}
	return sent
}

func (a *App) sendPickedNow() {
	if picked, queued := a.view.PickedQueued(); queued && a.busy {
		a.sendNow(picked)
	}
}

func (a *App) dropSteering() {
	if a.options.Host != nil {
		a.options.Host.DropSteering()
	}
}

func (a *App) unqueue() {
	picked, queued := a.view.PickedQueued()
	leadTookIt := queued && a.options.Host != nil && !a.options.Host.Unsteer(picked) && a.busy && !a.view.Stopping
	if !leadTookIt {
		a.view.Unqueue()
	}
}

const questionAnsweredHere = "tui"

func (a *App) questionKey(key string) bool {
	if len(a.questions) == 0 {
		return false
	}
	form := a.questions[0]
	typed := a.view.Value()
	action, took := form.Key(key, typed)
	if !took {
		return false
	}
	if typed != "" {
		a.view.Redraft("")
	}
	answer := host.QuestionAnswer{Outcome: host.QuestionSubmitted, Answers: form.Replies()}
	switch action {
	case question.Kept:
		return true
	case question.Dismissed:
		answer = host.QuestionAnswer{Outcome: host.QuestionCancelled}
	case question.Submitted:
	}
	a.questions = a.questions[1:]
	if a.options.Host != nil {
		a.options.Host.AnswerQuestion(form.ID, answer, questionAnsweredHere)
	}
	return true
}

func (a *App) answer(answer Answer) {
	if asked := a.view.AskedID(); a.options.Host != nil && a.options.Host.Answer(asked, answer) {
		a.view.Resume(asked)
	}
}

func (a *App) recorded(event Event) {
	var said isession.MessageBody
	if event.Agent != "" || event.Logged == nil || event.Logged.Kind != isession.EventMessage || json.Unmarshal(event.Logged.Body, &said) != nil {
		return
	}
	a.view.Recorded(said.Role, said.Content, event.Logged.ID, event.Logged.At)
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
		if a.happened[index].Actor == orchestrator && a.happened[index].Kind != feed.KindThinking {
			return a.happened[index].ID
		}
	}
	return ""
}

func (a *App) interrupt() tea.Cmd {
	subAgentsRun := a.status.Agents > 0
	if a.busy && subAgentsRun && (!a.leading || a.view.Stopping) {
		a.askToStopSubAgents()
		return nil
	}
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
	case subAgentsRun, a.view.LettingToolsFinish, a.running == 0 || len(a.subAgentCalls) > 0 || a.view.TakesAnswerDigits():
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
	if a.status.Agents > 0 {
		a.stopLead()
		return
	}
	a.stopEverything()
}

func (a *App) askToStopSubAgents() {
	if !a.view.AskedToStop() {
		a.view.AskToStop(a.status.Agents, a.options.Now().Add(konst.StopSubAgentsMillis*time.Millisecond))
		return
	}
	a.view.AskToStop(0, time.Time{})
	a.stopEverything()
}

func (a *App) stopEverything() {
	a.view.LettingToolsFinish, a.view.Stopping = false, true
	a.options.Host.Stop()
	a.dropSteering()
	a.noteStop(stoppingNote + a.queueTail())
}

func (a *App) stopLead() {
	if !a.leading {
		return
	}
	a.view.LettingToolsFinish, a.view.Stopping = false, true
	a.options.Host.StopLead()
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

func (a *App) leadTurnBegins() {
	a.view.Follow()
	a.status.Fresh = false
	a.intro.shown = false
	a.happenedAtTurn, a.keptAnswer, a.subAgentCalls, a.view.Spawns = len(a.happened), "", nil, 0
	a.leading, a.running = true, 0
	a.view.Start()
}

func (a *App) leadTurnEnds(words string) {
	labelled := a.turnEventID()
	if a.view.Stopping && a.keptAnswer == "" {
		labelled = ""
	}
	a.view.Close(words, labelled)
	a.view.Stop()
	a.leading, a.running = false, 0
	a.view.WaitOn(a.status.Agents)
	a.drawHeldReports()
}

func (a *App) drawHeldReports() {
	for _, held := range a.heldReports {
		a.view.Reported(a.mintID(), held)
	}
	a.heldReports = nil
}

func (a *App) start(task string) {
	if a.options.Host == nil {
		a.view.Append(session.Entry{Kind: session.Failure, Body: noEngine})
		return
	}
	a.busy, a.edits.Busy = true, true
	a.leadTurnBegins()
	if !a.options.Host.Send(Pick{Wire: a.wire, Model: a.picked, Effort: a.effort}, task) {
		a.steer(task)
	}
}

func (a *App) waitForEvent() tea.Cmd {
	if a.options.Host == nil {
		return nil
	}
	events := a.options.Host.Events()
	return func() tea.Msg {
		event := <-events
		if event.Kind == host.EventTurnEnded {
			return Closed{}
		}
		return event
	}
}

func (a *App) listen() tea.Cmd {
	read := a.waitForEvent()
	if read == nil || !a.listening.CompareAndSwap(false, true) {
		return nil
	}
	return func() tea.Msg {
		msg := read()
		a.listening.Store(false)
		return msg
	}
}

func (a *App) absorb(event Event) {
	if answered(event) && event.Agent == "" {
		a.view.Returned()
	}
	if len(a.subAgentCalls) > 0 && (event.Kind == EventText || event.Kind == EventTextDelta) {
		return
	}
	at := a.options.Now()
	switch event.Kind {
	case EventTurnStarted:
		if !a.busy {
			a.busy, a.edits.Busy = true, true
			a.leadTurnBegins()
		}
		a.countCrons()
	case EventRequesting:
		if !a.leading {
			a.leadTurnBegins()
		}
		a.view.Requesting()
	case EventTask:
		if event.Origin.Kind != host.OriginTofu {
			a.view.Append(session.Entry{Kind: session.User, Body: event.Text})
		}
	case EventText:
		a.view.Append(session.Entry{Kind: session.Assistant, Body: event.Text, ID: event.ID})
	case EventTextDelta:
		a.view.Stream(event.Text)
	case EventStreamReset:
		if event.Agent == "" {
			a.view.TakePartial()
		}
		if at := a.happenedAt(short(event.ID)); event.ID != "" && at >= 0 {
			a.forget(at)
		}
	case EventThinking:
		if event.Agent == "" {
			a.view.Thinks()
		}
		a.thought(event, at)
	case EventToolCall:
		a.called(event, at)
	case EventToolResult:
		a.answered(event, at)
	case EventNote:
		a.view.Append(session.Entry{Kind: session.Note, Body: event.Text})
		a.countCrons()
	case EventDone:
		a.leadTurnEnds(event.Text)
		if event.SubAgents != nil {
			a.showSubAgents(event.SubAgents)
		}
	case EventFailure:
		id := cmp.Or(event.ID, a.mintID())
		a.record(feed.Event{ID: short(id), Actor: orchestrator, Kind: feed.KindFailure, State: feed.StateFailed, Title: strings.TrimSpace(failureHead + " " + event.Tool), Body: event.Text, At: at})
		a.view.Append(session.Entry{Kind: session.Failure, ID: id, Body: event.Text})
	case EventDecision:
		if event.Decision == nil {
			break
		}
		if event.Agent == "" && !event.Promote {
			a.view.Decide(event.ID, *event.Decision)
		}
		a.judged(*event.Decision, cmp.Or(event.Agent, orchestrator))
	case EventSession:
		if root := cmp.Or(event.Root, event.ID); root != a.sessionRoot {
			a.started, a.sessionRoot = at, root
			if event.Identity != nil {
				a.started = event.Identity.Started
			}
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
		if event.Questions != nil {
			a.questions = append(a.questions, question.Open(event.ID, event.Questions, event.Wait, at))
			return
		}
		a.view.Await(event.ID, event.Tool, event.Text, event.Decision)
		a.showLeadMemoryAsk()
	case EventResumed:
		a.questions = slices.DeleteFunc(a.questions, func(form *question.Form) bool { return form.ID == event.ID })
		a.view.Resume(event.ID)
		a.showLeadMemoryAsk()
	case EventPersisted:
		a.recorded(event)
	case EventSteered:
		a.view.Delivered(event.Text, readHead+strconv.Itoa(event.Step)+" · "+at.Format(readClock))
		a.sentNow = false
	case EventForkStart:
		a.forking = true
	case EventForkEnd:
		a.forking = false
	case EventStats:
		a.status.TokensIn, a.status.TokensOut, a.status.CacheRead = event.TokensIn, event.TokensOut, event.CacheRead
		a.status.Decisions = event.Decisions
	}
}

func (a *App) thought(event Event, at time.Time) {
	id := short(event.ID)
	thinking := feed.Event{ID: id, Actor: cmp.Or(event.Agent, orchestrator), Kind: feed.KindThinking, State: feed.StateComplete, At: at}
	if found := a.happenedAt(id); found >= 0 {
		thinking = a.happened[found]
	}
	thinking.Body += event.Text
	a.record(thinking)
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
		a.view.Spawns = len(a.subAgentCalls)
		return
	}
	a.view.Append(session.Entry{Kind: session.Tool, ID: event.ID, Head: event.Tool, Body: event.Text, Detail: event.Detail})
}

func (a *App) answered(event Event, at time.Time) {
	a.subAgentCalls = slices.DeleteFunc(a.subAgentCalls, func(called string) bool { return called == event.ID })
	a.view.Spawns = len(a.subAgentCalls)
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
	a.rememberedByTheLead(event.Detail)
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
	if at, held := a.happenedIndex[id]; held {
		return at
	}
	return -1
}

func (a *App) record(event feed.Event) {
	if at := a.happenedAt(event.ID); at >= 0 {
		a.happened[at] = event
	} else {
		a.happenedIndex[event.ID] = len(a.happened)
		a.happened = append(a.happened, event)
	}
	a.feedStale = true
}

func (a *App) forget(at int) {
	a.happened = slices.Delete(a.happened, at, at+1)
	clear(a.happenedIndex)
	for index, held := range a.happened {
		a.happenedIndex[held.ID] = index
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
		a.stepped(subAgent)
		a.drawReport(subAgent)
		for _, call := range subAgent.Calls {
			a.rosterCall(subAgent.Name, call)
		}
	}
	if a.busy && !a.leading {
		a.view.WaitOn(a.status.Agents)
	}
}

func (a *App) drawReport(subAgent subagent.Row) {
	seen, spawnedByTheLead := a.reports[subAgent.Name]
	stillRunning := subAgent.State == roster.Working || subAgent.State == roster.Reopened || subAgent.State == roster.WaitingAnswer
	if !spawnedByTheLead || stillRunning || subAgent.Report == "" || subAgent.Report == seen {
		return
	}
	a.reports[subAgent.Name] = subAgent.Report
	if a.leading {
		a.heldReports = append(a.heldReports, subAgent)
		return
	}
	a.view.Reported(a.mintID(), subAgent)
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
	if slices.ContainsFunc(a.happened, func(held feed.Event) bool { return held.Kind == feed.KindSpawn && held.Target == subAgent.Name }) {
		return
	}
	turn := a.happened[min(a.happenedAtTurn, len(a.happened)):]
	at := slices.IndexFunc(turn, func(held feed.Event) bool {
		return held.Kind == feed.KindSpawn && held.State != feed.StateFailed && held.Target == ""
	})
	if at < 0 {
		return
	}
	spawn := turn[at]
	spawn.Target, spawn.Title = subAgent.Name, subAgent.Doing
	a.record(spawn)
	if spawn.Actor == orchestrator {
		a.view.Spawned(subAgent.Name)
		a.reports[subAgent.Name] = ""
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

func (a *App) shellActivity() []session.Activity {
	var rows []session.Activity
	for _, entry := range a.liveShells {
		counted := func(row session.Activity) bool { return row.Name == entry.Owner }
		working := func(row subagent.Row) bool { return row.Name == entry.Owner && row.State == roster.Working }
		if slices.ContainsFunc(a.view.Activity, counted) || slices.ContainsFunc(rows, counted) || !slices.ContainsFunc(a.subAgents, working) {
			continue
		}
		rows = append(rows, session.Activity{Name: entry.Owner, Doing: session.RunningBash})
	}
	return rows
}

func (a *App) showShells(entries []shells.Entry) {
	a.printed(entries)
	a.shells.SetKillConfirm(a.flag(isettings.KillConfirm))
	a.shells.Set(slices.DeleteFunc(slices.Clone(entries), func(entry shells.Entry) bool { return entry.OneShot }))
}
