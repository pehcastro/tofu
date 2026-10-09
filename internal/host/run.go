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
	"tofu/internal/sys"
	"tofu/internal/turn"
	"tofu/internal/turn/tools"
)

const (
	placeWords    = 2
	standingPoint = "standing"
	cancelledAt   = "cancelled at"
	hookTrustTool = "hooks"
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
}

func (h *Host) run(ctx context.Context, pick Pick, task string, live Live) {
	emit := live.Emit
	fail := func(err error) { emit(Event{Kind: EventFailure, Text: err.Error()}) }
	say := func(text string) { emit(Event{Kind: EventNote, Text: text}) }
	id := h.pendingID()
	releaseTurn, err := h.holdTurn(id)
	if err != nil {
		fail(err)
		return
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
	person, asking := awaitPerson(emit, h.asks), turn.WithQuestionsBlock(turn.WithShellRegistry(ctx, h.shells), h.questionsBlock)
	if live.Origin.Kind == OriginCron {
		person = unattended
	} else {
		asking = turn.WithPersonForm(tools.WithMemoryPick(asking, pickMemory(emit, h.memoryPicks)), askForm(emit, h.questions))
	}
	var prepared Prepared
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
		return
	}
	sessions, config := prepared.Sessions, prepared.Config
	if err := h.cron.Keep(cronFile(sessions, id)); err != nil {
		say("cron jobs were not written: " + err.Error())
	}
	if labelled, named := h.label(sessions, h.ID()); named {
		emit(labelled)
	}
	if prepared.GateOff != nil {
		emit(gateOffEvent(prepared.GateOff))
	}
	watch.spawner, watch.maxSteps, watch.decisions = prepared.Spawner, prepared.MaxSteps, prepared.Decisions
	watch.ran = func(agent string) session.AgentRun { return recordedRuns(sessions, h.ID())[agent] }
	h.mu.Lock()
	config.SessionSource, h.started = h.started, ""
	config.History, config.Prefix = h.carried, h.prefix
	config.TaskOrigin.Source = live.Origin.recorded()
	h.mu.Unlock()
	var steps atomic.Int64
	config.Steering = func() []string { return h.steering.take(emit, int(steps.Load())+1) }
	config.ImagesOf = imagesOf
	config.ToolResult = func(answered llm.Message) { watch.result(answered, "") }
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
	heard := func(typed string) { emit(Event{Kind: EventSteered, ID: h.steering.heard(), Text: typed, Step: 1}) }
	var reported []error
	leadErr := turn.Lead(asking, config, live.Steering, heard, func(row turn.Row, err error) {
		watch.stop.reset()
		steps.Store(0)
		if err != nil {
			reported = append(reported, err)
		}
		words, status := doneWords(row.Outcome, row.Guard)
		switch {
		case errors.Is(err, context.Canceled):
			words, status = cancelledAt, StatusStopped
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
	}
	return store.HoldTurn()
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

func awaitPerson(emit func(Event), book *asks) turn.Person {
	return func(ctx context.Context, request turn.GateRequest, decision turn.GateDecision) (turn.PersonAnswer, error) {
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
		case stands:
			recordStanding(request, place, stood)
			if stood == AlwaysHere {
				return turn.PersonAlwaysHere, nil
			}
			return turn.PersonDenied, nil
		}
		asked := Event{Kind: EventAwaitPerson, ID: id, Tool: request.Tool, Text: place, Args: request.Args, Agent: turn.SubAgentAsking(ctx), Accepts: accepts}
		if decision.Verdict != ledger.VerdictUnset {
			judged := gateDecision(request.Tool, decision)
			asked.Decision = &judged
		}
		reply, forget := book.wait(asked.ID, standsAt)
		defer forget()
		emit(asked)
		defer emit(Event{Kind: EventResumed, ID: asked.ID, Agent: asked.Agent})
		select {
		case answered := <-reply:
			var out turn.PersonAnswer
			switch answered {
			case AlwaysHere:
				out = turn.PersonAlwaysHere
			case AllowedOnce:
				out = turn.PersonAllowedOnce
			case Denied, NeverHere:
				out = turn.PersonDenied
			default:
				panic("host: unknown answer from the person")
			}
			recordPersonAnswer(decision.ID, out)
			return out, nil
		case <-ctx.Done():
			return turn.PersonDenied, ctx.Err()
		}
	}
}

func recordPersonAnswer(id string, answer turn.PersonAnswer) {
	if id == "" {
		return
	}
	dir, err := sys.LogDir()
	if err != nil {
		return
	}
	_ = ledger.NewWriter(dir).Backfill(id, answer.Outcome())
}

func recordStanding(request turn.GateRequest, place string, stood Answer) {
	dir, err := sys.LogDir()
	if err != nil {
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
	_, _ = ledger.NewWriter(dir).Append(ledger.Row{Point: standingPoint, StateHash: ledger.HashOf(body), State: body, Verdict: verdict, TurnID: request.TurnID})
}

type noPerson struct{}

func (noPerson) Error() string {
	return "no person: a cron job started this turn and nobody is there to answer, so a call that would ask is refused"
}

func unattended(context.Context, turn.GateRequest, turn.GateDecision) (turn.PersonAnswer, error) {
	return turn.PersonDenied, noPerson{}
}

func askedPlace(request turn.GateRequest) string {
	var fields struct {
		Path    string `json:"path"`
		Command string `json:"command"`
	}
	if err := json.Unmarshal(request.Args, &fields); err != nil {
		return request.Tool + " " + string(request.Args)
	}
	switch {
	case fields.Path != "":
		return request.Tool + " " + fields.Path
	case fields.Command != "":
		return request.Tool + " " + commandPlace(fields.Command)
	}
	return request.Tool + " " + string(request.Args)
}

func commandPlace(command string) string {
	words, named := make([]string, 0, placeWords), 0
	for _, word := range strings.Fields(command) {
		if strings.HasPrefix(word, "-") {
			words = append(words, word)
			continue
		}
		if named++; named <= placeWords {
			words = append(words, word)
		}
	}
	return strings.Join(words, " ")
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
