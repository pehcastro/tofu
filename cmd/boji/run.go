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
	"boji/internal/konst"
	"boji/internal/llm"
	"boji/internal/llm/wire/openrouter"
	"boji/internal/sys"
	"boji/internal/transport"
	"boji/internal/turn"
)

type runOpts struct {
	dir        string
	task       string
	dryRun     bool
	model      string
	maxSteps   int
	maxCostUSD float64
	maxWallMS  int
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

	row, runErr := turn.Run(context.Background(), turn.Config{
		Model: client,
		Tools: tools,
		Task:  opts.task,
		Caps: turn.Caps{
			MaxSteps:     opts.maxSteps,
			MaxCostUSD:   opts.maxCostUSD,
			MaxWallClock: time.Duration(opts.maxWallMS) * time.Millisecond,
		},
		ResultBytesCap: konst.TurnResultBytesCap,
	})
	printRunRow(out, row)
	if writeErr := writeRunRow(row); writeErr != nil {
		_, _ = fmt.Fprintf(errOut, "boji run: writing the turn row: %v\n", writeErr)
	}
	if runErr != nil {
		return runFail(errOut, runErr)
	}
	return exitOK
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
		model:      konst.TurnModelAlias,
		maxSteps:   konst.TurnMaxSteps,
		maxCostUSD: konst.TurnMaxCostUSD,
		maxWallMS:  konst.TurnMaxWallClockMillis,
	}
	for i := 0; i < len(args); i++ {
		arg := args[i]
		var err error
		switch arg {
		case "--dir":
			opts.dir, err = nextArg(args, &i, arg)
		case "--dry-run":
			opts.dryRun = true
		case "--model":
			opts.model, err = nextArg(args, &i, arg)
		case "--max-steps":
			opts.maxSteps, err = nextInt(args, &i, arg)
		case "--max-cost":
			opts.maxCostUSD, err = nextFloat(args, &i, arg)
		case "--max-wall-clock-ms":
			opts.maxWallMS, err = nextInt(args, &i, arg)
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
