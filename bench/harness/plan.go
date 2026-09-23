package harness

import (
	"fmt"
	"io"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"time"

	"tofu/bench/harness/task"
)

const (
	defaultWallClockCap = 30 * time.Minute
	defaultTurnCap      = 40
)

const playgroundRoot = ".playground"

const (
	claudeArmModel = "opus"
	codexArmModel  = "gpt-5.6-sol"
	tofuArmModel   = "claude-sub/claude-opus-5"
)

const AskedEffort = EffortMedium

const (
	claudeEffortSetBy = "--effort medium, one of low, medium, high, xhigh and max in claude --help"
	codexEffortSetBy  = "-c model_reasoning_effort='medium', the config override codex exec --help documents, reaching model_reasoning_effort in codex-rs/core/src/config/mod.rs:967"
	tofuEffortSetBy   = "--effort medium, one of none, minimal, low, medium, high, xhigh and max in tofu run --help, reaching output_config.effort on the anthropic wire in internal/llm/wire/anthropic/request.go"
)

type Caps struct {
	WallClock time.Duration
	TurnCap   int
}

const defaultRepeats = 1

type Plan struct {
	Arm         Arm
	Task        string
	Version     int
	Prompt      string
	PromptPath  string
	Command     []string
	Dir         string
	WorkingDir  string
	Branch      string
	Env         []string
	Model       string
	Caps        Caps
	Setup       Setup
	Seed        Seed
	Effort      Effort
	EffortSetBy string
	Repeats     int
}

func (p Plan) EffortLine() string {
	return string(p.Effort) + ", " + p.EffortSetBy
}

func (p Plan) Runs() []RunMeta {
	repeats := p.Repeats
	if repeats < defaultRepeats {
		repeats = defaultRepeats
	}
	metas := make([]RunMeta, 0, repeats)
	for repeat := 1; repeat <= repeats; repeat++ {
		metas = append(metas, RunMeta{Arm: p.Arm, Task: p.Task, Version: p.Version, Run: repeat, Setup: p.Setup, Seed: p.Seed, Effort: p.Effort})
	}
	return metas
}

func (p Plan) RepeatLine() string {
	if p.Repeats > defaultRepeats {
		return fmt.Sprintf("%d repeats, and the report reads their median and range", p.Repeats)
	}
	return "1 repeat, which is one sample of a stochastic run and carries no spread"
}

func PromptPath(root string, version int) string {
	return task.Path(root, version, task.Prompt)
}

func CheckerPath(root string, version int) string {
	return task.Path(root, version, task.Checker)
}

func ArmDir(root string, arm Arm, name string, version int) string {
	return filepath.Join(root, playgroundRoot, fmt.Sprintf("%s-v%d-%s", name, version, arm))
}

func BuildPlan(root string, arm Arm, name string, version int) (Plan, error) {
	benched, err := task.Of(version)
	if err != nil {
		return Plan{}, err
	}
	if name != benched.Name {
		return Plan{}, fmt.Errorf("v%d is the %s task and it was asked for as %q, and running it under another name would point it at that task's directory",
			version, benched.Name, name)
	}
	seed, err := SeedOf(root, version)
	if err != nil {
		return Plan{}, err
	}
	promptPath := PromptPath(root, version)
	raw, err := os.ReadFile(promptPath)
	if err != nil {
		return Plan{}, fmt.Errorf("the v%d prompt is the task every arm gets, and it could not be read: %w", version, err)
	}
	prompt := strings.TrimSpace(string(raw))

	setup, err := SetupOf(stockSetupName, filepath.Join(root, stockRuleDir), prompt)
	if err != nil {
		return Plan{}, fmt.Errorf("the setup under test is what a row is read back by, and it could not be recorded: %w", err)
	}

	dir := ArmDir(root, arm, benched.Name, version)
	caps := Caps{WallClock: defaultWallClockCap, TurnCap: defaultTurnCap}
	plan := Plan{
		Arm: arm, Task: benched.Name, Version: version,
		Prompt: prompt, PromptPath: promptPath,
		Dir: dir, Branch: fmt.Sprintf("v%d", version), Caps: caps, Setup: setup, Seed: seed,
		Repeats: defaultRepeats,
	}

	switch arm {
	case ArmClaude:
		plan.Model = claudeArmModel
		plan.Effort, plan.EffortSetBy = AskedEffort, claudeEffortSetBy
		plan.WorkingDir = dir
		plan.Command = []string{
			"claude", "-p", prompt,
			"--model", claudeArmModel,
			"--effort", string(AskedEffort),
			"--output-format", "json",
			"--permission-mode", "bypassPermissions",
			"--safe-mode",
		}
		plan.Env = []string{"the anthropic subscription credential claude auth already holds"}
	case ArmCodex:
		plan.Model = codexArmModel
		plan.Effort, plan.EffortSetBy = AskedEffort, codexEffortSetBy
		plan.Command = []string{
			"codex", "exec", prompt,
			"-m", codexArmModel,
			"-c", "model_reasoning_effort='" + string(AskedEffort) + "'",
			"--json",
			"--sandbox", "workspace-write",
			"-c", "sandbox_workspace_write.network_access=true",
			"-C", dir,
		}
		plan.Env = []string{"the chatgpt subscription credential codex login already holds"}
	case ArmTofu:
		plan.Model = tofuArmModel
		plan.Effort, plan.EffortSetBy = AskedEffort, tofuEffortSetBy
		plan.Command = []string{
			"tofu", "run", prompt,
			"--dir", dir,
			"--model", tofuArmModel,
			"--effort", string(AskedEffort),
			"--max-steps", strconv.Itoa(caps.TurnCap),
			"--no-instructions",
		}
		plan.Env = []string{"OPENROUTER_KEY for the jev gate, the anthropic subscription credential for the model"}
	default:
		panic("harness: unknown arm " + string(arm))
	}
	return plan, nil
}

func Fprint(w io.Writer, p Plan) error {
	_, err := fmt.Fprintf(w,
		"arm %s task %s version %d\ncommand: %s\ndir: %s\nbranch: %s\nprompt: %s\nmodel: %s\neffort: %s\nenv: %s\nsetup %s: %s\nseed: %s\nrepeats: %s\ncaps, unset rather than measured, nobody has given a number: wall clock %s, turns %d\n\n",
		p.Arm, p.Task, p.Version, Shell(p.Command), p.Dir, p.Branch, p.PromptPath, p.Model, p.EffortLine(), strings.Join(p.Env, ", "),
		p.Setup.Name, p.Setup.Line(), p.Seed.Line(), p.RepeatLine(), p.Caps.WallClock, p.Caps.TurnCap)
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
