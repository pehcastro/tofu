package tools

import (
	"context"
	"encoding/json"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"tofu/internal/turn"
)

const slowTsc = `const fs = require('fs');
const say = (line) => process.stdout.write(line + '\n');
const check = () => {
  const broken = fs.readdirSync('.').filter((name) => name.endsWith('.ts') && fs.readFileSync(name, 'utf8').includes('"many"'));
  broken.forEach((name) => say(name + "(1,14): error TS2322: Type 'string' is not assignable to type 'number'."));
  say('Found ' + broken.length + ' errors. Watching for file changes.');
};
let pending;
fs.watch('.', () => {
  clearTimeout(pending);
  pending = setTimeout(() => {
    say('File change detected. Starting incremental compilation...');
    setTimeout(check, 3000);
  }, 100);
});
say('Starting compilation in watch mode...');
check();
`

func warmEditor(t *testing.T, files map[string]string) (string, *turn.Typecheckers, func(oldString, newString string) (string, time.Duration)) {
	dir := t.TempDir()
	files["tsconfig.json"], files["package.json"], files["bun.lock"] = `{"compilerOptions":{"strict":true}}`, `{"name":"edited"}`, "{}"
	files["count.ts"] = "export const count: number = 3;\n"
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
	checkers.Warm(dir)
	if warm, err := checkers.Typecheck(context.Background(), dir); err != nil || !strings.Contains(warm, "errors in .: 0") {
		t.Fatalf("the watcher did not finish its first check: %v\n%s", err, warm)
	}
	editor, err := NewEdit(dir)
	if err != nil {
		t.Fatal(err)
	}
	editor = editor.Checking(checkers)
	return dir, checkers, func(oldString, newString string) (string, time.Duration) {
		raw, _ := json.Marshal(map[string]string{"path": "count.ts", "old_string": oldString, "new_string": newString})
		started := time.Now()
		result, err := editor.Run(context.Background(), raw)
		if err != nil {
			t.Fatal(err)
		}
		return result.Content, time.Since(started)
	}
}

func TestAnEditOnASlowProjectReturnsAtOnceAndTypecheckReportsItAfter(t *testing.T) {
	if _, err := exec.LookPath("node"); err != nil {
		t.Skip("node is not on PATH")
	}
	dir, checkers, edit := warmEditor(t, map[string]string{
		"node_modules/typescript/package.json": `{"name":"typescript","version":"5.9.0"}`,
		"node_modules/typescript/bin/tsc":      slowTsc,
	})
	edited, took := edit("3", `"many"`)
	if took >= time.Second || !strings.Contains(edited, "in the background") {
		t.Fatalf("the edit took %s and read:\n%s", took, edited)
	}
	after, err := checkers.Typecheck(context.Background(), dir)
	if err != nil || !strings.Contains(after, "count.ts(1,14): error TS2322") {
		t.Fatalf("the typecheck after the edit did not report its error: %v\n%s", err, after)
	}
	again, took := edit("count: number", "count:  number")
	if took >= time.Second || !strings.Contains(again, "in the background") || !strings.Contains(again, "count.ts(1,14): error TS2322") {
		t.Fatalf("the next edit took %s and did not carry the error the last check found:\n%s", took, again)
	}
	t.Logf("edit, %s:\n%s\ntypecheck after:\n%s\nnext edit:\n%s", took, edited, after, again)
}

func TestAnEditOnAFastProjectKeepsItsInlineTypecheck(t *testing.T) {
	if _, err := exec.LookPath("bun"); err != nil {
		t.Skip("bun is not on PATH")
	}
	_, _, edit := warmEditor(t, map[string]string{})
	edited, took := edit("3", `"many"`)
	if !strings.Contains(edited, "count.ts(1,14): error TS2322") || !strings.Contains(edited, "errors in count.ts: 1") || strings.Contains(edited, "in the background") {
		t.Fatalf("the edit on a fast project lost its inline typecheck after %s:\n%s", took, edited)
	}
	t.Logf("edit, %s:\n%s", took, edited)
}
