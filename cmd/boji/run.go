package main

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"path/filepath"
	"strconv"
	"strings"
	"time"

	"boji/internal/judge/jev"
	jevwire "boji/internal/judge/jev/wire/openrouter"
	"boji/internal/judge/ledger"
	"boji/internal/judge/state"
	"boji/internal/konst"
	"boji/internal/llm"
	"boji/internal/llm/wire/openrouter"
	"boji/internal/sys"
	"boji/internal/transport"
	"boji/internal/turn"
)

const runGatePoint = "tool_gate@1"

type runOpts struct {
	dir          string
	task         string
	dryRun       bool
	noGate       bool
	model        string
	maxSteps     int
	maxCostUSD   float64
	maxWallMS    int
	maxDecisions int
}

func runVerb(args []string, out, errOut io.Writer) int {
	opts, err := parseRunArgs(args)
	if err != nil {
		return runFail(errOut, err)
	}

	tools, err := buildRunTools(opts.dir)
	if err != nil {
		return runFail(errOut, err)
	}

	if opts.dryRun {
		request := llm.Request{
			Messages: []llm.Message{{Role: llm.RoleUser, Content: opts.task}},
			Tools:    tools.Definitions(),
		}
		body, err := request.Encode(opts.model)
		if err != nil {
			return runFail(errOut, err)
		}
		_, _ = fmt.Fprintln(out, string(body))
		return exitOK
	}

	key, err := jev.Key(".env")
	if err != nil {
		return runFail(errOut, err)
	}
	wire, err := openrouter.New(openrouter.Config{
		Model: opts.model,
		Key:   key,
		Transport: transport.Config{
			AttemptTimeout: time.Duration(konst.TurnAttemptTimeoutMillis) * time.Millisecond,
			Retries:        konst.TurnRetries,
			Backoff:        time.Duration(konst.TurnBackoffMillis) * time.Millisecond,
			MaxBackoff:     time.Duration(konst.TurnMaxBackoffMillis) * time.Millisecond,
			Concurrency:    1,
		},
	})
	if err != nil {
		return runFail(errOut, err)
	}
	client, err := llm.NewClient(wire)
	if err != nil {
		return runFail(errOut, err)
	}

	config := turn.Config{
		Model: client,
		Tools: tools,
		Task:  opts.task,
		Caps: turn.Caps{
			MaxSteps:     opts.maxSteps,
			MaxCostUSD:   opts.maxCostUSD,
			MaxWallClock: time.Duration(opts.maxWallMS) * time.Millisecond,
			MaxDecisions: opts.maxDecisions,
		},
		ResultBytesCap: konst.TurnResultBytesCap,
	}
	var gate *toolGate
	if !opts.noGate {
		gate, err = newToolGate(opts.dir)
		if err != nil {
			return runFail(errOut, err)
		}
		config.Gate = gate
	}

	row, runErr := turn.Run(context.Background(), config)
	printRunRow(out, row)
	if gate != nil {
		_, _ = fmt.Fprintf(out, "gate decisions %d cost $%.6f mode %s\n", gate.decisions, gate.costUSD, gate.set.Mode)
	}
	if writeErr := writeRunRow(row); writeErr != nil {
		_, _ = fmt.Fprintf(errOut, "boji run: writing the turn row: %v\n", writeErr)
	}
	if runErr != nil {
		return runFail(errOut, runErr)
	}
	return exitOK
}

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
	pol, err := resolvePolicy(runGatePoint, set)
	if err != nil {
		return nil, err
	}
	resolution, err := resolvePolicyMode(pol)
	if err != nil {
		return nil, err
	}
	set.Policy, set.Mode, set.ModeReason = &pol, resolution.Mode, resolution.Reason

	key, err := jev.Key(".env")
	if err != nil {
		return nil, err
	}
	wire, err := jevwire.New(jevwire.Config{
		Key: key,
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
	if err != nil {
		return ledger.Row{}, err
	}
	g.decisions++
	g.costUSD += decision.Usage.Cost
	return appendRow(builtState, g.set, rowInput{
		decision:     &decision,
		answers:      toLedgerAnswers(g.set.QuestionsVersion, decision.Answers),
		turnID:       request.TurnID,
		stateBuilder: builder,
	})
}

func buildRunTools(dir string) (turn.Registry, error) {
	readTool, err := turn.NewReadTool(dir)
	if err != nil {
		return turn.Registry{}, err
	}
	writeTool, err := turn.NewWriteTool(dir)
	if err != nil {
		return turn.Registry{}, err
	}
	bashTool, err := turn.NewBashTool(dir)
	if err != nil {
		return turn.Registry{}, err
	}
	return turn.NewRegistry(readTool, writeTool, bashTool), nil
}

func printRunRow(out io.Writer, row turn.Row) {
	_, _ = fmt.Fprintf(out, "turn %s outcome %s model %s cost $%.6f wall_clock_ms %d\n",
		row.ID, row.Outcome, row.Model, row.TotalCostUSD, row.WallClockMS)
	for _, step := range row.Steps {
		if len(step.ToolCalls) == 0 {
			_, _ = fmt.Fprintf(out, "step %d: assistant_text %q cost $%.6f\n", step.Index, step.AssistantText, step.CostUSD)
			continue
		}
		for _, call := range step.ToolCalls {
			exitCode := "<nil>"
			if call.ExitCode != nil {
				exitCode = strconv.Itoa(*call.ExitCode)
			}
			_, _ = fmt.Fprintf(out, "step %d: tool_call tool=%s command=%q exit_code=%s error=%q cost $%.6f\n",
				step.Index, call.Tool, call.Command, exitCode, call.Error, step.CostUSD)
		}
	}
}

func writeRunRow(row turn.Row) error {
	state, err := sys.ProjectStateDir()
	if err != nil {
		return err
	}
	body, err := json.MarshalIndent(row, "", "  ")
	if err != nil {
		return err
	}
	path := filepath.Join(state, "sessions", row.ID+".json")
	return sys.WriteFile(path, body, 0o644)
}

func runFail(errOut io.Writer, err error) int {
	_, _ = fmt.Fprintf(errOut, "boji run: %v\n", err)
	return exitUsage
}

func parseRunArgs(args []string) (runOpts, error) {
	opts := runOpts{
		model:        konst.TurnModelAlias,
		maxSteps:     konst.TurnMaxSteps,
		maxCostUSD:   konst.TurnMaxCostUSD,
		maxWallMS:    konst.TurnMaxWallClockMillis,
		maxDecisions: konst.TurnMaxDecisions,
	}
	for i := 0; i < len(args); i++ {
		arg := args[i]
		var err error
		switch arg {
		case "--dir":
			opts.dir, err = nextArg(args, &i, arg)
		case "--dry-run":
			opts.dryRun = true
		case "--no-gate":
			opts.noGate = true
		case "--model":
			opts.model, err = nextArg(args, &i, arg)
		case "--max-steps":
			opts.maxSteps, err = nextInt(args, &i, arg)
		case "--max-cost":
			opts.maxCostUSD, err = nextFloat(args, &i, arg)
		case "--max-wall-clock-ms":
			opts.maxWallMS, err = nextInt(args, &i, arg)
		case "--max-decisions":
			opts.maxDecisions, err = nextInt(args, &i, arg)
		default:
			switch {
			case strings.HasPrefix(arg, "--"):
				err = fmt.Errorf("unknown argument %q", arg)
			case opts.task != "":
				err = fmt.Errorf("boji run takes one task, got %q and %q", opts.task, arg)
			default:
				opts.task = arg
			}
		}
		if err != nil {
			return runOpts{}, err
		}
	}
	if opts.dir == "" {
		return runOpts{}, errors.New("--dir is required: boji run never defaults to the current directory, because its bash tool is not sandboxed")
	}
	if strings.TrimSpace(opts.task) == "" {
		return runOpts{}, errors.New("boji run needs a task")
	}
	return opts, nil
}

func nextArg(args []string, i *int, flag string) (string, error) {
	*i++
	if *i >= len(args) {
		return "", fmt.Errorf("%s needs a value", flag)
	}
	return args[*i], nil
}

func nextInt(args []string, i *int, flag string) (int, error) {
	raw, err := nextArg(args, i, flag)
	if err != nil {
		return 0, err
	}
	n, err := strconv.Atoi(raw)
	if err != nil {
		return 0, fmt.Errorf("%s %q is not a number", flag, raw)
	}
	return n, nil
}

func nextFloat(args []string, i *int, flag string) (float64, error) {
	raw, err := nextArg(args, i, flag)
	if err != nil {
		return 0, err
	}
	value, err := strconv.ParseFloat(raw, 64)
	if err != nil {
		return 0, fmt.Errorf("%s %q is not a number", flag, raw)
	}
	return value, nil
}
