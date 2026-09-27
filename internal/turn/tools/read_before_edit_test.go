package tools_test

import (
	"context"
	"encoding/json"
	"os"
	"path/filepath"
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
