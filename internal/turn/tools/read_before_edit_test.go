package tools_test

import (
	"context"
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"tofu/internal/turn"
	"tofu/internal/turn/tools"
)

const packageJSON = "{\n  \"name\": \"moth\"\n}\n"

func sessionDir(t *testing.T) (string, *turn.ReadLedger) {
	t.Helper()
	dir := t.TempDir()
	if err := os.WriteFile(filepath.Join(dir, "package.json"), []byte(packageJSON), 0o644); err != nil {
		t.Fatal(err)
	}
	return dir, turn.NewReadLedger()
}

func call(t *testing.T, tool turn.Tool, args map[string]string) error {
	t.Helper()
	raw, err := json.Marshal(args)
	if err != nil {
		t.Fatal(err)
	}
	_, err = tool.Run(context.Background(), raw)
	return err
}

func turnTools(t *testing.T, dir string, ledger *turn.ReadLedger) (turn.Tool, turn.Tool, turn.Tool) {
	t.Helper()
	read, readErr := turn.NewReadTool(dir)
	write, writeErr := turn.NewWriteTool(dir)
	edit, editErr := tools.NewEdit(dir)
	for _, err := range []error{readErr, writeErr, editErr} {
		if err != nil {
			t.Fatal(err)
		}
	}
	return read.Reading(ledger), write.Reading(ledger), edit.Reading(ledger)
}

func TestAFileReadInTurnOneAndUnchangedIsWrittenAndEditedInTurnTwo(t *testing.T) {
	dir, ledger := sessionDir(t)
	read, _, _ := turnTools(t, dir, ledger)
	if err := call(t, read, map[string]string{"path": "package.json"}); err != nil {
		t.Fatalf("turn 1 read: %v", err)
	}
	_, write, edit := turnTools(t, dir, ledger)
	if err := call(t, write, map[string]string{"path": "package.json", "content": "{\n  \"name\": \"inky\"\n}\n"}); err != nil {
		t.Fatalf("turn 2 write was refused: %v", err)
	}
	if err := call(t, edit, map[string]string{"path": "package.json", "old_string": "inky", "new_string": "quiet"}); err != nil {
		t.Fatalf("turn 2 edit was refused: %v", err)
	}
	got, _ := os.ReadFile(filepath.Join(dir, "package.json"))
	if string(got) != "{\n  \"name\": \"quiet\"\n}\n" {
		t.Fatalf("package.json is %q", got)
	}
}

func TestAFileChangedOnDiskAfterTheReadIsRefused(t *testing.T) {
	changed := "{\n  \"name\": \"moth\",\n  \"private\": true\n}\n"
	for _, name := range []string{"write", "edit"} {
		t.Run(name, func(t *testing.T) {
			dir, ledger := sessionDir(t)
			read, write, edit := turnTools(t, dir, ledger)
			if err := call(t, read, map[string]string{"path": "package.json"}); err != nil {
				t.Fatalf("read: %v", err)
			}
			if err := os.WriteFile(filepath.Join(dir, "package.json"), []byte(changed), 0o644); err != nil {
				t.Fatal(err)
			}
			tool, args := turn.Tool(write), map[string]string{"path": "package.json", "content": "{}\n"}
			if name == "edit" {
				tool, args = edit, map[string]string{"path": "package.json", "old_string": "moth", "new_string": "inky"}
			}
			if err := call(t, tool, args); err == nil {
				t.Fatalf("%s of a file that changed since it was read was allowed", name)
			}
			got, _ := os.ReadFile(filepath.Join(dir, "package.json"))
			if string(got) != changed {
				t.Fatalf("a refused %s changed package.json to %q", name, got)
			}
		})
	}
}

func TestARefusalThatPrintsTheWholeFileCountsAsItsRead(t *testing.T) {
	dir, ledger := sessionDir(t)
	_, _, edit := turnTools(t, dir, ledger)
	args := map[string]string{"path": "package.json", "old_string": "moth", "new_string": "inky"}
	if err := call(t, edit, args); err == nil {
		t.Fatal("an edit of a file never read was allowed")
	}
	if err := call(t, edit, args); err != nil {
		t.Fatalf("the edit after a refusal that printed the whole file was refused again: %v", err)
	}
}

type rangeStep struct {
	tool    string
	args    map[string]any
	refused string
}

func fiftyLines(ending string) string {
	lines := []string{"package a", ""}
	for n := 3; n <= 50; n++ {
		lines = append(lines, fmt.Sprintf("func f%d() int { return %d }", n, n))
	}
	return strings.Join(lines, ending) + ending
}

func readLines(first, last int) rangeStep {
	return rangeStep{tool: "read", args: map[string]any{"path": "a.go", "start_line": first, "end_line": last}}
}

func editText(old, becomes, refused string) rangeStep {
	return rangeStep{tool: "edit", args: map[string]any{"path": "a.go", "old_string": old, "new_string": becomes}, refused: refused}
}

func editSymbol(refused string) rangeStep {
	return rangeStep{tool: "edit", args: map[string]any{"path": "a.go", "symbol": "f40", "new_string": "func f40() int { return 400 }"}, refused: refused}
}

func writeWhole(appending bool, refused string) rangeStep {
	return rangeStep{tool: "write", args: map[string]any{"path": "a.go", "content": "func g() {}\n", "append": appending}, refused: refused}
}

func TestAnEditOrWriteReachingLinesNoReadShowedIsRefused(t *testing.T) {
	readWhole := rangeStep{tool: "read", args: map[string]any{"path": "a.go"}}
	rows := []struct {
		name   string
		ending string
		steps  []rangeStep
	}{
		{"an edit at line 40 after reading 1-5", "\n", []rangeStep{readLines(1, 5), editText("return 40 }", "return 400 }", "read only at lines 1-5")}},
		{"an edit inside the range read", "\n", []rangeStep{readLines(1, 5), editText("return 4 }", "return 44 }", "")}},
		{"an insertion right below the last line read", "\n", []rangeStep{readLines(1, 5), editText("return 5 }", "return 5 }\nfunc g() {}", ""), editText("func g() {}", "func h() {}", "")}},
		{"an insertion right above the first line read", "\n", []rangeStep{readLines(6, 10), editText("func f6()", "func g() {}\nfunc f6()", "")}},
		{"an insertion between two lines no read showed", "\n", []rangeStep{readLines(1, 5), editText("return 40 }", "return 40 }\nfunc g() {}", "read only at lines 1-5")}},
		{"a whole read after a range read", "\n", []rangeStep{readLines(1, 5), readWhole, editText("return 40 }", "return 400 }", "")}},
		{"a range read after a whole read", "\n", []rangeStep{readWhole, readLines(1, 5), editText("return 40 }", "return 400 }", "")}},
		{"an edit crossing the edge of two merged ranges", "\n", []rangeStep{readLines(1, 5), readLines(4, 10),
			editText("return 10 }\nfunc f11() int { return 11 }", "return 10 }", "read only at lines 1-10")}},
		{"ranges shift with an edit that adds lines", "\n", []rangeStep{readLines(38, 42),
			editText("return 40 }", "return 40 }\nfunc g1() {}\nfunc g2() {}", ""),
			editText("return 42 }", "return 420 }", ""),
			editText("return 43 }", "return 430 }", "read only at lines 38-44")}},
		{"an edit whose whitespace was repaired onto line 40", "\n", []rangeStep{readLines(1, 5), editText("\tfunc f40() int { return 40 }", "func f40() int { return 400 }", "read only at lines 1-5")}},
		{"a symbol edit outside the range", "\n", []rangeStep{readLines(1, 5), editSymbol("read only at lines 1-5")}},
		{"a symbol edit inside the range", "\n", []rangeStep{readLines(38, 42), editSymbol("")}},
		{"a crlf file, inside and outside the range", "\r\n", []rangeStep{readLines(38, 42), editText("return 40 }", "return 400 }", ""), editText("return 10 }", "return 100 }", "read only at lines 38-42")}},
		{"a whole write after a range read", "\n", []rangeStep{readLines(1, 5), writeWhole(false, "read only at lines 1-5")}},
		{"a whole write after a whole read", "\n", []rangeStep{readWhole, writeWhole(false, "")}},
		{"an append after reading the last lines", "\n", []rangeStep{readLines(48, 50), writeWhole(true, ""),
			editText("func g() {}", "func h() {}", ""),
			editText("return 47 }", "return 470 }", "read only at lines 48-51")}},
		{"an append after reading only the top", "\n", []rangeStep{readLines(1, 5), writeWhole(true, "read only at lines 1-5")}},
	}
	for _, row := range rows {
		t.Run(row.name, func(t *testing.T) {
			dir := t.TempDir()
			target := filepath.Join(dir, "a.go")
			if err := os.WriteFile(target, []byte(fiftyLines(row.ending)), 0o644); err != nil {
				t.Fatal(err)
			}
			read, write, edit := turnTools(t, dir, turn.NewReadLedger())
			byName := map[string]turn.Tool{"read": read, "write": write, "edit": edit}
			for i, step := range row.steps {
				raw, _ := json.Marshal(step.args)
				held, _ := os.ReadFile(target)
				_, err := byName[step.tool].Run(context.Background(), raw)
				switch {
				case step.refused == "" && err != nil:
					t.Fatalf("step %d %s was refused: %v", i+1, step.tool, err)
				case step.refused != "" && err == nil:
					t.Fatalf("step %d %s was accepted, want it refused naming %q", i+1, step.tool, step.refused)
				case step.refused != "" && !strings.Contains(err.Error(), step.refused):
					t.Fatalf("step %d %s was refused without naming %q: %v", i+1, step.tool, step.refused, err)
				}
				if after, _ := os.ReadFile(target); step.refused != "" && string(after) != string(held) {
					t.Fatalf("step %d: a refused %s changed a.go", i+1, step.tool)
				}
			}
		})
	}
}

func TestWithoutALedgerAnEditAnywhereIsAccepted(t *testing.T) {
	dir := t.TempDir()
	if err := os.WriteFile(filepath.Join(dir, "a.go"), []byte(fiftyLines("\n")), 0o644); err != nil {
		t.Fatal(err)
	}
	_, _, edit := turnTools(t, dir, nil)
	if err := call(t, edit, map[string]string{"path": "a.go", "old_string": "return 40 }", "new_string": "return 400 }"}); err != nil {
		t.Fatalf("an edit with read-before-edit off was refused: %v", err)
	}
}
