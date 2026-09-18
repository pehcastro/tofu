package harness

import (
	"fmt"
	"io"
	"path/filepath"
	"strings"
	"time"
)

const (
	defaultWallClockCap = 30 * time.Minute
	defaultDollarCap    = 5.00
	defaultTurnCap      = 40
)

type Caps struct {
	WallClock time.Duration
	DollarCap float64
	TurnCap   int
}

type Plan struct {
	Arm     Arm
	Task    string
	Version int
	Command []string
	Dir     string
	Branch  string
	Env     []string
	Caps    Caps
}

func BuildPlan(arm Arm, task string, version int) Plan {
	dir := filepath.Join(".playground", "hono-"+string(arm))
	branch := fmt.Sprintf("v%d", version)
	prompt := filepath.Join(dir, fmt.Sprintf("v%d.prompt.txt", version))
	caps := Caps{WallClock: defaultWallClockCap, DollarCap: defaultDollarCap, TurnCap: defaultTurnCap}
	plan := Plan{Arm: arm, Task: task, Version: version, Dir: dir, Branch: branch, Caps: caps}

	switch arm {
	case ArmClaude:
		plan.Command = []string{
			"claude", "-p", prompt,
			"--output-format", "json",
			"--permission-mode", "bypassPermissions",
			"--max-budget-usd", fmt.Sprintf("%.2f", caps.DollarCap),
			"--add-dir", dir,
		}
		plan.Env = []string{"ANTHROPIC_API_KEY"}
	case ArmCodex:
		plan.Command = []string{
			"codex", "exec", prompt,
			"--json",
			"--sandbox", "workspace-write",
			"-C", dir,
		}
		plan.Env = []string{"OPENAI_API_KEY"}
	case ArmBoji:
		plan.Command = []string{
			"boji", "run", prompt,
			"--dir", dir,
			"--wall-clock-cap", caps.WallClock.String(),
			"--dollar-cap", fmt.Sprintf("%.2f", caps.DollarCap),
			"--turn-cap", fmt.Sprintf("%d", caps.TurnCap),
		}
		plan.Env = []string{"OPENROUTER_KEY"}
	default:
		panic("harness: unknown arm " + string(arm))
	}
	return plan
}

func Fprint(w io.Writer, p Plan) error {
	_, err := fmt.Fprintf(w,
		"arm %s task %s version %d\ncommand: %s\ndir: %s\nbranch: %s\nenv: %s\ncaps: wall clock %s, dollar cap $%.2f, turn cap %d\n\n",
		p.Arm, p.Task, p.Version, strings.Join(p.Command, " "), p.Dir, p.Branch, strings.Join(p.Env, ", "),
		p.Caps.WallClock, p.Caps.DollarCap, p.Caps.TurnCap)
	return err
}
