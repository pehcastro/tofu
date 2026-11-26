package main

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"slices"
	"strconv"
	"strings"
	"sync"
	"time"

	"tofu/apps/cli/wiring"
	"tofu/internal/agent/loop"
	"tofu/internal/host"
	"tofu/internal/judge/gate"
	"tofu/internal/judge/ledger"
	toolgate "tofu/internal/judge/points/tool_gate"
	"tofu/internal/konst"
	"tofu/internal/models/classifiers"
	"tofu/internal/models/classifiers/core"
	"tofu/internal/models/llm/catalog"
	"tofu/internal/models/transport"
	platformproject "tofu/internal/platform/project"
	"tofu/internal/platform/scratch"
	"tofu/internal/tools/contract"
	"tofu/library/questions"
)

const (
	runGatePoint       = questions.ToolGatePin
	judgeEndpointEnvar = classifiers.EndpointVariable
)

type toolGate struct {
	classifier wiring.Classifier
	set        wiring.Battery
	cwd        string
	rows       wiring.RowLedger
	watch      func(ctx context.Context, tool string, decision loop.GateDecision, err error)
	decisions  int
	costUSD    float64
	allowing   sync.Mutex
	allowed    map[string]string
}

type jevUnavailable struct{ cause error }

func (u jevUnavailable) Error() string { return "jev could not judge the call: " + u.cause.Error() }

type unusableRule struct{ err error }

func (u unusableRule) Error() string { return u.err.Error() }

func (u unusableRule) Unwrap() error { return u.err }

func toolGateOn(dir, log string, keys core.Keyring) (*toolGate, error) {
	set, err := resolvePoint(runGatePoint, dir)
	if err != nil {
		return nil, unusableRule{err}
	}
	classifier, err := wiring.OpenBound(keys, konst.TurnParallelToolCalls)
	if err != nil {
		return nil, err
	}
	rows, err := wiring.OpenRowLedger(log)
	if err != nil {
		return nil, err
	}
	return &toolGate{classifier: classifier, set: set, cwd: dir, rows: rows, allowed: map[string]string{}}, nil
}

func resolvePoint(point, dir string) (wiring.Battery, error) {
	set, err := wiring.ResolveLibrary(point, dir)
	if err != nil {
		return wiring.Battery{}, err
	}
	pol, origin, err := wiring.LoadRulePoint(point, dir)
	if err != nil {
		return wiring.Battery{}, err
	}
	if err := thresholdsInRange(pol, set); err != nil {
		return wiring.Battery{}, err
	}
	places, err := platformproject.ResolvePlaces(".")
	if err != nil {
		return wiring.Battery{}, err
	}
	resolution, err := wiring.ResolveRuleMode(places, pol)
	if err != nil {
		return wiring.Battery{}, err
	}
	pol = resolution.Rule
	set.Rule, set.Mode = &pol, resolution.Mode
	set.ModeReason = fmt.Sprintf("the rule came from %s as %s", origin, pol.File)
	if resolution.Reason != "" && !strings.Contains(resolution.Reason, pol.File) {
		set.ModeReason += "; " + resolution.Reason
	}
	return set, nil
}

func thresholdsInRange(r gate.Rule, set wiring.Battery) error {
	topRisk := 0.0
	for _, q := range set.Questions {
		if q.ID == r.RiskQuestion {
			topRisk = float64(len(q.Levels) - 1)
		}
	}
	for _, field := range []struct {
		name  string
		value float64
		top   float64
	}{
		{"risk_ask_at", r.Thresholds.RiskAskAt, topRisk},
		{"risk_deny_at", r.Thresholds.RiskDenyAt, topRisk},
		{"user_requested_relax_at", r.Thresholds.UserRequestedRelaxAt, 1},
		{"approval_relax_at", r.Thresholds.ApprovalRelaxAt, 1},
		{"from_untrusted_block_at", r.Thresholds.FromUntrustedBlockAt, 1},
	} {
		if field.value < 0 || field.value > field.top {
			return fmt.Errorf("%s: thresholds.%s is %g and it reads between 0 and %g", r.File, field.name, field.value, field.top)
		}
	}
	return nil
}

func (g *toolGate) Decide(ctx context.Context, request loop.GateRequest) (decision loop.GateDecision, err error) {
	g.allowing.Lock()
	by, allowed := g.allowed[allowanceOf(request)]
	g.allowing.Unlock()
	cited := loop.CitedAllowance(request.Args)
	if !allowed && cited != "" && cited != loop.AskThePerson {
		if err := citedByThePerson(ctx, request, cited); err != nil {
			return loop.GateDecision{}, err
		}
		by, allowed = "the person's own message \""+cited+"\"", true
	}
	if allowed {
		decision, err = g.allow(request, by, wiring.RowInput{TurnID: request.TurnID}, loop.YesThisCall)
		if g.watch != nil {
			g.watch(host.JudgingCall(ctx, request.Call), request.Tool, decision, err)
		}
		return decision, err
	}
	write := g.ask(ctx, request)
	loop.InCallOrder(ctx, func() {
		var row ledger.Row
		row, err = write()
		decision = loop.GateDecision{ID: row.ID, Verdict: row.Verdict, Answers: row.Answers, Reason: row.Reason,
			Provider: row.Provider, Model: row.Build, DurationMS: row.LatencyMS}
		var unavailable jevUnavailable
		switch {
		case errors.As(err, &unavailable):
			decision.Failure, err = unavailable.Error(), nil
		case err != nil:
			decision = loop.GateDecision{Verdict: ledger.VerdictAsk}
		}
		if decision.Verdict != ledger.VerdictDeny && loop.PersonOnly(g.cwd, request) {
			decision.Verdict, decision.PersonOnly = ledger.VerdictAsk, true
		}
		if g.watch != nil {
			g.watch(host.JudgingCall(ctx, request.Call), request.Tool, decision, err)
		}
	})
	return decision, err
}

func (g *toolGate) AllowedByPerson(request loop.GateRequest, yes loop.PersonYes) (loop.GateDecision, error) {
	by := "the person, who answered \"" + yes.Scope.Label() + "\" to question " + yes.Question
	if yes.Scope != loop.YesThisCall {
		by += ", for every " + loop.CallKind(request) + " call"
	}
	return g.allow(request, by, wiring.RowInput{TurnID: request.TurnID, Session: yes.Session}, yes.Scope)
}

func (g *toolGate) allow(request loop.GateRequest, by string, in wiring.RowInput, scope loop.YesScope) (loop.GateDecision, error) {
	g.allowing.Lock()
	g.allowed[allowanceOf(request)] = by
	g.allowing.Unlock()
	row, err := g.rows.AppendPersonAllowed(request.Args, g.set, in, by, scope)
	return loop.GateDecision{ID: row.ID, Verdict: row.Verdict, Reason: row.Reason}, err
}

func (g *toolGate) AllowedBefore(request loop.GateRequest, session string) (loop.GateDecision, bool, error) {
	yeses, err := g.rows.PersonYeses(g.set.SetName, session)
	if err != nil {
		return loop.GateDecision{}, false, err
	}
	exact, kind := allowanceOf(request), loop.CallKind(request)
	lasting := konst.GateKindYesHours * time.Hour
	covering := slices.IndexFunc(yeses, func(yes wiring.GrantedYes) bool {
		earlier := loop.GateRequest{Tool: request.Tool, Args: yes.State}
		switch yes.Scope {
		case loop.YesThisCall:
			return allowanceOf(earlier) == exact
		case loop.YesKindForHours:
			return kind != "" && loop.CallKind(earlier) == kind && time.Since(yes.At) < lasting
		case loop.YesKindForSession:
			return kind != "" && loop.CallKind(earlier) == kind
		}
		panic("tofu: unknown yes scope")
	})
	if covering < 0 {
		return loop.GateDecision{}, false, nil
	}
	yes := yeses[covering]
	by := "an earlier yes: " + yes.By
	if yes.Scope == loop.YesKindForHours {
		by += ", until " + yes.At.Add(lasting).Local().Format("15:04")
	}
	row, err := g.rows.AppendCoveredByYes(request.Args, g.set, wiring.RowInput{TurnID: request.TurnID, Session: session}, by)
	return loop.GateDecision{ID: row.ID, Verdict: row.Verdict, Reason: row.Reason}, err == nil, err
}

type citationRefused struct{ why string }

func (c citationRefused) Error() string {
	return "the call cites " + loop.AllowedByField + " and " + c.why + ", so nothing allows it: ask the person instead"
}

func citedByThePerson(ctx context.Context, request loop.GateRequest, cited string) error {
	switch {
	case contract.SubAgentAsking(ctx) != "":
		return citationRefused{"a sub-agent's call never runs on the person's word"}
	case len(request.Said) == 0 || request.Said[len(request.Said)-1] != cited:
		return citationRefused{"those words are not the person's own newest typed message in this session"}
	case !loop.RegexArmAllows(request):
		return citationRefused{"the call is one the gate always refuses"}
	}
	return nil
}

func allowanceOf(request loop.GateRequest) string {
	var args map[string]any
	if json.Unmarshal(request.Args, &args) != nil {
		return request.Tool + " " + string(request.Args)
	}
	delete(args, loop.AllowedByField)
	same, _ := json.Marshal(args)
	return request.Tool + " " + string(same)
}

func (g *toolGate) ask(ctx context.Context, request loop.GateRequest) func() (ledger.Row, error) {
	var input map[string]any
	if err := json.Unmarshal(request.Args, &input); err != nil {
		return func() (ledger.Row, error) {
			return ledger.Row{}, fmt.Errorf("the %s call carries arguments the gate cannot read: %w", request.Tool, err)
		}
	}
	call := toolgate.ToolGateInput{
		Agent:      "tofu-run",
		Tool:       request.Tool,
		Input:      input,
		Cwd:        g.cwd,
		ProjectDir: g.cwd,
		Context:    toolgate.ToolGateContext{UserRecentMessages: request.Said},
	}
	if place, found := scratch.ScratchOf(ctx); found {
		call.ScratchDir = place.Dir()
	}
	built, builder, err := g.fitted(call)
	if err != nil {
		return func() (ledger.Row, error) { return ledger.Row{}, err }
	}
	written := wiring.RowInput{TurnID: request.TurnID, StateBuilder: builder, Fingerprint: toolgate.FingerprintOf(call)}
	builtState := json.RawMessage(built)
	if own := loop.OwnShellsCalled(ctx, request); len(own) > 0 {
		return func() (ledger.Row, error) { return g.rows.AppendOwnShell(builtState, g.set, written, own) }
	}
	chain := host.JudgeChainOf(ctx)
	provider := string(g.classifier.Model.Provider)
	failed := chain.Skipped(provider)
	if failed == nil {
		var decision core.Decision
		decision, failed = g.classifier.Client.Ask(ctx, core.Request{State: builtState, Questions: g.set.Questions})
		chain.Tried(provider, failed)
		if failed == nil {
			written.Decision = &decision
			written.Answers = wiring.ToLedgerAnswers(g.set.QuestionsVersion, decision.Answers)
			return g.written(builtState, written, func() { chain.JudgedByClassifier(g.classifier.Model.Slug()) })
		}
	}
	cheap, err := wiring.CheapModelOf(g.cwd, chain.Source())
	var judged wiring.ModelJudged
	if err == nil {
		judged, written.Answers, err = g.judgeOnModel(ctx, chain, cheap, builtState)
	}
	if err == nil {
		judged.Why = "judged by " + judged.Slug + " because the classifier failed: " + provider + " " + failureOf(failed)
		written.Judged = &judged
		return g.written(builtState, written, func() { chain.JudgedByModel(judged.Slug) })
	}
	return func() (ledger.Row, error) {
		g.decisions++
		chain.JudgedByRegex(err)
		row, writeErr := g.rows.AppendFallback(builtState, g.set, written, failed)
		if writeErr != nil {
			return row, writeErr
		}
		return row, jevUnavailable{failed}
	}
}

func (g *toolGate) fitted(call toolgate.ToolGateInput) ([]byte, string, error) {
	room := g.classifier.Client.Caps().MaxRequestBytes
	for {
		built, builder, err := toolgate.BuildToolGateV3(call)
		if err != nil {
			return nil, "", err
		}
		body, err := core.Request{State: json.RawMessage(built), Questions: g.set.Questions}.Encode(g.classifier.Client.Model())
		if err != nil || room <= 0 || len(body) <= room {
			return built, builder, err
		}
		var shortened bool
		if call, shortened = toolgate.ShortenLongest(call, len(body)-room); !shortened {
			return nil, "", fmt.Errorf("the %s call alone is %d bytes over what %s accepts", call.Tool, len(body)-room, g.classifier.Model.Slug())
		}
	}
}

func (g *toolGate) written(state json.RawMessage, in wiring.RowInput, judged func()) func() (ledger.Row, error) {
	return func() (ledger.Row, error) {
		g.decisions++
		judged()
		row, err := g.rows.Append(state, g.set, in)
		g.costUSD += row.Charge.USD
		return row, err
	}
}

func (g *toolGate) judgeOnModel(ctx context.Context, chain *host.JudgeChain, cheap catalog.Model, state json.RawMessage) (wiring.ModelJudged, []ledger.Answer, error) {
	model, err := chain.Open(ctx, cheap.Slug())
	if err != nil {
		return wiring.ModelJudged{}, nil, err
	}
	started := time.Now()
	answers, decision, err := wiring.JudgeOnModel(ctx, model, state, g.set.Questions)
	if err != nil {
		return wiring.ModelJudged{}, nil, fmt.Errorf("%s could not judge: %w", cheap.Slug(), err)
	}
	judged := wiring.ModelJudged{Slug: cheap.Slug(), Source: chain.Source(), Build: decision.Build, Latency: time.Since(started), CostUSD: decision.Usage.Cost}
	return judged, wiring.ToLedgerAnswers(g.set.QuestionsVersion, answers), nil
}

func failureOf(err error) string {
	var refused *transport.Error
	if errors.As(err, &refused) && refused.Status != 0 {
		return refused.Kind.String() + " " + strconv.Itoa(refused.Status)
	}
	return transport.KindOf(err).String()
}
