package harness

import (
	"fmt"
	"io"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"time"
)

const (
	defaultWallClockCap = 30 * time.Minute
	defaultTurnCap      = 40
)

const playgroundRoot = ".playground"

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
	Branch     string
	Env        []string
	Caps       Caps
}

func PromptPath(root string, version int) string {
	return filepath.Join(root, playgroundRoot, fmt.Sprintf("v%d.prompt.txt", version))
}

func CheckerPath(root string, version int) string {
	return filepath.Join(root, playgroundRoot, fmt.Sprintf("v%d.check.ts", version))
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
		plan.Command = []string{
			"claude", "-p", prompt,
			"--output-format", "json",
			"--permission-mode", "bypassPermissions",
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
			"--max-wall-clock-ms", strconv.FormatInt(caps.WallClock.Milliseconds(), 10),
			"--max-steps", strconv.Itoa(caps.TurnCap),
		}
		plan.Env = []string{"OPENROUTER_KEY"}
	default:
		panic("harness: unknown arm " + string(arm))
	}
	return plan, nil
}

func Fprint(w io.Writer, p Plan) error {
	_, err := fmt.Fprintf(w,
		"arm %s task %s version %d\ncommand: %s\ndir: %s\nbranch: %s\nprompt: %s\nenv: %s\ncaps, unset rather than measured, nobody has given a number: wall clock %s, turns %d\n\n",
		p.Arm, p.Task, p.Version, Shell(p.Command), p.Dir, p.Branch, p.PromptPath, strings.Join(p.Env, ", "),
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
