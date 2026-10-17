package tui

import (
	"cmp"
	"encoding/json"
	"maps"
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
	"tofu/internal/command"
	"tofu/internal/host"
	"tofu/internal/judge/jev"
	"tofu/internal/konst"
	isession "tofu/internal/session"
	isettings "tofu/internal/settings"
	roster "tofu/internal/subagent"
	"tofu/internal/turn"
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
	stillStoppingNote  = "the turn has not stopped, so everything it started is stopping too; if it still runs, ctrl+c quits tofu"
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
	foldKey            = "f"
	readBackFailed     = "the folded sub-agents were not all read back from the session: "
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

func (a *App) shownID(full string) string {
	if longer, lengthened := a.lengthenedIDs[full]; lengthened {
		return longer
	}
	return short(full)
}

func (a *App) shortOf(full string) string {
	runes, base := []rune(full), short(full)
	for length := len([]rune(a.shownID(full))); length < len(runes); length++ {
		id := string(runes[len(runes)-length:])
		if at := a.happenedAt(id); at < 0 || a.happened[at].FullID == full {
			if id != base {
				a.lengthenedIDs[full] = id
			}
			return id
		}
	}
	return full
}

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
	whole, refused := command.Expand(whole, cmp.Or(a.options.Root, "."))
	if refused != nil {
		a.view.Append(session.Entry{Kind: session.Note, Body: refused.Error()})
		return nil
	}
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
	at := a.reach(func(event feed.Event) bool {
		return strings.HasPrefix(event.ID, typed) || strings.HasSuffix(event.ID, typed)
	})
	if at < 0 {
		a.view.Append(session.Entry{Kind: session.Note, Body: "nothing in sub-agents carries the id #" + typed})
		return
	}
	a.show(screenAgents)
	a.flushFeed()
	a.feed.Focus(a.happened[at].ID)
}

func (a *App) reach(matches func(feed.Event) bool) int {
	at := slices.IndexFunc(a.happened, matches)
	if at >= 0 || len(a.hidden()) == 0 {
		return at
	}
	a.toggleFold()
	if at = slices.IndexFunc(a.happened, matches); at < 0 {
		a.toggleFold()
	}
	a.syncFeed()
	return at
}

func (a *App) reachedAt(id string) int {
	return a.reach(func(event feed.Event) bool { return event.ID == id })
}

func (a *App) steer(task string) {
	if a.options.Host != nil {
		a.steered[task] = a.options.Host.Steer(task)
	}
}

func (a *App) queueSteeredElsewhere(task string) {
	if !slices.Contains(a.view.Queued(), task) {
		a.view.Queue(task, task, nil)
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

func (a *App) cronFired(prompt string) {
	title, _, _ := strings.Cut(prompt, " (")
	full := a.mintID()
	id := a.shortOf(full)
	a.record(feed.Event{ID: id, FullID: full, Actor: orchestrator, Kind: feed.KindNote, State: feed.StateComplete, Title: title, Body: prompt, At: a.options.Now()})
	a.view.Append(session.Entry{Kind: session.Note, Head: session.FiredHead, ID: id, Body: title})
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
	if a.stopOverdue() && a.options.Host.Cancelled() {
		a.options.Host.ClosingBy(a.askedBy("ctrl+c while a stop hung").By)
		return tea.Quit
	}
	subAgentsRun := a.status.Agents > 0
	if a.busy && subAgentsRun && (!a.leading || a.view.Stopping) {
		a.askToStopSubAgents()
		return nil
	}
	if a.stopOverdue() {
		a.stopEverythingAs(a.askedBy("ctrl+c over " + strconv.Itoa(konst.StopSubAgentsMillis/1000) + " s after a stop"))
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
			if a.options.Host != nil {
				a.options.Host.ClosingBy(a.askedBy("ctrl+c twice to quit").By)
			}
			return tea.Quit
		}
		a.view.Append(session.Entry{Kind: session.Note, Body: quitAgainNote})
	case subAgentsRun, a.view.LettingToolsFinish, a.running == 0 || len(a.view.Spawning) > 0 || a.view.TakesAnswerDigits():
		a.stopTurnAs(a.askedBy("ctrl+c"))
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

func (a *App) stopOverdue() bool {
	return a.busy && a.view.Stopping && a.options.Now().Sub(a.pressedAt) >= konst.StopSubAgentsMillis*time.Millisecond
}

func (a *App) stopTurn() { a.stopTurnAs(a.askedBy("esc")) }

func (a *App) stopTurnAs(asked host.StopAsked) {
	if a.view.Stopping {
		return
	}
	if a.status.Agents > 0 {
		a.stopLead(asked)
		return
	}
	a.stopEverythingAs(asked)
}

func (a *App) askedBy(key string) host.StopAsked {
	screen := "settings"
	if names := tabNames(); int(a.current) < len(names) {
		screen = names[a.current]
	}
	return host.StopAsked{By: "the person, " + key + " on the " + screen + " screen"}
}

func (a *App) askToStopSubAgents() {
	if !a.view.AskedToStop() {
		a.view.AskToStop(a.status.Agents, a.options.Now().Add(konst.StopSubAgentsMillis*time.Millisecond))
		return
	}
	a.view.AskToStop(0, time.Time{})
	asked := a.askedBy("ctrl+c twice")
	asked.Why, asked.Everything = "the person confirmed stopping every running sub-agent", true
	a.stopEverythingAs(asked)
}

func (a *App) stopEverything() { a.stopEverythingAs(a.askedBy("the cancel choice of an ask")) }

func (a *App) stopEverythingAs(asked host.StopAsked) {
	note := stillStoppingNote
	if !a.view.Stopping {
		a.pressedAt, note = a.options.Now(), stoppingNote
	}
	a.view.LettingToolsFinish, a.view.Stopping = false, true
	a.options.Host.Stop(asked)
	a.dropSteering()
	a.noteStop(note + a.queueTail())
}

func (a *App) stopLead(asked host.StopAsked) {
	if !a.leading {
		return
	}
	a.view.LettingToolsFinish, a.view.Stopping, a.pressedAt = false, true, a.options.Now()
	a.options.Host.StopLead(asked)
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
	a.keptAnswer = a.shortOf(id)
	a.record(feed.Event{ID: a.keptAnswer, FullID: id, Actor: orchestrator, Kind: feed.KindNote, State: feed.StateComplete, Title: partialHead, Body: partial, At: a.options.Now()})
	return strconv.Itoa(len([]rune(partial))) + charactersKept + "[note#" + a.keptAnswer + "]"
}

func (a *App) leadTurnBegins() {
	a.view.Follow()
	a.status.Fresh = false
	a.intro.shown = false
	a.happenedAtTurn, a.keptAnswer, a.view.Spawning = len(a.happened), "", nil
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
	events := a.options.Host.Events()
	for queued := len(events); a.busy && queued > 0; queued-- {
		event := <-events
		if event.Kind == host.EventTurnEnded {
			read = func() tea.Msg { return Closed{} }
			break
		}
		a.followStatus(event)
		a.absorb(event)
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
	if event.Agent != "" && (event.Kind == EventText || event.Kind == EventTextDelta) {
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
		if event.Origin.Kind == host.OriginCron {
			a.cronFired(event.Text)
		}
	case EventRequesting:
		if !a.leading {
			a.leadTurnBegins()
		}
		a.view.Requesting()
	case EventTask:
		switch {
		case event.Origin.Kind == host.OriginCron:
			a.cronFired(event.Text)
		case event.Origin.Kind != host.OriginTofu:
			a.view.Append(session.Entry{Kind: session.User, Body: event.Text})
		case event.Origin.Source == turn.SourceReplyCut:
			a.view.Append(session.Entry{Kind: session.Note, Body: event.Text})
		}
	case EventText:
		a.view.Append(session.Entry{Kind: session.Assistant, Body: event.Text, ID: event.ID})
	case EventTextDelta:
		a.view.Stream(event.Text)
	case EventStreamReset:
		if event.Agent == "" {
			a.view.TakePartial()
		}
		if id := a.shortOf(event.ID); event.ID != "" && a.happenedAt(id) >= 0 {
			a.keepOnly(func(held feed.Event) bool { return held.ID != id })
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
		if event.Origin.Kind == host.OriginCron {
			a.cronFired(event.Detail)
		} else {
			a.view.Append(session.Entry{Kind: session.Note, Body: event.Text})
		}
		a.countCrons()
	case EventDone:
		a.leadTurnEnds(event.Text)
		if event.SubAgents != nil {
			a.showSubAgents(event.SubAgents)
		}
	case EventFailure:
		id := cmp.Or(event.ID, a.mintID())
		a.record(feed.Event{ID: a.shortOf(id), FullID: id, Actor: orchestrator, Kind: feed.KindFailure, State: feed.StateFailed, Title: strings.TrimSpace(failureHead + " " + event.Tool), Body: event.Text, At: at})
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
			full := a.mintID()
			id := a.shortOf(full)
			a.record(feed.Event{ID: id, FullID: full, Actor: orchestrator, Kind: feed.KindNote, State: feed.StateComplete, Title: gateOffHead, Body: event.Text, At: at})
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
	case host.EventSteerQueued:
		a.queueSteeredElsewhere(event.Text)
		a.steered[event.Text] = event.ID
	case EventSteered:
		a.queueSteeredElsewhere(event.Text)
		a.view.Delivered(event.Text, readHead+strconv.Itoa(event.Step)+" · "+at.Format(readClock))
		a.sentNow = false
	case EventForkStart:
		a.forking = true
	case EventForkEnd:
		a.forking = false
	case EventStats:
		a.status.TokensIn, a.status.TokensOut, a.status.CacheRead = event.TokensIn, event.TokensOut, event.CacheRead
		a.status.Decisions = event.Decisions
	case host.EventAccount:
		a.followSource(event.Account)
		a.followAccount(event.Account)
	}
}

func (a *App) followSource(spent *host.TurnAccount) {
	if spent == nil || spent.Source == a.provider {
		return
	}
	at := slices.IndexFunc(a.wires, func(wire Wire) bool { return wire.Provider == spent.Source })
	if at < 0 {
		a.notify("the turn moved to " + spent.Source + ", which the model picker does not list, so the next turn runs " + a.slug())
		return
	}
	a.wire, a.provider, a.model, a.picked = a.wires[at].Name, spent.Source, spent.Model, spent.Source+"/"+spent.Model
	a.view.Append(session.Entry{Kind: session.Note, Body: "the turn moved to " + a.picked + ", so the next turn runs there too; pick another model to change it"})
}

func (a *App) thinkingOf(event Event, at time.Time) feed.Event {
	return feed.Event{ID: a.shortOf(event.ID), FullID: event.ID, Actor: cmp.Or(event.Agent, orchestrator), Kind: feed.KindThinking, State: feed.StateComplete, At: at}
}

func (a *App) thought(event Event, at time.Time) {
	thinking := a.thinkingOf(event, at)
	if found := a.happenedAt(thinking.ID); found >= 0 {
		thinking = a.happened[found]
	}
	if thinking.Thought == nil {
		thinking.Thought = &strings.Builder{}
		thinking.Thought.WriteString(thinking.Body)
	}
	thinking.Thought.WriteString(event.Text)
	thinking.Body = thinking.Thought.String()
	a.record(thinking)
}

func (a *App) callOf(event Event, at time.Time) feed.Event {
	call := feed.Event{ID: a.shortOf(event.ID), FullID: event.ID, Actor: cmp.Or(event.Agent, orchestrator), Kind: feed.KindTool, Title: event.Tool, Body: event.Text, Detail: lines(event.Detail), At: at}
	if event.Promote {
		call.Kind, call.Title, call.Body, call.Detail = feed.KindSpawn, event.Text, event.Detail, nil
	}
	return call
}

func (a *App) finished(held *feed.Event, event Event, at time.Time) (feed.Event, edits.Edit, bool) {
	done := feed.Event{ID: a.shortOf(event.ID), FullID: event.ID, Actor: orchestrator, Kind: feed.KindTool, At: at}
	if held != nil {
		done = *held
		done.Detail = slices.Clone(done.Detail)
		done.Elapsed = at.Sub(done.At)
	}
	done.State = feed.StateComplete
	if event.Failed {
		done.State = feed.StateFailed
	}
	done.Detail = append(done.Detail, lines(event.Text)...)
	edit, changed := edits.Changed(event.Agent, done.Body, event.Diff, event.Created, event.ID, at)
	if changed {
		done.Kind, done.Op, done.Path, done.Added, done.Removed = feed.KindEdit, feed.Op(edit.Op()), edit.Path, edit.Added(), edit.Removed()
	}
	return done, edit, changed
}

func (a *App) called(event Event, at time.Time) {
	a.record(a.callOf(event, at))
	if event.Agent != "" {
		return
	}
	a.running++
	if event.Promote {
		a.view.Spawn(event.ID, event.Tool, event.Text)
		return
	}
	a.view.Append(session.Entry{Kind: session.Tool, ID: event.ID, Head: event.Tool, Body: event.Text, Detail: event.Detail})
}

func (a *App) answered(event Event, at time.Time) {
	a.view.Spawning = slices.DeleteFunc(a.view.Spawning, func(called string) bool { return called == event.ID })
	var held *feed.Event
	if found := a.happenedAt(a.shortOf(event.ID)); found >= 0 {
		held = &a.happened[found]
	}
	done, edit, edited := a.finished(held, event, at)
	status := event.Text
	if edited {
		a.edits.Add(edit)
		status = edit.Tally()
	}
	if done.Kind == feed.KindSpawn && done.Target == "" && len(event.SubAgents) > 0 {
		a.spawnedAs(&done, event.SubAgents[0])
	}
	if done.Kind == feed.KindSpawn && a.view.Stopping {
		done.State = feed.StateStopped
	}
	a.record(done)
	if event.Agent != "" {
		return
	}
	if done.Kind != feed.KindSpawn {
		a.view.Finish(event.ID, session.Result{Status: status, Output: event.Detail, Bytes: event.Bytes, Failed: event.Failed})
	}
	a.rememberedByTheLead(event.Detail)
	a.running = max(a.running-1, 0)
	if a.view.LettingToolsFinish && a.running == 0 {
		a.stopTurnAs(a.askedBy("ctrl+c, once the running tools finished"))
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
	if a.hidden()[event.Actor] {
		return
	}
	if at := a.happenedAt(event.ID); at >= 0 {
		a.happened[at] = event
	} else {
		a.happenedIndex[event.ID] = len(a.happened)
		a.happened = append(a.happened, event)
	}
	a.feedStale = true
}

func (a *App) hidden() map[string]bool {
	if a.unfolded {
		return nil
	}
	return a.folded
}

func (a *App) fold(folded map[string]bool, unfolded bool) {
	was := a.hidden()
	a.folded, a.unfolded = folded, unfolded
	now := a.hidden()
	var shown []string
	for name := range was {
		if !now[name] {
			shown = append(shown, name)
		}
	}
	for name := range now {
		if !was[name] {
			a.keepOnly(func(event feed.Event) bool { return !now[event.Actor] })
			break
		}
	}
	a.merge(a.readBack(shown))
}

func (a *App) keepOnly(kept func(feed.Event) bool) {
	var held []feed.Event
	turnAt := a.happenedAtTurn
	for index, event := range a.happened {
		switch {
		case kept(event):
			held = append(held, event)
		case index < a.happenedAtTurn:
			turnAt--
		}
	}
	a.reindex(held, turnAt)
}

func (a *App) merge(back []feed.Event) {
	back = slices.DeleteFunc(back, func(event feed.Event) bool { return a.happenedAt(event.ID) >= 0 })
	if len(back) == 0 {
		return
	}
	merged, live, turnAt := make([]feed.Event, 0, len(a.happened)+len(back)), a.happened, a.happenedAtTurn
	for _, event := range back {
		for len(live) > 0 && !event.At.Before(live[0].At) {
			merged, live = append(merged, live[0]), live[1:]
		}
		if len(a.happened)-len(live) <= a.happenedAtTurn {
			turnAt++
		}
		merged = append(merged, event)
	}
	a.reindex(append(merged, live...), turnAt)
}

func (a *App) reindex(events []feed.Event, turnAt int) {
	a.happened, a.happenedAtTurn, a.happenedIndex = events, turnAt, make(map[string]int, len(events))
	for index, event := range events {
		a.happenedIndex[event.ID] = index
	}
	a.feedStale = true
}

func (a *App) readBack(names []string) []feed.Event {
	if a.options.Host == nil || len(names) == 0 {
		return nil
	}
	logged, err := a.options.Host.SubAgentEvents(names)
	if err != nil {
		a.notify(readBackFailed + err.Error())
	}
	var back []feed.Event
	index := map[string]int{}
	for _, event := range logged {
		var held *feed.Event
		if found, read := index[event.ID]; read {
			held = &back[found]
		}
		var made feed.Event
		at := event.Logged.At
		switch event.Kind {
		case EventThinking:
			made = a.thinkingOf(event, at)
			made.Body = event.Text
		case EventToolCall:
			made = a.callOf(event, at)
		case EventToolResult:
			made, _, _ = a.finished(held, event, at)
		default:
			continue
		}
		if held != nil {
			*held = made
			continue
		}
		index[event.ID] = len(back)
		back = append(back, made)
	}
	return back
}

func (a *App) flushFeed() {
	if !a.feedStale || a.current != screenAgents {
		return
	}
	a.feedStale = false
	a.feed.SetEvents(a.happened)
}

func (a *App) stopWhatStillRuns() {
	for _, held := range a.happened {
		if held.State == feed.StateRunning {
			held.State = feed.StateStopped
			a.record(held)
		}
	}
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
	callsBefore := map[string][]subagent.Call{}
	for _, held := range a.allSubAgents {
		callsBefore[held.Name] = held.Calls
	}
	a.allSubAgents = subAgents
	a.refold()
	a.status.Agents = 0
	a.linkSpawns(subAgents)
	for _, subAgent := range subAgents {
		if subAgent.State == roster.Working {
			a.status.Agents++
		}
		a.stepped(subAgent)
		a.drawReport(subAgent)
		for _, call := range subAgent.Calls[settledPrefix(callsBefore[subAgent.Name], subAgent.Calls):] {
			a.rosterCall(subAgent.Name, call)
		}
	}
	if a.busy && !a.leading {
		a.view.WaitOn(a.status.Agents)
	}
}

func (a *App) refold() {
	now := a.options.Now()
	cut := now.Add(-konst.SettledSubAgentFoldMinutes * time.Minute)
	folded := map[string]bool{}
	a.foldDue = time.Time{}
	for _, row := range a.allSubAgents {
		at, settled := settledAt(row)
		switch due := now.Add(at.Sub(cut)); {
		case !settled:
		case at.Before(cut):
			folded[row.Name] = true
		case a.foldDue.IsZero() || due.Before(a.foldDue):
			a.foldDue = due
		}
	}
	if !maps.Equal(folded, a.folded) {
		a.fold(folded, a.unfolded)
	}
	a.showUnfolded()
}

func (a *App) foldLater() tea.Cmd {
	if a.foldDue.IsZero() || !a.foldArmed.IsZero() && !a.foldDue.Before(a.foldArmed) {
		return nil
	}
	a.foldArmed = a.foldDue
	return tea.Tick(a.foldDue.Sub(a.options.Now()), func(time.Time) tea.Msg { return foldDueMsg{} })
}

func (a *App) toggleFold() {
	a.fold(a.folded, !a.unfolded)
	a.showUnfolded()
}

func (a *App) unfoldFor(actor string) {
	if a.hidden()[actor] {
		a.toggleFold()
		a.syncFeed()
	}
}

func (a *App) showUnfolded() {
	hidden := a.hidden()
	a.subAgents = slices.DeleteFunc(slices.Clone(a.allSubAgents), func(row subagent.Row) bool { return hidden[row.Name] })
	a.view.SubAgents, a.edits.SubAgents = a.subAgents, a.subAgents
}

func (a *App) ageWorking() {
	now := a.options.Now()
	for index := range a.subAgents {
		if row := &a.subAgents[index]; row.State == roster.Working && !row.Started.IsZero() {
			row.Since = max(row.Since, now.Sub(row.Started))
		}
	}
}

func settledAt(row subagent.Row) (time.Time, bool) {
	switch row.State {
	case roster.Working, roster.WaitingAnswer, roster.InReview, roster.Reopened:
		return time.Time{}, false
	case roster.Parked, roster.Errored, roster.Finished:
		at := row.Started.Add(row.Since)
		if row.Ended.After(at) {
			at = row.Ended
		}
		return at, !row.Started.IsZero()
	}
	panic("tui: unknown sub-agent state " + row.State.String())
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
	id := a.shortOf(call.ID)
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
	a.record(feed.Event{ID: id, FullID: call.ID, Actor: actor, Kind: feed.KindTool, State: state, Title: call.Tool, Body: call.Text, Detail: lines(call.Result), At: cmp.Or(call.At, a.options.Now())})
}

func settledPrefix(before, now []subagent.Call) int {
	settled := 0
	for settled < min(len(before), len(now)) && before[settled].ID == now[settled].ID && before[settled].Result != "" {
		settled++
	}
	return settled
}

func (a *App) spawnedAs(spawn *feed.Event, subAgent subagent.Row) {
	spawn.Target, spawn.Title = subAgent.Name, subAgent.Doing
	if spawn.Actor == orchestrator {
		a.view.Spawned(subAgent.Name)
		a.reports[subAgent.Name] = ""
	}
}

func (a *App) linkSpawns(subAgents []subagent.Row) {
	turn := a.happened[min(a.happenedAtTurn, len(a.happened)):]
	unlinked := func(held feed.Event) bool {
		return held.Kind == feed.KindSpawn && held.State != feed.StateFailed && held.State != feed.StateStopped && held.Target == ""
	}
	if !slices.ContainsFunc(turn, unlinked) {
		return
	}
	linked := map[string]bool{}
	for _, held := range a.happened {
		if held.Kind == feed.KindSpawn {
			linked[held.Target] = true
		}
	}
	for _, subAgent := range subAgents {
		startedBeforeThisTurn := a.busy && !subAgent.Started.IsZero() && subAgent.Started.Before(turn[0].At)
		if linked[subAgent.Name] || startedBeforeThisTurn {
			continue
		}
		at := slices.IndexFunc(turn, unlinked)
		if at < 0 {
			return
		}
		spawn := turn[at]
		a.spawnedAs(&spawn, subAgent)
		a.record(spawn)
		linked[subAgent.Name] = true
	}
}

func (a *App) parkSubAgentsTheTurnLeftBehind() {
	parked := slices.Clone(a.allSubAgents)
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
