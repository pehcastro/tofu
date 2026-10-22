package main

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"strings"

	"tofu/internal/classifier"
	"tofu/internal/host"
	"tofu/internal/judge/gate"
	"tofu/internal/judge/jev"
	"tofu/internal/judge/ledger"
	"tofu/internal/judge/state"
	"tofu/internal/llm/models"
	"tofu/internal/sys"
	"tofu/internal/turn"
)

const oneCallAtATime = 1

const (
	runGatePoint       = "tool_gate@3"
	judgeEndpointEnvar = classifier.EndpointVariable
)

type toolGate struct {
	client    *jev.Client
	set       battery
	cwd       string
	rows      rowLedger
	watch     func(ctx context.Context, tool string, decision turn.GateDecision, err error)
	decisions int
	costUSD   float64
}

type jevUnavailable struct{ cause error }

func (u jevUnavailable) Error() string { return "jev could not judge the call: " + u.cause.Error() }

type unusableRule struct{ err error }

func (u unusableRule) Error() string { return u.err.Error() }

func (u unusableRule) Unwrap() error { return u.err }

func newToolGate(dir, log string) (*toolGate, error) {
	return toolGateOn(dir, log, jev.ReadKeyring())
}

func toolGateOn(dir, log string, keys jev.Keyring) (*toolGate, error) {
	set, err := resolvePoint(runGatePoint, dir)
	if err != nil {
		return nil, unusableRule{err}
	}
	client, err := jevClientFrom(keys, oneCallAtATime)
	if err != nil {
		return nil, err
	}
	rows, err := openRowLedger(log)
	if err != nil {
		return nil, err
	}
	return &toolGate{client: client, set: set, cwd: dir, rows: rows}, nil
}

func resolvePoint(point, dir string) (battery, error) {
	set, err := resolveLibrary(point, dir)
	if err != nil {
		return battery{}, err
	}
	pol, origin, err := loadRulePoint(point, dir)
	if err != nil {
		return battery{}, err
	}
	if err := thresholdsInRange(pol, set); err != nil {
		return battery{}, err
	}
	resolution, err := resolveRuleMode(pol)
	if err != nil {
		return battery{}, err
	}
	pol = resolution.Rule
	set.Rule, set.Mode = &pol, resolution.Mode
	set.ModeReason = fmt.Sprintf("the rule came from %s as %s", origin, pol.File)
	if resolution.Reason != "" && !strings.Contains(resolution.Reason, pol.File) {
		set.ModeReason += "; " + resolution.Reason
	}
	return set, nil
}

func thresholdsInRange(r gate.Rule, set battery) error {
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

func newJevClient() (*jev.Client, error) {
	return jevClientFrom(jev.ReadKeyring(), oneCallAtATime)
}

func jevClientFrom(keys jev.Keyring, concurrency int) (*jev.Client, error) {
	return classifier.Open(".", keys, concurrency)
}

func boundClassifier() (models.Model, error) {
	return classifier.Bound(".", jev.ReadKeyring())
}

func jevClientOn(key string, concurrency int) (*jev.Client, error) {
	model, err := boundClassifier()
	if err != nil {
		return nil, err
	}
	return classifier.Route{Model: model, Key: key}.Client(concurrency)
}

func gateKey() (string, error) {
	return gateKeyFrom(jev.ReadKeyring())
}

func gateKeyFrom(keys jev.Keyring) (string, error) {
	route, err := classifier.Resolve(".", keys)
	return route.Key, err
}

func (g *toolGate) Decide(ctx context.Context, request turn.GateRequest) (turn.GateDecision, error) {
	row, err := g.ask(ctx, request)
	decision := turn.GateDecision{ID: row.ID, Verdict: row.Verdict, Answers: row.Answers, Reason: row.Reason}
	var unavailable jevUnavailable
	switch {
	case errors.As(err, &unavailable):
		decision.Failure, err = unavailable.Error(), nil
	case err != nil:
		decision = turn.GateDecision{Verdict: ledger.VerdictAsk}
	}
	if decision.Verdict != ledger.VerdictDeny && turn.PersonOnly(g.cwd, request) {
		decision.Verdict, decision.PersonOnly = ledger.VerdictAsk, true
	}
	if g.watch != nil {
		g.watch(host.JudgingCall(ctx, request.Call), request.Tool, decision, err)
	}
	return decision, err
}

func (g *toolGate) ask(ctx context.Context, request turn.GateRequest) (ledger.Row, error) {
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
	written := rowInput{turnID: request.TurnID, stateBuilder: builder, fingerprint: state.FingerprintOf(call)}
	builtState := json.RawMessage(built)
	if own := turn.OwnShellsCalled(ctx, request); len(own) > 0 {
		return g.rows.appendOwnShell(builtState, g.set, written, own)
	}
	decision, err := g.client.Ask(ctx, jev.Request{State: builtState, Questions: g.set.Questions})
	g.decisions++
	if err != nil {
		row, writeErr := g.rows.appendFallback(builtState, g.set, written, err)
		if writeErr != nil {
			return row, writeErr
		}
		return row, jevUnavailable{err}
	}
	g.costUSD += decision.Usage.Cost
	written.decision = &decision
	written.answers = toLedgerAnswers(g.set.QuestionsVersion, decision.Answers)
	return g.rows.append(builtState, g.set, written)
}

func (l rowLedger) appendOwnShell(state json.RawMessage, set battery, in rowInput, own []string) (ledger.Row, error) {
	row, err := l.skeleton(state, set, in)
	if err != nil {
		return ledger.Row{}, err
	}
	sentence := "the call acts on " + strings.Join(own, ", ") + ", which tofu started, so it is allowed without asking jev, and a kill runs as shell stop"
	row.Model, row.Answers, row.Verdict = "", []ledger.Answer{}, ledger.VerdictAllow
	row.Reason = &ledger.Reason{Mode: set.Mode.Ledger(), ModeReason: &sentence}
	if set.Rule != nil {
		row.Policy, row.PolicyVersion = set.Rule.Name, set.Rule.RuleVersion
	}
	return l.writer.Append(row)
}
