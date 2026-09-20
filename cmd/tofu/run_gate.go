package main

import (
	"context"
	"encoding/json"
	"fmt"
	"os"
	"strings"
	"time"

	"tofu/internal/judge/jev"
	jevwire "tofu/internal/judge/jev/wire/openrouter"
	"tofu/internal/judge/ledger"
	"tofu/internal/judge/state"
	"tofu/internal/konst"
	"tofu/internal/sys"
	"tofu/internal/transport"
	"tofu/internal/turn"
)

const (
	runGatePoint       = "tool_gate@3"
	judgeEndpointEnvar = "TOFU_JUDGE_ENDPOINT"
)

type toolGate struct {
	client    *jev.Client
	set       battery
	cwd       string
	watch     func(tool string, decision turn.GateDecision, err error)
	decisions int
	costUSD   float64
}

func newToolGate(dir string) (*toolGate, error) {
	set, err := resolvePoint(runGatePoint)
	if err != nil {
		return nil, err
	}
	client, err := newJevClient()
	if err != nil {
		return nil, err
	}
	return &toolGate{client: client, set: set, cwd: dir}, nil
}

func resolvePoint(point string) (battery, error) {
	set, err := resolveCatalog(point)
	if err != nil {
		return battery{}, err
	}
	pol, origin, err := loadPolicyPoint(point)
	if err != nil {
		return battery{}, err
	}
	resolution, err := resolvePolicyMode(pol)
	if err != nil {
		return battery{}, err
	}
	pol = resolution.Policy
	set.Policy, set.Mode = &pol, resolution.Mode
	set.ModeReason = fmt.Sprintf("the policy came from %s as %s", origin, pol.File)
	if resolution.Reason != "" && !strings.Contains(resolution.Reason, pol.File) {
		set.ModeReason += "; " + resolution.Reason
	}
	return set, nil
}

func newJevClient() (*jev.Client, error) {
	key, err := gateKey()
	if err != nil {
		return nil, err
	}
	wire, err := jevwire.New(jevwire.Config{
		Key:      key,
		Endpoint: os.Getenv(judgeEndpointEnvar),
		Transport: transport.Config{
			AttemptTimeout: time.Duration(konst.JudgeTimeoutMillis) * time.Millisecond,
			Retries:        konst.JudgeRetries,
			Backoff:        time.Duration(konst.JudgeBackoffMillis) * time.Millisecond,
			Concurrency:    1,
		},
	})
	if err != nil {
		return nil, err
	}
	return jev.NewClient(jev.Config{Wire: wire})
}

func gateKey() (string, error) {
	key, err := jev.Key(".env")
	if err == nil {
		return key, nil
	}
	home, homeErr := sys.HomeConfigDir()
	if homeErr != nil {
		return "", err
	}
	return jev.Key(sys.Join(home, ".env"))
}

func (g *toolGate) Decide(ctx context.Context, request turn.GateRequest) (turn.GateDecision, error) {
	row, err := g.ask(ctx, request)
	decision := turn.GateDecision{ID: row.ID, Verdict: row.Verdict, Answers: row.Answers, Reason: row.Reason}
	if err != nil {
		decision = turn.GateDecision{Verdict: ledger.VerdictAsk}
	}
	if g.watch != nil {
		g.watch(request.Tool, decision, err)
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
	built, builder, err := state.BuildToolGateV3(call)
	if err != nil {
		return ledger.Row{}, err
	}
	written := rowInput{turnID: request.TurnID, stateBuilder: builder, fingerprint: state.FingerprintOf(call)}
	builtState := json.RawMessage(built)
	decision, err := g.client.Ask(ctx, jev.Request{State: builtState, Questions: g.set.Questions})
	g.decisions++
	if err != nil {
		return appendFallbackRow(builtState, g.set, written, err)
	}
	g.costUSD += decision.Usage.Cost
	written.decision = &decision
	written.answers = toLedgerAnswers(g.set.QuestionsVersion, decision.Answers)
	return appendRow(builtState, g.set, written)
}
