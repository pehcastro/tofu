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
	"time"
	"unicode/utf8"

	"tofu/internal/agent/subagents"
	"tofu/internal/konst"
	"tofu/internal/models/llm/core"
	"tofu/internal/store/session"
	"tofu/internal/tools/contract"
)

type estimateFor struct {
	scope session.EstimateScope
	agent string
	model string
	task  string
}

const noTestsRecorded = "no test or build call recorded"

func (c Config) estimate(asked estimateFor) (session.EstimateBody, error) {
	body := session.EstimateBody{Scope: asked.scope, Agent: asked.agent, Basis: session.EstimateNoHistory, Model: asked.model,
		PromptChars: utf8.RuneCountInString(asked.task), TestSource: noTestsRecorded}
	if c.Sessions == nil {
		return body, nil
	}
	body.Project = c.Sessions.Project()
	sameModel := func(model string) bool { return model == asked.model || strings.HasSuffix(asked.model, "/"+model) }
	here := func(project string) bool { return project == "" || project == body.Project }
	runs, built, err := settledRuns(c.Sessions.State(), here, sameModel)
	if err != nil {
		return body, err
	}
	body.Basis = session.EstimateFromHistory
	if len(runs) == 0 {
		places, err := c.ResolvedPlaces()
		if err != nil {
			return body, err
		}
		elsewhere, builtThere, err := historyElsewhere(places.State, func(project string) bool { return !here(project) }, sameModel)
		if err != nil {
			return body, err
		}
		runs, built, body.Basis = elsewhere, built+builtThere, session.EstimateFirstRun
	}
	if built > 0 && c.Notify != nil {
		c.Notify(fmt.Sprintf("estimates: the run history was built once by reading %d sessions into %s, and each finished run adds one line to it from now on", built, settledRunsName))
	}
	if len(runs) == 0 {
		body.Basis = session.EstimateNoHistory
		return body, nil
	}
	body.Runs = len(runs)
	body.AgentMS, body.TestMS, body.TestSource = saidFrom(runs, body.PromptChars)
	body.SaidMS = body.AgentMS + body.TestMS
	factor, pad := konst.EstimateDoneByFactor, float64(konst.EstimateShortPadMillis)
	if body.Basis == session.EstimateFirstRun {
		factor, pad = konst.EstimateFirstRunFactor, pad*konst.EstimateFirstRunFactor/konst.EstimateDoneByFactor
	}
	body.DoneByMS = int64(float64(body.SaidMS) * factor)
	if body.SaidMS < konst.EstimateShortMillis {
		body.DoneByMS = body.SaidMS + int64(pad)
	}
	return body, nil
}

func (c Config) settledState() string {
	if c.Sessions == nil {
		return ""
	}
	return c.Sessions.State()
}

func saidFrom(runs []historyRun, chars int) (int64, int64, string) {
	distance := func(run historyRun) float64 {
		return math.Abs(math.Log1p(float64(run.Chars)) - math.Log1p(float64(chars)))
	}
	near := slices.SortedStableFunc(slices.Values(runs), func(a, b historyRun) int { return cmp.Compare(distance(a), distance(b)) })[:(len(runs)+1)/2]
	agent, counts := make([]float64, len(near)), []float64(nil)
	for i, run := range near {
		agent[i] = float64(run.TookMS)
		if run.Split {
			agent[i] -= float64(run.TestMS + run.PermissionMS)
			counts = append(counts, float64(run.TestCalls))
		}
	}
	var testMS, calls int64
	for _, run := range runs {
		if run.Split {
			testMS, calls = testMS+run.TestMS, calls+int64(run.TestCalls)
		}
	}
	if calls == 0 || len(counts) == 0 {
		return int64(median(agent)), 0, noTestsRecorded
	}
	each, count := testMS/calls, median(counts)
	return int64(median(agent)), int64(float64(each) * count), fmt.Sprintf("%d earlier test and build calls in this history, %s each, %s a run",
		calls, roundedMS(each), strconv.FormatFloat(count, 'f', -1, 64))
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

func estimateWords(who string, body session.EstimateBody) string {
	on := who + " on " + cmp.Or(body.Model, "an unnamed model")
	switch body.Basis {
	case session.EstimateFromHistory:
		return fmt.Sprintf("estimate for %s: about %s, done by %s, from %d earlier runs in this project on this model (agent %s, tests and builds %s: %s)",
			on, roundedMS(body.SaidMS), roundedMS(body.DoneByMS), body.Runs, roundedMS(body.AgentMS), roundedMS(body.TestMS), body.TestSource)
	case session.EstimateFirstRun:
		return fmt.Sprintf("estimate for %s: first run in this project, so about %s from %d runs in other projects, done by %s with the first run margin (agent %s, tests and builds %s: %s)",
			on, roundedMS(body.SaidMS), body.Runs, roundedMS(body.DoneByMS), roundedMS(body.AgentMS), roundedMS(body.TestMS), body.TestSource)
	case session.EstimateNoHistory:
		return "estimate for " + on + ": first run, and no finished run on this model is recorded in any project, so there is no estimate"
	}
	panic("loop: unknown estimate basis " + string(body.Basis))
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
		Description: "says how long a piece of work will take, from the finished runs recorded in this project's sessions on the same model, without starting anything or asking a model. " +
			"call it whenever the person asks how long something takes, how much time it needs or when it will be done, and answer with its numbers: about, done by, whether history or a first run, and the test and build part. " +
			"work is what the work is, in the words a brief would use; agent names the sub-agent definition that would run it, left out for you; model overrides the model, left out for the one running it.",
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
	if args.Agent != "" {
		if e.spawner == nil {
			return contract.Result{}, errors.New("this turn starts no sub-agent, so " + args.Agent + " has no definition to read")
		}
		definition, err := e.spawner.SubAgents.Named(args.Agent)
		if err != nil {
			return contract.Result{}, err
		}
		asked.agent, asked.model, who = definition.Name, cmp.Or(args.Model, definition.Model, e.base.Slug), definition.Name
	}
	if asked.model == "" {
		return contract.Result{}, errors.New("model is required: no model runs this call")
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
	return contract.Result{Content: estimateWords(who, body), Command: who + " on " + asked.model}, nil
}

type openEstimate struct {
	id    string
	at    time.Time
	body  session.EstimateBody
	state string
}

func (r *record) estimate(config Config, at time.Time) *openEstimate {
	if r == nil || config.SpawnedFrom != "" {
		return nil
	}
	body, err := config.estimate(estimateFor{scope: session.EstimateTurn, model: config.Slug, task: config.Task})
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

func (r *record) settleAskedLastTurn(outcome session.EstimateOutcome, took time.Duration, split session.EstimateSplit) error {
	events, err := r.store.EventsOf(r.session(), session.EventTurnEnd, session.EventEstimate, session.EventEstimateSettled)
	if err != nil {
		return err
	}
	estimated, err := session.Estimates(events)
	if err != nil {
		return err
	}
	lastTurn := ""
	for _, event := range events {
		if event.Kind == session.EventTurnEnd && event.Agent == "" && event.Turn != r.turn {
			lastTurn = event.Turn
		}
	}
	var failed []error
	for _, one := range estimated {
		if one.Scope == session.EstimateCall && one.Settled == nil && one.Event.Turn == lastTurn {
			failed = append(failed, (&openEstimate{id: one.Event.ID}).settle(r.log, session.Event{Turn: r.turn}, outcome, took, split))
		}
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
	return appendSettled(o.state, historyRun{Project: log.Header().Project, Model: o.body.Model, Chars: o.body.PromptChars, TookMS: took.Milliseconds(),
		Split: true, TestMS: split.TestMS, PermissionMS: split.PermissionMS, TestCalls: split.TestCalls})
}

func splitOf(rows []Row) session.EstimateSplit {
	var split session.EstimateSplit
	for _, row := range rows {
		for _, step := range row.Steps {
			split.ModelMS += step.DurationMS
			for _, call := range step.ToolCalls {
				split.PermissionMS += call.PermissionWaitMS
				ran := max(call.DurationMS-call.PermissionWaitMS, 0)
				if runsACheck(call) {
					split.TestMS, split.TestCalls = split.TestMS+ran, split.TestCalls+1
				} else {
					split.ToolsMS += ran
				}
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
