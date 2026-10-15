package host

import (
	"cmp"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"slices"
	"strconv"
	"strings"
	"sync/atomic"
	"time"

	"tofu/internal/cron"
	"tofu/internal/judge/jev"
	"tofu/internal/judge/ledger"
	"tofu/internal/llm"
	"tofu/internal/session"
	roster "tofu/internal/subagent"
	"tofu/internal/turn"
	"tofu/internal/turn/tools"
)

const (
	processDir      = "."
	standingPoint   = "standing"
	cancelledAt     = "cancelled at"
	hookTrustTool   = "hooks"
	resumedTask     = "continue"
	continueOn      = "continue on "
	waitForTheLimit = "I'll wait for the limit to reset"
	limitQuestion   = "limit"
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
	Lead     func(turn.Model) turn.Model
	SubAgent func(turn.Model) turn.Model
	Say      func(string)
	Now      func() time.Time
	Person   turn.Person
	Gate     func(ctx context.Context, tool string, gated turn.GateDecision, err error)
	Roster   *roster.Roster
	Inbox    *turn.Inbox
	Reads    *turn.ReadLedger
	Cron     *cron.Book
}

type Prepared struct {
	Config    turn.Config
	Spawner   *turn.SpawnTool
	Plan      *tools.Plan
	Ceiling   int
	MaxSteps  int
	Decisions func() int
	Sessions  *session.Store
	GateOff   error
	Close     func()
	Account   func(id int64) TurnAccount
	Others    func() []OtherSource
}

type OtherSource struct {
	Name string
	Pick Pick
}

type limitReached struct {
	*turn.RateLimited
	source string
	others []OtherSource
}

func (h *Host) run(ctx context.Context, pick Pick, task string, live Live) {
	h.runFrom(ctx, pick, task, live, false)
}

func (h *Host) runFrom(ctx context.Context, pick Pick, task string, live Live, resumed bool) {
	reached := h.lead(ctx, pick, task, live, resumed)
	if reached.RateLimited == nil {
		return
	}
	resumesAt := reached.ResetAt.Local().Format(time.DateTime) + ", in " + reached.ResetAt.Sub(h.now()).Round(time.Second).String()
	other, goesOn := h.anotherSource(ctx, reached, resumesAt, live)
	switch {
	case !goesOn:
	case other != nil:
		h.mu.Lock()
		h.carried, h.pick = turn.WithoutThinking(h.carried), other.Pick
		h.mu.Unlock()
		live.Emit(Event{Kind: EventNote, Text: "the turn continues on " + other.Name + " from the same conversation"})
		h.runFrom(ctx, other.Pick, resumedTask, live, true)
	default:
		if task, resumes := h.waitForTheReset(ctx, reached, resumesAt, live); resumes {
			h.runFrom(ctx, pick, task, live, true)
		}
	}
}

func (h *Host) heardTyped(emit func(Event), typed string) {
	emit(Event{Kind: EventSteered, ID: h.steering.heard(), Text: typed, Step: 1})
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
	person, asking := awaitPerson(emit, h.asks, &prepared.Config), turn.WithQuestionsBlock(turn.WithShellRegistry(tools.WithMemoryKept(ctx, keptMemory(emit)), h.shells), h.questionsBlock)
	if live.Origin.Kind == OriginCron {
		person = unattended
	} else {
		asking = turn.WithPersonForm(tools.WithMemoryPick(asking, pickMemory(emit, h.memoryPicks)), askForm(emit, h.questions))
	}
	prepared, err = h.engine.Prepare(Turn{ID: watch.turnID, Session: id, Task: task, Pick: pick, Images: images}, Hooks{
		Lead:     func(model turn.Model) turn.Model { watch.inner = model; return watch },
		SubAgent: func(model turn.Model) turn.Model { return watchedSubAgent{watch: watch, inner: model} },
		Say:      say,
		Now:      h.now,
		Person:   person,
		Gate: func(ctx context.Context, tool string, gated turn.GateDecision, err error) {
			asker := turn.SubAgentAsking(ctx)
			decided := Decision{Tool: tool, Verdict: Ask}
			switch {
			case err != nil:
				decided.Failure = err.Error()
			case gated.Verdict != ledger.VerdictUnset:
				decided = gateDecision(tool, gated)
				decided.Enforced = prepared.Config.GateMode == turn.GateEnforce
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
	config.History, config.Prefix = h.carried, h.prefix
	config.TaskOrigin.Source = live.Origin.recorded()
	h.mu.Unlock()
	var steps atomic.Int64
	config.Steering = func() []string { return h.steering.take(emit, int(steps.Load())+1) }
	config.ImagesOf = imagesOf
	config.ToolResult = func(answered llm.Message) { watch.result(answered, "") }
	config.AccountTaken = func(taken turn.AccountTaken) {
		spent := TurnAccount{AccountID: taken.ID}
		if prepared.Account != nil {
			spent = prepared.Account(taken.ID)
		}
		spent.Reason, spent.FromAccount = AccountReason(taken.Reason), taken.From
		emit(Event{Kind: EventAccount, Account: &spent})
	}
	config.Appended = func(logged session.Event) {
		emit(Event{Kind: EventPersisted, ID: logged.ID, Agent: logged.Agent, Logged: &logged})
	}
	var endedSession atomic.Pointer[string]
	config.Step = func(step turn.StepRow) {
		steps.Store(int64(step.Index))
		if prepared.Plan != nil {
			emit(Event{Kind: EventPlan, Plan: statedPlan(prepared.Plan.Items())})
		}
		if step.Occupancy != nil {
			emit(Event{Kind: EventContext, Context: Context{Used: step.Occupancy.Total(), Budget: prepared.Ceiling}})
		}
		if ended := endedSession.Swap(nil); ended != nil {
			if header, err := sessions.Header(*ended); err == nil && header.ForkedInto != "" {
				emit(labelledAs(sessions, Event{Kind: EventSession, ID: header.ForkedInto, Root: header.ForkedInto}))
			}
		}
	}
	config.EndedSession = func(ended turn.Row) error {
		if ended.SpawnedFrom == "" && ended.Session != "" {
			endedSession.Store(&ended.Session)
		}
		forked := forkOf(ended)
		emit(Event{Kind: EventForkStart})
		emit(Event{Kind: EventNote, Text: forkWords(forked)})
		emit(Event{Kind: EventForkEnd, Fork: &forked})
		return nil
	}
	stopClocks := watch.clockRunningSubAgents()
	stopListening := watch.stop.listen(live.LeadStop, h.sendNow)
	heard := func(typed string) { h.heardTyped(emit, typed) }
	var reported []error
	var reached limitReached
	if scratched, err := turn.WithLeadScratch(asking, config); err == nil {
		asking = scratched
	}
	leadErr := turn.Lead(asking, config, live.Steering, heard, func(row turn.Row, err error) {
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
			h.carried = turn.Sendable(row.Conversation)
		}
		forked := row.Session != "" && row.Session != h.id
		if forked {
			h.prefix = &turn.Prefix{}
		}
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
	if reached.RateLimited == nil || ctx.Err() != nil {
		return limitReached{}
	}
	if prepared.Others != nil {
		reached.others = prepared.Others()
	}
	return reached
}

func (h *Host) anotherSource(ctx context.Context, reached limitReached, resumesAt string, live Live) (*OtherSource, bool) {
	if live.Origin.Kind == OriginCron || len(reached.others) == 0 {
		return nil, true
	}
	options := make([]turn.PersonOption, 0, len(reached.others)+1)
	for _, other := range reached.others {
		options = append(options, turn.PersonOption{Label: continueOn + other.Name, Description: "another subscription, paid apart from " + reached.source + ": the conversation carries over"})
	}
	options = append(options, turn.PersonOption{Label: waitForTheLimit, Description: "the turn resumes by itself at " + resumesAt})
	replies, err := askForm(live.Emit, h.questions)(ctx, []turn.PersonQuestion{{ID: limitQuestion, Header: reached.source + " limit", Type: turn.QuestionChoice, Options: options,
		Question: "every " + reached.source + " account is at its limit: go on with another subscription, or wait for the limit to reset?"}}, 0)
	var undelivered turn.QuestionUndelivered
	if err != nil && !errors.As(err, &undelivered) {
		return nil, false
	}
	for _, other := range reached.others {
		if len(replies) > 0 && slices.Contains(replies[0].Chosen, continueOn+other.Name) {
			return &other, true
		}
	}
	return nil, true
}

func (h *Host) waitForTheReset(ctx context.Context, reached limitReached, resumesAt string, live Live) (string, bool) {
	live.Emit(Event{Kind: EventNote, Text: "every " + reached.source + " account is at its limit, so the turn resumes by itself at " + resumesAt +
		": type to go on now, or press esc to cancel"})
	timer := time.NewTimer(reached.ResetAt.Sub(h.now()))
	defer timer.Stop()
	select {
	case <-timer.C:
		live.Emit(Event{Kind: EventNote, Text: "the " + reached.source + " limit has reset, so the turn resumes"})
		return resumedTask, true
	case typed := <-live.Steering:
		h.heardTyped(live.Emit, typed)
		return typed, true
	case <-ctx.Done():
		live.Emit(Event{Kind: EventNote, Text: "the wait for the " + reached.source + " limit was cancelled, and the turn does not resume"})
		return "", false
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
	err = h.readOnly
	if err == nil {
		err = h.hold(id)
	}
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
	if _, err := store.Header(id); err != nil {
		_ = store.Write(session.Header{ID: id, Root: id, At: h.now()}, nil)
	}
	return labelledAs(store, Event{Kind: EventSession, ID: id, Root: id}), true
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
	var missing jev.MissingKey
	errors.As(gateErr, &missing)
	return Event{Kind: EventGateOff, Text: gateErr.Error(), GateWhy: missing.Why}
}

func awaitPerson(emit func(Event), book *asks, config *turn.Config) turn.Person {
	return func(ctx context.Context, request turn.GateRequest, decision turn.GateDecision) (turn.PersonAnswer, error) {
		places, _ := config.ResolvedPlaces()
		place := askedPlace(request)
		var overriding struct{ Rule, Question string }
		if request.Tool == (tools.RuleOverride{}).Name() {
			_ = json.Unmarshal(request.Args, &overriding)
		}
		standsAt, accepts := place, []ApprovalDecision{AllowOnce, AllowAlways, RejectOnce, RejectAlways, Cancelled}
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
			turn.CodeEntry{Turn: request.TurnID, Code: *request.Code, By: "the person, standing", Answer: codeAnswer(stood)}.Record(places.Log)
			return personAnswer(stood), nil
		case stands:
			recordStanding(places.Log, request, place, stood)
			return personAnswer(stood), nil
		case turn.StandingOnly(ctx):
			return turn.PersonDenied, turn.NotStanding{}
		}
		asked := Event{Kind: EventAwaitPerson, ID: id, Tool: request.Tool, Text: place, Args: request.Args, Agent: turn.SubAgentAsking(ctx), Accepts: accepts}
		if request.Code != nil {
			emit(Event{Kind: EventNote, Text: asked.Agent + " asks you to run this inline code:\n" + turn.CodeShown(*request.Code)})
		}
		if decision.Verdict != ledger.VerdictUnset {
			judged := gateDecision(request.Tool, decision)
			asked.Decision = &judged
		}
		reply, forget := book.wait(asked.ID, standsAt)
		defer forget()
		emit(asked)
		defer emit(Event{Kind: EventResumed, ID: asked.ID, Agent: asked.Agent})
		started := time.Now()
		select {
		case answered := <-reply:
			if request.Code != nil {
				turn.CodeEntry{Turn: request.TurnID, Code: *request.Code, By: "the person", Answer: codeAnswer(answered), Took: time.Since(started)}.Record(places.Log)
			}
			out := personAnswer(answered)
			recordPersonAnswer(places.Log, decision.ID, out)
			return out, nil
		case <-ctx.Done():
			return turn.PersonDenied, ctx.Err()
		}
	}
}

func personAnswer(answered Answer) turn.PersonAnswer {
	switch answered {
	case AlwaysHere:
		return turn.PersonAlwaysHere
	case AllowedOnce:
		return turn.PersonAllowedOnce
	case Denied, NeverHere:
		return turn.PersonDenied
	}
	panic("host: unknown answer from the person")
}

func codeAnswer(answered Answer) turn.CodeAnswer {
	switch answered {
	case AlwaysHere:
		return turn.CodeAllowAlways
	case AllowedOnce:
		return turn.CodeAllowOnce
	case Denied:
		return turn.CodeDeny
	case NeverHere:
		return turn.CodeDenyAlways
	}
	panic("host: unknown answer from the person")
}

func recordPersonAnswer(log, id string, answer turn.PersonAnswer) {
	if log == "" || id == "" {
		return
	}
	_ = ledger.NewWriter(log).Backfill(id, answer.Outcome())
}

func recordStanding(log string, request turn.GateRequest, place string, stood Answer) {
	if log == "" {
		return
	}
	body, err := ledger.Canonical(map[string]string{"tool": request.Tool, "target": place})
	if err != nil {
		return
	}
	verdict := ledger.VerdictDeny
	if stood == AlwaysHere {
		verdict = ledger.VerdictAllow
	}
	_, _ = ledger.NewWriter(log).Append(ledger.Row{Point: standingPoint, StateHash: ledger.HashOf(body), State: body, Verdict: verdict, TurnID: request.TurnID})
}

type noPerson struct{}

func (noPerson) Error() string {
	return "no person: a cron job started this turn and nobody is there to answer, so a call that would ask is refused"
}

func unattended(context.Context, turn.GateRequest, turn.GateDecision) (turn.PersonAnswer, error) {
	return turn.PersonDenied, noPerson{}
}

func askedPlace(request turn.GateRequest) string {
	return turn.CallPlace(request, processDir)
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

func forkOf(ended turn.Row) Fork {
	forked := Fork{From: ended.Session, To: ended.ForkedInto}
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

func WordsAfterLastCalls(row turn.Row) (string, bool) {
	if len(row.Steps) == 0 || len(row.Steps[len(row.Steps)-1].ToolCalls) == 0 || len(row.Conversation) == 0 {
		return "", false
	}
	if last := row.Conversation[len(row.Conversation)-1]; last.Role == llm.RoleAssistant {
		return last.Content, strings.TrimSpace(last.Content) != ""
	}
	step := row.Steps[len(row.Steps)-1]
	onlySpawned := !slices.ContainsFunc(step.ToolCalls, func(call turn.ToolCallRow) bool { return call.SubAgentID == "" })
	return step.AssistantText, onlySpawned && strings.TrimSpace(step.AssistantText) != ""
}

func doneWords(outcome turn.Outcome, guard *turn.LoopGuardStop) (string, Status) {
	switch outcome {
	case turn.OutcomeUnset, turn.OutcomeForked:
		return "finished in", StatusFinished
	case turn.OutcomeStopped:
		return "cooked for", StatusFinished
	case turn.OutcomeStepCap:
		return "stopped at the step cap after", StatusStopped
	case turn.OutcomeDecisionCap:
		return "stopped at the decision cap after", StatusStopped
	case turn.OutcomeTruncated:
		return "stopped on a reply it could not finish, after", StatusStopped
	case turn.OutcomeError:
		return "failed after", StatusFailed
	case turn.OutcomeRetiredCostCap:
		return "stopped at a cap this build no longer sets, after", StatusStopped
	case turn.OutcomeRetiredWallClockCap:
		return "stopped at the wall clock cap after", StatusStopped
	case turn.OutcomeLoopGuard:
		return loopGuardWords(guard) + ", after", StatusStopped
	}
	panic("host: unknown outcome " + outcome.String())
}

func loopGuardWords(guard *turn.LoopGuardStop) string {
	if guard == nil {
		return "stopped itself after repeating a tool call"
	}
	return "stopped itself after calling " + strconv.Quote(guard.Tool) + " with " + string(guard.Args) +
		" and getting the same result " + strconv.Itoa(guard.Repeats) + " times in a row"
}

func statedPlan(items []tools.PlanItem) []PlanItem {
	drawn := make([]PlanItem, 0, len(items))
	for _, item := range items {
		drawn = append(drawn, PlanItem{Phase: item.Phase, Text: item.Text, State: drawnPlanState(item.State)})
	}
	return drawn
}

func drawnPlanState(state tools.PlanState) PlanState {
	switch state {
	case tools.PlanPending:
		return PlanPending
	case tools.PlanRunning:
		return PlanRunning
	case tools.PlanDone:
		return PlanDone
	case tools.PlanDropped:
		return PlanDropped
	}
	panic("host: unknown plan item state " + string(state))
}

func gateDecision(tool string, gated turn.GateDecision) Decision {
	decision := Decision{Tool: tool, Verdict: verdictOf(gated.Verdict)}
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
