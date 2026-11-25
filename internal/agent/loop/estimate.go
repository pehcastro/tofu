package loop

import (
	"cmp"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"math"
	"slices"
	"strconv"
	"strings"
	"sync"
	"time"
	"unicode/utf8"

	"tofu/internal/agent/subagents"
	"tofu/internal/konst"
	"tofu/internal/models/llm/core"
	"tofu/internal/platform/project"
	"tofu/internal/platform/safe"
	"tofu/internal/store/session"
	"tofu/internal/tools/contract"
)

type estimateFor struct {
	scope      session.EstimateScope
	agent      string
	definition string
	model      string
	task       string
	elapsed    time.Duration
}

func (c Config) estimate(asked estimateFor) (session.EstimateBody, error) {
	body := session.EstimateBody{Scope: asked.scope, Agent: asked.agent, Definition: asked.definition, Basis: session.EstimateNoHistory, Model: asked.model,
		PromptChars: utf8.RuneCountInString(asked.task), ElapsedMS: asked.elapsed.Milliseconds()}
	if c.Sessions == nil {
		return body, nil
	}
	body.Project = c.Sessions.Project()
	here := func(project string) bool { return project == "" || project == body.Project }
	runs, built, err := settledRuns(c.Sessions.State(), here)
	if err != nil {
		return body, err
	}
	elsewhere := sync.OnceValues(func() ([]historyRun, error) {
		places, err := c.ResolvedPlaces()
		if err != nil {
			return nil, err
		}
		found, builtThere, err := historyElsewhere(places.State, func(project string) bool { return !here(project) })
		built += builtThere
		return found, err
	})
	var sample []historyRun
	for _, tier := range []struct {
		basis session.EstimateBasis
		match session.EstimateMatch
	}{
		{session.EstimateFromHistory, session.MatchAgentModel}, {session.EstimateFromHistory, session.MatchAgent},
		{session.EstimateFirstRun, session.MatchAgentModel}, {session.EstimateFirstRun, session.MatchAgent},
		{session.EstimateFromHistory, session.MatchModel}, {session.EstimateFirstRun, session.MatchModel},
	} {
		from := runs
		if tier.basis == session.EstimateFirstRun {
			if from, err = elsewhere(); err != nil {
				return body, err
			}
		}
		if sample = sampleOf(from, body, tier.match); len(sample) > 0 {
			body.Basis, body.Match = tier.basis, tier.match
			break
		}
	}
	if built > 0 && c.Notify != nil {
		c.Notify(fmt.Sprintf("estimates: the run history was built once by reading %d sessions into %s, and each finished run adds one line to it from now on", built, settledRunsName))
	}
	if len(sample) == 0 {
		return body, nil
	}
	if body.ElapsedMS > 0 {
		remainingFrom(sample, &body)
	} else {
		saidFrom(sample, &body)
	}
	if body.SaidMS > 0 {
		body.DoneByMS = body.ElapsedMS + doneBy(body.SaidMS-body.ElapsedMS, body.Basis)
	}
	return body, nil
}

func ranAs(body session.EstimateBody) session.EstimateScope {
	if body.Scope == session.EstimateAgent || body.Definition != "" {
		return session.EstimateAgent
	}
	return session.EstimateTurn
}

func sampleOf(runs []historyRun, body session.EstimateBody, match session.EstimateMatch) []historyRun {
	var sample []historyRun
	for _, run := range runs {
		sameAgent := run.Scope == ranAs(body) && run.Agent == body.Definition
		sameModel := body.Model == "" || run.Model == body.Model || strings.HasSuffix(body.Model, "/"+run.Model)
		var kept bool
		switch match {
		case session.MatchAgentModel:
			kept = sameAgent && sameModel
		case session.MatchAgent:
			kept = sameAgent
		case session.MatchModel:
			kept = sameModel
		}
		if kept {
			if sample = append(sample, run); len(sample) == konst.EstimateSampleRuns {
				break
			}
		}
	}
	return sample
}

func saidFrom(sample []historyRun, body *session.EstimateBody) {
	briefed := slices.DeleteFunc(slices.Clone(sample), func(run historyRun) bool { return run.Chars == 0 })
	byChars := func(a, b historyRun) int { return cmp.Compare(a.Chars, b.Chars) }
	sized := sample
	body.Runs = len(sample)
	if len(briefed) > 1 && body.PromptChars >= slices.MinFunc(briefed, byChars).Chars && body.PromptChars <= slices.MaxFunc(briefed, byChars).Chars {
		distance := func(run historyRun) float64 {
			return math.Abs(math.Log1p(float64(run.Chars)) - math.Log1p(float64(body.PromptChars)))
		}
		sized = slices.SortedStableFunc(slices.Values(briefed), func(a, b historyRun) int { return cmp.Compare(distance(a), distance(b)) })[:(len(briefed)+1)/2]
		body.SizedRuns = len(sized)
	}
	byTook := slices.SortedFunc(slices.Values(sized), func(a, b historyRun) int { return cmp.Compare(a.TookMS, b.TookMS) })
	took := make([]float64, len(byTook))
	for i, run := range byTook {
		took[i] = float64(run.TookMS)
	}
	total, middle := median(took), byTook[len(byTook)/4:len(byTook)-len(byTook)/4]
	var test, build, verify float64
	for _, run := range middle {
		whole := float64(max(run.TookMS, run.TestMS+run.BuildMS+run.VerifyMS, 1))
		test, build, verify = test+float64(run.TestMS)/whole, build+float64(run.BuildMS)/whole, verify+float64(run.VerifyMS)/whole
	}
	share := func(sum float64) int64 { return int64(total * sum / float64(len(middle))) }
	body.SaidMS, body.TestMS, body.BuildMS, body.VerifyMS = int64(total), share(test), share(build), share(verify)
	body.AgentMS = body.SaidMS - body.TestMS - body.BuildMS - body.VerifyMS
	for _, run := range sized {
		if run.TestMS > 0 {
			body.TestRuns++
		}
		if run.BuildMS > 0 {
			body.BuildRuns++
		}
		if run.VerifyMS > 0 {
			body.VerifyRuns++
		}
	}
}

func remainingFrom(sample []historyRun, body *session.EstimateBody) {
	var left []float64
	for _, run := range sample {
		body.LongestMS = max(body.LongestMS, run.TookMS)
		if run.TookMS > body.ElapsedMS {
			left = append(left, float64(run.TookMS-body.ElapsedMS))
		}
	}
	body.Runs, body.LastedRuns = len(sample), len(left)
	if len(left) > 0 {
		body.SaidMS = body.ElapsedMS + int64(median(left))
	}
}

func doneBy(said int64, basis session.EstimateBasis) int64 {
	factor, pad := konst.EstimateDoneByFactor, float64(konst.EstimateShortPadMillis)
	if basis == session.EstimateFirstRun {
		factor, pad = konst.EstimateFirstRunFactor, pad*konst.EstimateFirstRunFactor/konst.EstimateDoneByFactor
	}
	if said < konst.EstimateShortMillis {
		return said + int64(pad)
	}
	return int64(float64(said) * factor)
}

func (c Config) settledState() string {
	if c.Sessions == nil {
		return ""
	}
	return c.Sessions.State()
}

func median(values []float64) float64 {
	sorted, middle := slices.Sorted(slices.Values(values)), len(values)/2
	if len(sorted)%2 == 1 {
		return sorted[middle]
	}
	return (sorted[middle-1] + sorted[middle]) / 2
}

func roundedMS(ms int64) time.Duration {
	return (time.Duration(ms) * time.Millisecond).Round(time.Second)
}

func ranWord(body session.EstimateBody) string {
	switch {
	case ranAs(body) == session.EstimateTurn:
		return "lead"
	case body.Definition == "":
		return "unnamed sub-agent"
	}
	return body.Definition
}

func sampleWords(body session.EstimateBody) string {
	ran, model := ranWord(body), cmp.Or(body.Model, "any model")
	runs := fmt.Sprintf("the %d newest %s runs", body.Runs, ran)
	switch {
	case body.Basis == session.EstimateFromHistory && body.Match == session.MatchAgentModel:
		return runs + " in this project on " + model
	case body.Basis == session.EstimateFromHistory && body.Match == session.MatchAgent:
		return "no " + ran + " run in this project on " + model + ", so " + runs + " here on other models"
	case body.Basis == session.EstimateFirstRun && body.Match == session.MatchAgentModel:
		return "no " + ran + " run in this project, so " + runs + " in other projects on " + model + ", with the first run margin"
	case body.Basis == session.EstimateFirstRun && body.Match == session.MatchAgent:
		return "no " + ran + " run in this project or on " + model + ", so " + runs + " in other projects on other models, with the first run margin"
	case body.Basis == session.EstimateFromHistory && body.Match == session.MatchModel:
		return fmt.Sprintf("no %s run is recorded in any project, so the %d newest runs of every agent in this project on %s", ran, body.Runs, model)
	case body.Basis == session.EstimateFirstRun && body.Match == session.MatchModel:
		return fmt.Sprintf("no %s run is recorded in any project, so the %d newest runs of every agent in other projects on %s, with the first run margin", ran, body.Runs, model)
	}
	panic("loop: unknown estimate sample " + string(body.Basis) + " " + string(body.Match))
}

func estimateWords(who string, body session.EstimateBody) string {
	on := who + " on " + cmp.Or(body.Model, "any model")
	switch {
	case body.Basis == session.EstimateNoHistory:
		return "estimate for " + on + ": first run, and no finished run of " + ranWord(body) + " or of any agent on this model is recorded in any project, so there is no estimate"
	case body.ElapsedMS > 0 && body.SaidMS == 0:
		return fmt.Sprintf("%s is running, %s elapsed: it has run longer than every one of %s, the longest took %s, so there is no figure for what remains",
			on, roundedMS(body.ElapsedMS), sampleWords(body), roundedMS(body.LongestMS))
	case body.ElapsedMS > 0:
		return fmt.Sprintf("%s is running, %s elapsed: about %s remain, done by %s in all. basis: %d of %s ran past %s, and they took %s in all at the median",
			on, roundedMS(body.ElapsedMS), roundedMS(body.SaidMS-body.ElapsedMS), roundedMS(body.DoneByMS), body.LastedRuns, sampleWords(body), roundedMS(body.ElapsedMS), roundedMS(body.SaidMS))
	}
	counted, sized := body.Runs, ""
	switch {
	case body.SizedRuns > 0:
		counted, sized = body.SizedRuns, fmt.Sprintf("sized to the %d of them nearest this work's %d chars", body.SizedRuns, body.PromptChars)
	case body.Runs > 1:
		sized = fmt.Sprintf("the work is %d chars, outside the lengths of their briefs, so all %d count unsized", body.PromptChars, body.Runs)
	default:
		sized = "one run, so no size match"
	}
	return fmt.Sprintf("estimate for %s, the whole work: about %s, done by %s. agent %s, tests %s (%d of %d runs tested), builds %s (%d built), verify %s (%d verified). "+
		"basis: %s; %s; each part is its average share of the middle half of those runs by time, applied to their median",
		on, roundedMS(body.SaidMS), roundedMS(body.DoneByMS), roundedMS(body.AgentMS), roundedMS(body.TestMS), body.TestRuns, counted, roundedMS(body.BuildMS), body.BuildRuns,
		roundedMS(body.VerifyMS), body.VerifyRuns, sampleWords(body), sized)
}

const EstimateTimeToolName = "estimate_time"

type EstimateTime struct {
	base    Config
	spawner *SpawnTool
}

func (EstimateTime) Name() string { return EstimateTimeToolName }

func (EstimateTime) Definition() core.Tool {
	return core.Tool{
		Name: EstimateTimeToolName,
		Description: "says how long a piece of work will take in all, agent time plus test, build and verify time, from the finished runs of the same agent recorded in this project's sessions, without starting anything or asking a model. " +
			"call it whenever the person asks how long something takes, how much time it needs or when it will be done, and answer with its numbers: the total, done by, the agent, test, build and verify parts, and their basis. " +
			"work is what the work is, in the words a brief would use; agent names the sub-agent definition that would run it, left out for you, or the id of a sub-agent already running, which answers with its elapsed time and what remains; model overrides the model.",
		Parameters: map[string]any{
			"type": "object",
			"properties": map[string]any{
				"work":  map[string]any{"type": "string"},
				"agent": map[string]any{"type": "string"},
				"model": map[string]any{"type": "string"},
			},
			"required": []string{"work"},
		},
	}
}

func (e EstimateTime) Run(ctx context.Context, raw json.RawMessage) (contract.Result, error) {
	var args struct{ Work, Agent, Model string }
	if err := json.Unmarshal(raw, &args); err != nil {
		return contract.Result{}, err
	}
	if strings.TrimSpace(args.Work) == "" {
		return contract.Result{}, errors.New("work is required: say what the work is")
	}
	asked, who := estimateFor{scope: session.EstimateCall, model: cmp.Or(args.Model, e.base.Slug), task: args.Work}, "the lead"
	running, isRunning := e.base.runningAgent(args.Agent)
	switch {
	case isRunning:
		asked.agent, asked.definition, asked.model, asked.elapsed, who = running.Agent, running.Definition, cmp.Or(args.Model, running.Model), time.Since(running.StartedAt), running.Agent
	case args.Agent != "" && e.spawner != nil:
		definition, err := e.spawner.SubAgents.Named(args.Agent)
		if err != nil {
			return contract.Result{}, err
		}
		asked.agent, asked.definition, asked.model, who = definition.Name, definition.Name, cmp.Or(args.Model, definition.Model, e.base.Slug), definition.Name
	case args.Agent != "":
		asked.agent, asked.definition, who = args.Agent, args.Agent, args.Agent
	}
	body, err := e.base.estimate(asked)
	if err != nil {
		return contract.Result{}, err
	}
	if site, _ := ctx.Value(spawnSiteKey{}).(spawnSite); site.log != nil {
		if _, err := site.log.Append(session.Event{Turn: site.turn, Agent: site.agent, Call: site.call, Kind: session.EventEstimate}, body); err != nil {
			return contract.Result{}, err
		}
	}
	return contract.Result{Content: estimateWords(who, body), Command: who + " on " + cmp.Or(asked.model, "any model")}, nil
}

func (c Config) runningAgent(id string) (session.AgentRun, bool) {
	if id == "" || c.Sessions == nil || c.Session == "" {
		return session.AgentRun{}, false
	}
	header, err := c.Sessions.Header(c.Session)
	if err != nil {
		return session.AgentRun{}, false
	}
	at := slices.IndexFunc(header.Agents, func(run session.AgentRun) bool { return run.Agent == id && run.EndedAt == nil })
	if at < 0 {
		return session.AgentRun{}, false
	}
	return header.Agents[at], true
}

type openEstimate struct {
	id    string
	at    time.Time
	body  session.EstimateBody
	state string
}

type estimateRead func() (session.EstimateBody, []string, error)

func (c Config) estimateAhead() estimateRead {
	if c.SpawnedFrom != "" {
		return nil
	}
	var body session.EstimateBody
	var notes []string
	var err error
	var reading sync.WaitGroup
	c.Notify = func(note string) { notes = append(notes, note) }
	reading.Go(func() {
		err = safe.Call(project.Crashes(c.Project), "turn estimate", func() (estimateErr error) {
			body, estimateErr = c.estimate(estimateFor{scope: session.EstimateTurn, model: c.Slug, task: c.Task})
			return estimateErr
		})
	})
	return func() (session.EstimateBody, []string, error) {
		reading.Wait()
		return body, notes, err
	}
}

func (r *record) estimate(ahead estimateRead, config Config, at time.Time) *openEstimate {
	if r == nil || ahead == nil {
		return nil
	}
	body, notes, err := ahead()
	for _, note := range notes {
		if config.Notify != nil {
			config.Notify(note)
		}
	}
	if err != nil {
		r.note(errors.New("the estimate for this turn: " + err.Error()))
		return nil
	}
	return &openEstimate{id: r.add(session.Event{At: at, Kind: session.EventEstimate}, body).ID, at: at, body: body, state: config.settledState()}
}

func (r *record) carry(estimate *openEstimate) {
	if r != nil && estimate != nil && estimate.id != "" {
		estimate.id = r.add(session.Event{At: estimate.at, Kind: session.EventEstimate}, estimate.body).ID
	}
}

func (r *record) settle(estimate *openEstimate, outcome Outcome, took time.Duration, ran []Row) {
	if r == nil {
		return
	}
	split, ended := splitOf(ran), turnOutcome(outcome)
	r.note(estimate.settle(r.log, session.Event{Turn: r.turn, Agent: r.agent, SpawnedBy: r.spawnedBy}, ended, took, split))
	if r.own {
		r.note(r.settleAskedLastTurn(ended, took, split))
	}
}

type askedRead func() (session string, unsettled []string, err error)

func (r *record) readAskedLastTurn() askedRead {
	read := r.session()
	var unsettled []string
	var err error
	var reading sync.WaitGroup
	reading.Go(func() { unsettled, err = r.askedLastTurn(read) })
	return func() (string, []string, error) {
		reading.Wait()
		return read, unsettled, err
	}
}

func (r *record) askedLastTurn(id string) ([]string, error) {
	events, err := r.store.EventsOf(id, session.EventTurnEnd, session.EventEstimate, session.EventEstimateSettled)
	if err != nil {
		return nil, err
	}
	estimated, err := session.Estimates(events)
	if err != nil {
		return nil, err
	}
	lastTurn := ""
	for _, event := range events {
		if event.Kind == session.EventTurnEnd && event.Agent == "" && event.Turn != r.turn {
			lastTurn = event.Turn
		}
	}
	var unsettled []string
	for _, one := range estimated {
		if one.Scope == session.EstimateCall && one.Settled == nil && one.Event.Turn == lastTurn {
			unsettled = append(unsettled, one.Event.ID)
		}
	}
	return unsettled, nil
}

func (r *record) settleAskedLastTurn(outcome session.EstimateOutcome, took time.Duration, split session.EstimateSplit) error {
	read, unsettled, err := r.askedLast()
	if read != r.session() {
		unsettled, err = r.askedLastTurn(r.session())
	}
	if err != nil {
		return err
	}
	var failed []error
	for _, id := range unsettled {
		failed = append(failed, (&openEstimate{id: id}).settle(r.log, session.Event{Turn: r.turn}, outcome, took, split))
	}
	return errors.Join(failed...)
}

func (o *openEstimate) settle(log *session.Log, at session.Event, outcome session.EstimateOutcome, took time.Duration, split session.EstimateSplit) error {
	if o == nil || o.id == "" || log == nil {
		return nil
	}
	at.Kind = session.EventEstimateSettled
	_, err := log.Append(at, session.EstimateSettledBody{Estimate: o.id, Outcome: outcome, TookMS: took.Milliseconds(), EstimateSplit: split})
	o.id = ""
	if err != nil || o.state == "" || outcome != session.EstimateDone || o.body.Scope == session.EstimateCall {
		return err
	}
	return appendSettled(o.state, historyRun{At: time.Now(), Project: log.Header().Project, Model: o.body.Model, Scope: o.body.Scope, Agent: o.body.Definition,
		Chars: o.body.PromptChars, TookMS: took.Milliseconds(), EstimateSplit: split})
}

type checkKind int

const (
	notACheck checkKind = iota
	checkBuild
	checkVerify
	checkTest
)

func checkWordKind(word string) checkKind {
	switch strings.ToLower(word) {
	case "test", "tests", "pytest", "jest", "vitest":
		return checkTest
	case "verify", "check", "typecheck", "lint", "vet", "clippy", "drive", "e2e", "smoke":
		return checkVerify
	case "build", "compile", "tsc", "make":
		return checkBuild
	}
	return notACheck
}

func checkOf(tool, command string, shells map[string]checkKind) checkKind {
	if tool == TicketMoveToolName {
		return checkVerify
	}
	if op, name, _ := strings.Cut(command, " "); tool == ShellToolName && (op == "wait" || op == "logs") {
		return shells[name]
	}
	name, ran, background := "", command, false
	if rest, found := strings.CutPrefix(command, "background "); found {
		name, ran, background = strings.Cut(rest, ": ")
	}
	words := strings.Split(tool, "_")
	if tool == bashToolName {
		for _, segment := range strings.FieldsFunc(ran, func(r rune) bool { return strings.ContainsRune("&|;\n", r) }) {
			fields := strings.Fields(segment)
			for _, field := range fields[:min(konst.EstimateCommandFieldsRead, len(fields))] {
				words = append(words, strings.FieldsFunc(field, func(r rune) bool { return strings.ContainsRune(`/\:-`, r) })...)
			}
		}
	}
	kind := notACheck
	for _, word := range words {
		kind = max(kind, checkWordKind(word))
	}
	if background {
		shells[name] = kind
	}
	return kind
}

func addCall(split *session.EstimateSplit, shells map[string]checkKind, tool string, call ToolCallRow) {
	split.PermissionMS += call.PermissionWaitMS
	ran := max(call.DurationMS-call.PermissionWaitMS, 0)
	switch checkOf(tool, call.Command, shells) {
	case notACheck:
		split.ToolsMS += ran
		return
	case checkTest:
		split.TestMS += ran
	case checkBuild:
		split.BuildMS += ran
	case checkVerify:
		split.VerifyMS += ran
	}
	split.CheckCalls++
}

func splitOf(rows []Row) session.EstimateSplit {
	var split session.EstimateSplit
	shells := map[string]checkKind{}
	for _, row := range rows {
		for _, step := range row.Steps {
			split.ModelMS += step.DurationMS
			for _, call := range step.ToolCalls {
				addCall(&split, shells, call.Tool, call)
			}
		}
	}
	return split
}

func turnOutcome(outcome Outcome) session.EstimateOutcome {
	switch outcome {
	case session.OutcomeStopped:
		return session.EstimateDone
	case session.OutcomeCancelled:
		return session.EstimateKilled
	case session.OutcomeUnset, session.OutcomeStepCap, session.OutcomeRetiredCostCap, session.OutcomeRetiredWallClockCap, session.OutcomeDecisionCap,
		session.OutcomeForked, session.OutcomeError, session.OutcomeTruncated, session.OutcomeLoopGuard, session.OutcomeForkStop:
		return session.EstimateStopped
	}
	panic("loop: unknown turn outcome " + strconv.Itoa(int(outcome)))
}

func agentOutcome(run context.Context, stopping bool, state subagents.State) session.EstimateOutcome {
	turn, inWave := run.Value(beforeTheWave{}).(context.Context)
	switch {
	case stopping, inWave && turn.Err() != nil:
		return session.EstimateStopped
	case run.Err() != nil:
		return session.EstimateKilled
	}
	switch state {
	case subagents.Finished, subagents.InReview:
		return session.EstimateDone
	case subagents.Working, subagents.WaitingAnswer, subagents.Reopened, subagents.Parked, subagents.Errored:
		return session.EstimateStopped
	}
	panic("loop: unknown sub-agent state " + strconv.Itoa(int(state)))
}
