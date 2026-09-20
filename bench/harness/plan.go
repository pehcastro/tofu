package harness

import (
	"fmt"
	"io"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"time"

	"boji/bench/harness/task"
)

const (
	defaultWallClockCap = 30 * time.Minute
	defaultTurnCap      = 40
)

const playgroundRoot = ".playground"

const (
	claudeArmModel = "opus"
	codexArmModel  = "gpt-5.6-sol"
	bojiArmModel   = "anthropic/claude-opus-5"
)

type Caps struct {
	WallClock time.Duration
	TurnCap   int
}

type Plan struct {
	Arm        Arm
	Task       string
	Version    int
	Prompt     string
	PromptPath string
	Command    []string
	Dir        string
	WorkingDir string
	Branch     string
	Env        []string
	Model      string
	Caps       Caps
}

func PromptPath(root string, version int) string {
	return task.Path(root, version, task.Prompt)
}

func CheckerPath(root string, version int) string {
	return task.Path(root, version, task.Checker)
}

func ArmDir(root string, arm Arm, task string) string {
	return filepath.Join(root, playgroundRoot, task+"-"+string(arm))
}

func BuildPlan(root string, arm Arm, task string, version int) (Plan, error) {
	promptPath := PromptPath(root, version)
	raw, err := os.ReadFile(promptPath)
	if err != nil {
		return Plan{}, fmt.Errorf("the v%d prompt is the task every arm gets, and it could not be read: %w", version, err)
	}
	prompt := strings.TrimSpace(string(raw))

	dir := ArmDir(root, arm, task)
	caps := Caps{WallClock: defaultWallClockCap, TurnCap: defaultTurnCap}
	plan := Plan{
		Arm: arm, Task: task, Version: version,
		Prompt: prompt, PromptPath: promptPath,
		Dir: dir, Branch: fmt.Sprintf("v%d", version), Caps: caps,
	}

	switch arm {
	case ArmClaude:
		plan.Model = claudeArmModel
		plan.WorkingDir = dir
		plan.Command = []string{
			"claude", "-p", prompt,
			"--model", claudeArmModel,
			"--output-format", "json",
			"--permission-mode", "bypassPermissions",
			"--safe-mode",
		}
		plan.Env = []string{"the anthropic subscription credential claude auth already holds"}
	case ArmCodex:
		plan.Model = codexArmModel
		plan.Command = []string{
			"codex", "exec", prompt,
			"-m", codexArmModel,
			"--json",
			"--sandbox", "workspace-write",
			"-C", dir,
		}
		plan.Env = []string{"the chatgpt subscription credential codex login already holds"}
	case ArmBoji:
		plan.Model = bojiArmModel
		plan.Command = []string{
			"boji", "run", prompt,
			"--dir", dir,
			"--model", bojiArmModel,
			"--max-steps", strconv.Itoa(caps.TurnCap),
		}
		plan.Env = []string{"OPENROUTER_KEY for the jev gate, the anthropic subscription credential for the model"}
	default:
		panic("harness: unknown arm " + string(arm))
	}
	return plan, nil
}

func Fprint(w io.Writer, p Plan) error {
	_, err := fmt.Fprintf(w,
		"arm %s task %s version %d\ncommand: %s\ndir: %s\nbranch: %s\nprompt: %s\nmodel: %s\nenv: %s\ncaps, unset rather than measured, nobody has given a number: wall clock %s, turns %d\n\n",
		p.Arm, p.Task, p.Version, Shell(p.Command), p.Dir, p.Branch, p.PromptPath, p.Model, strings.Join(p.Env, ", "),
		p.Caps.WallClock, p.Caps.TurnCap)
	return err
}

func Shell(command []string) string {
	quoted := make([]string, len(command))
	for i, arg := range command {
		if strings.ContainsAny(arg, " \t\r\n\"") {
			quoted[i] = strconv.Quote(arg)
			continue
		}
		quoted[i] = arg
	}
	return strings.Join(quoted, " ")
}
