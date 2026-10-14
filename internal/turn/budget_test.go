package turn

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"os"
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
		{"echo 'package x' > x.go", true},
		{"cat > main.go <<'EOF'\npackage main\nEOF", true},
		{"printf 'x' | tee y.go", true},
		{"echo x >> NOTES.md", false},
		{"tofu serve --schema > apps/desk/schema/tofu.host.json", false},
		{"go run ./gen > types_gen.go", false},
		{"stringer -type=Kind > kind_string.go", false},
	} {
		raw, _ := json.Marshal(map[string]string{"command": c.command})
		_, err := shell.Run(context.Background(), raw)
		var refusal SourceBudgetError
		if errors.As(err, &refusal) != c.refused {
			t.Errorf("%q: refused %v, want %v, error %v", c.command, err != nil, c.refused, err)
		}
	}
	if len(ran) != 4 {
		t.Errorf("ran %d commands, want the four that carry no source: %v", len(ran), ran)
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
