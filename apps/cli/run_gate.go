package main

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"strings"

	"tofu/apps/cli/wiring"
	"tofu/internal/agent/loop"
	"tofu/internal/host"
	"tofu/internal/judge/gate"
	"tofu/internal/judge/ledger"
	"tofu/internal/judge/state"
	"tofu/internal/models/classifiers"
	"tofu/internal/models/classifiers/core"
	"tofu/internal/sys"
	"tofu/library/questions"
)

const (
	runGatePoint       = questions.ToolGatePin
	judgeEndpointEnvar = classifiers.EndpointVariable
)

type toolGate struct {
	client    *core.Client
	set       wiring.Battery
	cwd       string
	rows      wiring.RowLedger
	watch     func(ctx context.Context, tool string, decision loop.GateDecision, err error)
	decisions int
	costUSD   float64
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
	client, err := wiring.JevClientFrom(keys, wiring.OneCallAtATime)
	if err != nil {
		return nil, err
	}
	rows, err := wiring.OpenRowLedger(log)
	if err != nil {
		return nil, err
	}
	return &toolGate{client: client, set: set, cwd: dir, rows: rows}, nil
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
	resolution, err := wiring.ResolveRuleMode(pol)
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

func (g *toolGate) Decide(ctx context.Context, request loop.GateRequest) (loop.GateDecision, error) {
	row, err := g.ask(ctx, request)
	decision := loop.GateDecision{ID: row.ID, Verdict: row.Verdict, Answers: row.Answers, Reason: row.Reason,
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
	return decision, err
}

func (g *toolGate) ask(ctx context.Context, request loop.GateRequest) (ledger.Row, error) {
	var input map[string]any
	if err := json.Unmarshal(request.Args, &input); err != nil {
		return ledger.Row{}, fmt.Errorf("the %s call carries arguments the gate cannot read: %w", request.Tool, err)
	}
	call := state.ToolGateInput{
		Agent:      "tofu-run",
		Tool:       request.Tool,
		Input:      input,
		Cwd:        g.cwd,
		ProjectDir: g.cwd,
		Context:    state.ToolGateContext{UserRecentMessages: []string{request.Task}},
	}
	if place, found := sys.ScratchOf(ctx); found {
		call.ScratchDir = place.Dir()
	}
	built, builder, err := state.BuildToolGateV3(call)
	if err != nil {
		return ledger.Row{}, err
	}
	written := wiring.RowInput{TurnID: request.TurnID, StateBuilder: builder, Fingerprint: state.FingerprintOf(call)}
	builtState := json.RawMessage(built)
	if own := loop.OwnShellsCalled(ctx, request); len(own) > 0 {
		return g.rows.AppendOwnShell(builtState, g.set, written, own)
	}
	decision, err := g.client.Ask(ctx, core.Request{State: builtState, Questions: g.set.Questions})
	g.decisions++
	if err != nil {
		row, writeErr := g.rows.AppendFallback(builtState, g.set, written, err)
		if writeErr != nil {
			return row, writeErr
		}
		return row, jevUnavailable{err}
	}
	written.Decision = &decision
	written.Answers = wiring.ToLedgerAnswers(g.set.QuestionsVersion, decision.Answers)
	row, err := g.rows.Append(builtState, g.set, written)
	g.costUSD += row.Charge.USD
	return row, err
}
