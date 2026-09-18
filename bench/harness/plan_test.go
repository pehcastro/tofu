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
	for _, arm := range []Arm{ArmClaude, ArmCodex, ArmBoji} {
		plan := BuildPlan(arm, "hono", 1)
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
	dir := t.TempDir()
	marker := filepath.Join(dir, "marker.txt")
	script := "@echo ran > \"" + marker + "\"\r\n"
	for _, name := range []string{"claude.cmd", "codex.cmd", "boji.cmd"} {
		if err := os.WriteFile(filepath.Join(dir, name), []byte(script), 0o755); err != nil {
			t.Fatalf("write sentinel %s: %v", name, err)
		}
	}
	oldPath := os.Getenv("PATH")
	if err := os.Setenv("PATH", dir+string(os.PathListSeparator)+oldPath); err != nil {
		t.Fatalf("set PATH: %v", err)
	}
	defer func() { _ = os.Setenv("PATH", oldPath) }()

	for _, arm := range []Arm{ArmClaude, ArmCodex, ArmBoji} {
		plan := BuildPlan(arm, "hono", 1)
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
