package turn

import (
	"time"

	iturn "tofu/internal/turn"
)

const (
	ArmLoop   = "loop"
	ArmCapped = "capped, one step"
)

type RunResult struct {
	Task      string
	Arm       string
	Rep       int
	Row       iturn.Row
	CallErr   string
	Passed    bool
	CheckNote string
}

func (r RunResult) WallClockMS() float64 { return float64(r.Row.WallClockMS) }
func (r RunResult) Steps() int           { return len(r.Row.Steps) }

func (r RunResult) ToolCalls() map[string]int {
	counts := make(map[string]int)
	for _, step := range r.Row.Steps {
		for _, call := range step.ToolCalls {
			counts[call.Tool]++
		}
	}
	return counts
}

type TaskArmStats struct {
	Task              string
	Arm               string
	Runs              int
	Passed            int
	Failed            int
	TimedRuns         int
	MedianWallClockMS float64
	P95WallClockMS    float64
	MinWallClockMS    float64
	MaxWallClockMS    float64
	MedianSteps       float64
	MedianCostUSD     float64
	ToolCalls         map[string]int
}

type Result struct {
	GeneratedAt  time.Time
	Model        string
	Runs         []RunResult
	Stats        []TaskArmStats
	TotalCostUSD float64
	TotalTurns   int
	FailedRuns   int
}
