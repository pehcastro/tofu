package tools

import (
	"context"
	"encoding/json"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"

	"tofu/internal/turn"
)

func typecheckProject(t *testing.T, files map[string]string) (string, func(path string) string) {
	if _, err := exec.LookPath("bun"); err != nil {
		t.Skip("bun is not on PATH")
	}
	dir := t.TempDir()
	files["package.json"], files["bun.lock"] = `{"name":"typecheck"}`, "{}"
	for name, body := range files {
		if err := os.MkdirAll(filepath.Dir(filepath.Join(dir, name)), 0o755); err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(filepath.Join(dir, name), []byte(body), 0o600); err != nil {
			t.Fatal(err)
		}
	}
	checkers := turn.NewTypecheckers()
	t.Cleanup(checkers.Close)
	tool, err := NewTypecheck(dir, checkers)
	if err != nil {
		t.Fatal(err)
	}
	return dir, func(path string) string {
		raw, _ := json.Marshal(map[string]string{"path": path})
		result, err := tool.Run(context.Background(), raw)
		if err != nil {
			t.Fatal(err)
		}
		return result.Content
	}
}

func TestTypecheckAnswersAnErrorAndThenItsShellFixFromTheWatcher(t *testing.T) {
	dir, typecheck := typecheckProject(t, map[string]string{
		"tsconfig.json": `{"compilerOptions":{"strict":true}}`,
		"count.ts":      "export const count: number = \"many\";\n",
	})
	broken := typecheck("")
	if !strings.Contains(broken, "errors in .: 1") || !strings.Contains(broken, "count.ts(1,14): error TS2322") || !strings.Contains(broken, "--watch") {
		t.Fatalf("the type error was not answered by the watching tsc:\n%s", broken)
	}
	if err := os.WriteFile(filepath.Join(dir, "count.ts"), []byte("export const count: number = 3;\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	fixed := typecheck(".")
	if !strings.Contains(fixed, "errors in .: 0") || !strings.Contains(fixed, "--watch") {
		t.Fatalf("the fix made outside write and edit was not answered clean by the watching tsc:\n%s", fixed)
	}
	t.Logf("broken:\n%s\nfixed:\n%s", broken, fixed)
}

func TestTypecheckOnADirectoryShowsItsErrorsAndCountsTheRest(t *testing.T) {
	_, typecheck := typecheckProject(t, map[string]string{
		"tsconfig.json":   `{"compilerOptions":{"strict":true}}`,
		"src/core/a.ts":   "export const a: number = \"a\";\n",
		"src/core/b.ts":   "export const b: string = 2;\n",
		"src/cli/main.ts": "export const main: boolean = 1;\n",
	})
	got := typecheck("src/core")
	if !strings.Contains(got, "errors in src/core: 2, in other files: 1") || strings.Contains(got, "src/cli/main.ts(") {
		t.Fatalf("a directory was not scoped to the errors under it:\n%s", got)
	}
}

func TestTypecheckWithoutATsconfigSaysSoAndAMissingPathIsRefused(t *testing.T) {
	dir, typecheck := typecheckProject(t, map[string]string{"loose.ts": "export const a = 1;\n"})
	if got := typecheck("loose.ts"); !strings.Contains(got, "no tsconfig.json in") {
		t.Fatalf("a project with no tsconfig did not say it has none:\n%s", got)
	}
	tool, err := NewTypecheck(dir, nil)
	if err != nil {
		t.Fatal(err)
	}
	for _, path := range []string{"absent.ts", "../outside"} {
		if result, err := tool.Run(context.Background(), json.RawMessage(`{"path":"`+path+`"}`)); err == nil {
			t.Fatalf("%s was not refused:\n%s", path, result.Content)
		}
	}
}
