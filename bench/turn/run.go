package turn

import (
	"context"
	"fmt"
	"os"
	"time"

	"tofu/bench/stat"
	"tofu/internal/konst"
	"tofu/internal/llm"
	"tofu/internal/llm/wire/openrouter"
	"tofu/internal/transport"
	iturn "tofu/internal/turn"
)

const (
	Reps           = 3
	Model          = "anthropic/claude-fable-5-1"
	loopMaxSteps   = 5
	cappedMaxSteps = 1
)

type armSpec struct {
	name     string
	maxSteps int
}

var runArms = []armSpec{
	{ArmLoop, loopMaxSteps},
	{ArmCapped, cappedMaxSteps},
}

func Run(ctx context.Context, key string) (Result, error) {
	wire, err := openrouter.New(openrouter.Config{
		Model: Model,
		Key:   key,
		Transport: transport.Config{
			AttemptTimeout: 30 * time.Second,
			Retries:        1,
			Backoff:        250 * time.Millisecond,
			MaxBackoff:     2 * time.Second,
			Concurrency:    1,
		},
	})
	if err != nil {
		return Result{}, err
	}
	client, err := llm.NewClient(wire)
	if err != nil {
		return Result{}, err
	}

	result := Result{GeneratedAt: time.Now(), Model: Model}
	for _, task := range Tasks() {
		for _, arm := range runArms {
			var armRuns []RunResult
			for rep := 1; rep <= Reps; rep++ {
				run := runOne(ctx, client, task, arm.name, arm.maxSteps, rep)
				result.Runs = append(result.Runs, run)
				armRuns = append(armRuns, run)
				result.TotalCostUSD += run.Row.TotalCostUSD
				result.TotalTurns++
				if !run.Passed {
					result.FailedRuns++
				}
			}
			result.Stats = append(result.Stats, statsFor(task.Name, arm.name, armRuns))
		}
	}
	return result, nil
}

func runOne(ctx context.Context, model iturn.Model, task Task, armName string, maxSteps, rep int) RunResult {
	failed := func(err error) RunResult {
		return RunResult{Task: task.Name, Arm: armName, Rep: rep, CallErr: err.Error()}
	}

	dir, err := os.MkdirTemp("", "tofu-bench-turn-*")
	if err != nil {
		return failed(err)
	}
	defer func() { _ = os.RemoveAll(dir) }()

	if task.Setup != nil {
		if err := task.Setup(dir); err != nil {
			return failed(fmt.Errorf("setup: %w", err))
		}
	}

	tools, err := buildTools(dir)
	if err != nil {
		return failed(err)
	}

	row, runErr := iturn.Run(ctx, iturn.Config{
		Model: model,
		Spend: iturn.SpendAPIKey,
		Tools: tools,
		Task:  task.Prompt,
		Caps: iturn.Caps{
			MaxSteps:         maxSteps,
			LoopGuardRepeats: konst.TurnLoopGuardRepeats,
			LoopGuardWindow:  konst.TurnLoopGuardWindow,
		},
		ResultBytesCap: 4096,
	})
	if runErr != nil {
		return RunResult{Task: task.Name, Arm: armName, Rep: rep, Row: row, CallErr: runErr.Error()}
	}

	passed, note := task.Check(dir)
	return RunResult{Task: task.Name, Arm: armName, Rep: rep, Row: row, Passed: passed, CheckNote: note}
}

func buildTools(dir string) (iturn.Registry, error) {
	readTool, err := iturn.NewReadTool(dir)
	if err != nil {
		return iturn.Registry{}, err
	}
	writeTool, err := iturn.NewWriteTool(dir)
	if err != nil {
		return iturn.Registry{}, err
	}
	bashTool, err := iturn.NewBashTool(dir)
	if err != nil {
		return iturn.Registry{}, err
	}
	ledger := iturn.NewReadLedger()
	return iturn.NewRegistry(readTool.Reading(ledger), writeTool.Reading(ledger), bashTool), nil
}

func statsFor(taskName, armName string, runs []RunResult) TaskArmStats {
	stats := TaskArmStats{Task: taskName, Arm: armName, Runs: len(runs), ToolCalls: map[string]int{}}
	var wallClocks, steps, costs []float64
	for _, r := range runs {
		if r.Passed {
			stats.Passed++
		} else {
			stats.Failed++
		}
		if r.CallErr != "" {
			continue
		}
		if r.Passed {
			wallClocks = append(wallClocks, r.WallClockMS())
			steps = append(steps, float64(r.Steps()))
			costs = append(costs, r.Row.TotalCostUSD)
			for tool, n := range r.ToolCalls() {
				stats.ToolCalls[tool] += n
			}
		}
	}
	stats.TimedRuns = len(wallClocks)
	stats.MedianWallClockMS = stat.Median(wallClocks)
	stats.P95WallClockMS = stat.Percentile(wallClocks, 95)
	stats.MinWallClockMS, stats.MaxWallClockMS = stat.Spread(wallClocks)
	stats.MedianSteps = stat.Median(steps)
	stats.MedianCostUSD = stat.Median(costs)
	return stats
}
