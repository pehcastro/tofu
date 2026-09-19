package main

import (
	"context"
	"encoding/json"
	"fmt"
	"os"
	"strings"
	"time"

	catalogpolicy "boji/catalog/policy"
	"boji/catalog/questions"
	"boji/internal/judge/jev"
	jevwire "boji/internal/judge/jev/wire/openrouter"
	"boji/internal/judge/ledger"
	"boji/internal/judge/policy"
	"boji/internal/judge/question"
	"boji/internal/judge/state"
	"boji/internal/konst"
	"boji/internal/sys"
	"boji/internal/transport"
	"boji/internal/turn"
)

const (
	runGatePoint       = "tool_gate@1"
	judgeEndpointEnvar = "BOJI_JUDGE_ENDPOINT"
)

type toolGate struct {
	client    *jev.Client
	set       battery
	cwd       string
	decisions int
	costUSD   float64
}

func newToolGate(dir string) (*toolGate, error) {
	set, err := resolveCatalog(runGatePoint)
	if err != nil {
		return nil, err
	}
	pol, origin, err := gatePolicy()
	if err != nil {
		return nil, err
	}
	resolution, err := resolvePolicyMode(pol)
	if err != nil {
		return nil, err
	}
	set.Policy, set.Mode = &pol, resolution.Mode
	set.ModeReason = fmt.Sprintf("the policy came from %s as %s", origin, pol.File)
	if resolution.Reason != "" && !strings.Contains(resolution.Reason, pol.File) {
		set.ModeReason += "; " + resolution.Reason
	}

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
	client, err := jev.NewClient(jev.Config{Wire: wire})
	if err != nil {
		return nil, err
	}
	return &toolGate{client: client, set: set, cwd: dir}, nil
}

func gatePolicy() (policy.Policy, policy.Origin, error) {
	layers, err := question.DefaultLayers(questions.Files())
	if err != nil {
		return policy.Policy{}, "", err
	}
	set, _, err := question.Resolve(runGatePoint, layers)
	if err != nil {
		return policy.Policy{}, "", err
	}
	return policy.LoadPoint(catalogpolicy.Files(), runGatePoint, set)
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
	if err != nil {
		return turn.GateDecision{Verdict: string(ledger.VerdictAsk)}, err
	}
	return turn.GateDecision{ID: row.ID, Verdict: string(row.Verdict)}, nil
}

func (g *toolGate) ask(ctx context.Context, request turn.GateRequest) (ledger.Row, error) {
	var input map[string]any
	if err := json.Unmarshal(request.Args, &input); err != nil {
		return ledger.Row{}, fmt.Errorf("the %s call carries arguments the gate cannot read: %w", request.Tool, err)
	}
	built, builder, err := state.BuildToolGate(state.ToolGateInput{
		Agent:   "boji-run",
		Tool:    request.Tool,
		Input:   input,
		Cwd:     g.cwd,
		Context: state.ToolGateContext{UserRecentMessages: []string{request.Task}},
	})
	if err != nil {
		return ledger.Row{}, err
	}
	builtState := json.RawMessage(built)
	decision, err := g.client.Ask(ctx, jev.Request{State: builtState, Questions: g.set.Questions})
	g.decisions++
	if err != nil {
		return appendFallbackRow(builtState, g.set, rowInput{turnID: request.TurnID, stateBuilder: builder}, err)
	}
	g.costUSD += decision.Usage.Cost
	return appendRow(builtState, g.set, rowInput{
		decision:     &decision,
		answers:      toLedgerAnswers(g.set.QuestionsVersion, decision.Answers),
		turnID:       request.TurnID,
		stateBuilder: builder,
	})
}
