package host

import (
	"bufio"
	"bytes"
	"cmp"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"slices"
	"strconv"
	"strings"
	"sync"
	"sync/atomic"
	"time"

	"tofu/internal/agent/cron"
	"tofu/internal/agent/loop"
	roster "tofu/internal/agent/subagents"
	"tofu/internal/judge/ledger"
	"tofu/internal/konst"
	"tofu/internal/models/classifiers/core"
	llm "tofu/internal/models/llm/core"
	"tofu/internal/platform/fs"
	"tofu/internal/store/session"
	"tofu/internal/store/settings"
	"tofu/internal/tools/contract"
	"tofu/internal/tools/human/plan"
	"tofu/internal/tools/human/remember"
	humansettings "tofu/internal/tools/human/settings"
)

const (
	processDir    = "."
	standingPoint = "standing"
	cancelledAt   = "cancelled at"
	hookTrustTool = "hooks"
	resumedTask   = "continue"
)

type Engine interface {
	Prepare(start Turn, hooks Hooks) (Prepared, error)
	Renew()
	OneTurnPerProject() bool
}

type Turn struct {
	ID      string
	Session string
	Task    string
	Pick    Pick
	Images  []llm.Image
}

type Hooks struct {
	Lead     func(loop.Model) loop.Model
	SubAgent func(loop.Model) loop.Model
	Say      func(string)
	Now      func() time.Time
	Person   loop.Person
	Gate     func(ctx context.Context, tool string, gated loop.GateDecision, err error)
	Roster   *roster.Roster
	Inbox    *loop.Inbox
	Reads    *contract.ReadLedger
	Cron     *cron.Book
}

type Prepared struct {
	Config    loop.Config
	Spawner   *loop.SpawnTool
	Plan      *plan.Plan
	Ceiling   int
	MaxSteps  int
	Decisions func() int
	Sessions  *session.Store
	GateOff   error
	Close     func()
	Account   func(id int64) TurnAccount
}

type limitReached struct {
	*loop.RateLimited
	source string
}

func (h *Host) run(ctx context.Context, pick Pick, task string, live Live) {
	h.runFrom(ctx, pick, task, live, false)
}

func (h *Host) runFrom(ctx context.Context, pick Pick, task string, live Live, resumed bool) {
	reached := h.lead(ctx, pick, task, live, resumed)
	if reached.RateLimited == nil {
		return
	}
	if task, origin, resumes := h.waitForTheReset(ctx, reached, live); resumes {
		live.Origin = origin
		h.runFrom(ctx, pick, task, live, true)
	}
}

func (h *Host) heardTyped(emit func(Event), typed string) {
	emit(h.withImages(Event{Kind: EventSteered, ID: h.steering.heard(), Text: typed, Step: 1, Origin: wireOrigin(llm.Origin{Source: llm.SourceTyped})}))
}

func leadUserMessage(logged session.Event) (session.MessageBody, bool) {
	var said session.MessageBody
	return said, logged.Agent == "" && logged.Kind == session.EventMessage && json.Unmarshal(logged.Body, &said) == nil && said.Role == session.RoleUser
}

type takenTask struct {
	at   time.Time
	text string
}

func enteredUnshown(said session.MessageBody, origin llm.Origin) (Event, bool) {
	switch origin.Source {
	case llm.SourceTyped, llm.SourceSteer, llm.SourceEnvironment, llm.SourceSpawnLine:
		return Event{}, false
	}
	task, _ := loop.TaskIn(said.Content)
	return Event{Kind: EventTask, Text: task, Origin: wireOrigin(origin)}, true
}

func (h *Host) lead(ctx context.Context, pick Pick, task string, live Live, resumed bool) limitReached {
	emit := live.Emit
	fail := func(err error) { emit(Event{Kind: EventFailure, Text: err.Error()}) }
	var started atomic.Bool
	say := func(text string) {
		if started.Load() || !resumed {
			emit(Event{Kind: EventNote, Text: text})
		}
	}
	id := h.pendingID()
	releaseTurn, err := h.holdTurn(id)
	if err != nil {
		fail(err)
		return limitReached{}
	}
	defer func() { _ = releaseTurn() }()
	task += h.takeRan()
	imagesOf := func(said string) []llm.Image {
		attached, err := h.takePendingImages(said)
		if err != nil {
			say("an image in your message did not read, so the model does not see it: " + err.Error())
		}
		return attached
	}
	images := imagesOf(task)
	watch := &watcher{held: h.roster, spent: h.spent, emit: emit, now: h.now, turnID: live.Turn, seen: h.shown, stop: &leadStop{}}
	var prepared Prepared
	person, asking := awaitPerson(emit, h.asks, &prepared.Config), loop.WithQuestionsBlock(loop.WithShellRegistry(remember.WithMemoryKept(context.WithValue(ctx, cronFiredKey{}, h.cronFired), keptMemory(emit)), h.shells), h.questionsBlock)
	fired, err := h.cron.Job(live.Origin.ID)
	switch {
	case live.Unattended && err == nil && fired.Spec().Away:
		person, asking = unattended, loop.WithPersonAway(asking)
	case live.Unattended:
		person = unattended
	default:
		asking = loop.WithPersonForm(remember.WithMemoryPick(asking, pickMemory(emit, h.memoryPicks)), askForm(emit, h.questions))
	}
	prepared, err = h.engine.Prepare(Turn{ID: watch.turnID, Session: id, Task: task, Pick: pick, Images: images}, Hooks{
		Lead:     func(model loop.Model) loop.Model { watch.inner = model; return watch },
		SubAgent: func(model loop.Model) loop.Model { return watchedSubAgent{watch: watch, inner: model} },
		Say:      say,
		Now:      h.now,
		Person:   person,
		Gate: func(ctx context.Context, tool string, gated loop.GateDecision, err error) {
			asker := contract.SubAgentAsking(ctx)
			decided := Decision{Tool: tool, Verdict: Ask}
			switch {
			case err != nil:
				decided.Failure = err.Error()
			case gated.Verdict != ledger.VerdictUnset:
				decided = gateDecision(tool, gated)
				decided.Enforced = prepared.Config.GateMode == loop.GateEnforce
			default:
				return
			}
			if call, judging := ctx.Value(judgingCallKey{}).(string); judging {
				decided.Call = watch.eventID(asker, call)
			}
			emit(Event{Kind: EventDecision, ID: gated.ID, Decision: &decided, Agent: asker, Promote: watch.spawning(tool)})
		},
		Roster: h.roster,
		Inbox:  h.inbox,
		Reads:  h.reads,
		Cron:   h.cron,
	})
	if prepared.Close != nil {
		defer prepared.Close()
	}
	if err != nil {
		fail(err)
		return limitReached{}
	}
	started.Store(true)
	sessions, config := prepared.Sessions, prepared.Config
	if err := h.cron.Keep(cronFile(sessions, id)); err != nil {
		say("cron jobs were not written: " + err.Error())
	}
	if labelled, named := h.label(sessions, h.ID()); named {
		emit(labelled)
	}
	if prepared.GateOff != nil && !resumed {
		emit(gateOffEvent(prepared.GateOff))
	}
	watch.spawner, watch.maxSteps, watch.decisions = prepared.Spawner, prepared.MaxSteps, prepared.Decisions
	watch.runs = func() map[string]session.AgentRun { return recordedRuns(sessions, h.ID()) }
	h.mu.Lock()
	config.SessionSource, h.started = h.started, ""
	if h.carriedOn != "" && h.carriedOn != config.Wire {
		h.carried = loop.WithoutThinking(h.carried)
	}
	h.carriedOn = config.Wire
	config.History, config.Prefix = h.carried, h.prefix
	config.TaskOrigin = live.Origin
	h.mu.Unlock()
	var steps atomic.Int64
	readSteer := func(event Event) { emit(h.withImages(event)) }
	config.Steering = func() []string { return h.steering.take(readSteer, int(steps.Load())+1) }
	config.ImagesOf = imagesOf
	config.ToolResult = func(answered llm.Message) { watch.result(answered, "") }
	movedTo, ceiling := "", prepared.Ceiling
	spentOn := func(id int64) TurnAccount {
		spent := TurnAccount{AccountID: id}
		if prepared.Account != nil && id != 0 {
			spent = prepared.Account(id)
		}
		if movedTo != "" {
			spent.Source, spent.Model, _ = strings.Cut(movedTo, "/")
		}
		return spent
	}
	config.AccountTaken = func(taken loop.AccountTaken) {
		spent := spentOn(taken.ID)
		spent.Reason, spent.FromAccount = AccountReason(taken.Reason), taken.From
		emit(Event{Kind: EventAccount, Account: &spent})
	}
	config.Accounts = announcingMoves(config.Accounts, func(moved loop.Moved) {
		movedTo, ceiling = moved.To, cmp.Or(moved.Budget.CeilingTokens, ceiling)
		spent := spentOn(moved.Account.ID)
		spent.Reason = AccountReason(loop.AccountMoved)
		emit(Event{Kind: EventAccount, Account: &spent})
	})
	config.Accounts = keepingHeld(config.Accounts, heldAccount(sessions, id, h.now()), func(why string) { emit(Event{Kind: EventNote, Text: why}) })
	announce := func(ended loop.Row, agent, into string) {
		forked := forkOf(ended, into)
		emit(Event{Kind: EventForkStart, Agent: agent})
		emit(Event{Kind: EventNote, Text: forkWords(forked), Agent: agent})
		emit(Event{Kind: EventForkEnd, Fork: &forked, Agent: agent})
	}
	var forking sync.Mutex
	var awaitingSession *loop.Row
	announceIntoSession := func(ended loop.Row, into string) {
		kept := cronFile(sessions, into)
		if err := h.cron.Keep(kept); err != nil {
			say("cron jobs were not written to " + kept + ": " + err.Error())
		}
		announce(ended, "", into)
		emit(labelledAs(sessions, Event{Kind: EventSession, ID: into, Root: into}))
	}
	announcedFromHeader := func(ended loop.Row) bool {
		header, err := sessions.Header(ended.Session)
		if err != nil || header.ForkedInto == "" {
			return false
		}
		announceIntoSession(ended, header.ForkedInto)
		return true
	}
	announceAwaitedIn := func(reached string) {
		if awaitingSession == nil {
			return
		}
		ended := *awaitingSession
		awaitingSession = nil
		if reached == ended.Session {
			announce(ended, "", ended.ForkedInto)
			return
		}
		announceIntoSession(ended, reached)
	}
	var opened atomic.Bool
	opened.Store(resumed)
	heldByFork := map[takenTask]bool{}
	config.Appended = func(logged session.Event) {
		forking.Lock()
		defer forking.Unlock()
		if logged.Kind == session.EventTurnStart && logged.Agent == "" {
			watch.turnRecorded(logged.Turn)
		}
		if said, user := leadUserMessage(logged); user {
			origin := loop.OriginOf(said)
			if endsTheRow(origin.Source) {
				watch.endLeadRow()
			}
			entered, unshown := enteredUnshown(said, origin)
			if unshown && said.TakenAt != nil {
				taken := takenTask{at: *said.TakenAt, text: entered.Text}
				unshown = !heldByFork[taken]
				heldByFork[taken] = true
			}
			if unshown && (entered.Origin.Sub != live.Origin.Source || opened.Swap(true)) {
				emit(entered)
			}
		}
		if logged.Kind != session.EventSent {
			emit(Event{Kind: EventPersisted, ID: logged.ID, Agent: logged.Agent, Logged: &logged})
		}
		if awaitingSession != nil && logged.Kind == session.EventTurnStart && logged.Turn == awaitingSession.ForkedInto && announcedFromHeader(*awaitingSession) {
			awaitingSession = nil
		}
	}
	config.Sent = func(sent session.Event) {
		emit(Event{Kind: EventRequestSent, ID: sent.ID, Agent: sent.Agent, Logged: &sent})
	}
	if prepared.Spawner != nil {
		prepared.Spawner.Announce(config.Sent, func(agent string, step loop.StepRow) {
			if step.Occupancy != nil {
				emit(Event{Kind: EventContext, Agent: agent, Context: Context{Used: step.Occupancy.Total(), Budget: ceiling}})
			}
		})
	}
	config.Step = func(step loop.StepRow) {
		steps.Store(int64(step.Index))
		if prepared.Plan != nil {
			emit(Event{Kind: EventPlan, Plan: statedPlan(prepared.Plan.Items())})
		}
		if step.Occupancy != nil {
			emit(Event{Kind: EventContext, Context: Context{Used: step.Occupancy.Total(), Budget: ceiling}})
		}
	}
	config.EndedSession = func(ended loop.Row) error {
		if ended.SpawnedFrom != "" {
			announce(ended, ended.Root, ended.ForkedInto)
			return nil
		}
		if ended.Session == "" {
			announce(ended, "", ended.ForkedInto)
			return nil
		}
		forking.Lock()
		defer forking.Unlock()
		announceAwaitedIn(ended.Session)
		if !announcedFromHeader(ended) {
			awaitingSession = &ended
		}
		return nil
	}
	stopClocks := watch.clockRunningSubAgents(h.report)
	stopListening := watch.stop.listen(h.report, live.LeadStop, h.sendNow)
	heard := func(typed string) { h.heardTyped(emit, typed) }
	var reported []error
	var reached limitReached
	if scratched, err := loop.WithLeadScratch(asking, config); err == nil {
		asking = scratched
	}
	leadErr := loop.Lead(asking, config, live.Steering, heard, func(row loop.Row, err error) {
		forking.Lock()
		announceAwaitedIn(row.Session)
		forking.Unlock()
		watch.stop.reset()
		steps.Store(0)
		if err != nil {
			reported = append(reported, err)
		}
		reached = limitReached{}
		words, status := doneWords(row.Outcome, row.Guard)
		switch {
		case errors.Is(err, context.Canceled):
			words, status = cancelledAt, StatusStopped
		case errors.As(err, &reached.RateLimited):
			words, status = "stopped at the rate limit after", StatusStopped
			reached.source = pick.Wire
			if prepared.Account != nil {
				reached.source = cmp.Or(prepared.Account(row.Account).Source, pick.Wire)
			}
		case err != nil:
			status = StatusFailed
			fail(err)
		}
		h.mu.Lock()
		if row.Conversation != nil {
			h.carried = loop.Sendable(row.Conversation)
		}
		forked := row.Session != "" && row.Session != h.id
		if row.Session != "" {
			h.id = row.Session
		}
		h.mu.Unlock()
		if row.Session != "" {
			if headErr := sessions.SetHead(row.Session); headErr != nil {
				fail(headErr)
			}
			if keepErr := h.cron.Keep(cronFile(sessions, row.Session)); keepErr != nil && forked {
				say("cron jobs were not written: " + keepErr.Error())
			}
			if labelled, named := h.label(sessions, h.ID()); named {
				emit(labelled)
			}
		}
		watch.readCalls()
		if said, ended := WordsAfterLastCalls(row); ended && strings.TrimSpace(row.Steps[len(row.Steps)-1].AssistantText) == "" {
			emit(Event{Kind: EventText, Text: said})
		}
		done := Event{Kind: EventDone, Text: words, Status: status}
		if ctx.Err() == nil {
			done.SubAgents = watch.subAgents()
		}
		emit(done)
	})
	stopClocks()
	stopListening()
	watch.sendSubAgents()
	if unseen := unreported(leadErr, reported); unseen != nil {
		fail(unseen)
	}
	h.mu.Lock()
	err = h.hold(h.id)
	h.mu.Unlock()
	if err != nil {
		fail(err)
	}
	if ctx.Err() != nil {
		return limitReached{}
	}
	return reached
}

func heldAccount(sessions *session.Store, id string, now time.Time) loop.Held {
	if held, found := heldIn(sessions.EventsPath(id), now); found {
		return held
	}
	header, err := sessions.Header(id)
	if err != nil || header.CarriedFrom == nil || !slices.Contains([]loop.ForkKind{loop.ForkCompact, loop.ForkByHand, loop.ForkBranch}, loop.ForkKind(header.ForkKind)) {
		return loop.Held{}
	}
	return heldAccount(sessions, header.CarriedFrom.Session, now)
}

func heldIn(events string, now time.Time) (loop.Held, bool) {
	file, err := os.Open(events)
	if err != nil {
		return loop.Held{}, false
	}
	defer func() { _ = file.Close() }()
	info, err := file.Stat()
	if err != nil {
		return loop.Held{}, false
	}
	for window := int64(bufio.MaxScanTokenSize); ; window *= 2 {
		start := max(info.Size()-window, 0)
		tail := make([]byte, info.Size()-start)
		if read, _ := file.ReadAt(tail, start); read < len(tail) {
			return loop.Held{}, false
		}
		if held, found := lastHeld(tail, start == 0, now); found || start == 0 {
			return held, found
		}
	}
}

func lastHeld(tail []byte, whole bool, now time.Time) (loop.Held, bool) {
	lines := bytes.Split(tail, []byte{'\n'})
	if !whole {
		lines = lines[1:]
	}
	turnEnd, request := []byte(`"kind":"`+session.EventTurnEnd+`"`), []byte(`"kind":"`+session.EventRequest+`"`)
	var held loop.Held
	var ended bool
	var asked time.Time
	for at := len(lines) - 1; at >= 0 && (!ended || asked.IsZero()); at-- {
		if !bytes.Contains(lines[at], turnEnd) && !bytes.Contains(lines[at], request) {
			continue
		}
		var event struct {
			At    time.Time
			Agent string
			Kind  session.EventKind
			Body  struct {
				Account int64 `json:"account"`
			}
		}
		if json.Unmarshal(lines[at], &event) != nil || event.Agent != "" {
			continue
		}
		switch {
		case event.Kind == session.EventTurnEnd && !ended:
			held.ID, ended = event.Body.Account, true
		case event.Kind == session.EventRequest && asked.IsZero():
			asked = event.At
		}
	}
	if asked.IsZero() {
		return loop.Held{}, false
	}
	held.Idle = now.Sub(asked)
	return held, ended
}

func keepingHeld(accounts loop.Accounts, held loop.Held, note func(string)) loop.Accounts {
	pick, keep := accounts.Pick, accounts.Keep
	if pick == nil || keep == nil || held.ID == 0 {
		return accounts
	}
	accounts.Pick = func(ctx context.Context) (loop.Account, error) {
		kept, leftBecause, err := keep(ctx, held)
		if err != nil || kept.ID != 0 {
			return kept, err
		}
		picked, err := pick(ctx)
		if err == nil && picked.ID != held.ID {
			note(leftBecause)
		}
		return picked, err
	}
	return accounts
}

func announcingMoves(accounts loop.Accounts, announce func(loop.Moved)) loop.Accounts {
	elsewhere := accounts.Elsewhere
	if elsewhere == nil {
		return accounts
	}
	accounts.Elsewhere = func(ctx context.Context, left []string, conversationTokens int) (loop.Moved, error) {
		moved, err := elsewhere(ctx, left, conversationTokens)
		if err == nil {
			moved.Accounts = announcingMoves(moved.Accounts, announce)
			announce(moved)
		}
		return moved, err
	}
	return accounts
}

func (h *Host) waitForTheReset(ctx context.Context, reached limitReached, live Live) (string, llm.Origin, bool) {
	resumesAt := reached.ResetAt.Local().Format(time.DateTime) + ", in " + reached.ResetAt.Sub(h.now()).Round(time.Second).String()
	said, _, _ := strings.Cut(reached.Error(), "\n")
	live.Emit(Event{Kind: EventNote, Text: said + "\nevery " + reached.source + " account is at its limit, so the turn resumes by itself at " + resumesAt +
		": type to go on now, or press esc to cancel"})
	timer := time.NewTimer(reached.ResetAt.Sub(h.now()))
	defer timer.Stop()
	select {
	case <-timer.C:
		live.Emit(Event{Kind: EventNote, Text: "the " + reached.source + " limit has reset, so the turn resumes"})
		return resumedTask, llm.Origin{Source: llm.SourceContinue}, true
	case typed := <-live.Steering:
		h.heardTyped(live.Emit, typed)
		return typed, llm.Origin{Source: llm.SourceTyped}, true
	case <-ctx.Done():
		live.Emit(Event{Kind: EventNote, Text: "the wait for the " + reached.source + " limit was cancelled, and the turn does not resume"})
		return "", llm.Origin{}, false
	}
}

type judgingCallKey struct{}

func JudgingCall(ctx context.Context, call string) context.Context {
	return context.WithValue(ctx, judgingCallKey{}, call)
}

func (h *Host) holdTurn(id string) (func() error, error) {
	store, err := session.OpenIn(h.dir)
	if err != nil {
		return nil, err
	}
	h.mu.Lock()
	err = h.hold(id)
	h.mu.Unlock()
	free := func() error { return nil }
	if err != nil {
		return free, err
	}
	side, found, err := store.Side(id)
	switch {
	case err != nil:
		return free, err
	case found && len(side.Owns) == 0:
		return free, nil
	case found:
		return store.Claim(side)
	case !h.engine.OneTurnPerProject():
		return free, nil
	case h.turnLock == nil:
		return store.HoldTurn()
	case !h.turnLock.TryLock():
		return free, errProjectTurn
	}
	release, err := store.HoldTurn()
	if err != nil {
		h.turnLock.Unlock()
		return free, err
	}
	return func() error {
		defer h.turnLock.Unlock()
		return release()
	}, nil
}

func (h *Host) label(store *session.Store, id string) (Event, bool) {
	if id == "" {
		return Event{}, false
	}
	_ = h.recordHeader(store, id)
	return labelledAs(store, Event{Kind: EventSession, ID: id, Root: id}), true
}

func (h *Host) recordHeader(store *session.Store, id string) error {
	if _, err := store.Header(id); err == nil {
		return nil
	}
	return store.Write(session.Header{ID: id, Root: id, At: h.now()}, nil)
}

func labelledAs(store *session.Store, labelled Event) Event {
	identity, err := store.Identity(labelled.ID)
	if err != nil {
		return labelled
	}
	labelled.Root, labelled.Text, labelled.Identity, labelled.LastAt = identity.Family, identity.Handle(), &identity, LastAt(store, labelled.ID)
	return labelled
}

func LastAt(store *session.Store, id string) time.Time {
	recorded, err := os.Stat(store.EventsPath(id))
	if err != nil {
		return time.Time{}
	}
	return recorded.ModTime()
}

func gateOffEvent(gateErr error) Event {
	var missing core.MissingKey
	errors.As(gateErr, &missing)
	return Event{Kind: EventGateOff, Text: gateErr.Error(), GateWhy: missing.Why}
}

type cronFiredKey struct{}

func (h *Host) cronFired() bool {
	h.mu.Lock()
	defer h.mu.Unlock()
	return len(h.unread) > 0
}

func awaitPerson(emit func(Event), book *asks, config *loop.Config) loop.Person {
	return func(ctx context.Context, request loop.GateRequest, decision loop.GateDecision) (loop.PersonAnswer, error) {
		places, _ := config.ResolvedPlaces()
		place := askedPlace(request)
		var overriding struct{ Rule, Question string }
		if request.Tool == (humansettings.RuleOverride{}).Name() {
			_ = json.Unmarshal(request.Args, &overriding)
		}
		standsAt, accepts := place, []ApprovalDecision{AllowOnce, AllowFor, AllowAlways, RejectOnce, RejectAlways, Cancelled}
		if overriding.Question != "" || request.Tool == hookTrustTool {
			standsAt, accepts = "", []ApprovalDecision{AllowOnce, AllowAlways, RejectOnce, Cancelled}
		}
		stood, stands := book.stood(standsAt)
		id := cmp.Or(decision.ID, session.NewEventID())
		switch {
		case overriding.Question != "":
			emit(Event{Kind: EventNote, Text: overriding.Question})
			emit(Event{Kind: EventDecision, ID: id, Decision: &Decision{Tool: request.Tool, Verdict: Ask, OverridesRule: overriding.Rule}})
		case stands && request.Code != nil:
			loop.CodeEntry{Turn: request.TurnID, Code: *request.Code, By: "the person, standing", Answer: codeAnswer(stood)}.Record(places.Log)
			return personAnswer(stood), nil
		case stands:
			recordStanding(places.Log, request, place, stood)
			return personAnswer(stood), nil
		case loop.StandingOnly(ctx):
			return loop.PersonDenied, loop.NotStanding{}
		}
		asked := Event{Kind: EventAwaitPerson, ID: id, Tool: request.Tool, Text: place, Args: request.Args, Agent: contract.SubAgentAsking(ctx), Accepts: accepts,
			Reason: agentReason(request.Reason), Risk: riskOf(decision)}
		if request.Code != nil {
			emit(Event{Kind: EventNote, Text: asked.Agent + " asks you to run this inline code:\n" + loop.CodeShown(*request.Code)})
		}
		if decision.Verdict != ledger.VerdictUnset {
			judged := gateDecision(request.Tool, decision)
			asked.Decision = &judged
		}
		unanswered := unansweredIn(config.Project)
		asked.Wait = unanswered.waited
		reply, forget := book.wait(asked.ID, standsAt)
		defer forget()
		if fired, held := ctx.Value(cronFiredKey{}).(func() bool); held && fired() {
			unanswered.cronFired = true
			return unanswered.decide(emit, request, places.Log)
		}
		emit(asked)
		resumed := Event{Kind: EventResumed, ID: asked.ID, Agent: asked.Agent, Resolution: cancelledByTofu()}
		defer func() { emit(resumed) }()
		emit(Event{Kind: EventNote, Text: "if nobody answers by " + time.Now().Add(unanswered.waited).Format(time.TimeOnly) + ", in " + unanswered.waited.String() +
			", " + settings.GateAskTimeout + " " + unanswered.outcome + " decides the ask about " + place})
		timer := time.NewTimer(unanswered.waited)
		defer timer.Stop()
		started := time.Now()
		defer func() { loop.PersonWaited(ctx, time.Since(started)) }()
		select {
		case <-timer.C:
			resumed.Resolution.Decision = unanswered.applied()
			return unanswered.decide(emit, request, places.Log)
		case answered := <-reply:
			if answered == unattendedNow {
				unanswered.cronFired = true
				resumed.Resolution.Decision = unanswered.applied()
				return unanswered.decide(emit, request, places.Log)
			}
			resumed.Resolution = nil
			if request.Code != nil {
				loop.CodeEntry{Turn: request.TurnID, Code: *request.Code, By: "the person", Answer: codeAnswer(answered), Took: time.Since(started)}.Record(places.Log)
			}
			out := personAnswer(answered)
			recordPersonAnswer(places.Log, decision.ID, out)
			return out, nil
		case <-ctx.Done():
			return loop.PersonDenied, ctx.Err()
		}
	}
}

const unattendedNow Answer = -1

func agentReason(said string) string {
	runes := []rune(strings.Join(strings.Fields(said), " "))
	if len(runes) <= konst.ApprovalReasonRunes {
		return string(runes)
	}
	return string(runes[:konst.ApprovalReasonRunes-1]) + "…"
}

func riskOf(decision loop.GateDecision) Risk {
	switch {
	case decision.PersonOnly, decision.Verdict == ledger.VerdictDeny, decision.Reason != nil && decision.Reason.Blocked:
		return RiskDestructive
	case decision.Failure != "", decision.Reason == nil:
		return RiskUnjudged
	case decision.Verdict == ledger.VerdictAsk:
		return RiskRisky
	case decision.Verdict == ledger.VerdictAllow:
		return RiskLow
	}
	return RiskUnjudged
}

type unansweredAsk struct {
	waited    time.Duration
	outcome   string
	cronFired bool
}

func unansweredIn(project string) unansweredAsk {
	home, _ := fs.HomeConfigDir()
	store, err := settings.Open(filepath.Join(home, settings.FileName), filepath.Join(fs.StateDir(project), settings.FileName))
	if err != nil {
		return unansweredAsk{waited: time.Duration(settings.DeclaredDefault(settings.GateAskWaitSeconds)) * time.Second, outcome: settings.GateAskRefuse}
	}
	outcome := settings.GateAskRefuse
	if store.Text(settings.GateAskTimeout) == settings.GateAskAllow {
		outcome = settings.GateAskAllow
	}
	return unansweredAsk{waited: time.Duration(store.Int(settings.GateAskWaitSeconds)) * time.Second, outcome: outcome}
}

func (u unansweredAsk) why() string {
	if u.cronFired {
		return "a cron job fired while it waited, so nobody is there to answer, and " + settings.GateAskTimeout + " is " + u.outcome
	}
	return "nobody answered in " + u.waited.String() + ", and " + settings.GateAskTimeout + " is " + u.outcome
}

func (u unansweredAsk) Error() string { return u.why() + ", so the call did not run" }

func (u unansweredAsk) applied() ApprovalDecision {
	if u.outcome == settings.GateAskAllow {
		return AllowOnce
	}
	return RejectOnce
}

func (u unansweredAsk) decide(emit func(Event), request loop.GateRequest, log string) (loop.PersonAnswer, error) {
	answer, code, err, said := loop.PersonDenied, loop.CodeDeny, error(u), u.Error()
	if u.outcome == settings.GateAskAllow {
		answer, code, err, said = loop.PersonAllowedOnce, loop.CodeAllowOnce, nil, u.why()+", so the call runs"
	}
	if request.Code != nil {
		loop.CodeEntry{Turn: request.TurnID, Code: *request.Code, By: "nobody, " + settings.GateAskTimeout + " " + u.outcome, Answer: code}.Record(log)
	}
	emit(Event{Kind: EventNote, Text: "the ask about " + askedPlace(request) + ": " + said})
	return answer, err
}

func personAnswer(answered Answer) loop.PersonAnswer {
	switch answered {
	case AlwaysHere:
		return loop.PersonAlwaysHere
	case AllowedOnce, AllowedAWhile:
		return loop.PersonAllowedOnce
	case Denied, NeverHere:
		return loop.PersonDenied
	}
	panic("host: unknown answer from the person")
}

func codeAnswer(answered Answer) loop.CodeAnswer {
	switch answered {
	case AlwaysHere:
		return loop.CodeAllowAlways
	case AllowedOnce, AllowedAWhile:
		return loop.CodeAllowOnce
	case Denied:
		return loop.CodeDeny
	case NeverHere:
		return loop.CodeDenyAlways
	}
	panic("host: unknown answer from the person")
}

func recordPersonAnswer(log, id string, answer loop.PersonAnswer) {
	if log == "" || id == "" {
		return
	}
	_ = ledger.NewWriter(log).Backfill(id, answer.Outcome())
}

func recordStanding(log string, request loop.GateRequest, place string, stood Answer) {
	if log == "" {
		return
	}
	body, err := ledger.Canonical(map[string]string{"tool": request.Tool, "target": place})
	if err != nil {
		return
	}
	verdict := ledger.VerdictDeny
	if stood == AlwaysHere || stood == AllowedAWhile {
		verdict = ledger.VerdictAllow
	}
	_, _ = ledger.NewWriter(log).Append(ledger.Row{Point: standingPoint, StateHash: ledger.HashOf(body), State: body, Verdict: verdict, TurnID: request.TurnID})
}

type noPerson struct{}

func (noPerson) Error() string {
	return "no person: a cron job started this turn and nobody is there to answer, so a call that would ask is refused"
}

func unattended(context.Context, loop.GateRequest, loop.GateDecision) (loop.PersonAnswer, error) {
	return loop.PersonDenied, noPerson{}
}

func askedPlace(request loop.GateRequest) string {
	return loop.CallPlace(request, processDir)
}

func unreported(err error, reported []error) error {
	parts := []error{err}
	if joined, many := err.(interface{ Unwrap() []error }); many {
		parts = joined.Unwrap()
	}
	unseen := slices.DeleteFunc(slices.Clone(parts), func(part error) bool {
		return slices.ContainsFunc(reported, func(seen error) bool { return errors.Is(part, seen) })
	})
	return errors.Join(unseen...)
}

func forkOf(ended loop.Row, into string) Fork {
	forked := Fork{From: ended.Session, To: into}
	if fork := ended.EndedInFork; fork != nil {
		forked.Kind, forked.Before, forked.After = string(fork.Kind), fork.TokensBefore, fork.TokensAfter
	}
	return forked
}

func forkWords(forked Fork) string {
	if forked.Kind == "" {
		return "forked into " + forked.To + ", and no step in this session recorded the counts"
	}
	return fmt.Sprintf("forked into %s as a %s at %d tokens, which began at %d", forked.To, forked.Kind, forked.Before, forked.After)
}

func WordsAfterLastCalls(row loop.Row) (string, bool) {
	if len(row.Steps) == 0 || len(row.Steps[len(row.Steps)-1].ToolCalls) == 0 || len(row.Conversation) == 0 {
		return "", false
	}
	if last := row.Conversation[len(row.Conversation)-1]; last.Role == llm.RoleAssistant {
		return last.Content, strings.TrimSpace(last.Content) != ""
	}
	step := row.Steps[len(row.Steps)-1]
	onlySpawned := !slices.ContainsFunc(step.ToolCalls, func(call loop.ToolCallRow) bool { return call.SubAgentID == "" })
	return step.AssistantText, onlySpawned && strings.TrimSpace(step.AssistantText) != ""
}

func doneWords(outcome loop.Outcome, guard *loop.LoopGuardStop) (string, Status) {
	switch outcome {
	case loop.OutcomeUnset, loop.OutcomeForked:
		return "finished in", StatusFinished
	case loop.OutcomeStopped:
		return "cooked for", StatusFinished
	case loop.OutcomeStepCap:
		return "stopped at the step cap after", StatusStopped
	case loop.OutcomeDecisionCap:
		return "stopped at the decision cap after", StatusStopped
	case loop.OutcomeTruncated:
		return "stopped on a reply it could not finish, after", StatusStopped
	case loop.OutcomeError:
		return "failed after", StatusFailed
	case loop.OutcomeCancelled:
		return cancelledAt, StatusStopped
	case loop.OutcomeRetiredCostCap:
		return "stopped at a cap this build no longer sets, after", StatusStopped
	case loop.OutcomeRetiredWallClockCap:
		return "stopped at the wall clock cap after", StatusStopped
	case loop.OutcomeLoopGuard:
		return loopGuardWords(guard) + ", after", StatusStopped
	case loop.OutcomeForkStop:
		return "stopped forking after", StatusStopped
	}
	panic("host: unknown outcome " + outcome.String())
}

func loopGuardWords(guard *loop.LoopGuardStop) string {
	if guard == nil {
		return "stopped itself after repeating a tool call"
	}
	return "stopped itself after calling " + strconv.Quote(guard.Tool) + " with " + string(guard.Args) +
		" and getting the same result " + strconv.Itoa(guard.Repeats) + " times in a row"
}

func statedPlan(items []plan.PlanItem) []PlanItem {
	drawn := make([]PlanItem, 0, len(items))
	for _, item := range items {
		drawn = append(drawn, PlanItem{Phase: item.Phase, Text: item.Text, State: drawnPlanState(item.State), Check: item.Check, Call: item.Call})
	}
	return drawn
}

func drawnPlanState(state plan.PlanState) PlanState {
	switch state {
	case plan.PlanPending:
		return PlanPending
	case plan.PlanRunning:
		return PlanRunning
	case plan.PlanDone:
		return PlanDone
	case plan.PlanDropped:
		return PlanDropped
	}
	panic("host: unknown plan item state " + string(state))
}

func gateDecision(tool string, gated loop.GateDecision) Decision {
	decision := Decision{Tool: tool, Verdict: verdictOf(gated.Verdict), Provider: gated.Provider, Model: gated.Model, DurationMS: gated.DurationMS}
	for _, answer := range gated.Answers {
		decision.Answers = append(decision.Answers, gateAnswer(answer))
	}
	if gated.Reason == nil {
		return decision
	}
	decision.Reason = Reason{
		Question:  gated.Reason.Question,
		Limit:     gated.Reason.Comparison,
		Threshold: gated.Reason.Threshold,
		Value:     gated.Reason.Value,
		DeadBand:  gated.Reason.DeadBand,
		RelaxedBy: gated.Reason.RelaxedBy,
		Blocked:   gated.Reason.Blocked,
	}
	for _, answer := range gated.Answers {
		if answer.Question == gated.Reason.Question {
			decision.Reason.Levels = answer.Legend
		}
	}
	if len(gated.Answers) == 0 && gated.Reason.ModeReason != nil {
		decision.Failure = *gated.Reason.ModeReason
	}
	return decision
}

func verdictOf(verdict ledger.Verdict) Verdict {
	switch verdict {
	case ledger.VerdictAllow:
		return Allow
	case ledger.VerdictDeny:
		return Deny
	case ledger.VerdictAsk, ledger.VerdictUnset:
		return Ask
	}
	panic("host: unknown verdict " + string(verdict))
}

func gateAnswer(answer ledger.Answer) GateAnswer {
	out := GateAnswer{Question: answer.Question, Max: 1}
	switch answer.Kind {
	case ledger.AnswerNoul:
		out.Value = answer.Noul
	case ledger.AnswerScore:
		out.Value, out.Max = answer.Score, float64(max(len(answer.Dist)-1, 1))
	case ledger.AnswerChoice:
		out.Choice = answer.Choice
		for _, slice := range answer.Dist {
			if slice.Option == answer.Choice {
				out.Value = slice.P
			}
		}
	default:
		panic("host: unknown answer kind " + string(answer.Kind))
	}
	return out
}
