package loop

import (
	"cmp"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"math"
	"os"
	"path/filepath"
	"runtime"
	"slices"
	"strconv"
	"strings"
	"sync"
	"sync/atomic"
	"time"
	"unicode/utf8"

	"tofu/internal/agent/subagents"
	"tofu/internal/konst"
	"tofu/internal/models/llm/core"
	"tofu/internal/store/session"
	"tofu/internal/tools/contract"
)

type personWaitKey struct{}

func withPersonWait(ctx context.Context) (context.Context, *atomic.Int64) {
	waited := new(atomic.Int64)
	return context.WithValue(ctx, personWaitKey{}, waited), waited
}

func PersonWaited(ctx context.Context, took time.Duration) {
	if waited, timed := ctx.Value(personWaitKey{}).(*atomic.Int64); timed {
		waited.Add(took.Milliseconds())
	}
}

type estimateFor struct {
	scope session.EstimateScope
	agent string
	model string
	task  string
}

type historyRun struct {
	chars  int
	tookMS int64
	split  *session.EstimateSplit
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
	here := func(header session.Header) bool { return header.Project == "" || header.Project == body.Project }
	runs, err := historyIn(c.Sessions, here, sameModel)
	if err != nil {
		return body, err
	}
	body.Basis = session.EstimateFromHistory
	if len(runs) == 0 {
		places, err := c.ResolvedPlaces()
		if err != nil {
			return body, err
		}
		if runs, err = historyElsewhere(places.State, func(header session.Header) bool { return !here(header) }, sameModel); err != nil {
			return body, err
		}
		body.Basis = session.EstimateFirstRun
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

func saidFrom(runs []historyRun, chars int) (int64, int64, string) {
	distance := func(run historyRun) float64 {
		return math.Abs(math.Log1p(float64(run.chars)) - math.Log1p(float64(chars)))
	}
	near := slices.SortedStableFunc(slices.Values(runs), func(a, b historyRun) int { return cmp.Compare(distance(a), distance(b)) })[:(len(runs)+1)/2]
	agent, counts := make([]float64, len(near)), []float64(nil)
	for i, run := range near {
		agent[i] = float64(run.tookMS)
		if run.split != nil {
			agent[i] -= float64(run.split.TestMS + run.split.PermissionMS)
			counts = append(counts, float64(run.split.TestCalls))
		}
	}
	var testMS, calls int64
	for _, run := range runs {
		if run.split != nil {
			testMS, calls = testMS+run.split.TestMS, calls+int64(run.split.TestCalls)
		}
	}
	if calls == 0 || len(counts) == 0 {
		return int64(median(agent)), 0, noTestsRecorded
	}
	each, count := testMS/calls, median(counts)
	return int64(median(agent)), int64(float64(each) * count), fmt.Sprintf("%d earlier test and build calls in this history, %s each, %s a run",
		calls, roundedMS(each), strconv.FormatFloat(count, 'f', -1, 64))
}

func historyElsewhere(state string, keep func(session.Header) bool, model func(string) bool) ([]historyRun, error) {
	root := filepath.Dir(state)
	entries, err := os.ReadDir(root)
	if err != nil {
		return nil, err
	}
	type project struct {
		state   string
		changed time.Time
	}
	var projects []project
	for _, entry := range entries {
		dir := filepath.Join(root, entry.Name())
		if sessions, err := os.Stat(session.SessionsDir(dir)); err == nil && entry.IsDir() && dir != filepath.Clean(state) {
			projects = append(projects, project{state: dir, changed: sessions.ModTime()})
		}
	}
	slices.SortFunc(projects, func(a, b project) int { return b.changed.Compare(a.changed) })
	var runs []historyRun
	for _, other := range projects {
		found, err := historyIn(session.OpenAt(other.state), keep, model)
		if err != nil {
			return nil, err
		}
		if runs = append(runs, found...); len(runs) >= konst.SpawnEstimateRunsMost {
			break
		}
	}
	return runs[:min(len(runs), konst.SpawnEstimateRunsMost)], nil
}

func historyIn(store *session.Store, keep func(session.Header) bool, model func(string) bool) ([]historyRun, error) {
	once := store.ReadOnce()
	listing, err := once.Listing()
	if err != nil {
		return nil, err
	}
	headers := slices.DeleteFunc(listing.Sessions, func(header session.Header) bool {
		return !keep(header) || !model(header.Model) && !slices.ContainsFunc(header.Models, model) &&
			!slices.ContainsFunc(header.Agents, func(run session.AgentRun) bool { return model(run.Model) })
	})
	slices.SortFunc(headers, func(a, b session.Header) int { return b.At.Compare(a.At) })
	var runs []historyRun
	for read, wide := 0, 1; read < len(headers) && len(runs) < konst.SpawnEstimateRunsMost; read, wide = read+wide, min(wide*2, runtime.GOMAXPROCS(0)) {
		window := headers[read:min(read+wide, len(headers))]
		found, failed := make([][]historyRun, len(window)), make([]error, len(window))
		var reading sync.WaitGroup
		for at, header := range window {
			reading.Go(func() { found[at], failed[at] = runsIn(once, header, model) })
		}
		reading.Wait()
		if err := errors.Join(failed...); err != nil {
			return nil, err
		}
		runs = append(runs, slices.Concat(found...)...)
	}
	return runs[:min(len(runs), konst.SpawnEstimateRunsMost)], nil
}

func runsIn(store *session.Store, header session.Header, model func(string) bool) ([]historyRun, error) {
	events, err := store.EventsOf(header.ID, session.EventTurnEnd, session.EventToolCall, session.EventEstimate, session.EventEstimateSettled)
	if err != nil {
		return nil, err
	}
	estimated, err := session.Estimates(events)
	if err != nil {
		return nil, err
	}
	var runs []historyRun
	counted := map[string]bool{}
	for _, one := range estimated {
		if one.Settled != nil && one.Settled.Outcome == session.EstimateDone && model(one.Model) && one.Scope != session.EstimateCall {
			runs = append(runs, historyRun{chars: one.PromptChars, tookMS: one.Settled.TookMS, split: &one.Settled.EstimateSplit})
			counted[cmp.Or(one.Agent, one.Event.Turn)] = true
		}
	}
	briefs := map[string]int{}
	for _, event := range events {
		switch event.Kind {
		case session.EventToolCall:
			var call session.CallBody
			var args spawnArgs
			if json.Unmarshal(event.Body, &call) == nil && call.Tool == SpawnToolName && json.Unmarshal(call.Args, &args) == nil {
				briefs[event.Call] = utf8.RuneCountInString(args.Task)
			}
		case session.EventTurnEnd:
			var ended struct {
				Task        string          `json:"task"`
				Model       string          `json:"model"`
				Outcome     session.Outcome `json:"outcome"`
				WallClockMS int64           `json:"wall_clock_ms"`
			}
			if event.Agent == "" && !counted[event.Turn] && json.Unmarshal(event.Body, &ended) == nil && ended.Outcome == session.OutcomeStopped && model(ended.Model) {
				runs = append(runs, historyRun{chars: utf8.RuneCountInString(ended.Task), tookMS: ended.WallClockMS})
			}
		}
	}
	for _, run := range header.Agents {
		if !counted[run.Agent] && run.EndedAt != nil && run.Status == subagents.Finished.String() && model(run.Model) {
			runs = append(runs, historyRun{chars: briefs[run.SpawnCall], tookMS: run.EndedAt.Sub(run.StartedAt).Milliseconds()})
		}
	}
	return runs, nil
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
	id   string
	at   time.Time
	body session.EstimateBody
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
	return &openEstimate{id: r.add(session.Event{At: at, Kind: session.EventEstimate}, body).ID, at: at, body: body}
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
	return err
}

func splitOf(rows []Row) session.EstimateSplit {
	var split session.EstimateSplit
	for _, row := range rows {
		for _, step := range row.Steps {
			split.ModelMS += step.DurationMS
			for _, call := range step.ToolCalls {
				split.PermissionMS += call.PermissionWaitMS
				if runsACheck(call) {
					split.TestMS, split.TestCalls = split.TestMS+call.DurationMS, split.TestCalls+1
				} else {
					split.ToolsMS += call.DurationMS
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
