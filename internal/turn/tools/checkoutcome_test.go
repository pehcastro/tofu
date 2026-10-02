package tools_test

import (
	"context"
	"encoding/json"
	"os/exec"
	"strings"
	"testing"

	"tofu/internal/konst"
	"tofu/internal/llm"
	"tofu/internal/turn"
	"tofu/internal/turn/tools"
)

func checkedProject(t *testing.T, files map[string]string) string {
	t.Helper()
	dir := t.TempDir()
	for name, body := range files {
		seed(t, dir, name, body)
	}
	return dir
}

func checkTools(t *testing.T, dir string) (tools.Typecheck, tools.Test) {
	t.Helper()
	checkers, runners := turn.NewTypecheckers(), turn.NewTestRunners()
	t.Cleanup(checkers.Close)
	t.Cleanup(runners.Close)
	typecheck, err := tools.NewTypecheck(dir, checkers)
	if err != nil {
		t.Fatal(err)
	}
	test, err := tools.NewTest(dir, runners)
	if err != nil {
		t.Fatal(err)
	}
	return typecheck, test
}

func pathCall(tool, path string) llm.ToolCall {
	raw, _ := json.Marshal(map[string]string{"path": path})
	return llm.ToolCall{ID: tool + " " + path, Name: tool, Arguments: raw}
}

func TestATypecheckOrTestThatFindsAFailureRecordsItAndACleanOneDoesNot(t *testing.T) {
	for _, runner := range []string{"bun", "node"} {
		if _, err := exec.LookPath(runner); err != nil {
			t.Skip(runner + " is not on PATH")
		}
	}
	dir := checkedProject(t, map[string]string{
		"package.json":       `{"name":"checked","type":"module","devDependencies":{"vitest":"3.2.4"}}`,
		"tsconfig.json":      `{"compilerOptions":{"strict":true,"skipLibCheck":true,"module":"esnext","moduleResolution":"bundler"},"include":["src"]}`,
		"src/sum.ts":         "export const sum = (a: number, b: number): number => a + b;\n",
		"src/sum.test.ts":    "import { expect, it } from 'vitest';\nimport { sum } from './sum';\n\nit('adds', () => {\n  expect(sum(1, 2)).toBe(3);\n});\n",
		"src/broken.ts":      "export const count: number = \"many\";\n",
		"src/broken.test.ts": "import { expect, it } from 'vitest';\n\nit('subtracts', () => {\n  expect(3 - 1).toBe(1);\n});\n",
	})
	install := exec.Command("bun", "install", "--prefer-offline")
	install.Dir = dir
	if out, err := install.CombinedOutput(); err != nil {
		t.Skipf("vitest could not be installed from the bun cache: %v\n%s", err, out)
	}
	typecheck, test := checkTools(t, dir)
	model := &recordingModel{calls: []llm.ToolCall{
		pathCall("typecheck", "src/broken.ts"), pathCall("typecheck", "src/sum.ts"),
		pathCall("test", "src/broken.test.ts"), pathCall("test", "src/sum.test.ts"),
	}}
	row, err := turn.Run(context.Background(), turn.Config{
		Model:          model,
		Spend:          turn.SpendSubscription,
		Tools:          turn.NewRegistry(typecheck, test),
		Task:           "check the project",
		ResultBytesCap: konst.TurnResultBytesCap,
		ArtifactDir:    t.TempDir(),
	})
	if err != nil {
		t.Fatalf("turn.Run: %v", err)
	}
	rows := loggedRows(t, row)
	if len(rows) != 4 {
		t.Fatalf("%d tool call rows, want 4", len(rows))
	}
	for i, failed := range []bool{true, false, true, false} {
		if (rows[i].Error != "") != failed {
			t.Errorf("%s %s: error %q, want failed %v", rows[i].Tool, rows[i].Command, rows[i].Error, failed)
		}
	}
	if seen := model.toolResults(); !strings.Contains(seen, "broken.ts(1,14): error TS2322") || !strings.Contains(seen, "broken.test.ts: 0 passed, 1 failed") {
		t.Fatalf("the model did not read the failures:\n%s", seen)
	}
}

func TestATestCallOnAProjectWithoutVitestIsNotAPass(t *testing.T) {
	for _, project := range []struct {
		files map[string]string
		error string
	}{
		{map[string]string{"package.json": `{"name":"jested","scripts":{"test":"jest --ci"}}`, "a.test.ts": "export {};\n"}, "test runs through the shell: npm run test"},
		{map[string]string{"package.json": `{"name":"bare"}`, "src/a.ts": "export {};\n"}, "test: this project has no tests"},
	} {
		dir := checkedProject(t, project.files)
		_, test := checkTools(t, dir)
		name := "a.test.ts"
		if _, found := project.files[name]; !found {
			name = "src/a.ts"
		}
		raw, _ := json.Marshal(map[string]string{"path": name})
		result, err := test.Run(context.Background(), raw)
		if err != nil || result.FailureText != project.error {
			t.Errorf("%s: failure %q, want %q, %v\n%s", project.files["package.json"], result.FailureText, project.error, err, result.Content)
		}
	}
}
