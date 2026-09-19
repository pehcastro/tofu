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
)

func TestBuildPlanPrintsCommandDirEnvAndCapsForAllThreeArms(t *testing.T) {
	root := repositoryRoot(t)
	for _, arm := range []Arm{ArmClaude, ArmCodex, ArmBoji} {
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
		if !strings.Contains(out, "wall clock") || !strings.Contains(out, "dollar cap") || !strings.Contains(out, "turn cap") {
			t.Errorf("%s: printed plan missing the caps, got %q", arm, out)
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

	plan, err := BuildPlan(root, ArmBoji, "hono", 1)
	if err != nil {
		t.Fatalf("BuildPlan: %v", err)
	}
	if plan.Command[2] != want {
		t.Errorf("the positional argument is %q, want the prompt text itself", plan.Command[2])
	}
	for _, arg := range plan.Command {
		if strings.HasSuffix(arg, ".prompt.txt") {
			t.Errorf("the command still carries a prompt file path %q, boji run takes the task as a string", arg)
		}
	}
}

func TestBuildPlanFailsWhenThePromptIsMissing(t *testing.T) {
	if _, err := BuildPlan(t.TempDir(), ArmBoji, "hono", 1); err == nil {
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
	for _, name := range []string{"claude.cmd", "codex.cmd", "boji.cmd"} {
		if err := os.WriteFile(filepath.Join(dir, name), []byte(script), 0o755); err != nil {
			t.Fatalf("write sentinel %s: %v", name, err)
		}
	}
	t.Setenv("PATH", dir+string(os.PathListSeparator)+os.Getenv("PATH"))

	for _, arm := range []Arm{ArmClaude, ArmCodex, ArmBoji} {
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
