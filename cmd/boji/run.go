package main

import (
	"context"
	"crypto/rand"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"path/filepath"
	"strconv"
	"strings"
	"time"

	"boji/internal/judge/jev"
	"boji/internal/konst"
	"boji/internal/llm"
	"boji/internal/llm/cred"
	"boji/internal/llm/wire/anthropic"
	"boji/internal/llm/wire/openrouter"
	"boji/internal/sys"
	"boji/internal/transport"
	"boji/internal/turn"
)

const (
	wireSubscription = "anthropic"
	wireKey          = "openrouter"
)

type runOpts struct {
	dir          string
	task         string
	wire         string
	dryRun       bool
	noGate       bool
	model        string
	maxSteps     int
	maxWallMS    int
	maxDecisions int
}

func (o runOpts) modelID() string {
	switch {
	case o.model != "":
		return o.model
	case o.wire == wireKey:
		return konst.TurnOpenRouterModel
	default:
		return konst.TurnAnthropicModel
	}
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
		body, err := dryRunBody(opts, tools)
		if err != nil {
			return runFail(errOut, err)
		}
		_, _ = fmt.Fprintln(out, string(body))
		return exitOK
	}
	return runTurn(opts, tools, out, errOut)
}

func runTurn(opts runOpts, tools turn.Registry, out, errOut io.Writer) int {
	model, spend, store, err := runModel(opts)
	if store != nil {
		defer func() { _ = store.Close() }()
	}
	if err != nil {
		return runFail(errOut, err)
	}

	var gate *toolGate
	if !opts.noGate {
		if gate, err = newToolGate(opts.dir); err != nil {
			return runFail(errOut, err)
		}
	}

	row, runErr := turn.Run(context.Background(), runConfig(opts, tools, model, spend, gate))
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

func runConfig(opts runOpts, tools turn.Registry, model turn.Model, spend turn.Spend, gate *toolGate) turn.Config {
	config := turn.Config{
		Model: model,
		Spend: spend,
		Tools: tools,
		Task:  opts.task,
		Caps: turn.Caps{
			MaxSteps:     opts.maxSteps,
			MaxWallClock: time.Duration(opts.maxWallMS) * time.Millisecond,
			MaxDecisions: opts.maxDecisions,
		},
		ResultBytesCap: konst.TurnResultBytesCap,
	}
	if gate != nil {
		config.Gate = gate
	}
	return config
}

func runModel(opts runOpts) (turn.Model, turn.Spend, *cred.Store, error) {
	if opts.wire == wireKey {
		model, err := keyModel(opts.modelID())
		return model, turn.SpendAPIKey, nil, err
	}
	model, store, err := subscriptionModel(opts.modelID())
	return model, turn.SpendSubscription, store, err
}

func dryRunBody(opts runOpts, tools turn.Registry) ([]byte, error) {
	if opts.wire == wireKey {
		request := llm.Request{
			Messages: []llm.Message{{Role: llm.RoleUser, Content: opts.task}},
			Tools:    tools.Definitions(),
		}
		return request.Encode(opts.modelID())
	}
	request := anthropic.Request{
		Model:    opts.modelID(),
		Messages: []llm.Message{{Role: llm.RoleUser, Content: opts.task}},
		Tools:    tools.Definitions(),
	}
	return request.Encode(true)
}

func keyModel(model string) (turn.Model, error) {
	key, err := jev.Key(".env")
	if err != nil {
		return nil, err
	}
	wire, err := openrouter.New(openrouter.Config{
		Model: model,
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
		return nil, err
	}
	return llm.NewClient(wire)
}

func subscriptionModel(model string) (turn.Model, *cred.Store, error) {
	spec, err := cred.Lookup(string(cred.Anthropic))
	if err != nil {
		return nil, nil, err
	}
	path, err := cred.Path()
	if err != nil {
		return nil, nil, err
	}
	store, err := cred.Open(path)
	if err != nil {
		return nil, nil, err
	}
	row, present, err := store.Row(cred.Anthropic)
	if err != nil {
		return nil, store, err
	}
	if !present {
		return nil, store, errors.New("no anthropic subscription credential, run boji login anthropic")
	}
	session, err := sessionID()
	if err != nil {
		return nil, store, err
	}
	wire, err := anthropic.New(anthropic.Config{
		Model:     model,
		Token:     cred.NewManager(store, spec).Access,
		SessionID: session,
		AccountID: row.Credential.Identity.AccountID,
	})
	if err != nil {
		return nil, store, err
	}
	return turn.Subscription{Wire: wire}, store, nil
}

func sessionID() (string, error) {
	var raw [16]byte
	if _, err := rand.Read(raw[:]); err != nil {
		return "", err
	}
	raw[6] = raw[6]&0x0f | 0x40
	raw[8] = raw[8]&0x3f | 0x80
	text := hex.EncodeToString(raw[:])
	return text[:8] + "-" + text[8:12] + "-" + text[12:16] + "-" + text[16:20] + "-" + text[20:], nil
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
	spend := "spend subscription quota, no money"
	if row.Spend != turn.SpendSubscription {
		spend = fmt.Sprintf("spend api key $%.6f", row.TotalCostUSD)
	}
	_, _ = fmt.Fprintf(out, "turn %s outcome %s model %s %s wall_clock_ms %d\n",
		row.ID, row.Outcome, row.Model, spend, row.WallClockMS)
	for _, step := range row.Steps {
		_, _ = fmt.Fprintf(out, "step %d: stop_reason %s in %d out %d cache_read %d cache_write %d\n",
			step.Index, step.StopReason, step.PromptTokens, step.CompletionTokens,
			step.CacheReadTokens, step.CacheWriteTokens)
		for _, warning := range step.Warnings {
			_, _ = fmt.Fprintf(out, "step %d: warning %s\n", step.Index, warning)
		}
		if len(step.ToolCalls) == 0 {
			_, _ = fmt.Fprintf(out, "step %d: assistant_text %q\n", step.Index, step.AssistantText)
			continue
		}
		for _, call := range step.ToolCalls {
			exitCode := "<nil>"
			if call.ExitCode != nil {
				exitCode = strconv.Itoa(*call.ExitCode)
			}
			_, _ = fmt.Fprintf(out, "step %d: tool_call tool=%s command=%q exit_code=%s gate=%s error=%q\n",
				step.Index, call.Tool, call.Command, exitCode, call.GateVerdict, call.Error)
		}
	}
}

func wireDoctorLines() []string {
	return []string{
		"wire " + wireSubscription + ": the default, boji run spends the anthropic subscription quota and no money",
		"wire " + wireKey + ": --wire openrouter spends the openrouter key, which is real money on the account that issued it",
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
		wire:         wireSubscription,
		maxSteps:     konst.TurnMaxSteps,
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
		case "--wire":
			opts.wire, err = nextArg(args, &i, arg)
		case "--model":
			if opts.model, err = nextArg(args, &i, arg); err == nil && strings.TrimSpace(opts.model) == "" {
				err = errors.New("--model needs a model id")
			}
		case "--max-steps":
			opts.maxSteps, err = nextInt(args, &i, arg)
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
	if opts.wire != wireSubscription && opts.wire != wireKey {
		return runOpts{}, fmt.Errorf("--wire %q is neither %s nor %s", opts.wire, wireSubscription, wireKey)
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
