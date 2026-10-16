package turn

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"slices"
	"strings"
	"testing"

	"tofu/internal/konst"
	"tofu/internal/llm"
)

type ranTool struct {
	name    string
	ran     *[]string
	printed string
}

func (t ranTool) Name() string         { return t.name }
func (t ranTool) Definition() llm.Tool { return llm.Tool{Name: t.name} }
func (t ranTool) Run(_ context.Context, raw json.RawMessage) (Result, error) {
	*t.ran = append(*t.ran, string(raw))
	return Result{Content: t.printed}, nil
}

func TestShellWriteToSourceIsRefusedOnlyWhenTheCommandCarriesTheSource(t *testing.T) {
	var ran []string
	shell := WithSourceBudget([]Tool{ranTool{name: "bash", ran: &ran}}, nil)[0]
	for _, c := range []struct {
		command string
		refused bool
	}{
		{"sed -i s/a/b/ src/app.ts", true},
		{"sed -i 's/CalcTotal/Total/g' *.go", true},
		{"perl -pi -e 's/a/b/g' x.go", true},
		{"go test ./... && sed -i.bak -E 's|a|b|' x.go", true},
		{"sed -i s/a/b/ $(find . -name '*.go')", true},
		{`echo x > "$OUT"`, true},
		{"echo 'package x' > x.go", true},
		{"cat > main.go <<'EOF'\npackage main\nEOF", true},
		{"printf 'x' | tee y.go", true},
		{"cp notes.txt z.go", true},
		{"mv old.go new.go", true},
		{"echo x >> NOTES.md", false},
		{"sed -i s/a/b/ README.md", false},
		{"sed -n 1,5p a.go", false},
		{"go test ./...", false},
		{"tofu serve --schema > apps/desk/schema/tofu.host.json", false},
		{"go run ./gen > types_gen.go", false},
		{"stringer -type=Kind > kind_string.go", false},
	} {
		raw, _ := json.Marshal(map[string]string{"command": c.command})
		_, err := shell.Run(context.Background(), raw)
		if (err != nil) != c.refused {
			t.Errorf("%q: refused %v, want %v, error %v", c.command, err != nil, c.refused, err)
		}
	}
	if len(ran) != 7 {
		t.Errorf("ran %d commands, want the seven that carry no source: %v", len(ran), ran)
	}
}

func TestARefusedShellWriteNamesTheCallThatDoesItInOneReply(t *testing.T) {
	var ran []string
	shell := WithSourceBudget([]Tool{ranTool{name: "bash", ran: &ran}}, nil)[0]
	for _, c := range []struct {
		command string
		want    []string
	}{
		{"sed -i 's/CalcTotal/Total/g' cart.go report.go", []string{"edit", "replace_all true", `old_string "CalcTotal"`, `new_string "Total"`, `"cart.go"`, `"report.go"`}},
		{"sed -i -E 's|Acct|Account|g' *.go", []string{"edit", "replace_all true", `old_string "Acct"`, `new_string "Account"`, "each file *.go matches"}},
		{"perl -pi -e 's/a/b/g' x.go", []string{"edit", "replace_all true", `old_string "a"`, `"x.go"`}},
		{"sed -i 's/Acct/Account/g' $(find . -name '*.go')", []string{"edit", "replace_all true", `old_string "Acct"`, "until the shell runs"}},
		{"cat > main.go <<'EOF'\npackage main\nEOF", []string{`write with path "main.go"`, "whole file as content"}},
		{"echo 'package x' > x.go", []string{`write with path "x.go"`, "whole file as content"}},
		{`echo x > "$OUT"`, []string{"until the shell runs", "write"}},
	} {
		raw, _ := json.Marshal(map[string]string{"command": c.command})
		_, err := shell.Run(context.Background(), raw)
		if err == nil {
			t.Fatalf("%q ran", c.command)
		}
		text := err.Error()
		for _, want := range append(c.want, "nothing ran", "no file changed", "in your next reply") {
			if !strings.Contains(text, want) {
				t.Errorf("%q: refusal lacks %q: %s", c.command, want, text)
			}
		}
		if strings.Contains(text, "MUST") || strings.Contains(text, "NEVER") || strings.Contains(text, "never does") {
			t.Errorf("%q: refusal scolds rather than says what to send: %s", c.command, text)
		}
	}
	if len(ran) != 0 {
		t.Errorf("a refused command ran: %v", ran)
	}
}

func TestTheLeadSpendsOneLineBudgetAcrossDirectories(t *testing.T) {
	var ran []string
	budgeted := WithSourceBudget([]Tool{ranTool{name: "edit", ran: &ran}, ranTool{name: "write", ran: &ran}}, nil)
	edit, write := budgeted[0], budgeted[1]
	lines := func(n int) string { return strings.Repeat("x\n", n) }
	budget := konst.OrchestratorSourceLinesPerTurn
	quarter := budget / 4
	for _, c := range []struct {
		tool   Tool
		path   string
		lines  int
		reason BudgetRefusal
	}{
		{edit, "store/store.go", quarter, ""},
		{write, "./export.go", quarter, ""},
		{edit, "api/handlers.go", quarter, ""},
		{write, "ANSWER.txt", 10 * budget, ""},
		{edit, "cmd/main.go", 1, ""},
		{write, "notes/plan.md", 10 * budget, ""},
		{edit, "report/report.go", quarter, RefusedOverLines},
		{edit, "store/store.go", budget - 3*quarter - 1, ""},
		{edit, "transfer/transfer.go", 1, RefusedOverLines},
	} {
		raw, _ := json.Marshal(map[string]string{"path": c.path, "content": lines(c.lines), "old_string": lines(c.lines), "new_string": lines(c.lines)})
		_, err := c.tool.Run(context.Background(), raw)
		var refusal SourceBudgetError
		errors.As(err, &refusal)
		if refusal.Reason != c.reason {
			t.Errorf("%s %s with %d lines: refused for %q, want %q, error %v", c.tool.Name(), c.path, c.lines, refusal.Reason, c.reason, err)
		}
	}
	if len(ran) != 7 {
		t.Errorf("ran %d writes, want the seven inside the budget", len(ran))
	}
}

func TestAWriteOverAnExistingFileSpendsTheLinesItsDiffChanges(t *testing.T) {
	dir := t.TempDir()
	var held string
	for i := range 150 {
		held += fmt.Sprintf("var x%d = %d\n", i, i)
	}
	if err := os.WriteFile(filepath.Join(dir, "big.go"), []byte(held), 0o644); err != nil {
		t.Fatal(err)
	}
	writer, err := NewWriteTool(dir)
	if err != nil {
		t.Fatal(err)
	}
	ledger := NewReadLedger()
	ledger.Mark("big.go", []byte(held))
	var ran []string
	budgeted := WithSourceBudget([]Tool{writer.Reading(ledger), ranTool{name: "edit", ran: &ran}}, nil)
	write, edit := budgeted[0], budgeted[1]
	raw, _ := json.Marshal(map[string]string{"path": "big.go", "content": "var y0 = 2\nvar y1 = 3\n" + held[len("var x0 = 0\nvar x1 = 1\n"):]})
	if _, err := write.Run(context.Background(), raw); err != nil {
		t.Fatalf("a two line change to a 150 line file was refused: %v", err)
	}
	room := konst.OrchestratorSourceLinesPerTurn - 2
	raw, _ = json.Marshal(map[string]string{"path": "sub/other.go", "old_string": "a", "new_string": strings.Repeat("y\n", room)})
	if _, err := edit.Run(context.Background(), raw); err != nil {
		t.Fatalf("an edit of %d lines after a write that changed 2 was refused: %v", room, err)
	}
	raw, _ = json.Marshal(map[string]string{"path": "new.go", "content": "package x\n"})
	var refusal SourceBudgetError
	if _, err := write.Run(context.Background(), raw); !errors.As(err, &refusal) || refusal.Reason != RefusedOverLines || refusal.Spent != konst.OrchestratorSourceLinesPerTurn {
		t.Errorf("a new file past a spent budget: error %v, want a refusal over lines with the whole budget spent", err)
	}
}

func TestAFileTheShellPrintedWholeAfterCatCountsAsRead(t *testing.T) {
	files := map[string]string{
		"a.go":     "package a\n\nfunc A() {}\n",
		"small.go": "package a\n",
		"sub/b.go": "package sub\n\nvar B = 1\n",
	}
	for _, c := range []struct {
		name    string
		command string
		printed string
		read    []string
	}{
		{"cat of one file", "cat a.go", files["a.go"], []string{"a.go"}},
		{"cat cut by head", "cat a.go | head -2", "package a\n\n", nil},
		{"no cat, whole file printed", "sed -n 1,1p small.go", files["small.go"], nil},
		{"a loop naming each file", `for f in $(git ls-files '*.go'); do echo "=== $f"; cat $f; done`, "=== a.go\n" + files["a.go"] + "=== sub/b.go\n" + files["sub/b.go"], []string{"a.go", "sub/b.go"}},
		{"a glob", "cat sub/*.go", files["sub/b.go"], []string{"sub/b.go"}},
		{"content older than the file", "cat a.go", "package a\n\nfunc Old() {}\n", nil},
		{"a named file that printed nothing", "cat a.go sub/b.go", files["a.go"] + "cat: sub/b.go: Permission denied\n", []string{"a.go"}},
		{"the rtk rewrite of a cat", "rtk read a.go", files["a.go"], []string{"a.go"}},
		{"an rtk read cut short", "rtk read a.go --max-lines 2", "package a\n\n", nil},
		{"crlf output", "cat sub/b.go", strings.ReplaceAll(files["sub/b.go"], "\n", "\r\n"), []string{"sub/b.go"}},
	} {
		t.Run(c.name, func(t *testing.T) {
			dir := t.TempDir()
			for path, body := range files {
				if err := os.MkdirAll(filepath.Join(dir, filepath.Dir(path)), 0o755); err != nil {
					t.Fatal(err)
				}
				if err := os.WriteFile(filepath.Join(dir, path), []byte(body), 0o644); err != nil {
					t.Fatal(err)
				}
			}
			writer, err := NewWriteTool(dir)
			if err != nil {
				t.Fatal(err)
			}
			ledger := NewReadLedger()
			var ran []string
			shell := WithSourceBudget([]Tool{writer.Reading(ledger), ranTool{name: "bash", ran: &ran, printed: c.printed}}, nil)[1]
			raw, _ := json.Marshal(map[string]string{"command": c.command})
			if _, err := shell.Run(context.Background(), raw); err != nil {
				t.Fatal(err)
			}
			for path, body := range files {
				want := slices.Contains(c.read, path)
				if got := ledger.Saw(path, []byte(body)); got != want {
					t.Errorf("%s read %v, want %v", path, got, want)
				}
			}
		})
	}
}

type scriptTool struct {
	dir string
	ran *[]string
	run func(dir string)
}

func (t scriptTool) Name() string         { return "bash" }
func (t scriptTool) Definition() llm.Tool { return llm.Tool{Name: "bash"} }
func (t scriptTool) Run(_ context.Context, raw json.RawMessage) (Result, error) {
	*t.ran = append(*t.ran, string(raw))
	t.run(t.dir)
	return Result{Content: "ok"}, nil
}

func scriptProject(t *testing.T, git bool) string {
	dir := t.TempDir()
	put := func(path, body string) {
		if err := os.WriteFile(filepath.Join(dir, path), []byte(body), 0o644); err != nil {
			t.Fatal(err)
		}
	}
	numbered := func(name string, from, to int, value string) string {
		var lines string
		for i := from; i < to; i++ {
			lines += fmt.Sprintf("var %s%d = %s\n", name, i, value)
		}
		return lines
	}
	put("a.go", numbered("a", 0, 10, "1"))
	put("b.go", numbered("b", 0, 10, "1"))
	put("notes.md", "# notes\n")
	if git {
		for _, args := range [][]string{{"init", "-q"}, {"add", "."}, {"-c", "user.name=t", "-c", "user.email=t@t", "commit", "-qm", "x"}} {
			if out, err := exec.Command("git", append([]string{"-C", dir}, args...)...).CombinedOutput(); err != nil {
				t.Fatalf("git %v: %v %s", args, err, out)
			}
		}
	}
	put("b.go", numbered("b", 0, 4, "2")+numbered("b", 4, 10, "1"))
	return dir
}

func TestAScriptThatWritesSourceSpendsTheLinesItChanged(t *testing.T) {
	rewrite := func(path, old, replacement string, count int) func(string) {
		return func(dir string) {
			body, _ := os.ReadFile(filepath.Join(dir, path))
			_ = os.WriteFile(filepath.Join(dir, path), []byte(strings.Replace(string(body), old, replacement, count)), 0o644)
		}
	}
	create := func(path string, lines int) func(string) {
		return func(dir string) {
			_ = os.WriteFile(filepath.Join(dir, path), []byte(strings.Repeat("x\n", lines)), 0o644)
		}
	}
	for _, c := range []struct {
		name    string
		command string
		run     func(string)
		lines   int
	}{
		{"a python heredoc over every go file", "python - <<'E'\nimport glob\nE", func(dir string) {
			rewrite("a.go", " = 1", " = 9", 1)(dir)
			rewrite("b.go", " = 1", " = 9", 1)(dir)
		}, 2},
		{"a bash script file", "bash scratch/fix.sh", rewrite("a.go", " = 1", " = 3", 3), 3},
		{"a node script making a file", "node fix.js", create("new.go", 5), 5},
		{"a script run by path", "./fix.py", func(dir string) { _ = os.Remove(filepath.Join(dir, "a.go")) }, 10},
		{"a script behind env", "env python3 fix.py", rewrite("a.go", " = 1", " = 4", 1), 1},
		{"go run", "go run ./fix", rewrite("a.go", " = 1", " = 5", 2), 2},
		{"a file dirty before the script", "python3 fix.py", rewrite("b.go", " = 2", " = 3", 1), 1},
		{"a script writing markdown only", "python fix.py", rewrite("notes.md", "notes", "plans", 1), 0},
		{"a generator's redirect target", "python gen.py > gen.go", create("gen.go", 20), 0},
		{"a command that runs no script while a sub-agent writes", "go test ./...", rewrite("a.go", " = 1", " = 6", 10), 0},
	} {
		for _, git := range []bool{true, false} {
			t.Run(fmt.Sprintf("%s git %v", c.name, git), func(t *testing.T) {
				dir := scriptProject(t, git)
				writer, err := NewWriteTool(dir)
				if err != nil {
					t.Fatal(err)
				}
				var ran []string
				shell := WithSourceBudget([]Tool{writer, scriptTool{dir: dir, ran: &ran, run: c.run}}, nil)[1]
				raw, _ := json.Marshal(map[string]string{"command": c.command})
				result, err := shell.Run(context.Background(), raw)
				if err != nil || len(ran) != 1 {
					t.Fatalf("ran %d times, error %v", len(ran), err)
				}
				if spent := shell.(budgetedTool).budget.spent; spent != c.lines {
					t.Errorf("spent %d lines, want %d", spent, c.lines)
				}
				if told := strings.Contains(result.Content, fmt.Sprintf("changed %d source lines", c.lines)); told != (c.lines > 0) {
					t.Errorf("told %v, want %v: %s", told, c.lines > 0, result.Content)
				}
			})
		}
	}
}

func TestAScriptPastTheBudgetRunsThenEveryFurtherWriteIsRefusedNamingIt(t *testing.T) {
	dir := scriptProject(t, true)
	writer, err := NewWriteTool(dir)
	if err != nil {
		t.Fatal(err)
	}
	budget := konst.OrchestratorSourceLinesPerTurn
	var ran []string
	big := scriptTool{dir: dir, ran: &ran, run: func(dir string) {
		_ = os.WriteFile(filepath.Join(dir, "big.go"), []byte(strings.Repeat("x\n", budget+50)), 0o644)
	}}
	tools := WithSourceBudget([]Tool{writer, big, ranTool{name: "edit", ran: &ran}}, nil)
	shell, edit := tools[1], tools[2]
	command := func(text string) json.RawMessage {
		raw, _ := json.Marshal(map[string]string{"command": text})
		return raw
	}
	result, err := shell.Run(context.Background(), command("python - <<'E'\nopen('big.go','w')\nE"))
	if err != nil || !strings.Contains(result.Content, fmt.Sprintf("changed %d source lines", budget+50)) || !strings.Contains(result.Content, "is spent") {
		t.Fatalf("the script that crossed the budget: error %v, told %q", err, result.Content)
	}
	_, err = shell.Run(context.Background(), command("node fix.js"))
	var refusal SourceBudgetError
	if !errors.As(err, &refusal) || refusal.Reason != RefusedSpentScript || len(ran) != 1 || !strings.Contains(err.Error(), "python - <<'E'") || !strings.Contains(err.Error(), "nothing ran") {
		t.Errorf("a second script after the budget was spent: ran %d, error %v", len(ran), err)
	}
	raw, _ := json.Marshal(map[string]string{"path": "a.go", "old_string": "a", "new_string": "b"})
	if _, err := edit.Run(context.Background(), raw); !errors.As(err, &refusal) || refusal.Reason != RefusedOverLines || !strings.Contains(err.Error(), "python - <<'E'") {
		t.Errorf("an edit after a script spent the budget: %v", err)
	}
	if _, err := shell.Run(context.Background(), command("go test ./...")); err != nil || len(ran) != 2 {
		t.Errorf("a command that runs no script was refused: %v", err)
	}
}
