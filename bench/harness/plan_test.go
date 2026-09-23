package harness

import (
	"bytes"
	"go/parser"
	"go/token"
	"os"
	"path/filepath"
	"runtime"
	"strings"
	"testing"

	"tofu/internal/judge/ledger"
)

func TestAPlanOfThreeRepeatsPutsItsRepeatIndexOnEveryRow(t *testing.T) {
	plan := Plan{Arm: ArmTofu, Task: "hono", Version: 2, Repeats: 3}

	var rows []Row
	for _, meta := range plan.Runs() {
		meta.CredentialKind = CredentialKindSubscription
		row, _, err := ParseTofu(t.TempDir(), ledger.Filter{}, meta)
		if err != nil {
			t.Fatalf("ParseTofu: %v", err)
		}
		rows = append(rows, row)
	}

	if len(rows) != 3 {
		t.Fatalf("a plan of three repeats produced %d rows", len(rows))
	}
	for i, row := range rows {
		if row.Run != i+1 {
			t.Fatalf("row %d carries repeat index %d, so a repeat cannot be told from the one before it", i+1, row.Run)
		}
	}

	var buf bytes.Buffer
	if err := Fprint(&buf, plan); err != nil {
		t.Fatalf("Fprint: %v", err)
	}
	if !strings.Contains(buf.String(), "repeats: 3") {
		t.Fatalf("the printed plan does not say how many repeats it asks for:\n%s", buf.String())
	}

	once := Plan{Arm: ArmTofu, Task: "hono", Version: 2}
	if len(once.Runs()) != 1 || !strings.Contains(once.RepeatLine(), "carries no spread") {
		t.Fatalf("a plan that asks for nothing runs %d times and says %q, and one run must say plainly that it has no spread",
			len(once.Runs()), once.RepeatLine())
	}
}

func TestBuildPlanPrintsCommandDirEnvAndUnsetCapsForAllThreeArms(t *testing.T) {
	root := repositoryRoot(t)
	for _, arm := range []Arm{ArmClaude, ArmCodex, ArmTofu} {
		plan, err := BuildPlan(root, arm, "hono", 1)
		if err != nil {
			t.Fatalf("%s: BuildPlan: %v", arm, err)
		}
		var buf bytes.Buffer
		if err := Fprint(&buf, plan); err != nil {
			t.Fatalf("%s: Fprint: %v", arm, err)
		}
		out := buf.String()
		if len(plan.Command) == 0 || !strings.Contains(out, plan.Command[0]) {
			t.Errorf("%s: printed plan missing the command, got %q", arm, out)
		}
		if !strings.Contains(out, plan.Dir) {
			t.Errorf("%s: printed plan missing the directory, got %q", arm, out)
		}
		if len(plan.Env) == 0 || !strings.Contains(out, plan.Env[0]) {
			t.Errorf("%s: printed plan missing the environment, got %q", arm, out)
		}
		if !strings.Contains(out, "wall clock") || !strings.Contains(out, "turns") {
			t.Errorf("%s: printed plan missing the caps, got %q", arm, out)
		}
		if !strings.Contains(out, "unset rather than measured") {
			t.Errorf("%s: printed plan states its caps as though they were agreed, got %q", arm, out)
		}
		for _, banned := range []string{"dollar", "$", "budget"} {
			if strings.Contains(out, banned) {
				t.Errorf("%s: printed plan still carries %q, the spend cap is gone: %q", arm, banned, out)
			}
		}
	}
}

func TestBuildPlanPassesThePromptTextNotItsPath(t *testing.T) {
	root := repositoryRoot(t)
	raw, err := os.ReadFile(PromptPath(root, 1))
	if err != nil {
		t.Fatalf("read the v1 prompt: %v", err)
	}
	want := strings.TrimSpace(string(raw))

	plan, err := BuildPlan(root, ArmTofu, "hono", 1)
	if err != nil {
		t.Fatalf("BuildPlan: %v", err)
	}
	if plan.Command[2] != want {
		t.Errorf("the positional argument is %q, want the prompt text itself", plan.Command[2])
	}
	for _, arg := range plan.Command {
		if strings.HasSuffix(arg, ".prompt.txt") {
			t.Errorf("the command still carries a prompt file path %q, tofu run takes the task as a string", arg)
		}
	}
}

func TestEveryArmPlanNamesItsModelSoTheComparisonIsLikeForLike(t *testing.T) {
	root := repositoryRoot(t)
	want := map[Arm]string{ArmClaude: claudeArmModel, ArmCodex: codexArmModel, ArmTofu: tofuArmModel}
	for arm, model := range want {
		plan, err := BuildPlan(root, arm, "hono", 1)
		if err != nil {
			t.Fatalf("%s: BuildPlan: %v", arm, err)
		}
		if plan.Model != model {
			t.Errorf("%s: plan names model %q, want %q", arm, plan.Model, model)
		}
		named := false
		for _, arg := range plan.Command {
			if arg == model {
				named = true
			}
		}
		if !named {
			t.Errorf("%s: the command does not pass %q, so the arm would run whatever its default is: %v", arm, model, plan.Command)
		}
		var buf bytes.Buffer
		if err := Fprint(&buf, plan); err != nil {
			t.Fatalf("%s: Fprint: %v", arm, err)
		}
		if !strings.Contains(buf.String(), "model: "+model) {
			t.Errorf("%s: printed plan does not state its model, got %q", arm, buf.String())
		}
	}
}

func TestEveryArmAsksForTheSameThinkingEffort(t *testing.T) {
	root := repositoryRoot(t)
	onTheCommandLine := map[Arm]string{
		ArmClaude: "--effort medium",
		ArmCodex:  "-c model_reasoning_effort='medium'",
	}
	for arm, flag := range onTheCommandLine {
		plan, err := BuildPlan(root, arm, "hono", 1)
		if err != nil {
			t.Fatalf("%s: BuildPlan: %v", arm, err)
		}
		if plan.Effort != EffortMedium {
			t.Errorf("%s: plan runs at effort %q, want %q, so the three arms are not asked the same thing", arm, plan.Effort, EffortMedium)
		}
		if !strings.Contains(Shell(plan.Command), flag) {
			t.Errorf("%s: the command is %s and does not carry %s, so the arm runs at its own default", arm, Shell(plan.Command), flag)
		}
	}

	tofu, err := BuildPlan(root, ArmTofu, "hono", 1)
	if err != nil {
		t.Fatalf("tofu: BuildPlan: %v", err)
	}
	if tofu.Effort != EffortNone {
		t.Errorf("the tofu arm claims effort %q, and nothing in tofu run or the turn loop sets one", tofu.Effort)
	}
	if !strings.Contains(tofu.EffortSetBy, "no flag") {
		t.Errorf("the tofu arm says its effort was set by %q, and it must say plainly that no flag sets it", tofu.EffortSetBy)
	}

	for _, arm := range []Arm{ArmClaude, ArmCodex, ArmTofu} {
		plan, err := BuildPlan(root, arm, "hono", 1)
		if err != nil {
			t.Fatalf("%s: BuildPlan: %v", arm, err)
		}
		var buf bytes.Buffer
		if err := Fprint(&buf, plan); err != nil {
			t.Fatalf("%s: Fprint: %v", arm, err)
		}
		if !strings.Contains(buf.String(), "effort: "+plan.EffortLine()) {
			t.Errorf("%s: the printed plan does not say what effort it runs at:\n%s", arm, buf.String())
		}
	}
}

func TestBuildPlanFailsWhenThePromptIsMissing(t *testing.T) {
	if _, err := BuildPlan(t.TempDir(), ArmTofu, "hono", 1); err == nil {
		t.Fatal("BuildPlan returned no error for a root with no prompt file")
	}
}

func TestBuildPlanFileNeverImportsOSExec(t *testing.T) {
	fset := token.NewFileSet()
	file, err := parser.ParseFile(fset, "plan.go", nil, parser.ImportsOnly)
	if err != nil {
		t.Fatalf("parse plan.go: %v", err)
	}
	for _, imp := range file.Imports {
		if imp.Path.Value == `"os/exec"` {
			t.Fatalf("plan.go imports os/exec, BuildPlan and Fprint must execute nothing")
		}
	}
}

func TestBuildPlanStartsNoProcessForAnyArm(t *testing.T) {
	if runtime.GOOS != "windows" {
		t.Skip("sentinel scripts here are written for Windows PATHEXT resolution")
	}
	root := repositoryRoot(t)
	dir := t.TempDir()
	marker := filepath.Join(dir, "marker.txt")
	script := "@echo ran > \"" + marker + "\"\r\n"
	for _, name := range []string{"claude.cmd", "codex.cmd", "tofu.cmd"} {
		if err := os.WriteFile(filepath.Join(dir, name), []byte(script), 0o755); err != nil {
			t.Fatalf("write sentinel %s: %v", name, err)
		}
	}
	t.Setenv("PATH", dir+string(os.PathListSeparator)+os.Getenv("PATH"))

	for _, arm := range []Arm{ArmClaude, ArmCodex, ArmTofu} {
		plan, err := BuildPlan(root, arm, "hono", 1)
		if err != nil {
			t.Fatalf("%s: BuildPlan: %v", arm, err)
		}
		var buf bytes.Buffer
		if err := Fprint(&buf, plan); err != nil {
			t.Fatalf("%s: Fprint: %v", arm, err)
		}
	}

	if _, err := os.Stat(marker); err == nil {
		t.Fatal("a sentinel executable ran: BuildPlan or Fprint started a real process")
	} else if !os.IsNotExist(err) {
		t.Fatalf("stat marker: %v", err)
	}
}
